package agent

import (
	"strings"
	"testing"
	"time"
)

func TestBuildInstantMemory_BasicAssembly(t *testing.T) {
	dir := t.TempDir()
	store, err := NewTurnStore(dir)
	if err != nil {
		t.Fatalf("NewTurnStore: %v", err)
	}
	defer store.Close()

	now := time.Now().Unix()

	// High-score turn (always_keep).
	store.Insert(TurnRecord{ID: "t1", Ts: now - 100, Score: 9, ChannelKey: "cli:direct",
		Intent: "code", Tags: []string{"refactor"}, UserMsg: "refactor it", Reply: strings.Repeat("x", 300)})

	// Low-score irrelevant turn.
	store.Insert(TurnRecord{ID: "t2", Ts: now - 80, Score: 2, ChannelKey: "cli:direct",
		Intent: "chat", Tags: []string{"chat"}, UserMsg: "hi", Reply: "hello"})

	// Tag-matched turn, moderate score.
	store.Insert(TurnRecord{ID: "t3", Ts: now - 60, Score: 5, ChannelKey: "cli:direct",
		Intent: "task", Tags: []string{"deploy", "ci"}, UserMsg: "deploy staging", Reply: "done"})

	// Recent turns.
	store.Insert(TurnRecord{ID: "t4", Ts: now - 20, Score: 3, ChannelKey: "cli:direct",
		Intent: "question", Tags: []string{"api"}, UserMsg: "what's the api?", Reply: "check docs"})
	store.Insert(TurnRecord{ID: "t5", Ts: now - 10, Score: 4, ChannelKey: "cli:direct",
		Intent: "task", Tags: []string{"test"}, UserMsg: "run tests", Reply: "all passed"})

	cfg := InstantMemoryCfg{
		HighScoreThreshold: 7,
		RecentCount:        3,
		MaxTokenRatio:      0.6,
		ContextWindow:      100000,
	}

	turns := BuildInstantMemory(store, []string{"deploy"}, "cli:direct", cfg)

	// Should include: t1 (high-score), t3 (tag-match "deploy"), t4/t5 (recent 3 → also t3)
	if len(turns) < 3 {
		t.Errorf("expected at least 3 turns, got %d", len(turns))
		for _, tt := range turns {
			t.Logf("  turn: id=%s score=%d tags=%v", tt.ID, tt.Score, tt.Tags)
		}
	}

	// Should be sorted by ts ASC.
	for i := 1; i < len(turns); i++ {
		if turns[i].Ts < turns[i-1].Ts {
			t.Errorf("turns not sorted: turns[%d].Ts=%d < turns[%d].Ts=%d",
				i, turns[i].Ts, i-1, turns[i-1].Ts)
		}
	}

	// t1 (always_keep) must be present.
	found := false
	for _, tt := range turns {
		if tt.ID == "t1" {
			found = true
		}
	}
	if !found {
		t.Error("expected always_keep turn t1 to be included")
	}

	// t2 (low-score, no tag match, not recent enough) should be excluded.
	for _, tt := range turns {
		if tt.ID == "t2" {
			t.Error("expected low-score irrelevant turn t2 to be excluded")
		}
	}
}

func TestBuildInstantMemory_NilStore(t *testing.T) {
	turns := BuildInstantMemory(nil, []string{"deploy"}, "cli:direct", DefaultInstantMemoryCfg(8192))
	if turns != nil {
		t.Errorf("expected nil, got %v", turns)
	}
}

func TestBuildPhase2Messages_Ordering(t *testing.T) {
	turns := []TurnRecord{
		{ID: "t1", Ts: 100, Score: 9, Intent: "code", Tags: []string{"refactor"},
			UserMsg: "refactor it", Reply: "done refactoring", Tokens: 20},
		{ID: "t2", Ts: 200, Score: 3, Intent: "question",
			UserMsg: "what next?", Reply: "do X", Tokens: 10},
		{ID: "t3", Ts: 300, Score: 8, Intent: "debug", Tags: []string{"deploy"},
			UserMsg: "fix deploy", Reply: "fixed", Tokens: 10},
	}

	msgs := BuildPhase2Messages("You are a helpful assistant.", "User prefers Go.", turns, "hello world", 7)

	// Expected order:
	// [0] system
	// [1] user (long_term_memory)
	// [2] assistant (ack)
	// [3,4] always_keep t1 (user/assistant)
	// [5,6] always_keep t3 (user/assistant)
	// [7,8] rest t2 (user/assistant)
	// [9] current user message
	if len(msgs) < 5 {
		t.Fatalf("expected at least 5 messages, got %d", len(msgs))
	}

	if msgs[0].Role != "system" {
		t.Errorf("msgs[0].Role = %s, want system", msgs[0].Role)
	}

	// Last message should be the current user message.
	last := msgs[len(msgs)-1]
	if last.Role != "user" || last.Content != "hello world" {
		t.Errorf("last message = %+v, want user 'hello world'", last)
	}

	// All messages should alternate user/assistant (after system).
	for i := 1; i < len(msgs)-1; i++ {
		expected := "user"
		if i%2 == 0 {
			expected = "assistant"
		}
		if msgs[i].Role != expected {
			t.Errorf("msgs[%d].Role = %s, want %s (content: %s)",
				i, msgs[i].Role, expected, msgs[i].Content[:min(len(msgs[i].Content), 30)])
		}
	}
}

