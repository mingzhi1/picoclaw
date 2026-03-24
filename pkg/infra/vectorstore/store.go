// Package vectorstore provides vector storage and similarity search.
package vectorstore

import "context"

// Chunk is a single piece of indexed content with its relevance score.
type Chunk struct {
	ID      string
	Content string
	Source  string  // origin file path or URL
	Score   float64 // cosine similarity (0..1), set by Search
}

// VectorStore is the interface for vector storage backends.
type VectorStore interface {
	// Upsert inserts or replaces chunks with their embeddings.
	Upsert(ctx context.Context, chunks []Chunk, embeddings [][]float32) error
	// Search finds the topK most similar chunks to the query embedding.
	Search(ctx context.Context, embedding []float32, topK int) ([]Chunk, error)
	// DeleteBySource removes all chunks from the given source.
	DeleteBySource(ctx context.Context, source string) error
	// Close releases resources.
	Close() error
}
