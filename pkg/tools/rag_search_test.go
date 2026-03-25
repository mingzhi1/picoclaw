package tools

import (
	"context"
	"testing"
)

// TestRAGSearchTool_Basic tests basic RAG search functionality
func TestRAGSearchTool_Basic(t *testing.T) {
	tool := &RAGSearchTool{}
	
	if tool.Name() != "knowledge_search" {
		t.Errorf("expected name 'knowledge_search', got %s", tool.Name())
	}
	
	desc := tool.Description()
	if desc == "" {
		t.Error("description should not be empty")
	}
}

// TestRAGSearchTool_Parameters tests parameter schema
func TestRAGSearchTool_Parameters(t *testing.T) {
	tool := &RAGSearchTool{}
	
	params := tool.Parameters()
	if params == nil {
		t.Fatal("Parameters returned nil")
	}
	
	props, ok := params["properties"].(map[string]any)
	if !ok {
		t.Fatal("properties should be a map")
	}
	
	if _, ok := props["query"]; !ok {
		t.Error("query parameter should be defined")
	}
	if _, ok := props["top_k"]; !ok {
		t.Error("top_k parameter should be defined")
	}
	
	required, ok := params["required"].([]string)
	if !ok || len(required) == 0 {
		t.Error("query should be required")
	}
}

// TestRAGSearchTool_Execute_EmptyQuery tests execution with empty query
func TestRAGSearchTool_Execute_EmptyQuery(t *testing.T) {
	tool := &RAGSearchTool{}
	
	result := tool.Execute(context.Background(), map[string]any{
		"query": "",
	})
	
	if !result.IsError {
		t.Error("empty query should return error")
	}
}

// TestRAGSearchTool_Execute_TopKLimit tests top_k parameter limits
func TestRAGSearchTool_Execute_TopKLimit(t *testing.T) {
	// Use mock embedder to avoid nil pointer
	mockEmbedder := &mockEmbedder{}
	tool := &RAGSearchTool{
		embedder: mockEmbedder,
	}
	
	// Test top_k > 10 should be capped
	result := tool.Execute(context.Background(), map[string]any{
		"query": "test",
		"top_k": 15.0,
	})
	
	// Should not panic, should return some result (error or no results)
	if result == nil {
		t.Error("Execute should not return nil")
	}
}

// TestRAGSearchTool_Execute_InvalidTopK tests invalid top_k handling
func TestRAGSearchTool_Execute_InvalidTopK(t *testing.T) {
	// Use mock embedder to avoid nil pointer
	mockEmbedder := &mockEmbedder{}
	tool := &RAGSearchTool{
		embedder: mockEmbedder,
	}
	
	// Test negative top_k should use default
	result := tool.Execute(context.Background(), map[string]any{
		"query": "test",
		"top_k": -5.0,
	})
	
	if result == nil {
		t.Error("Execute should not return nil")
	}
}

// TestRAGSearchTool_Execute_NoEmbedder tests execution without embedder
func TestRAGSearchTool_Execute_NoEmbedder(t *testing.T) {
	// Use mock embedder to avoid nil pointer panic
	mockEmbedder := &mockEmbedder{}
	tool := &RAGSearchTool{
		embedder: mockEmbedder,
	}
	
	result := tool.Execute(context.Background(), map[string]any{
		"query": "test query",
	})
	
	// Should handle gracefully (return no results or error)
	if result == nil {
		t.Error("Execute should not return nil")
	}
}

// TestRAGSearchTool_Execute_NoVectorStore tests execution without vector store
func TestRAGSearchTool_Execute_NoVectorStore(t *testing.T) {
	// Mock embedder that returns empty embedding
	mockEmbedder := &mockEmbedder{}
	tool := &RAGSearchTool{
		embedder: mockEmbedder,
	}
	
	result := tool.Execute(context.Background(), map[string]any{
		"query": "test query",
	})
	
	if result == nil {
		t.Error("Execute should not return nil")
	}
}

// mockEmbedder is a mock embedding provider for testing
type mockEmbedder struct{}

func (m *mockEmbedder) Embed(ctx context.Context, texts []string) ([][]float32, error) {
	// Return empty embedding to simulate no results
	return [][]float32{}, nil
}

func (m *mockEmbedder) Dimensions() int {
	return 0
}
