package agent

import (
	"testing"
	"time"
)

func TestTurnStore_InsertAndQueryRecent(t *testing.T) {
	dir := t.TempDir()
	store, err := NewTurnStore(dir)
	if err != nil {
		t.Fatalf("NewTurnStore: %v", err)
	}
	defer store.Close()

	r := TurnRecord{
		Ts:         time.Now().Unix(),
		ChannelKey: "cli:direct",
		Score:      5,
		Intent:     "task",
		Tags:       []string{"deploy", "ci"},
		UserMsg:    "deploy now",
		Reply:      "done",
		ToolCalls:  []ToolCallRecord{{Name: "exec", Error: ""}},
		Status:     "pending",
	}

	if err := store.Insert(r); err != nil {
		t.Fatalf("Insert: %v", err)
	}

	rows, err := store.QueryRecent("cli:direct", 10)
	if err != nil {
		t.Fatalf("QueryRecent: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("expected 1 row, got %d", len(rows))
	}
	if rows[0].Intent != "task" {
		t.Errorf("unexpected intent: %s", rows[0].Intent)
	}
	if len(rows[0].Tags) != 2 {
		t.Errorf("expected 2 tags, got %v", rows[0].Tags)
	}
}

func TestTurnStore_QueryByScore(t *testing.T) {
	dir := t.TempDir()
	store, err := NewTurnStore(dir)
	if err != nil {
		t.Fatalf("NewTurnStore: %v", err)
	}
	defer store.Close()

	now := time.Now().Unix()
	store.Insert(TurnRecord{ID: "s-1", Ts: now, ChannelKey: "cli:test", Score: 3, UserMsg: "a", Reply: "b", Status: "pending"})
	store.Insert(TurnRecord{ID: "s-2", Ts: now + 1, ChannelKey: "cli:test", Score: 8, UserMsg: "c", Reply: "d", Status: "pending"})
	store.Insert(TurnRecord{ID: "s-3", Ts: now + 2, ChannelKey: "cli:test", Score: 9, UserMsg: "e", Reply: "f", Status: "pending"})

	high, err := store.QueryByScore("cli:test", 7)
	if err != nil {
		t.Fatalf("QueryByScore: %v", err)
	}
	if len(high) != 2 {
		t.Errorf("expected 2 always_keep turns, got %d", len(high))
	}
}

func TestTurnStore_SetStatus(t *testing.T) {
	dir := t.TempDir()
	store, err := NewTurnStore(dir)
	if err != nil {
		t.Fatalf("NewTurnStore: %v", err)
	}
	defer store.Close()

	r := TurnRecord{ID: "test-id-1", Ts: time.Now().Unix(), UserMsg: "x", Reply: "y", Status: "pending"}
	store.Insert(r)

	if err := store.SetStatus("test-id-1", "processed"); err != nil {
		t.Fatalf("SetStatus: %v", err)
	}

	pending, err := store.QueryPending(10)
	if err != nil {
		t.Fatalf("QueryPending: %v", err)
	}
	if len(pending) != 0 {
		t.Errorf("expected 0 pending, got %d", len(pending))
	}
}

func TestTurnStore_ArchiveOldProcessed(t *testing.T) {
	dir := t.TempDir()
	store, err := NewTurnStore(dir)
	if err != nil {
		t.Fatalf("NewTurnStore: %v", err)
	}
	defer store.Close()

	// Insert old processed turns (timestamp in the past).
	old := time.Now().AddDate(0, 0, -10).Unix()
	for i := 0; i < 3; i++ {
		r := TurnRecord{Ts: old, Score: 2, UserMsg: "old", Reply: "msg", Status: "processed"}
		store.Insert(r)
	}

	// Recent processed turn — should NOT be archived.
	recent := TurnRecord{Ts: time.Now().Unix(), Score: 2, UserMsg: "new", Reply: "msg", Status: "processed"}
	store.Insert(recent)

	if err := store.ArchiveOldProcessed(7); err != nil {
		t.Fatalf("ArchiveOldProcessed: %v", err)
	}

	// Query pending (should still be 0).
	pending, _ := store.QueryPending(100)
	if len(pending) != 0 {
		t.Errorf("expected 0 pending after archive, got %d", len(pending))
	}
}

func TestTurnStore_QueryByTags(t *testing.T) {
	dir := t.TempDir()
	store, err := NewTurnStore(dir)
	if err != nil {
		t.Fatalf("NewTurnStore: %v", err)
	}
	defer store.Close()

	now := time.Now().Unix()
	store.Insert(TurnRecord{ID: "tag-1", Ts: now, ChannelKey: "cli:test", Score: 5, Tags: []string{"deploy", "ci"}, UserMsg: "a", Reply: "b"})
	store.Insert(TurnRecord{ID: "tag-2", Ts: now + 1, ChannelKey: "cli:test", Score: 4, Tags: []string{"file", "read"}, UserMsg: "c", Reply: "d"})
	store.Insert(TurnRecord{ID: "tag-3", Ts: now + 2, ChannelKey: "cli:test", Score: 3, Tags: []string{"deploy", "log"}, UserMsg: "e", Reply: "f"})

	rows, err := store.QueryByTags("cli:test", []string{"deploy"})
	if err != nil {
		t.Fatalf("QueryByTags: %v", err)
	}
	if len(rows) < 2 {
		t.Errorf("expected at least 2 deploy turns, got %d", len(rows))
	}
}

// TestTurnStore_QueryByTags_EmptyTags tests that empty tag list returns nil
func TestTurnStore_QueryByTags_EmptyTags(t *testing.T) {
	dir := t.TempDir()
	store, err := NewTurnStore(dir)
	if err != nil {
		t.Fatalf("NewTurnStore: %v", err)
	}
	defer store.Close()

	// Add some turns
	store.Insert(TurnRecord{ID: "t1", Ts: time.Now().Unix(), ChannelKey: "cli:test",
		Score: 5, Tags: []string{"deploy"}, UserMsg: "a", Reply: "b"})

	// Empty tags should return nil
	result, err := store.QueryByTags("cli:test", []string{})
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if result != nil {
		t.Errorf("expected nil for empty tags, got %d turns", len(result))
	}

	// Whitespace-only tags should also return nil
	result2, err := store.QueryByTags("cli:test", []string{"", "   ", ""})
	if err != nil {
		t.Errorf("unexpected error for whitespace tags: %v", err)
	}
	if result2 != nil {
		t.Errorf("expected nil for whitespace-only tags, got %d turns", len(result2))
	}
}

// TestTurnStore_QueryByTags_CaseInsensitive tests tag case normalization
func TestTurnStore_QueryByTags_CaseInsensitive(t *testing.T) {
	dir := t.TempDir()
	store, err := NewTurnStore(dir)
	if err != nil {
		t.Fatalf("NewTurnStore: %v", err)
	}
	defer store.Close()

	now := time.Now().Unix()
	store.Insert(TurnRecord{ID: "ci-1", Ts: now, ChannelKey: "cli:test",
		Score: 5, Tags: []string{"Go", "DEPLOY", "Ci"}, UserMsg: "a", Reply: "b"})

	// Search with different cases
	result1, err := store.QueryByTags("cli:test", []string{"GO"})
	if err != nil {
		t.Fatalf("QueryByTags(GO) failed: %v", err)
	}
	if len(result1) != 1 {
		t.Errorf("expected 1 turn for GO tag, got %d", len(result1))
	}

	result2, err := store.QueryByTags("cli:test", []string{"ci"})
	if err != nil {
		t.Fatalf("QueryByTags(ci) failed: %v", err)
	}
	if len(result2) != 1 {
		t.Errorf("expected 1 turn for ci tag, got %d", len(result2))
	}
}

// TestTurnStore_QueryByTags_ORLogic tests that ANY tag match returns results
func TestTurnStore_QueryByTags_ORLogic(t *testing.T) {
	dir := t.TempDir()
	store, err := NewTurnStore(dir)
	if err != nil {
		t.Fatalf("NewTurnStore: %v", err)
	}
	defer store.Close()

	now := time.Now().Unix()
	store.Insert(TurnRecord{ID: "or-1", Ts: now, ChannelKey: "cli:test",
		Score: 5, Tags: []string{"go", "backend"}, UserMsg: "a", Reply: "b"})
	store.Insert(TurnRecord{ID: "or-2", Ts: now + 1, ChannelKey: "cli:test",
		Score: 4, Tags: []string{"python", "backend"}, UserMsg: "c", Reply: "d"})
	store.Insert(TurnRecord{ID: "or-3", Ts: now + 2, ChannelKey: "cli:test",
		Score: 3, Tags: []string{"javascript", "frontend"}, UserMsg: "e", Reply: "f"})

	// Search with OR logic - should match or-1 and or-2 (both have "backend")
	result, err := store.QueryByTags("cli:test", []string{"backend", "nonexistent"})
	if err != nil {
		t.Fatalf("QueryByTags failed: %v", err)
	}
	if len(result) != 2 {
		t.Errorf("expected 2 turns (OR logic), got %d", len(result))
		for _, tt := range result {
			t.Logf("  turn: id=%s tags=%v", tt.ID, tt.Tags)
		}
	}

	// Search with multiple tags - should match all 3 turns
	result2, err := store.QueryByTags("cli:test", []string{"go", "python", "javascript"})
	if err != nil {
		t.Fatalf("QueryByTags failed: %v", err)
	}
	if len(result2) != 3 {
		t.Errorf("expected 3 turns, got %d", len(result2))
	}
}

// TestTurnStore_QueryByTags_ScoreFilter tests that score >= 0 filter is applied
func TestTurnStore_QueryByTags_ScoreFilter(t *testing.T) {
	dir := t.TempDir()
	store, err := NewTurnStore(dir)
	if err != nil {
		t.Fatalf("NewTurnStore: %v", err)
	}
	defer store.Close()

	now := time.Now().Unix()
	// score = 0, should be included (score >= 0)
	store.Insert(TurnRecord{ID: "score-0", Ts: now, ChannelKey: "cli:test",
		Score: 0, Tags: []string{"deploy"}, UserMsg: "a", Reply: "b"})
	// score > 0, should be included
	store.Insert(TurnRecord{ID: "score-1", Ts: now + 1, ChannelKey: "cli:test",
		Score: 1, Tags: []string{"deploy"}, UserMsg: "c", Reply: "d"})
	store.Insert(TurnRecord{ID: "score-5", Ts: now + 2, ChannelKey: "cli:test",
		Score: 5, Tags: []string{"deploy"}, UserMsg: "e", Reply: "f"})

	result, err := store.QueryByTags("cli:test", []string{"deploy"})
	if err != nil {
		t.Fatalf("QueryByTags failed: %v", err)
	}

	// All turns with score >= 0 should be included
	if len(result) != 3 {
		t.Errorf("expected 3 turns (score >= 0), got %d", len(result))
		for _, tt := range result {
			t.Logf("  turn: id=%s score=%d", tt.ID, tt.Score)
		}
	}
}

// TestTurnStore_QueryByTags_ChannelIsolation tests channel key filtering
func TestTurnStore_QueryByTags_ChannelIsolation(t *testing.T) {
	dir := t.TempDir()
	store, err := NewTurnStore(dir)
	if err != nil {
		t.Fatalf("NewTurnStore: %v", err)
	}
	defer store.Close()

	now := time.Now().Unix()
	store.Insert(TurnRecord{ID: "ch1-1", Ts: now, ChannelKey: "channel1:user1",
		Score: 5, Tags: []string{"deploy"}, UserMsg: "a", Reply: "b"})
	store.Insert(TurnRecord{ID: "ch2-1", Ts: now + 1, ChannelKey: "channel2:user1",
		Score: 5, Tags: []string{"deploy"}, UserMsg: "c", Reply: "d"})

	// Query channel1 - should only get ch1-1
	result1, err := store.QueryByTags("channel1:user1", []string{"deploy"})
	if err != nil {
		t.Fatalf("QueryByTags(channel1) failed: %v", err)
	}
	if len(result1) != 1 {
		t.Errorf("expected 1 turn for channel1, got %d", len(result1))
	}
	if result1[0].ID != "ch1-1" {
		t.Errorf("expected ch1-1, got %s", result1[0].ID)
	}

	// Query channel2 - should only get ch2-1
	result2, err := store.QueryByTags("channel2:user1", []string{"deploy"})
	if err != nil {
		t.Fatalf("QueryByTags(channel2) failed: %v", err)
	}
	if len(result2) != 1 {
		t.Errorf("expected 1 turn for channel2, got %d", len(result2))
	}
	if result2[0].ID != "ch2-1" {
		t.Errorf("expected ch2-1, got %s", result2[0].ID)
	}
}

// TestTurnStore_QueryByTags_ArchivedExclusion tests that archived turns are excluded
func TestTurnStore_QueryByTags_ArchivedExclusion(t *testing.T) {
	dir := t.TempDir()
	store, err := NewTurnStore(dir)
	if err != nil {
		t.Fatalf("NewTurnStore: %v", err)
	}
	defer store.Close()

	now := time.Now().Unix()
	store.Insert(TurnRecord{ID: "arch-1", Ts: now, ChannelKey: "cli:test",
		Score: 5, Tags: []string{"deploy"}, UserMsg: "a", Reply: "b", Status: "pending"})
	store.Insert(TurnRecord{ID: "arch-2", Ts: now + 1, ChannelKey: "cli:test",
		Score: 5, Tags: []string{"deploy"}, UserMsg: "c", Reply: "d", Status: "archived"})

	result, err := store.QueryByTags("cli:test", []string{"deploy"})
	if err != nil {
		t.Fatalf("QueryByTags failed: %v", err)
	}

	// Should exclude archived
	for _, tt := range result {
		if tt.ID == "arch-2" {
			t.Error("archived turn should be excluded from tag search results")
		}
	}
	if len(result) != 1 {
		t.Errorf("expected 1 turn (excluding archived), got %d", len(result))
	}
}

// TestTurnStore_QueryByTags_TagDedup tests that duplicate tags are handled
func TestTurnStore_QueryByTags_TagDedup(t *testing.T) {
	dir := t.TempDir()
	store, err := NewTurnStore(dir)
	if err != nil {
		t.Fatalf("NewTurnStore: %v", err)
	}
	defer store.Close()

	now := time.Now().Unix()
	store.Insert(TurnRecord{ID: "dup-1", Ts: now, ChannelKey: "cli:test",
		Score: 5, Tags: []string{"deploy", "ci"}, UserMsg: "a", Reply: "b"})

	// Search with duplicate tags
	result, err := store.QueryByTags("cli:test", []string{"deploy", "deploy", "ci", "ci"})
	if err != nil {
		t.Fatalf("QueryByTags failed: %v", err)
	}
	if len(result) != 1 {
		t.Errorf("expected 1 turn (deduplicated tags), got %d", len(result))
	}
}

// TestTurnStore_QueryTagsForChannel tests listing all tags for a channel
func TestTurnStore_QueryTagsForChannel(t *testing.T) {
	dir := t.TempDir()
	store, err := NewTurnStore(dir)
	if err != nil {
		t.Fatalf("NewTurnStore: %v", err)
	}
	defer store.Close()

	now := time.Now().Unix()
	store.Insert(TurnRecord{ID: "tags-1", Ts: now, ChannelKey: "cli:test",
		Score: 5, Tags: []string{"go", "deploy"}, UserMsg: "a", Reply: "b"})
	store.Insert(TurnRecord{ID: "tags-2", Ts: now + 1, ChannelKey: "cli:test",
		Score: 4, Tags: []string{"python", "ci"}, UserMsg: "c", Reply: "d"})
	store.Insert(TurnRecord{ID: "tags-3", Ts: now + 2, ChannelKey: "cli:test",
		Score: 3, Tags: []string{"go", "test"}, UserMsg: "e", Reply: "f"})

	tags, err := store.QueryTagsForChannel("cli:test")
	if err != nil {
		t.Fatalf("QueryTagsForChannel failed: %v", err)
	}

	expectedTags := map[string]bool{
		"go":      true,
		"deploy":  true,
		"python":  true,
		"ci":      true,
		"test":    true,
	}
	if len(tags) != len(expectedTags) {
		t.Errorf("expected %d unique tags, got %d: %v", len(expectedTags), len(tags), tags)
	}
	for _, tag := range tags {
		if !expectedTags[tag] {
			t.Errorf("unexpected tag: %s", tag)
		}
	}
}

// TestTurnStore_QueryByTags_CrossTagRetrieval tests retrieval with overlapping tags
func TestTurnStore_QueryByTags_CrossTagRetrieval(t *testing.T) {
	dir := t.TempDir()
	store, err := NewTurnStore(dir)
	if err != nil {
		t.Fatalf("NewTurnStore: %v", err)
	}
	defer store.Close()

	now := time.Now().Unix()
	store.Insert(TurnRecord{ID: "cross-1", Ts: now, ChannelKey: "cli:test",
		Score: 5, Tags: []string{"go", "deploy"}, UserMsg: "a", Reply: "b"})
	store.Insert(TurnRecord{ID: "cross-2", Ts: now + 1, ChannelKey: "cli:test",
		Score: 4, Tags: []string{"go", "ci"}, UserMsg: "c", Reply: "d"})
	store.Insert(TurnRecord{ID: "cross-3", Ts: now + 2, ChannelKey: "cli:test",
		Score: 3, Tags: []string{"deploy", "ci"}, UserMsg: "e", Reply: "f"})
	store.Insert(TurnRecord{ID: "cross-4", Ts: now + 3, ChannelKey: "cli:test",
		Score: 2, Tags: []string{"go", "deploy", "ci"}, UserMsg: "g", Reply: "h"})

	// Search for "go" - should get cross-1, cross-2, cross-4
	result, err := store.QueryByTags("cli:test", []string{"go"})
	if err != nil {
		t.Fatalf("QueryByTags failed: %v", err)
	}
	if len(result) != 3 {
		t.Errorf("expected 3 turns for 'go' tag, got %d", len(result))
		for _, tt := range result {
			t.Logf("  turn: id=%s tags=%v", tt.ID, tt.Tags)
		}
	}

	// Search for "deploy" - should get cross-1, cross-3, cross-4
	result2, err := store.QueryByTags("cli:test", []string{"deploy"})
	if err != nil {
		t.Fatalf("QueryByTags failed: %v", err)
	}
	if len(result2) != 3 {
		t.Errorf("expected 3 turns for 'deploy' tag, got %d", len(result2))
	}
}
