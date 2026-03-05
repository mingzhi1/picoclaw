package agent

import (
	"database/sql"
	"path/filepath"
	"testing"

	_ "modernc.org/sqlite"
)

func testActiveCtxDB(t *testing.T) *sql.DB {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "test.db")
	db, err := sql.Open("sqlite", dbPath+"?_pragma=journal_mode(wal)&_pragma=busy_timeout(3000)")
	if err != nil {
		t.Fatalf("Failed to open test db: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func TestActiveContextStore_UpdateAndGet(t *testing.T) {
	s := NewActiveContextStore(nil)
	key := "telegram:12345"

	// Initially empty.
	ac := s.Get(key)
	if len(ac.CurrentFiles) != 0 || len(ac.RecentErrors) != 0 {
		t.Errorf("expected empty context, got %+v", ac)
	}

	// Add errors via Update.
	s.Update(key, RuntimeInput{
		ToolCalls: []ToolCallRecord{
			{Name: "exec", Error: "timeout after 30s"},
			{Name: "read_file", Error: ""},
		},
	})
	ac = s.Get(key)
	if len(ac.RecentErrors) != 1 {
		t.Errorf("expected 1 error, got %d: %v", len(ac.RecentErrors), ac.RecentErrors)
	}
	if ac.RecentErrors[0] != "[exec] timeout after 30s" {
		t.Errorf("unexpected error: %s", ac.RecentErrors[0])
	}
}

func TestActiveContextStore_FileCapping(t *testing.T) {
	s := NewActiveContextStore(nil)
	key := "cli:direct"

	// Add 7 file paths — should cap at 5, newest first.
	s.UpdateWithFiles(key, RuntimeInput{}, []string{"a.go", "b.go", "c.go", "d.go", "e.go", "f.go", "g.go"})
	ac := s.Get(key)
	if len(ac.CurrentFiles) != 5 {
		t.Fatalf("expected 5 files, got %d: %v", len(ac.CurrentFiles), ac.CurrentFiles)
	}
	// Last added (g.go) is prepended, so it should be first.
	if ac.CurrentFiles[0] != "g.go" {
		t.Errorf("expected g.go first, got %s (all: %v)", ac.CurrentFiles[0], ac.CurrentFiles)
	}
}

func TestActiveContextStore_ErrorCapping(t *testing.T) {
	s := NewActiveContextStore(nil)
	key := "wecom:alice"

	for i := 0; i < 5; i++ {
		s.Update(key, RuntimeInput{
			ToolCalls: []ToolCallRecord{{Name: "exec", Error: "err"}},
		})
	}
	ac := s.Get(key)
	if len(ac.RecentErrors) > 3 {
		t.Errorf("expected max 3 errors, got %d", len(ac.RecentErrors))
	}
}

func TestActiveContextStore_SQLitePersistence(t *testing.T) {
	db := testActiveCtxDB(t)

	s := NewActiveContextStore(db)
	key := "cli:direct"
	s.UpdateWithFiles(key, RuntimeInput{}, []string{"main.go"})
	s.Update(key, RuntimeInput{
		ToolCalls: []ToolCallRecord{{Name: "exec", Error: "failed"}},
	})

	// Load into new store from same DB to verify persistence.
	s2 := NewActiveContextStore(db)
	ac := s2.Get(key)
	if len(ac.CurrentFiles) != 1 || ac.CurrentFiles[0] != "main.go" {
		t.Errorf("unexpected files after reload: %v", ac.CurrentFiles)
	}
	if len(ac.RecentErrors) != 1 {
		t.Errorf("unexpected errors after reload: %v", ac.RecentErrors)
	}
}

func TestActiveContextStore_NilDB(t *testing.T) {
	s := NewActiveContextStore(nil)

	// Should not panic
	s.Update("test", RuntimeInput{
		ToolCalls: []ToolCallRecord{{Name: "exec", Error: "err"}},
	})
	ac := s.Get("test")
	if len(ac.RecentErrors) != 1 {
		t.Errorf("expected 1 error in memory-only mode, got %d", len(ac.RecentErrors))
	}
}

func TestActiveContext_Format(t *testing.T) {
	ac := &ActiveContext{
		CurrentFiles: []string{"main.go", "loop.go"},
		RecentErrors: []string{"[exec] timeout"},
	}
	formatted := ac.Format()
	if formatted == "" {
		t.Error("expected non-empty format")
	}
	if len(formatted) == 0 {
		t.Error("Format returned empty string")
	}
}
