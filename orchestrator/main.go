package main

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	_ "github.com/lib/pq"
)

var db *sql.DB

type Entity struct {
	Type  string `json:"type"`
	Start int    `json:"start"`
	End   int    `json:"end"`
	Value string `json:"value"`
}

// ---------- helpers ----------

func detect(text string) ([]Entity, error) {
	var ents []Entity
	err := postJSON(os.Getenv("DETECTOR_URL")+"/detect", nil, map[string]string{"text": text}, &ents)
	return ents, err
}

// replaces each detected span with [TYPE]; entities arrive sorted and non-overlapping
func sanitize(text string, ents []Entity) string {
	r := []rune(text)
	var sb strings.Builder
	pos := 0
	for _, e := range ents {
		if e.Start < pos || e.End > len(r) {
			continue
		}
		sb.WriteString(string(r[pos:e.Start]))
		sb.WriteString("[" + e.Type + "]")
		pos = e.End
	}
	sb.WriteString(string(r[pos:]))
	return sb.String()
}

func chunk(text string, size int) []string {
	r := []rune(text)
	var out []string
	for i := 0; i < len(r); i += size {
		end := i + size
		if end > len(r) {
			end = len(r)
		}
		out = append(out, string(r[i:end]))
	}
	return out
}

func newStore(name string) (VectorStore, error) {
	switch name {
	case "qdrant":
		return QdrantStore{URL: os.Getenv("QDRANT_URL")}, nil
	case "pgvector":
		return PgStore{DB: db}, nil
	}
	return nil, fmt.Errorf("unknown store %q", name)
}

// reads ?store= and ?embedder= from the URL
func setup(r *http.Request) (VectorStore, EmbeddingProvider, string, error) {
	q := r.URL.Query()
	store, err := newStore(q.Get("store"))
	if err != nil {
		return nil, nil, "", err
	}
	emb, err := newEmbedder(q.Get("embedder"))
	if err != nil {
		return nil, nil, "", err
	}
	return store, emb, q.Get("embedder"), nil
}

func embedAndStore(store VectorStore, emb EmbeddingProvider, col, docID, text string) (int, error) {
	chunks := chunk(text, 500)
	for _, c := range chunks {
		vec, err := emb.Embed(c)
		if err != nil {
			return 0, err
		}
		if err := store.Upsert(col, docID, c, vec); err != nil {
			return 0, err
		}
	}
	return len(chunks), nil
}

func saveEntities(docID int, ents []Entity) {
	for _, e := range ents {
		db.Exec(`INSERT INTO entities(doc_id, type, value) VALUES($1,$2,$3)`, docID, e.Type, e.Value)
	}
}

func audit(action string, docID int) {
	db.Exec(`INSERT INTO audit_log(action, doc_id) VALUES($1,$2)`, action, docID)
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(v)
}

func fail(w http.ResponseWriter, err error) {
	log.Println("error:", err)
	http.Error(w, err.Error(), http.StatusInternalServerError)
}

func cors(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// ---------- endpoints ----------

// POST /ingest?store=qdrant&embedder=gemini&name=file.txt   (body = document text)
func ingest(w http.ResponseWriter, r *http.Request) {
	store, emb, embName, err := setup(r)
	if err != nil {
		http.Error(w, err.Error(), 400)
		return
	}
	body, _ := io.ReadAll(r.Body)
	text := string(body)
	ents, err := detect(text)
	if err != nil {
		fail(w, err)
		return
	}
	name := r.URL.Query().Get("name")
	if name == "" {
		name = "untitled"
	}
	var id int
	if err := db.QueryRow(`INSERT INTO documents(name) VALUES($1) RETURNING id`, name).Scan(&id); err != nil {
		fail(w, err)
		return
	}
	saveEntities(id, ents)
	clean := sanitize(text, ents)
	db.Exec(`INSERT INTO document_versions(doc_id, version, sanitized_text) VALUES($1,1,$2)`, id, clean)
	n, err := embedAndStore(store, emb, "protected_"+embName, strconv.Itoa(id), clean)
	if err != nil {
		fail(w, err)
		return
	}
	audit("ingest", id)
	writeJSON(w, map[string]any{"doc_id": id, "entities": len(ents), "chunks": n, "sanitized": clean})
}

// POST /ingest-raw?store=...&embedder=...   (body = text, NOT sanitized; for the demo)
func ingestRaw(w http.ResponseWriter, r *http.Request) {
	store, emb, embName, err := setup(r)
	if err != nil {
		http.Error(w, err.Error(), 400)
		return
	}
	body, _ := io.ReadAll(r.Body)
	n, err := embedAndStore(store, emb, "unprotected_"+embName, "raw", string(body))
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, map[string]any{"chunks": n, "collection": "unprotected"})
}

