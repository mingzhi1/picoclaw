package tools

import (
	"context"
	"fmt"
	"strings"

	"github.com/mingzhi1/metaclaw/pkg/infra/logger"
	"github.com/mingzhi1/metaclaw/pkg/infra/vectorstore"
	"github.com/mingzhi1/metaclaw/pkg/llm/providers"
)

// RAGSearchTool allows the Agent to search the knowledge base via tool_call.
type RAGSearchTool struct {
	vectors  vectorstore.VectorStore
	embedder providers.EmbeddingProvider
}

// NewRAGSearchTool creates a knowledge_search tool.
func NewRAGSearchTool(vectors vectorstore.VectorStore, embedder providers.EmbeddingProvider) *RAGSearchTool {
	return &RAGSearchTool{vectors: vectors, embedder: embedder}
}

func (t *RAGSearchTool) Name() string { return "knowledge_search" }
func (t *RAGSearchTool) Description() string {
	return "Search the indexed knowledge base (code, docs, URLs) for relevant content. Use when you need to look up specific implementation details, API usage, or documentation."
}

func (t *RAGSearchTool) Parameters() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"query": map[string]any{
				"type":        "string",
				"description": "Natural language search query describing what you're looking for",
			},
			"top_k": map[string]any{
				"type":        "integer",
				"description": "Number of results to return (default: 5, max: 10)",
			},
		},
		"required": []string{"query"},
	}
}

func (t *RAGSearchTool) Execute(ctx context.Context, args map[string]any) *ToolResult {
	query, _ := args["query"].(string)
	if query == "" {
		return ErrorResult("query parameter is required")
	}

	topK := 5
	if k, ok := args["top_k"].(float64); ok && k > 0 {
		topK = int(k)
		if topK > 10 {
			topK = 10
		}
	}

	// Embed query
	embs, err := t.embedder.Embed(ctx, []string{query})
	if err != nil {
		logger.WarnCF("rag_tool", "Embed failed", map[string]any{"error": err.Error()})
		return ErrorResult(fmt.Sprintf("knowledge search failed: %v", err))
	}
	if len(embs) == 0 {
		return SilentResult("No embedding generated.")
	}

	// Vector search
	chunks, err := t.vectors.Search(ctx, embs[0], topK)
	if err != nil {
		logger.WarnCF("rag_tool", "Search failed", map[string]any{"error": err.Error()})
		return ErrorResult(fmt.Sprintf("knowledge search failed: %v", err))
	}

	if len(chunks) == 0 {
		logger.InfoCF("rag_tool", "No results", map[string]any{"query": query})
		return SilentResult("No relevant results found in the knowledge base.")
	}

	logger.InfoCF("rag_tool", "Search completed", map[string]any{
		"query":     query,
		"results":   len(chunks),
		"top_score": chunks[0].Score,
	})

	var sb strings.Builder
	fmt.Fprintf(&sb, "Found %d relevant results:\n\n", len(chunks))
	for i, c := range chunks {
		fmt.Fprintf(&sb, "--- Result %d (source: %s, score: %.2f) ---\n", i+1, c.Source, c.Score)
		sb.WriteString(c.Content)
		sb.WriteString("\n\n")
	}

	return SilentResult(sb.String())
}
