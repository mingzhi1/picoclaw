package vectorstore

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestSQLiteStore_UpsertAndSearch(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test_vector.db")

	store, err := NewSQLiteStore(dbPath)
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	defer store.Close()

	ctx := context.Background()

	// Insert 3 chunks with simple embeddings.
	chunks := []Chunk{
		{ID: "a", Content: "Go is a programming language", Source: "test.go"},
		{ID: "b", Content: "Python is great for ML", Source: "test.py"},
		{ID: "c", Content: "Rust focuses on safety", Source: "test.rs"},
	}
	embeddings := [][]float32{
		{1.0, 0.0, 0.0}, // Go
		{0.0, 1.0, 0.0}, // Python
		{0.0, 0.0, 1.0}, // Rust
	}

	if err := store.Upsert(ctx, chunks, embeddings); err != nil {
		t.Fatalf("Upsert: %v", err)
	}

	// Search: query close to "Go" vector.
	query := []float32{0.9, 0.1, 0.0}
	results, err := store.Search(ctx, query, 2)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}
	if results[0].ID != "a" {
		t.Errorf("expected top result 'a' (Go), got '%s'", results[0].ID)
	}
	if results[0].Score < 0.9 {
		t.Errorf("expected high score for Go, got %.4f", results[0].Score)
	}
}

func TestSQLiteStore_DeleteBySource(t *testing.T) {
	dir := t.TempDir()
	store, err := NewSQLiteStore(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	ctx := context.Background()
	chunks := []Chunk{
		{ID: "1", Content: "hello", Source: "file1"},
		{ID: "2", Content: "world", Source: "file2"},
	}
	embs := [][]float32{{1, 0}, {0, 1}}
	store.Upsert(ctx, chunks, embs)

	store.DeleteBySource(ctx, "file1")

	results, _ := store.Search(ctx, []float32{1, 0}, 10)
	if len(results) != 1 {
		t.Fatalf("expected 1 result after delete, got %d", len(results))
	}
	if results[0].Source != "file2" {
		t.Errorf("expected file2, got %s", results[0].Source)
	}
}

func TestSQLiteStore_SourceHash(t *testing.T) {
	dir := t.TempDir()
	store, err := NewSQLiteStore(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	if h := store.GetSourceHash("foo"); h != "" {
		t.Errorf("expected empty hash, got %s", h)
	}

	store.SetSourceHash("foo", "abc123")
	if h := store.GetSourceHash("foo"); h != "abc123" {
		t.Errorf("expected abc123, got %s", h)
	}
}

func TestCosineSimilarity(t *testing.T) {
	tests := []struct {
		a, b []float32
		want float64
	}{
		{[]float32{1, 0, 0}, []float32{1, 0, 0}, 1.0},
		{[]float32{1, 0, 0}, []float32{0, 1, 0}, 0.0},
		{[]float32{1, 0, 0}, []float32{-1, 0, 0}, -1.0},
		{[]float32{}, []float32{}, 0.0},
	}
	for i, tt := range tests {
		got := cosineSimilarity(tt.a, tt.b)
		if abs(got-tt.want) > 0.001 {
			t.Errorf("case %d: got %.4f, want %.4f", i, got, tt.want)
		}
	}
}

func abs(x float64) float64 {
	if x < 0 {
		return -x
	}
	return x
}

func TestMain(m *testing.M) {
	os.Exit(m.Run())
}
