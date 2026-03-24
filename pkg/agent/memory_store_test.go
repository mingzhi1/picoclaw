package agent

import (
	"strings"
	"testing"
)

// TestMemoryStore_SearchByAnyTag_EmptyTags tests that empty tag list returns nil
func TestMemoryStore_SearchByAnyTag_EmptyTags(t *testing.T) {
	dir := t.TempDir()
	store := NewMemoryStore(dir)
	defer store.Close()

	// Add some entries
	_, err := store.AddEntry("test content 1", []string{"go", "deploy"})
	if err != nil {
		t.Fatalf("AddEntry failed: %v", err)
	}

	// Empty tags should return nil
	result, err := store.SearchByAnyTag([]string{})
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if result != nil {
		t.Errorf("expected nil for empty tags, got %d entries", len(result))
	}

	// Whitespace-only tags should also return nil
	result2, err := store.SearchByAnyTag([]string{"", "   ", ""})
	if err != nil {
		t.Errorf("unexpected error for whitespace tags: %v", err)
	}
	if result2 != nil {
		t.Errorf("expected nil for whitespace-only tags, got %d entries", len(result2))
	}
}

// TestMemoryStore_SearchByAnyTag_CaseInsensitive tests tag case normalization
func TestMemoryStore_SearchByAnyTag_CaseInsensitive(t *testing.T) {
	dir := t.TempDir()
	store := NewMemoryStore(dir)
	defer store.Close()

	// Add entry with lowercase tags
	_, err := store.AddEntry("Go deployment guide", []string{"go", "deploy", "CI"})
	if err != nil {
		t.Fatalf("AddEntry failed: %v", err)
	}

	// Search with different cases
	result1, err := store.SearchByAnyTag([]string{"GO"})
	if err != nil {
		t.Fatalf("SearchByAnyTag(GO) failed: %v", err)
	}
	if len(result1) != 1 {
		t.Errorf("expected 1 entry for GO tag, got %d", len(result1))
	}

	result2, err := store.SearchByAnyTag([]string{"ci"})
	if err != nil {
		t.Fatalf("SearchByAnyTag(ci) failed: %v", err)
	}
	if len(result2) != 1 {
		t.Errorf("expected 1 entry for ci tag, got %d", len(result2))
	}
}

// TestMemoryStore_SearchByAnyTag_ORLogic tests that ANY tag match returns results
func TestMemoryStore_SearchByAnyTag_ORLogic(t *testing.T) {
	dir := t.TempDir()
	store := NewMemoryStore(dir)
	defer store.Close()

	// Add entries with different tags
	_, err := store.AddEntry("Entry A", []string{"go", "backend"})
	if err != nil {
		t.Fatalf("AddEntry failed: %v", err)
	}
	_, err = store.AddEntry("Entry B", []string{"python", "backend"})
	if err != nil {
		t.Fatalf("AddEntry failed: %v", err)
	}
	_, err = store.AddEntry("Entry C", []string{"javascript", "frontend"})
	if err != nil {
		t.Fatalf("AddEntry failed: %v", err)
	}

	// Search with OR logic - should match Entry A and Entry B (both have "backend")
	result, err := store.SearchByAnyTag([]string{"backend", "nonexistent"})
	if err != nil {
		t.Fatalf("SearchByAnyTag failed: %v", err)
	}
	if len(result) != 2 {
		t.Errorf("expected 2 entries (OR logic), got %d", len(result))
		for _, e := range result {
			t.Logf("  entry: %s tags=%v", e.Content, e.Tags)
		}
	}

	// Search with multiple tags - should match all 3 entries
	result2, err := store.SearchByAnyTag([]string{"go", "python", "javascript"})
	if err != nil {
		t.Fatalf("SearchByAnyTag failed: %v", err)
	}
	if len(result2) != 3 {
		t.Errorf("expected 3 entries, got %d", len(result2))
	}
}

// TestMemoryStore_SearchByAnyTag_Limit20 tests the 20 entry limit
func TestMemoryStore_SearchByAnyTag_Limit20(t *testing.T) {
	dir := t.TempDir()
	store := NewMemoryStore(dir)
	defer store.Close()

	// Add 25 entries with the same tag
	for i := 0; i < 25; i++ {
		_, err := store.AddEntry(
			strings.Repeat("content ", 10),
			[]string{"common"},
		)
		if err != nil {
			t.Fatalf("AddEntry failed: %v", err)
		}
	}

	result, err := store.SearchByAnyTag([]string{"common"})
	if err != nil {
		t.Fatalf("SearchByAnyTag failed: %v", err)
	}
	if len(result) > 20 {
		t.Errorf("expected max 20 entries, got %d", len(result))
	}
}

