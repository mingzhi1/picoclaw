// PicoClaw - Ultra-lightweight personal AI agent
// License: MIT

package topic

import (
	"path/filepath"
	"testing"
)

func newTestTracker(t *testing.T) (*Tracker, *Store) {
	t.Helper()
	dir := t.TempDir()
	s, err := NewStore(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	tr, err := NewTracker(s)
	if err != nil {
		t.Fatalf("NewTracker: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return tr, s
}

// --- Apply: ActionNew ---

func TestTracker_Apply_NewTopic(t *testing.T) {
	tr, _ := newTestTracker(t)
	tp, err := tr.Apply(Action{Type: ActionNew, Title: "Go性能"})
	if err != nil {
		t.Fatalf("Apply new: %v", err)
	}
	if tp.Title != "Go性能" {
		t.Errorf("expected title 'Go性能', got '%s'", tp.Title)
	}
	if tr.CurrentID() != tp.ID {
		t.Error("current should point to new topic")
	}
}

func TestTracker_Apply_NewTopic_EmptyTitle(t *testing.T) {
	tr, _ := newTestTracker(t)
	tp, err := tr.Apply(Action{Type: ActionNew, Title: ""})
	if err != nil {
		t.Fatalf("Apply new empty: %v", err)
	}
	if tp.Title != "未命名话题" {
		t.Errorf("expected fallback title, got '%s'", tp.Title)
	}
}

// --- Apply: ActionContinue ---

func TestTracker_Apply_Continue(t *testing.T) {
	tr, _ := newTestTracker(t)
	tp1, _ := tr.Apply(Action{Type: ActionNew, Title: "topic A"})
	_, _ = tr.Apply(Action{Type: ActionNew, Title: "topic B"})

	// Continue back to A
	tp, err := tr.Apply(Action{
		Type: ActionContinue, Primary: tp1.ID,
	})
	if err != nil {
		t.Fatalf("Apply continue: %v", err)
	}
	if tp.ID != tp1.ID {
		t.Error("expected to resume topic A")
	}
	if tr.CurrentID() != tp1.ID {
		t.Error("current should point to topic A")
	}
}

func TestTracker_Apply_Continue_InvalidID(t *testing.T) {
	tr, _ := newTestTracker(t)
	// Continue with invalid ID → should degrade to new
	tp, err := tr.Apply(Action{
		Type: ActionContinue, Primary: "nonexistent",
	})
	if err != nil {
		t.Fatalf("Apply continue invalid: %v", err)
	}
	if tp == nil {
		t.Fatal("expected fallback topic, got nil")
	}
	if tp.Title != "未命名话题" {
		t.Errorf("expected fallback title, got '%s'", tp.Title)
	}
}

// --- Apply: ActionMulti with Resolve ---

func TestTracker_Apply_Multi_WithResolve(t *testing.T) {
	tr, store := newTestTracker(t)
	tpA, _ := tr.Apply(Action{Type: ActionNew, Title: "topic A"})
	tpB, _ := tr.Apply(Action{Type: ActionNew, Title: "topic B"})

	// Multi: resolve A, continue B
	tp, err := tr.Apply(Action{
		Type:    ActionMulti,
		Primary: tpB.ID,
		Resolve: []string{tpA.ID},
	})
	if err != nil {
		t.Fatalf("Apply multi: %v", err)
	}
	if tp.ID != tpB.ID {
		t.Error("expected primary=B")
	}
	// Check A is resolved
	gotA, _ := store.Get(tpA.ID)
	if gotA.Status != StatusResolved {
		t.Errorf("topic A should be resolved, got %s", gotA.Status)
	}
}

// --- RecordTurnTokens ---

func TestTracker_RecordTurnTokens(t *testing.T) {
	tr, store := newTestTracker(t)
	tp, _ := tr.Apply(Action{Type: ActionNew, Title: "test"})
	_ = tr.RecordTurnTokens(500)
	_ = tr.RecordTurnTokens(300)
	got, _ := store.Get(tp.ID)
	if got.TotalTokens != 800 {
		t.Errorf("expected 800 tokens, got %d", got.TotalTokens)
	}
}

// --- CheckCompact ---

func TestTracker_CheckCompact_NotNeeded(t *testing.T) {
	tr, _ := newTestTracker(t)
	tr.Apply(Action{Type: ActionNew, Title: "test"})
	if tr.CheckCompact(100000) {
		t.Error("expected no compact needed for fresh topic")
	}
}

func TestTracker_CheckCompact_NeededByTokens(t *testing.T) {
	tr, _ := newTestTracker(t)
	tr.Apply(Action{Type: ActionNew, Title: "test"})
	// Simulate 5000 tokens
	for i := 0; i < 50; i++ {
		_ = tr.RecordTurnTokens(100)
	}
	// 5000 > 40% of 10000
	if !tr.CheckCompact(10000) {
		t.Error("expected compact needed when tokens > 40% context")
	}
}

// --- FormatForAnalyser ---

func TestTracker_FormatForAnalyser_Empty(t *testing.T) {
	tr, _ := newTestTracker(t)
	out := tr.FormatForAnalyser()
	if out != "" {
		t.Error("expected empty format when no topics")
	}
}

func TestTracker_FormatForAnalyser_WithTopics(t *testing.T) {
	tr, _ := newTestTracker(t)
	tr.Apply(Action{Type: ActionNew, Title: "Go性能"})
	tr.Apply(Action{Type: ActionNew, Title: "API集成"})
	out := tr.FormatForAnalyser()
	if out == "" {
		t.Error("expected non-empty format")
	}
	// Should contain both titles
	if !contains(out, "Go性能") || !contains(out, "API集成") {
		t.Errorf("format missing topic titles: %s", out)
	}
}

// --- Crash Recovery ---

func TestTracker_CrashRecovery(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")

	// Session 1: create a topic
	s1, _ := NewStore(dbPath)
	tr1, _ := NewTracker(s1)
	tp, _ := tr1.Apply(Action{Type: ActionNew, Title: "before crash"})
	s1.Close()

	// Session 2: reopen → should restore
	s2, _ := NewStore(dbPath)
	defer s2.Close()
	tr2, _ := NewTracker(s2)
	if tr2.CurrentID() != tp.ID {
		t.Errorf("expected restored ID %s, got %s", tp.ID, tr2.CurrentID())
	}
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && searchSubstr(s, sub)
}

func searchSubstr(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
