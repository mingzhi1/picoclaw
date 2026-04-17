package agent

import (
	"context"
	"crypto/sha256"
	"fmt"
	"strings"

	"github.com/mingzhi1/metaclaw/pkg/infra/logger"
	"github.com/mingzhi1/metaclaw/pkg/infra/vectorstore"
	"github.com/mingzhi1/metaclaw/pkg/llm/providers"
)

// RAGStore connects an EmbeddingProvider with a VectorStore to provide
// semantic search over indexed content.
type RAGStore struct {
	vectors  *vectorstore.SQLiteStore
	embedder providers.EmbeddingProvider
}

// NewRAGStore creates a RAGStore from an embedding provider and vector store.
func NewRAGStore(embedder providers.EmbeddingProvider, vectors *vectorstore.SQLiteStore) *RAGStore {
	return &RAGStore{embedder: embedder, vectors: vectors}
}

// Search finds the topK most relevant chunks for a natural language query.
func (r *RAGStore) Search(ctx context.Context, query string, topK int) ([]vectorstore.Chunk, error) {
	embs, err := r.embedder.Embed(ctx, []string{query})
	if err != nil {
		return nil, fmt.Errorf("embed query: %w", err)
	}
	if len(embs) == 0 {
		return nil, nil
	}
	return r.vectors.Search(ctx, embs[0], topK)
}

// Index chunks a source's content, generates embeddings, and upserts into the vector store.
// It skips re-indexing if the content hash hasn't changed.
func (r *RAGStore) Index(ctx context.Context, source, content string) error {
	hash := contentHash(content)
	if existing := r.vectors.GetSourceHash(source); existing == hash {
		logger.DebugCF("rag", "Source unchanged, skipping", map[string]any{"source": source})
		return nil
	}

	chunks := chunkText(content, 512, source)
	if len(chunks) == 0 {
		return nil
	}

	texts := make([]string, len(chunks))
	for i, c := range chunks {
		texts[i] = c.Content
	}

	embs, err := r.embedder.Embed(ctx, texts)
	if err != nil {
		return fmt.Errorf("embed chunks: %w", err)
	}

	// Delete old chunks for this source, then insert new ones.
	if err := r.vectors.DeleteBySource(ctx, source); err != nil {
		return fmt.Errorf("delete old chunks: %w", err)
	}
	if err := r.vectors.Upsert(ctx, chunks, embs); err != nil {
		return fmt.Errorf("upsert chunks: %w", err)
	}

	if err := r.vectors.SetSourceHash(source, hash); err != nil {
		logger.WarnCF("rag", "Failed to record source hash", map[string]any{"error": err.Error()})
	}

	logger.InfoCF("rag", "Indexed source", map[string]any{
		"source": source,
		"chunks": len(chunks),
	})
	return nil
}

// RemoveSource deletes all indexed data for a source.
func (r *RAGStore) RemoveSource(ctx context.Context, source string) error {
	return r.vectors.RemoveSource(ctx, source)
}

// ListSources returns all indexed source paths.
func (r *RAGStore) ListSources() ([]string, error) {
	return r.vectors.ListSources()
}

// VectorStore returns the underlying vector storage backend.
func (r *RAGStore) VectorStore() vectorstore.VectorStore {
	return r.vectors
}

// Embedder returns the embedding provider used by this RAG store.
func (r *RAGStore) Embedder() providers.EmbeddingProvider {
	return r.embedder
}

// Close releases the underlying vector store.
func (r *RAGStore) Close() error {
	if r == nil || r.vectors == nil {
		return nil
	}
	return r.vectors.Close()
}

// FormatChunks formats search results for LLM context injection.
func FormatChunks(chunks []vectorstore.Chunk) string {
	if len(chunks) == 0 {
		return ""
	}
	var sb strings.Builder
	sb.WriteString("## Knowledge Base Results\n\n")
	for i, c := range chunks {
		fmt.Fprintf(&sb, "### [%d] Source: %s (score: %.2f)\n", i+1, c.Source, c.Score)
		sb.WriteString(c.Content)
		sb.WriteString("\n\n")
	}
	return sb.String()
}

// --- helpers ---

func contentHash(content string) string {
	h := sha256.Sum256([]byte(content))
	return fmt.Sprintf("%x", h[:8]) // 16-char hex, enough for change detection
}

// chunkText splits text into chunks of approximately maxTokens tokens (~4 chars/token).
// It splits on double newlines first, then on single newlines if blocks are too long.
func chunkText(text string, maxTokens int, source string) []vectorstore.Chunk {
	maxChars := maxTokens * 4
	paragraphs := strings.Split(text, "\n\n")

	var chunks []vectorstore.Chunk
	var current strings.Builder
	idx := 0

	flush := func() {
		s := strings.TrimSpace(current.String())
		if len(s) > 0 {
			chunks = append(chunks, vectorstore.Chunk{
				ID:      fmt.Sprintf("%s#%d", source, idx),
				Content: s,
				Source:  source,
			})
			idx++
		}
		current.Reset()
	}

	for _, para := range paragraphs {
		para = strings.TrimSpace(para)
		if para == "" {
			continue
		}
		if current.Len()+len(para)+2 > maxChars {
			flush()
		}
		if current.Len() > 0 {
			current.WriteString("\n\n")
		}
		current.WriteString(para)
	}
	flush()

	return chunks
}
