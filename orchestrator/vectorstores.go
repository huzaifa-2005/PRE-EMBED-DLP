package main

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync/atomic"
	"time"
)

var pointID = uint64(time.Now().UnixNano())

func vecString(v []float32) string {
	parts := make([]string, len(v))
	for i, x := range v {
		parts[i] = strconv.FormatFloat(float64(x), 'f', -1, 32)
	}
	return "[" + strings.Join(parts, ",") + "]"
}

// ---------- Adapter 1: Qdrant (REST) ----------

type QdrantStore struct{ URL string }

func (q QdrantStore) call(method, path string, body any, out any) error {
	b, _ := json.Marshal(body)
	req, err := http.NewRequest(method, q.URL+path, bytes.NewReader(b))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("qdrant %s: %s", resp.Status, data)
	}
	if out != nil {
		return json.Unmarshal(data, out)
	}
	return nil
}

// creates the collection if missing (error ignored when it already exists)
func (q QdrantStore) ensure(col string) {
	q.call("PUT", "/collections/"+col, map[string]any{
		"vectors": map[string]any{"size": embedDim, "distance": "Cosine"},
	}, nil)
}

func (q QdrantStore) Upsert(col, docID, text string, vec []float32) error {
	q.ensure(col)
	body := map[string]any{"points": []map[string]any{{
		"id":      atomic.AddUint64(&pointID, 1),
		"vector":  vec,
		"payload": map[string]any{"doc_id": docID, "text": text},
	}}}
	return q.call("PUT", "/collections/"+col+"/points?wait=true", body, nil)
}

func (q QdrantStore) DeleteByDoc(col, docID string) error {
	q.ensure(col)
	body := map[string]any{"filter": map[string]any{"must": []map[string]any{
		{"key": "doc_id", "match": map[string]any{"value": docID}},
	}}}
	return q.call("POST", "/collections/"+col+"/points/delete?wait=true", body, nil)
}

func (q QdrantStore) Search(col string, vec []float32, k int) ([]string, error) {
	q.ensure(col)
	var out struct {
		Result []struct {
			Payload struct {
				Text string `json:"text"`
			} `json:"payload"`
		} `json:"result"`
	}
	err := q.call("POST", "/collections/"+col+"/points/search",
		map[string]any{"vector": vec, "limit": k, "with_payload": true}, &out)
	if err != nil {
		return nil, err
	}
	res := []string{}
	for _, r := range out.Result {
		res = append(res, r.Payload.Text)
	}
	return res, nil
}

// ---------- Adapter 2: pgvector (Postgres) ----------

type PgStore struct{ DB *sql.DB }

func (p PgStore) Upsert(col, docID, text string, vec []float32) error {
	_, err := p.DB.Exec(
		`INSERT INTO pg_chunks(doc_id, collection, content, embedding) VALUES($1,$2,$3,$4::vector)`,
		docID, col, text, vecString(vec))
	return err
}

func (p PgStore) DeleteByDoc(col, docID string) error {
	_, err := p.DB.Exec(`DELETE FROM pg_chunks WHERE doc_id=$1 AND collection=$2`, docID, col)
	return err
}

func (p PgStore) Search(col string, vec []float32, k int) ([]string, error) {
	rows, err := p.DB.Query(
		`SELECT content FROM pg_chunks WHERE collection=$1 ORDER BY embedding <=> $2::vector LIMIT $3`,
		col, vecString(vec), k)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	res := []string{}
	for rows.Next() {
		var s string
		rows.Scan(&s)
		res = append(res, s)
	}
	return res, nil
}