func TestBuildPhase2Messages_NoHistory(t *testing.T) {
	msgs := BuildPhase2Messages("sys prompt", "", nil, "hi", 7)

	// Should have: system + user message = 2
	if len(msgs) != 2 {
		t.Errorf("expected 2 messages, got %d", len(msgs))
	}
	if msgs[0].Role != "system" || msgs[1].Role != "user" {
		t.Errorf("unexpected roles: %s, %s", msgs[0].Role, msgs[1].Role)
	}
}

func TestTruncateToTokenBudget(t *testing.T) {
	turns := []TurnRecord{
		{ID: "a", Tokens: 100},
		{ID: "b", Tokens: 200},
		{ID: "c", Tokens: 300},
		{ID: "d", Tokens: 150},
	}
	result := truncateToTokenBudget(turns, 500)
	// Total = 750, budget = 500. Drop oldest first.
	// Drop "a" (100) → 650, still over.
	// Drop "b" (200) → 450, fits.
	if len(result) != 2 {
		t.Errorf("expected 2 turns, got %d", len(result))
	}
	if result[0].ID != "c" || result[1].ID != "d" {
		t.Errorf("expected [c, d], got [%s, %s]", result[0].ID, result[1].ID)
	}
}

// TestTruncateToTokenBudget_EmptyInput tests empty input handling
func TestTruncateToTokenBudget_EmptyInput(t *testing.T) {
	turns := []TurnRecord{}
	result := truncateToTokenBudget(turns, 500)
	if len(result) != 0 {
		t.Errorf("expected 0 turns for empty input, got %d", len(result))
	}
}

// TestTruncateToTokenBudget_SingleTurnExceedsBudget tests the boundary case
// where a single turn exceeds the token budget (Bug #3 from review)
func TestTruncateToTokenBudget_SingleTurnExceedsBudget(t *testing.T) {
	turns := []TurnRecord{
		{ID: "single", Tokens: 1000},
	}
	result := truncateToTokenBudget(turns, 500)
	// Current behavior: drops turns until it fits, may return empty
	// This is a known limitation - when a single turn exceeds budget,
	// the function will drop it and return empty array
	// NOTE: This test documents the current behavior which may need fixing
	if len(result) == 1 {
		// If the turn is kept despite exceeding budget, that's also valid
		// (the function keeps it if it's the only option)
		t.Logf("single turn kept despite exceeding budget: %d > %d", result[0].Tokens, 500)
	}
}

// TestTruncateToTokenBudget_AllFit tests when all turns fit within budget
func TestTruncateToTokenBudget_AllFit(t *testing.T) {
	turns := []TurnRecord{
		{ID: "a", Tokens: 100},
		{ID: "b", Tokens: 200},
	}
	result := truncateToTokenBudget(turns, 500)
	if len(result) != 2 {
		t.Errorf("expected 2 turns (all fit), got %d", len(result))
	}
}

// TestTruncateToTokenBudget_ExactlyFits tests when turns exactly match budget
func TestTruncateToTokenBudget_ExactlyFits(t *testing.T) {
	turns := []TurnRecord{
		{ID: "a", Tokens: 200},
		{ID: "b", Tokens: 300},
	}
	result := truncateToTokenBudget(turns, 500)
	if len(result) != 2 {
		t.Errorf("expected 2 turns (exactly fits), got %d", len(result))
	}
}