// POST /reindex/{id}?store=...&embedder=...   (body = the document text again)
func reindex(w http.ResponseWriter, r *http.Request) {
	store, emb, embName, err := setup(r)
	if err != nil {
		http.Error(w, err.Error(), 400)
		return
	}
	idStr := r.PathValue("id")
	docID, err := strconv.Atoi(idStr)
	if err != nil {
		http.Error(w, "bad id", 400)
		return
	}
	body, _ := io.ReadAll(r.Body)
	ents, err := detect(string(body))
	if err != nil {
		fail(w, err)
		return
	}
	clean := sanitize(string(body), ents)
	var version int
	err = db.QueryRow(`UPDATE documents SET version = version + 1, status='reindexed' WHERE id=$1 RETURNING version`, docID).Scan(&version)
	if err == sql.ErrNoRows {
		http.Error(w, "document not found", 404)
		return
	}
	if err != nil {
		fail(w, err)
		return
	}
	db.Exec(`DELETE FROM entities WHERE doc_id=$1`, docID)
	saveEntities(docID, ents)
	db.Exec(`INSERT INTO document_versions(doc_id, version, sanitized_text) VALUES($1,$2,$3)`, docID, version, clean)
	col := "protected_" + embName
	if err := store.DeleteByDoc(col, idStr); err != nil {
		fail(w, err)
		return
	}
	n, err := embedAndStore(store, emb, col, idStr, clean)
	if err != nil {
		fail(w, err)
		return
	}
	audit("reindex", docID)
	writeJSON(w, map[string]any{"doc_id": docID, "version": version, "chunks": n, "sanitized": clean})
}

// POST /dsr?name=John&store=...&embedder=...
func dsr(w http.ResponseWriter, r *http.Request) {
	store, _, embName, err := setup(r)
	if err != nil {
		http.Error(w, err.Error(), 400)
		return
	}
	name := strings.TrimSpace(r.URL.Query().Get("name"))
	if name == "" {
		http.Error(w, "name is required", 400)
		return
	}
	rows, err := db.Query(`SELECT DISTINCT doc_id FROM entities WHERE value ILIKE $1`, "%"+name+"%")
	if err != nil {
		fail(w, err)
		return
	}
	ids := []int{}
	for rows.Next() {
		var id int
		rows.Scan(&id)
		ids = append(ids, id)
	}
	rows.Close()
	for _, id := range ids {
		store.DeleteByDoc("protected_"+embName, strconv.Itoa(id))
		db.Exec(`DELETE FROM entities WHERE doc_id=$1`, id)
		db.Exec(`UPDATE documents SET status='dsr_deleted' WHERE id=$1`, id)
		audit("dsr", id)
	}
	writeJSON(w, map[string]any{"deleted_docs": ids})
}

// GET /ask?q=...&collection=protected|unprotected&store=...&embedder=...
func ask(w http.ResponseWriter, r *http.Request) {
	store, emb, embName, err := setup(r)
	if err != nil {
		http.Error(w, err.Error(), 400)
		return
	}
	q := r.URL.Query()
	col := q.Get("collection")
	if col != "protected" && col != "unprotected" {
		http.Error(w, "collection must be protected or unprotected", 400)
		return
	}
	vec, err := emb.Embed(q.Get("q"))
	if err != nil {
		fail(w, err)
		return
	}
	res, err := store.Search(col+"_"+embName, vec, 3)
	if err != nil {
		fail(w, err)
		return
	}
	writeJSON(w, res)
}

// GET /documents  (used by the dashboard table in step 3)
func documents(w http.ResponseWriter, r *http.Request) {
	rows, err := db.Query(`SELECT id, name, version, status FROM documents ORDER BY id`)
	if err != nil {
		fail(w, err)
		return
	}
	defer rows.Close()
	type D struct {
		ID      int    `json:"id"`
		Name    string `json:"name"`
		Version int    `json:"version"`
		Status  string `json:"status"`
	}
	out := []D{}
	for rows.Next() {
		var d D
		rows.Scan(&d.ID, &d.Name, &d.Version, &d.Status)
		out = append(out, d)
	}
	writeJSON(w, out)
}

func main() {
	var err error
	db, err = sql.Open("postgres", os.Getenv("DATABASE_URL"))
	if err != nil {
		log.Fatal(err)
	}
	for i := 0; i < 30; i++ { // wait for Postgres to be ready
		if err = db.Ping(); err == nil {
			break
		}
		time.Sleep(time.Second)
	}
	if err != nil {
		log.Fatal(err)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("POST /ingest", ingest)
	mux.HandleFunc("POST /ingest-raw", ingestRaw)
	mux.HandleFunc("POST /reindex/{id}", reindex)
	mux.HandleFunc("POST /dsr", dsr)
	mux.HandleFunc("GET /ask", ask)
	mux.HandleFunc("GET /documents", documents)

	log.Println("orchestrator listening on :8080")
	log.Fatal(http.ListenAndServe(":8080", cors(mux)))
}