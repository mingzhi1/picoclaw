// PicoClaw - Ultra-lightweight personal AI agent
// License: MIT

package topic

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func newTestStore(t *testing.T) *Store {
	t.Helper()
	dir := t.TempDir()
	s, err := NewStore(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

// --- Create ---

func TestStore_Create(t *testing.T) {
	s := newTestStore(t)
	tp, err := s.Create("Go性能优化")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if tp.ID == "" {
		t.Error("expected non-empty ID")
	}
	if tp.Status != StatusActive {
		t.Errorf("expected status=active, got %s", tp.Status)
	}
	if tp.Title != "Go性能优化" {
		t.Errorf("unexpected title: %s", tp.Title)
	}
}

// --- Get ---

func TestStore_GetExisting(t *testing.T) {
	s := newTestStore(t)
	created, _ := s.Create("test topic")
	got, err := s.Get(created.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.ID != created.ID {
		t.Errorf("ID mismatch: %s vs %s", got.ID, created.ID)
	}
}

func TestStore_GetMissing(t *testing.T) {
	s := newTestStore(t)
	got, err := s.Get("nonexistent")
	if err == nil && got != nil {
		t.Error("expected nil for missing topic")
	}
}

// --- Activate (single-active invariant) ---

func TestStore_Activate_SingleActiveInvariant(t *testing.T) {
	s := newTestStore(t)
	tp1, _ := s.Create("topic 1") // becomes active
	tp2, _ := s.Create("topic 2") // becomes active, tp1 stays active too (haven't switched yet)

	// Activate tp1
	if err := s.Activate(tp1.ID); err != nil {
		t.Fatalf("Activate: %v", err)
	}

	got1, _ := s.Get(tp1.ID)
	got2, _ := s.Get(tp2.ID)

	if got1.Status != StatusActive {
		t.Errorf("tp1 should be active, got %s", got1.Status)
	}
	if got2.Status != StatusIdle {
		t.Errorf("tp2 should be idle, got %s", got2.Status)
	}
}

// --- SetStatus ---

func TestStore_SetStatus_Resolved(t *testing.T) {
	s := newTestStore(t)
	tp, _ := s.Create("to resolve")
	if err := s.SetStatus(tp.ID, StatusResolved); err != nil {
		t.Fatalf("SetStatus: %v", err)
	}
	got, _ := s.Get(tp.ID)
	if got.Status != StatusResolved {
		t.Errorf("expected resolved, got %s", got.Status)
	}
}

// --- SetSummary ---

func TestStore_SetSummary(t *testing.T) {
	s := newTestStore(t)
	tp, _ := s.Create("with summary")
	if err := s.SetSummary(tp.ID, "用户在优化 parseConfig，从 850ns 到 420ns"); err != nil {
		t.Fatalf("SetSummary: %v", err)
	}
	got, _ := s.Get(tp.ID)
	if got.Summary == "" {
		t.Error("expected non-empty summary")
	}
}

// --- AddTokens ---

func TestStore_AddTokens_Accumulates(t *testing.T) {
	s := newTestStore(t)
	tp, _ := s.Create("token test")
	_ = s.AddTokens(tp.ID, 100)
	_ = s.AddTokens(tp.ID, 200)
	got, _ := s.Get(tp.ID)
	if got.TotalTokens != 300 {
		t.Errorf("expected 300 tokens, got %d", got.TotalTokens)
	}
	if got.TurnCount != 2 {
		t.Errorf("expected 2 turns, got %d", got.TurnCount)
	}
}

// --- ActiveTopic ---

func TestStore_ActiveTopic_NoneWhenEmpty(t *testing.T) {
	s := newTestStore(t)
	tp, err := s.ActiveTopic()
	if err != nil {
		t.Fatalf("ActiveTopic: %v", err)
	}
	if tp != nil {
		t.Error("expected nil when no active topic")
	}
}

func TestStore_ActiveTopic_ReturnsActive(t *testing.T) {
	s := newTestStore(t)
	created, _ := s.Create("active one")
	got, err := s.ActiveTopic()
	if err != nil {
		t.Fatalf("ActiveTopic: %v", err)
	}
	if got == nil || got.ID != created.ID {
		t.Error("expected recently created active topic")
	}
}

// --- RecentTopics ---

func TestStore_RecentTopics_LimitRespected(t *testing.T) {
	s := newTestStore(t)
	for i := 0; i < 7; i++ {
		s.Create("topic")
	}
	topics, err := s.RecentTopics(5)
	if err != nil {
		t.Fatalf("RecentTopics: %v", err)
	}
	if len(topics) > 5 {
		t.Errorf("expected <= 5, got %d", len(topics))
	}
}

func TestStore_RecentTopics_ExcludesResolved(t *testing.T) {
	s := newTestStore(t)
	tp, _ := s.Create("resolved one")
	_ = s.SetStatus(tp.ID, StatusResolved)
	topics, _ := s.RecentTopics(10)
	for _, t2 := range topics {
		if t2.ID == tp.ID {
			t.Error("resolved topic should not appear in recent topics")
		}
	}
}

// --- StaleTopics ---

func TestStore_StaleTopics(t *testing.T) {
	if os.Getenv("CI") != "" {
		t.Skip("skipping time-sensitive test in CI")
	}
	s := newTestStore(t)
	tp, _ := s.Create("stale topic")
	_ = s.SetStatus(tp.ID, StatusIdle)
	// Manually backdate updated_at
	s.db.Exec(`UPDATE topics SET updated_at=? WHERE id=?`,
		time.Now().Add(-10*time.Minute).Unix(), tp.ID)

	stale, err := s.StaleTopics(5 * time.Minute)
	if err != nil {
		t.Fatalf("StaleTopics: %v", err)
	}
	found := false
	for _, st := range stale {
		if st.ID == tp.ID {
			found = true
		}
	}
	if !found {
		t.Error("expected topic to appear in stale list after idle threshold")
	}
}