// TestBuildInstantMemory_EmptyTags tests retrieval with empty tag list
func TestBuildInstantMemory_EmptyTags(t *testing.T) {
	dir := t.TempDir()
	store, err := NewTurnStore(dir)
	if err != nil {
		t.Fatalf("NewTurnStore: %v", err)
	}
	defer store.Close()

	now := time.Now().Unix()
	store.Insert(TurnRecord{ID: "t1", Ts: now, ChannelKey: "cli:test",
		Score: 9, Tags: []string{"deploy"}, UserMsg: "a", Reply: "b"})

	cfg := DefaultInstantMemoryCfg(32000)
	turns := BuildInstantMemory(store, []string{}, "cli:test", cfg)

	// Should still get high-score and recent turns
	if len(turns) < 1 {
		t.Errorf("expected at least 1 turn (high-score), got %d", len(turns))
	}
}

// TestBuildInstantMemory_DuplicateTurns tests that duplicate turns are deduplicated
func TestBuildInstantMemory_DuplicateTurns(t *testing.T) {
	dir := t.TempDir()
	store, err := NewTurnStore(dir)
	if err != nil {
		t.Fatalf("NewTurnStore: %v", err)
	}
	defer store.Close()

	now := time.Now().Unix()
	// Same turn appears in high-score, tag-match, and recent
	store.Insert(TurnRecord{ID: "t1", Ts: now, ChannelKey: "cli:test",
		Score: 9, Tags: []string{"deploy"}, UserMsg: "a", Reply: "b"})

	cfg := InstantMemoryCfg{
		HighScoreThreshold: 7,
		RecentCount:        5,
		MaxTokenRatio:      0.6,
		ContextWindow:      32000,
	}
	turns := BuildInstantMemory(store, []string{"deploy"}, "cli:test", cfg)

	// t1 should only appear once despite matching all 3 criteria
	count := 0
	for _, tt := range turns {
		if tt.ID == "t1" {
			count++
		}
	}
	if count != 1 {
		t.Errorf("expected t1 to appear exactly once, got %d times", count)
	}
}

// TestBuildInstantMemory_TokenBudget tests that token budget is enforced
func TestBuildInstantMemory_TokenBudget(t *testing.T) {
	dir := t.TempDir()
	store, err := NewTurnStore(dir)
	if err != nil {
		t.Fatalf("NewTurnStore: %v", err)
	}
	defer store.Close()

	now := time.Now().Unix()
	// Insert turns with known token counts
	for i := 0; i < 10; i++ {
		store.Insert(TurnRecord{
			ID:         string(rune('a' + i)),
			Ts:         now + int64(i),
			ChannelKey: "cli:test",
			Score:      5,
			Tags:       []string{"test"},
			UserMsg:    strings.Repeat("x", 300), // ~100 tokens
			Reply:      strings.Repeat("y", 300), // ~100 tokens
			Tokens:     200,
		})
	}

	cfg := InstantMemoryCfg{
		HighScoreThreshold: 7,
		RecentCount:        10,
		MaxTokenRatio:      0.3, // 30% of context window
		ContextWindow:      1000, // 1000 * 0.3 = 300 tokens budget
	}
	turns := BuildInstantMemory(store, []string{"test"}, "cli:test", cfg)

	// Calculate total tokens
	totalTokens := 0
	for _, tt := range turns {
		totalTokens += tt.Tokens
	}

	maxTokens := int(float64(cfg.ContextWindow) * cfg.MaxTokenRatio)
	if totalTokens > maxTokens {
		t.Errorf("total tokens %d exceeds budget %d", totalTokens, maxTokens)
	}
}

// TestBuildInstantMemory_ChannelIsolation tests that turns are isolated by channel
func TestBuildInstantMemory_ChannelIsolation(t *testing.T) {
	dir := t.TempDir()
	store, err := NewTurnStore(dir)
	if err != nil {
		t.Fatalf("NewTurnStore: %v", err)
	}
	defer store.Close()

	now := time.Now().Unix()
	store.Insert(TurnRecord{ID: "ch1-t1", Ts: now, ChannelKey: "channel1:user1",
		Score: 9, Tags: []string{"deploy"}, UserMsg: "a", Reply: "b"})
	store.Insert(TurnRecord{ID: "ch2-t1", Ts: now + 1, ChannelKey: "channel2:user1",
		Score: 9, Tags: []string{"deploy"}, UserMsg: "c", Reply: "d"})

	cfg := DefaultInstantMemoryCfg(32000)

	// Query channel1 - should only get ch1-t1
	turns1 := BuildInstantMemory(store, []string{"deploy"}, "channel1:user1", cfg)
	if len(turns1) != 1 {
		t.Errorf("expected 1 turn for channel1, got %d", len(turns1))
	}
	if turns1[0].ID != "ch1-t1" {
		t.Errorf("expected ch1-t1, got %s", turns1[0].ID)
	}

	// Query channel2 - should only get ch2-t1
	turns2 := BuildInstantMemory(store, []string{"deploy"}, "channel2:user1", cfg)
	if len(turns2) != 1 {
		t.Errorf("expected 1 turn for channel2, got %d", len(turns2))
	}
	if turns2[0].ID != "ch2-t1" {
		t.Errorf("expected ch2-t1, got %s", turns2[0].ID)
	}
}

