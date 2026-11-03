package main

// EmbeddingProvider turns text into a vector.
type EmbeddingProvider interface {
	Embed(text string) ([]float32, error)
}

// VectorStore stores and searches vectors. "collection" is
// "protected" or "unprotected" (used for the FR5 demo).
type VectorStore interface {
	Upsert(collection, docID, text string, vec []float32) error
	DeleteByDoc(collection, docID string) error
	Search(collection string, vec []float32, k int) ([]string, error)
}