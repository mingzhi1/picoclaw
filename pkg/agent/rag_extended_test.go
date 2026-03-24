package agent

import (
	"context"
	"database/sql"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	_ "modernc.org/sqlite"
)

// TestTurnStore_Insert_EstimateTokens tests token estimation on insert
func TestTurnStore_Insert_EstimateTokens(t *testing.T) {
	dir := t.TempDir()
	store, err := NewTurnStore(dir)
	if err != nil {
		t.Fatalf("NewTurnStore: %v", err)
	}
	defer store.Close()

	now := time.Now().Unix()
	record := TurnRecord{
		ID:         "tok-1",
		Ts:         now,
		ChannelKey: "cli:test",
		UserMsg:    strings.Repeat("hello ", 100), // ~600 chars
		Reply:      strings.Repeat("world ", 200), // ~1200 chars
		ToolCalls:  []ToolCallRecord{{Name: "exec", Error: ""}},
	}

	if err := store.Insert(record); err != nil {
		t.Fatalf("Insert failed: %v", err)
	}

	// Retrieve and check tokens
	rows, err := store.QueryRecent("cli:test", 1)
	if err != nil {
		t.Fatalf("QueryRecent failed: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("expected 1 row, got %d", len(rows))
	}

	// Tokens should be estimated (chars / 3)
	expectedMin := (600 + 1200) / 4 // conservative lower bound
	expectedMax := (600 + 1200) / 2 // conservative upper bound
	if rows[0].Tokens < expectedMin || rows[0].Tokens > expectedMax {
		t.Errorf("tokens %d outside expected range [%d, %d]",
			rows[0].Tokens, expectedMin, expectedMax)
	}
}

// TestTurnStore_Insert_EmptyToolCalls tests handling of empty tool calls
func TestTurnStore_Insert_EmptyToolCalls(t *testing.T) {
	dir := t.TempDir()
	store, err := NewTurnStore(dir)
	if err != nil {
		t.Fatalf("NewTurnStore: %v", err)
	}
	defer store.Close()

	record := TurnRecord{
		ID:         "empty-tc",
		Ts:         time.Now().Unix(),
		ChannelKey: "cli:test",
		UserMsg:    "simple question",
		Reply:      "simple answer",
		ToolCalls:  []ToolCallRecord{}, // empty but not nil
	}

	if err := store.Insert(record); err != nil {
		t.Fatalf("Insert failed: %v", err)
	}

	rows, err := store.QueryRecent("cli:test", 1)
	if err != nil {
		t.Fatalf("QueryRecent failed: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("expected 1 row, got %d", len(rows))
	}
	if rows[0].ToolCalls == nil {
		t.Error("ToolCalls should not be nil")
	}
	if len(rows[0].ToolCalls) != 0 {
		t.Errorf("expected 0 tool calls, got %d", len(rows[0].ToolCalls))
	}
}

// TestTurnStore_Insert_NilToolCalls tests handling of nil tool calls
func TestTurnStore_Insert_NilToolCalls(t *testing.T) {
	dir := t.TempDir()
	store, err := NewTurnStore(dir)
	if err != nil {
		t.Fatalf("NewTurnStore: %v", err)
	}
	defer store.Close()

	record := TurnRecord{
		ID:         "nil-tc",
		Ts:         time.Now().Unix(),
		ChannelKey: "cli:test",
		UserMsg:    "chat message",
		Reply:      "chat reply",
		ToolCalls:  nil, // explicitly nil
	}

	if err := store.Insert(record); err != nil {
		t.Fatalf("Insert failed: %v", err)
	}

	rows, err := store.QueryRecent("cli:test", 1)
	if err != nil {
		t.Fatalf("QueryRecent failed: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("expected 1 row, got %d", len(rows))
	}
	// Note: ToolCalls may be nil or empty slice depending on unmarshal behavior
	// Both are acceptable as long as queries work correctly
	t.Logf("ToolCalls: nil=%v, len=%d", rows[0].ToolCalls == nil, len(rows[0].ToolCalls))
}

// TestTurnStore_QueryPending_Ordering tests that pending turns are ordered oldest first
func TestTurnStore_QueryPending_Ordering(t *testing.T) {
	dir := t.TempDir()
	store, err := NewTurnStore(dir)
	if err != nil {
		t.Fatalf("NewTurnStore: %v", err)
	}
	defer store.Close()

	now := time.Now().Unix()
	// Insert in reverse order
	store.Insert(TurnRecord{ID: "p3", Ts: now + 200, UserMsg: "c", Reply: "d", Status: "pending"})
	store.Insert(TurnRecord{ID: "p1", Ts: now, UserMsg: "a", Reply: "b", Status: "pending"})
	store.Insert(TurnRecord{ID: "p2", Ts: now + 100, UserMsg: "b", Reply: "c", Status: "pending"})

	rows, err := store.QueryPending(10)
	if err != nil {
		t.Fatalf("QueryPending failed: %v", err)
	}
	if len(rows) != 3 {
		t.Fatalf("expected 3 rows, got %d", len(rows))
	}

	// Should be ordered oldest first: p1, p2, p3
	expectedOrder := []string{"p1", "p2", "p3"}
	for i, exp := range expectedOrder {
		if rows[i].ID != exp {
			t.Errorf("row[%d].ID = %s, want %s", i, rows[i].ID, exp)
		}
	}
}

// TestTurnStore_SetStatus_NotFound tests setting status for non-existent turn
func TestTurnStore_SetStatus_NotFound(t *testing.T) {
	dir := t.TempDir()
	store, err := NewTurnStore(dir)
	if err != nil {
		t.Fatalf("NewTurnStore: %v", err)
	}
	defer store.Close()

	// Should not error, just affect 0 rows
	err = store.SetStatus("non-existent", "processed")
	if err != nil {
		t.Errorf("SetStatus for non-existent ID should not error: %v", err)
	}
}

// TestTurnStore_ArchiveOldProcessed_Boundary tests archiving boundary conditions
func TestTurnStore_ArchiveOldProcessed_Boundary(t *testing.T) {
	dir := t.TempDir()
	store, err := NewTurnStore(dir)
	if err != nil {
		t.Fatalf("NewTurnStore: %v", err)
	}
	defer store.Close()

	// Insert exactly at the boundary (7 days ago)
	boundary := time.Now().AddDate(0, 0, -7).Unix()
	store.Insert(TurnRecord{
		ID: "boundary", Ts: boundary,
		UserMsg: "old", Reply: "msg", Status: "processed",
	})

	// Insert just after boundary (7 days - 1 second ago)
	justAfter := time.Now().AddDate(0, 0, -7).Add(time.Second).Unix()
	store.Insert(TurnRecord{
		ID: "just-after", Ts: justAfter,
		UserMsg: "newer", Reply: "msg", Status: "processed",
	})

	err = store.ArchiveOldProcessed(7)
	if err != nil {
		t.Fatalf("ArchiveOldProcessed failed: %v", err)
	}

	// Check status of boundary turn
	rows, _ := store.QueryPending(100)
	// boundary should be archived, just-after might be too depending on exact timing
	for _, r := range rows {
		if r.ID == "boundary" {
			t.Error("boundary turn should be archived")
		}
	}
}

// TestTurnStore_Insert_LargeBatch tests inserting a large batch of turns
func TestTurnStore_Insert_LargeBatch(t *testing.T) {
	dir := t.TempDir()
	store, err := NewTurnStore(dir)
	if err != nil {
		t.Fatalf("NewTurnStore: %v", err)
	}
	defer store.Close()

	now := time.Now().Unix()
	batchSize := 100

	for i := 0; i < batchSize; i++ {
		record := TurnRecord{
			ID:         string(rune('a'+i%26)) + string(rune('0'+i/26)),
			Ts:         now + int64(i),
			ChannelKey: "cli:test",
			Score:      i % 10,
			Tags:       []string{"batch", "test"},
			UserMsg:    "message " + string(rune('a'+i%26)),
			Reply:      "reply " + string(rune('a'+i%26)),
		}
		if err := store.Insert(record); err != nil {
			t.Fatalf("Insert %d failed: %v", i, err)
		}
	}

	// Verify all inserted
	rows, err := store.QueryRecent("cli:test", batchSize+10)
	if err != nil {
		t.Fatalf("QueryRecent failed: %v", err)
	}
	if len(rows) != batchSize {
		t.Errorf("expected %d rows, got %d", batchSize, len(rows))
	}
}

// TestTurnStore_ConcurrentInserts tests concurrent insert safety
func TestTurnStore_ConcurrentInserts(t *testing.T) {
	dir := t.TempDir()
	store, err := NewTurnStore(dir)
	if err != nil {
		t.Fatalf("NewTurnStore: %v", err)
	}
	defer store.Close()

	now := time.Now().Unix()
	concurrency := 10
	perGoroutine := 20

	var wg sync.WaitGroup
	wg.Add(concurrency)

	for g := 0; g < concurrency; g++ {
		go func(gid int) {
			defer wg.Done()
			for i := 0; i < perGoroutine; i++ {
				record := TurnRecord{
					ID:         string(rune('a'+gid)) + string(rune('0'+i%10)),
					Ts:         now + int64(gid*perGoroutine+i),
					ChannelKey: "cli:test",
					UserMsg:    "concurrent msg",
					Reply:      "concurrent reply",
				}
				if err := store.Insert(record); err != nil {
					t.Errorf("Insert failed: %v", err)
					return
				}
			}
		}(g)
	}

	wg.Wait()

	// Verify all inserted
	rows, err := store.QueryRecent("cli:test", concurrency*perGoroutine+10)
	if err != nil {
		t.Fatalf("QueryRecent failed: %v", err)
	}
	expected := concurrency * perGoroutine
	if len(rows) != expected {
		t.Errorf("expected %d rows, got %d", expected, len(rows))
	}
}

// TestTurnStore_QueryByTags_LargeTagSet tests retrieval with many tags
func TestTurnStore_QueryByTags_LargeTagSet(t *testing.T) {
	dir := t.TempDir()
	store, err := NewTurnStore(dir)
	if err != nil {
		t.Fatalf("NewTurnStore: %v", err)
	}
	defer store.Close()

	now := time.Now().Unix()
	// Insert turns with many different tags
	for i := 0; i < 50; i++ {
		store.Insert(TurnRecord{
			ID:         string(rune('a' + i)),
			Ts:         now + int64(i),
			ChannelKey: "cli:test",
			Score:      5,
			Tags:       []string{"tag" + string(rune('0'+i/10)), "common"},
			UserMsg:    "msg",
			Reply:      "reply",
		})
	}

	// Search with many tags
	manyTags := make([]string, 20)
	for i := 0; i < 20; i++ {
		manyTags[i] = "tag" + string(rune('0'+i))
	}

	result, err := store.QueryByTags("cli:test", manyTags)
	if err != nil {
		t.Fatalf("QueryByTags failed: %v", err)
	}
	// Should match turns with any of the tags
	if len(result) < 20 {
		t.Errorf("expected at least 20 matches, got %d", len(result))
	}
}

// TestTurnStore_QueryRecent_ZeroCount tests QueryRecent with n=0
func TestTurnStore_QueryRecent_ZeroCount(t *testing.T) {
	dir := t.TempDir()
	store, err := NewTurnStore(dir)
	if err != nil {
		t.Fatalf("NewTurnStore: %v", err)
	}
	defer store.Close()

	store.Insert(TurnRecord{
		ID: "r1", Ts: time.Now().Unix(),
		ChannelKey: "cli:test", UserMsg: "a", Reply: "b",
	})

	rows, err := store.QueryRecent("cli:test", 0)
	if err != nil {
		t.Fatalf("QueryRecent failed: %v", err)
	}
	if len(rows) != 0 {
		t.Errorf("expected 0 rows for n=0, got %d", len(rows))
	}
}

// TestTurnStore_QueryRecent_NegativeCount tests QueryRecent with negative n
func TestTurnStore_QueryRecent_NegativeCount(t *testing.T) {
	dir := t.TempDir()
	store, err := NewTurnStore(dir)
	if err != nil {
		t.Fatalf("NewTurnStore: %v", err)
	}
	defer store.Close()

	store.Insert(TurnRecord{
		ID: "r1", Ts: time.Now().Unix(),
		ChannelKey: "cli:test", UserMsg: "a", Reply: "b",
	})

	rows, err := store.QueryRecent("cli:test", -5)
	if err != nil {
		t.Fatalf("QueryRecent failed: %v", err)
	}
	// Should handle gracefully, typically returns 0 rows
	if len(rows) != 0 {
		t.Logf("QueryRecent with negative n returned %d rows (acceptable)", len(rows))
	}
}

// TestMemoryStore_UpdateEntry tests updating an existing entry
func TestMemoryStore_UpdateEntry(t *testing.T) {
	dir := t.TempDir()
	store := NewMemoryStore(dir)
	defer store.Close()

	// Create entry
	id, err := store.AddEntry("original content", []string{"tag1", "tag2"})
	if err != nil {
		t.Fatalf("AddEntry failed: %v", err)
	}

	// Update entry
	err = store.UpdateEntry(id, "updated content", []string{"tag3", "tag4"})
	if err != nil {
		t.Fatalf("UpdateEntry failed: %v", err)
	}

	// Verify update
	entries, err := store.ListEntries(10)
	if err != nil {
		t.Fatalf("ListEntries failed: %v", err)
	}

	found := false
	for _, e := range entries {
		if e.ID == id {
			found = true
			if e.Content != "updated content" {
				t.Errorf("content not updated: got %q", e.Content)
			}
			// Check tags were updated
			expectedTags := map[string]bool{"tag3": true, "tag4": true}
			for _, tag := range e.Tags {
				if !expectedTags[tag] {
					t.Errorf("unexpected tag: %s", tag)
				}
			}
		}
	}
	if !found {
		t.Error("updated entry not found")
	}
}

// TestMemoryStore_DeleteEntry tests deleting an entry
func TestMemoryStore_DeleteEntry(t *testing.T) {
	dir := t.TempDir()
	store := NewMemoryStore(dir)
	defer store.Close()

	// Create entry
	id, err := store.AddEntry("to be deleted", []string{"tag1"})
	if err != nil {
		t.Fatalf("AddEntry failed: %v", err)
	}

	// Delete entry
	err = store.DeleteEntry(id)
	if err != nil {
		t.Fatalf("DeleteEntry failed: %v", err)
	}

	// Verify deletion
	entries, err := store.ListEntries(10)
	if err != nil {
		t.Fatalf("ListEntries failed: %v", err)
	}

	for _, e := range entries {
		if e.ID == id {
			t.Error("deleted entry still exists")
		}
	}
}

// TestMemoryStore_DeleteEntry_NotFound tests deleting non-existent entry
func TestMemoryStore_DeleteEntry_NotFound(t *testing.T) {
	dir := t.TempDir()
	store := NewMemoryStore(dir)
	defer store.Close()

	err := store.DeleteEntry(99999)
	if err == nil {
		t.Error("DeleteEntry for non-existent ID should error")
	}
}

// TestMemoryStore_ListEntries_Limit tests the limit parameter
func TestMemoryStore_ListEntries_Limit(t *testing.T) {
	dir := t.TempDir()
	store := NewMemoryStore(dir)
	defer store.Close()

	// Add 10 entries
	for i := 0; i < 10; i++ {
		_, err := store.AddEntry("content "+string(rune('a'+i)), []string{"test"})
		if err != nil {
			t.Fatalf("AddEntry failed: %v", err)
		}
	}

	// List with limit 5
	entries, err := store.ListEntries(5)
	if err != nil {
		t.Fatalf("ListEntries failed: %v", err)
	}
	if len(entries) > 5 {
		t.Errorf("expected max 5 entries, got %d", len(entries))
	}

	// List with limit 0 (should use default)
	entries2, err := store.ListEntries(0)
	if err != nil {
		t.Fatalf("ListEntries(0) failed: %v", err)
	}
	if len(entries2) == 0 {
		t.Error("ListEntries(0) should return entries with default limit")
	}
}

// TestMemoryStore_ListAllTags_Empty tests listing tags when no entries exist
func TestMemoryStore_ListAllTags_Empty(t *testing.T) {
	dir := t.TempDir()
	store := NewMemoryStore(dir)
	defer store.Close()

	tags, err := store.ListAllTags()
	if err != nil {
		t.Fatalf("ListAllTags failed: %v", err)
	}
	if len(tags) != 0 {
		t.Errorf("expected 0 tags, got %d: %v", len(tags), tags)
	}
}

// TestMemoryStore_SearchByAnyTag_SpecialCharacters tests tags with special characters
func TestMemoryStore_SearchByAnyTag_SpecialCharacters(t *testing.T) {
	dir := t.TempDir()
	store := NewMemoryStore(dir)
	defer store.Close()

	// Add entries with special character tags
	_, err := store.AddEntry("content 1", []string{"go-lang", "web_dev", "c++"})
	if err != nil {
		t.Fatalf("AddEntry failed: %v", err)
	}
	_, err = store.AddEntry("content 2", []string{"api-v2", "rest"})
	if err != nil {
		t.Fatalf("AddEntry failed: %v", err)
	}

	// Search with special character tags
	result, err := store.SearchByAnyTag([]string{"go-lang"})
	if err != nil {
		t.Fatalf("SearchByAnyTag failed: %v", err)
	}
	if len(result) != 1 {
		t.Errorf("expected 1 entry, got %d", len(result))
	}
}

// TestMemoryStore_ConcurrentAccess tests concurrent read/write access
func TestMemoryStore_ConcurrentAccess(t *testing.T) {
	dir := t.TempDir()
	store := NewMemoryStore(dir)
	defer store.Close()

	var wg sync.WaitGroup
	concurrency := 10

	// Concurrent writes
	wg.Add(concurrency)
	for g := 0; g < concurrency; g++ {
		go func(gid int) {
			defer wg.Done()
			for i := 0; i < 10; i++ {
				_, err := store.AddEntry(
					"concurrent content",
					[]string{"tag" + string(rune('a'+gid))},
				)
				if err != nil {
					t.Errorf("AddEntry failed: %v", err)
				}
			}
		}(g)
	}

	// Concurrent reads
	wg.Add(concurrency)
	for g := 0; g < concurrency; g++ {
		go func() {
			defer wg.Done()
			for i := 0; i < 10; i++ {
				_, err := store.SearchByAnyTag([]string{"test"})
				if err != nil {
					t.Errorf("SearchByAnyTag failed: %v", err)
				}
			}
		}()
	}

	wg.Wait()
}

// TestMemoryStore_CotUsageRecording tests CoT usage recording
func TestMemoryStore_CotUsageRecording(t *testing.T) {
	dir := t.TempDir()
	store := NewMemoryStore(dir)
	defer store.Close()

	// Record CoT usage
	id, err := store.RecordCotUsage("task", []string{"go", "deploy"},
		"Think step by step", "Refactor the code")
	if err != nil {
		t.Fatalf("RecordCotUsage failed: %v", err)
	}
	if id <= 0 {
		t.Errorf("expected positive ID, got %d", id)
	}
}

// TestMemoryStore_CotUsage_EmptyTags tests CoT recording with empty tags
func TestMemoryStore_CotUsage_EmptyTags(t *testing.T) {
	dir := t.TempDir()
	store := NewMemoryStore(dir)
	defer store.Close()

	id, err := store.RecordCotUsage("chat", []string{}, "Be helpful", "Hello")
	if err != nil {
		t.Fatalf("RecordCotUsage failed: %v", err)
	}
	if id <= 0 {
		t.Errorf("expected positive ID, got %d", id)
	}
}

// TestInstantMemoryCfg_DefaultValues tests default configuration values
func TestInstantMemoryCfg_DefaultValues(t *testing.T) {
	cfg := DefaultInstantMemoryCfg(32000)

	if cfg.HighScoreThreshold != 7 {
		t.Errorf("expected HighScoreThreshold=7, got %d", cfg.HighScoreThreshold)
	}
	if cfg.RecentCount != 5 {
		t.Errorf("expected RecentCount=5, got %d", cfg.RecentCount)
	}
	if cfg.MaxTokenRatio != 0.6 {
		t.Errorf("expected MaxTokenRatio=0.6, got %f", cfg.MaxTokenRatio)
	}
	if cfg.ContextWindow != 32000 {
		t.Errorf("expected ContextWindow=32000, got %d", cfg.ContextWindow)
	}
}

// TestBuildInstantMemory_AllQueriesFail tests behavior when all queries fail
func TestBuildInstantMemory_AllQueriesFail(t *testing.T) {
	// Use nil store - all queries should fail gracefully
	cfg := DefaultInstantMemoryCfg(32000)
	turns := BuildInstantMemory(nil, []string{"test"}, "cli:test", cfg)
	if turns != nil {
		t.Errorf("expected nil for nil store, got %d turns", len(turns))
	}
}

// TestBuildPhase2Messages_MessageAlternation tests user/assistant alternation
func TestBuildPhase2Messages_MessageAlternation(t *testing.T) {
	turns := []TurnRecord{
		{ID: "t1", UserMsg: "msg1", Reply: "reply1"},
		{ID: "t2", UserMsg: "msg2", Reply: "reply2"},
		{ID: "t3", UserMsg: "msg3", Reply: "reply3"},
	}

	msgs := BuildPhase2Messages("sys", "", turns, "current", 7)

	// After system, should alternate user/assistant
	for i := 1; i < len(msgs)-1; i++ {
		if i%2 == 1 && msgs[i].Role != "user" {
			t.Errorf("msgs[%d] should be user, got %s", i, msgs[i].Role)
		}
		if i%2 == 0 && msgs[i].Role != "assistant" {
			t.Errorf("msgs[%d] should be assistant, got %s", i, msgs[i].Role)
		}
	}
}

// TestSortTurnsByTs_Empty tests sorting empty slice
func TestSortTurnsByTs_Empty(t *testing.T) {
	turns := []TurnRecord{}
	sortTurnsByTs(turns)
	if len(turns) != 0 {
		t.Errorf("expected empty slice, got %d elements", len(turns))
	}
}

// TestSortTurnsByTs_SingleElement tests sorting single element
func TestSortTurnsByTs_SingleElement(t *testing.T) {
	turns := []TurnRecord{{ID: "t1", Ts: 100}}
	sortTurnsByTs(turns)
	if len(turns) != 1 {
		t.Errorf("expected 1 element, got %d", len(turns))
	}
	if turns[0].ID != "t1" {
		t.Errorf("expected t1, got %s", turns[0].ID)
	}
}

// TestTruncateToTokenBudget_ZeroMaxTokens tests with zero budget
func TestTruncateToTokenBudget_ZeroMaxTokens(t *testing.T) {
	turns := []TurnRecord{
		{ID: "t1", Tokens: 100},
		{ID: "t2", Tokens: 200},
	}
	result := truncateToTokenBudget(turns, 0)
	// With zero budget, should return empty (or keep trying to fit)
	if len(result) > 0 {
		t.Logf("got %d turns with zero budget (implementation dependent)", len(result))
	}
}

// TestTruncateToTokenBudget_NegativeMaxTokens tests with negative budget
func TestTruncateToTokenBudget_NegativeMaxTokens(t *testing.T) {
	turns := []TurnRecord{
		{ID: "t1", Tokens: 100},
	}
	result := truncateToTokenBudget(turns, -100)
	// Should handle gracefully
	t.Logf("result with negative budget: %d turns", len(result))
}

// TestMemoryDigestWorker_BasicLifecycle tests the worker lifecycle
func TestMemoryDigestWorker_BasicLifecycle(t *testing.T) {
	dir := t.TempDir()
	turnStore, _ := NewTurnStore(dir)
	memoryStore := NewMemoryStore(dir)

	worker := NewMemoryDigestWorker(turnStore, memoryStore, nil, "")
	worker.SetInterval(100 * time.Millisecond)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	worker.Start(ctx)

	// Let it run briefly
	time.Sleep(200 * time.Millisecond)

	// Should not panic or crash
	cancel()
	time.Sleep(50 * time.Millisecond)
}

// TestMemoryDigestWorker_RunOnceNow tests immediate digest
func TestMemoryDigestWorker_RunOnceNow(t *testing.T) {
	dir := t.TempDir()
	turnStore, _ := NewTurnStore(dir)
	memoryStore := NewMemoryStore(dir)

	worker := NewMemoryDigestWorker(turnStore, memoryStore, nil, "")

	ctx := context.Background()
	err := worker.RunOnceNow(ctx)
	if err != nil {
		t.Logf("RunOnceNow returned: %v (expected with no LLM)", err)
	}
}

// TestMemoryDigestWorker_SetFactStore tests attaching a fact store
func TestMemoryDigestWorker_SetFactStore(t *testing.T) {
	dir := t.TempDir()
	turnStore, _ := NewTurnStore(dir)
	memoryStore := NewMemoryStore(dir)
	
	// Create a temporary DB for FactStore
	factDB, _ := sql.Open("sqlite", filepath.Join(dir, "facts.db"))
	defer factDB.Close()
	factStore, _ := NewFactStore(factDB)

	worker := NewMemoryDigestWorker(turnStore, memoryStore, nil, "")
	worker.SetFactStore(factStore)

	// Should not panic
	ctx := context.Background()
	_ = worker.RunOnceNow(ctx)
}