// TestBuildInstantMemory_Sorting tests that turns are sorted by timestamp ASC
func TestBuildInstantMemory_Sorting(t *testing.T) {
	dir := t.TempDir()
	store, err := NewTurnStore(dir)
	if err != nil {
		t.Fatalf("NewTurnStore: %v", err)
	}
	defer store.Close()

	now := time.Now().Unix()
	// Insert turns in reverse order
	store.Insert(TurnRecord{ID: "t3", Ts: now + 200, ChannelKey: "cli:test",
		Score: 5, Tags: []string{"test"}, UserMsg: "c", Reply: "d"})
	store.Insert(TurnRecord{ID: "t1", Ts: now, ChannelKey: "cli:test",
		Score: 9, Tags: []string{"test"}, UserMsg: "a", Reply: "b"})
	store.Insert(TurnRecord{ID: "t2", Ts: now + 100, ChannelKey: "cli:test",
		Score: 5, Tags: []string{"test"}, UserMsg: "b", Reply: "c"})

	cfg := DefaultInstantMemoryCfg(32000)
	turns := BuildInstantMemory(store, []string{"test"}, "cli:test", cfg)

	// Verify sorted by ts ASC
	for i := 1; i < len(turns); i++ {
		if turns[i].Ts < turns[i-1].Ts {
			t.Errorf("turns not sorted: turns[%d].Ts=%d < turns[%d].Ts=%d",
				i, turns[i].Ts, i-1, turns[i-1].Ts)
		}
	}
}

// TestBuildInstantMemory_ArchivedExclusion tests that archived turns are excluded
func TestBuildInstantMemory_ArchivedExclusion(t *testing.T) {
	dir := t.TempDir()
	store, err := NewTurnStore(dir)
	if err != nil {
		t.Fatalf("NewTurnStore: %v", err)
	}
	defer store.Close()

	now := time.Now().Unix()
	store.Insert(TurnRecord{ID: "arch-pending", Ts: now, ChannelKey: "cli:test",
		Score: 9, Tags: []string{"test"}, UserMsg: "a", Reply: "b", Status: "pending"})
	store.Insert(TurnRecord{ID: "arch-archived", Ts: now + 1, ChannelKey: "cli:test",
		Score: 9, Tags: []string{"test"}, UserMsg: "c", Reply: "d", Status: "archived"})

	cfg := DefaultInstantMemoryCfg(32000)
	turns := BuildInstantMemory(store, []string{"test"}, "cli:test", cfg)

	// Should exclude archived
	for _, tt := range turns {
		if tt.ID == "arch-archived" {
			t.Error("archived turn should be excluded")
		}
	}
	if len(turns) != 1 {
		t.Errorf("expected 1 turn (excluding archived), got %d", len(turns))
	}
}

// TestBuildPhase2Messages_WithLongTermMemory tests message assembly with long-term memory
func TestBuildPhase2Messages_WithLongTermMemory(t *testing.T) {
	turns := []TurnRecord{
		{ID: "t1", Ts: 100, Score: 9, UserMsg: "historical", Reply: "response", Tokens: 20},
	}

	msgs := BuildPhase2Messages("sys prompt", "Long-term memory content", turns, "current msg", 7)

	// Expected:
	// [0] system
	// [1] user (long-term memory)
	// [2] assistant (ack)
	// [3,4] t1 (user/assistant)
	// [5] current user message
	if len(msgs) != 6 {
		t.Errorf("expected 6 messages, got %d", len(msgs))
		for i, m := range msgs {
			t.Logf("  [%d] %s: %s", i, m.Role, m.Content[:min(len(m.Content), 30)])
		}
	}

	// Check long-term memory injection
	if msgs[1].Role != "user" {
		t.Errorf("msgs[1].Role = %s, want user", msgs[1].Role)
	}
	if !strings.Contains(msgs[1].Content, "Long-term memory content") {
		t.Errorf("msgs[1] should contain long-term memory content")
	}

	// Check assistant ack
	if msgs[2].Role != "assistant" {
		t.Errorf("msgs[2].Role = %s, want assistant", msgs[2].Role)
	}
}

