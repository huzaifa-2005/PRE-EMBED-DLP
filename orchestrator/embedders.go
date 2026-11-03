package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"os"
)

const embedDim = 1024 // must match vector(1024) in schema.sql and the Qdrant collection

// ---------- shared helpers ----------

func postJSON(url string, headers map[string]string, body any, out any) error {
	b, err := json.Marshal(body)
	if err != nil {
		return err
	}
	req, err := http.NewRequest("POST", url, bytes.NewReader(b))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("%s: %s", resp.Status, data)
	}
	return json.Unmarshal(data, out)
}

func normalize(v []float32) []float32 {
	var sum float64
	for _, x := range v {
		sum += float64(x) * float64(x)
	}
	n := float32(math.Sqrt(sum))
	if n == 0 {
		return v
	}
	for i := range v {
		v[i] /= n
	}
	return v
}

// ---------- Adapter 1: Gemini ----------

type GeminiEmbedder struct{ Key string }

func (g GeminiEmbedder) Embed(text string) ([]float32, error) {
	url := "https://generativelanguage.googleapis.com/v1beta/models/gemini-embedding-001:embedContent"
	body := map[string]any{
		"content":              map[string]any{"parts": []map[string]string{{"text": text}}},
		"outputDimensionality": embedDim,
	}
	var out struct {
		Embedding struct {
			Values []float32 `json:"values"`
		} `json:"embedding"`
	}
	if err := postJSON(url, map[string]string{"x-goog-api-key": g.Key}, body, &out); err != nil {
		return nil, fmt.Errorf("gemini: %w", err)
	}
	if len(out.Embedding.Values) == 0 {
		return nil, fmt.Errorf("gemini: empty embedding")
	}
	return normalize(out.Embedding.Values), nil
}

// ---------- Adapter 2: Cohere ----------

type CohereEmbedder struct{ Key string }

func (c CohereEmbedder) Embed(text string) ([]float32, error) {
	body := map[string]any{
		"model":           "embed-english-v3.0",
		"texts":           []string{text},
		"input_type":      "search_document",
		"embedding_types": []string{"float"},
	}
	var out struct {
		Embeddings struct {
			Float [][]float32 `json:"float"`
		} `json:"embeddings"`
	}
	if err := postJSON("https://api.cohere.com/v2/embed",
		map[string]string{"Authorization": "Bearer " + c.Key}, body, &out); err != nil {
		return nil, fmt.Errorf("cohere: %w", err)
	}
	if len(out.Embeddings.Float) == 0 {
		return nil, fmt.Errorf("cohere: empty embedding")
	}
	return out.Embeddings.Float[0], nil
}

// ---------- Picks the adapter from the ?embedder= value ----------

func newEmbedder(name string) (EmbeddingProvider, error) {
	switch name {
	case "gemini":
		return GeminiEmbedder{Key: os.Getenv("GEMINI_API_KEY")}, nil
	case "cohere":
		return CohereEmbedder{Key: os.Getenv("COHERE_API_KEY")}, nil
	}
	return nil, fmt.Errorf("unknown embedder %q", name)
}