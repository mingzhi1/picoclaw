package providers

import "context"

// EmbeddingProvider generates vector embeddings from text.
// Implementations should call an embedding API (OpenAI, Ollama, etc.).
type EmbeddingProvider interface {
	// Embed converts one or more texts into float32 vectors.
	Embed(ctx context.Context, texts []string) ([][]float32, error)
	// Dimensions returns the dimensionality of the embedding vectors.
	Dimensions() int
}