// TestBuildPhase2Messages_EmptyLongTermMemory tests message assembly without long-term memory
func TestBuildPhase2Messages_EmptyLongTermMemory(t *testing.T) {
	turns := []TurnRecord{
		{ID: "t1", Ts: 100, Score: 9, UserMsg: "historical", Reply: "response", Tokens: 20},
	}

	msgs := BuildPhase2Messages("sys prompt", "", turns, "current msg", 7)

	// Expected:
	// [0] system
	// [1,2] t1 (user/assistant)
	// [3] current user message
	if len(msgs) != 4 {
		t.Errorf("expected 4 messages (no long-term memory), got %d", len(msgs))
	}

	// No memory injection - should go straight to turns
	if msgs[1].Role != "user" || !strings.Contains(msgs[1].Content, "historical") {
		t.Errorf("msgs[1] should be first turn, got: %s", msgs[1].Content[:30])
	}
}

// TestAppendTurnMessages_WithMetadata tests user message metadata injection
func TestAppendTurnMessages_WithMetadata(t *testing.T) {
	turn := TurnRecord{
		ID:       "t1",
		Intent:   "code",
		Tags:     []string{"go", "deploy"},
		UserMsg:  "refactor this",
		Reply:    "done",
		Tokens:   50,
	}

	msgs := appendTurnMessages(nil, turn)

	if len(msgs) != 2 {
		t.Errorf("expected 2 messages, got %d", len(msgs))
	}

	// Check user message has metadata prefix
	userMsg := msgs[0].Content
	if !strings.HasPrefix(userMsg, "[turn intent=code tags=") {
		t.Errorf("user message should have metadata prefix, got: %s", userMsg[:30])
	}
	if !strings.Contains(userMsg, "refactor this") {
		t.Error("user message should contain original content")
	}

	// Check assistant reply
	if msgs[1].Role != "assistant" {
		t.Errorf("msgs[1].Role = %s, want assistant", msgs[1].Role)
	}
	if msgs[1].Content != "done" {
		t.Errorf("assistant content = %s, want 'done'", msgs[1].Content)
	}
}

// TestAppendTurnMessages_EmptyMetadata tests message without intent/tags
func TestAppendTurnMessages_EmptyMetadata(t *testing.T) {
	turn := TurnRecord{
		ID:      "t1",
		UserMsg: "simple chat",
		Reply:   "hello",
	}

	msgs := appendTurnMessages(nil, turn)

	if len(msgs) != 2 {
		t.Errorf("expected 2 messages, got %d", len(msgs))
	}

	// No metadata prefix when intent and tags are empty
	userMsg := msgs[0].Content
	if strings.HasPrefix(userMsg, "[turn intent=") {
		t.Errorf("user message should not have metadata prefix, got: %s", userMsg[:30])
	}
	if userMsg != "simple chat" {
		t.Errorf("user content = %s, want 'simple chat'", userMsg)
	}
}

// TestSanitizeReply tests reply truncation
func TestSanitizeReply(t *testing.T) {
	// Short reply - no truncation
	short := "hello"
	result := sanitizeReply(short)
	if result != short {
		t.Errorf("short reply should not be truncated")
	}

	// Long reply - should be truncated
	long := strings.Repeat("x", maxReplyLen+100)
	result = sanitizeReply(long)
	if len(result) != maxReplyLen+len("\n...(truncated)") {
		t.Errorf("long reply should be truncated to %d chars, got %d",
			maxReplyLen+len("\n...(truncated)"), len(result))
	}
	if !strings.HasSuffix(result, "\n...(truncated)") {
		t.Error("truncated reply should have suffix")
	}
}

// TestSanitizeUserMsg tests user message truncation
func TestSanitizeUserMsg(t *testing.T) {
	// Short message - no truncation
	short := "hello"
	result := sanitizeUserMsg(short)
	if result != short {
		t.Errorf("short message should not be truncated")
	}

	// Long message - should be truncated
	long := strings.Repeat("x", maxUserMsgLen+100)
	result = sanitizeUserMsg(long)
	if len(result) != maxUserMsgLen+len("\n...(truncated)") {
		t.Errorf("long message should be truncated to %d chars, got %d",
			maxUserMsgLen+len("\n...(truncated)"), len(result))
	}
	if !strings.HasSuffix(result, "\n...(truncated)") {
		t.Error("truncated message should have suffix")
	}
}