// TestMemoryStore_SearchByAnyTag_TagDedup tests that duplicate tags are handled
func TestMemoryStore_SearchByAnyTag_TagDedup(t *testing.T) {
	dir := t.TempDir()
	store := NewMemoryStore(dir)
	defer store.Close()

	_, err := store.AddEntry("Entry with multiple tags", []string{"go", "deploy", "ci"})
	if err != nil {
		t.Fatalf("AddEntry failed: %v", err)
	}

	// Search with duplicate tags
	result, err := store.SearchByAnyTag([]string{"go", "go", "deploy", "deploy", "ci"})
	if err != nil {
		t.Fatalf("SearchByAnyTag failed: %v", err)
	}
	if len(result) != 1 {
		t.Errorf("expected 1 entry (deduplicated tags), got %d", len(result))
	}
}

// TestMemoryStore_AddEntry_LongContent tests handling of long content
func TestMemoryStore_AddEntry_LongContent(t *testing.T) {
	dir := t.TempDir()
	store := NewMemoryStore(dir)
	defer store.Close()

	// Add entry with very long content
	longContent := strings.Repeat("This is a long content. ", 1000)
	id, err := store.AddEntry(longContent, []string{"test"})
	if err != nil {
		t.Fatalf("AddEntry failed: %v", err)
	}
	if id <= 0 {
		t.Errorf("expected positive ID, got %d", id)
	}

	// Retrieve and verify
	entries, err := store.ListEntries(1)
	if err != nil {
		t.Fatalf("ListEntries failed: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}
	// Content should be preserved (may be truncated in storage)
	if len(entries[0].Content) == 0 {
		t.Error("content should not be empty")
	}
}

// TestMemoryStore_NormaliseTags tests tag normalization
func TestMemoryStore_NormaliseTags(t *testing.T) {
	tests := []struct {
		name     string
		input    []string
		expected []string
	}{
		{
			name:     "empty input",
			input:    []string{},
			expected: []string{},
		},
		{
			name:     "whitespace only",
			input:    []string{"", "  ", "\t"},
			expected: []string{},
		},
		{
			name:     "case normalization",
			input:    []string{"Go", "GO", "gO"},
			expected: []string{"go"},
		},
		{
			name:     "whitespace trimming",
			input:    []string{" go ", "deploy ", " ci"},
			expected: []string{"go", "deploy", "ci"},
		},
		{
			name:     "deduplication",
			input:    []string{"go", "go", "deploy", "go"},
			expected: []string{"go", "deploy"},
		},
		{
			name:     "mixed",
			input:    []string{" Go ", "GO", "deploy", "", "  "},
			expected: []string{"go", "deploy"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := normaliseTags(tt.input)
			if len(result) != len(tt.expected) {
				t.Errorf("expected %d tags, got %d: %v", len(tt.expected), len(result), result)
				return
			}
			for i, exp := range tt.expected {
				if result[i] != exp {
					t.Errorf("tag[%d]: expected %q, got %q", i, exp, result[i])
				}
			}
		})
	}
}

// TestMemoryStore_CrossTagRetrieval tests retrieval consistency across multiple tags
func TestMemoryStore_CrossTagRetrieval(t *testing.T) {
	dir := t.TempDir()
	store := NewMemoryStore(dir)
	defer store.Close()

	// Create entries with overlapping tags
	_, err := store.AddEntry("Entry 1: go+deploy", []string{"go", "deploy"})
	if err != nil {
		t.Fatalf("AddEntry failed: %v", err)
	}
	_, err = store.AddEntry("Entry 2: go+ci", []string{"go", "ci"})
	if err != nil {
		t.Fatalf("AddEntry failed: %v", err)
	}
	_, err = store.AddEntry("Entry 3: deploy+ci", []string{"deploy", "ci"})
	if err != nil {
		t.Fatalf("AddEntry failed: %v", err)
	}
	_, err = store.AddEntry("Entry 4: go+deploy+ci", []string{"go", "deploy", "ci"})
	if err != nil {
		t.Fatalf("AddEntry failed: %v", err)
	}

	// Search for "go" - should get entries 1, 2, 4
	result, err := store.SearchByAnyTag([]string{"go"})
	if err != nil {
		t.Fatalf("SearchByAnyTag failed: %v", err)
	}
	if len(result) != 3 {
		t.Errorf("expected 3 entries for 'go' tag, got %d", len(result))
	}

	// Search for "deploy" - should get entries 1, 3, 4
	result2, err := store.SearchByAnyTag([]string{"deploy"})
	if err != nil {
		t.Fatalf("SearchByAnyTag failed: %v", err)
	}
	if len(result2) != 3 {
		t.Errorf("expected 3 entries for 'deploy' tag, got %d", len(result2))
	}
}
