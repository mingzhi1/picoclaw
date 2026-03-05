package session

import (
	"database/sql"
	"path/filepath"
	"testing"

	_ "modernc.org/sqlite"
)

func testDB(t *testing.T) *sql.DB {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "test.db")
	db, err := sql.Open("sqlite", dbPath+"?_pragma=journal_mode(wal)&_pragma=busy_timeout(3000)")
	if err != nil {
		t.Fatalf("Failed to open test db: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func TestSave_WithColonInKey(t *testing.T) {
	db := testDB(t)
	sm := NewSessionManager(db)

	// Create a session with a key containing colon (typical channel session key).
	key := "telegram:123456"
	sm.GetOrCreate(key)
	sm.AddMessage(key, "user", "hello")

	// Save should succeed
	if err := sm.Save(key); err != nil {
		t.Fatalf("Save(%q) failed: %v", key, err)
	}

	// Load into a fresh manager and verify the session round-trips.
	sm2 := NewSessionManager(db)
	history := sm2.GetHistory(key)
	if len(history) != 1 {
		t.Fatalf("expected 1 message after reload, got %d", len(history))
	}
	if history[0].Content != "hello" {
		t.Errorf("expected message content %q, got %q", "hello", history[0].Content)
	}
}

func TestSave_MemoryOnly(t *testing.T) {
	sm := NewSessionManager(nil) // nil db = memory only
	key := "test"
	sm.GetOrCreate(key)
	sm.AddMessage(key, "user", "hello")

	if err := sm.Save(key); err != nil {
		t.Fatalf("Save with nil db should succeed, got: %v", err)
	}

	history := sm.GetHistory(key)
	if len(history) != 1 {
		t.Fatalf("expected 1 message, got %d", len(history))
	}
}

func TestSummaryRoundTrip(t *testing.T) {
	db := testDB(t)
	sm := NewSessionManager(db)

	key := "test:session"
	sm.GetOrCreate(key)
	sm.SetSummary(key, "This is a summary")
	sm.Save(key)

	sm2 := NewSessionManager(db)
	summary := sm2.GetSummary(key)
	if summary != "This is a summary" {
		t.Errorf("expected summary %q, got %q", "This is a summary", summary)
	}
}

func TestTruncateHistory(t *testing.T) {
	sm := NewSessionManager(nil)
	key := "test"
	sm.GetOrCreate(key)
	for i := range 10 {
		sm.AddMessage(key, "user", string(rune('a'+i)))
	}

	sm.TruncateHistory(key, 3)
	history := sm.GetHistory(key)
	if len(history) != 3 {
		t.Fatalf("expected 3 messages after truncate, got %d", len(history))
	}
}
