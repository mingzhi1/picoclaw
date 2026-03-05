package state

import (
	"database/sql"
	"fmt"
	"path/filepath"
	"sync"
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

func TestSetLastChannel(t *testing.T) {
	db := testDB(t)
	sm := NewManager(db)

	err := sm.SetLastChannel("test-channel")
	if err != nil {
		t.Fatalf("SetLastChannel failed: %v", err)
	}

	lastChannel := sm.GetLastChannel()
	if lastChannel != "test-channel" {
		t.Errorf("Expected channel 'test-channel', got '%s'", lastChannel)
	}

	if sm.GetTimestamp().IsZero() {
		t.Error("Expected timestamp to be updated")
	}

	// Create a new manager to verify persistence
	sm2 := NewManager(db)
	if sm2.GetLastChannel() != "test-channel" {
		t.Errorf("Expected persistent channel 'test-channel', got '%s'", sm2.GetLastChannel())
	}
}

func TestSetLastChatID(t *testing.T) {
	db := testDB(t)
	sm := NewManager(db)

	err := sm.SetLastChatID("test-chat-id")
	if err != nil {
		t.Fatalf("SetLastChatID failed: %v", err)
	}

	lastChatID := sm.GetLastChatID()
	if lastChatID != "test-chat-id" {
		t.Errorf("Expected chat ID 'test-chat-id', got '%s'", lastChatID)
	}

	if sm.GetTimestamp().IsZero() {
		t.Error("Expected timestamp to be updated")
	}

	sm2 := NewManager(db)
	if sm2.GetLastChatID() != "test-chat-id" {
		t.Errorf("Expected persistent chat ID 'test-chat-id', got '%s'", sm2.GetLastChatID())
	}
}

func TestConcurrentAccess(t *testing.T) {
	db := testDB(t)
	sm := NewManager(db)

	done := make(chan bool, 10)
	for i := range 10 {
		go func(idx int) {
			channel := fmt.Sprintf("channel-%d", idx)
			sm.SetLastChannel(channel)
			done <- true
		}(i)
	}

	for range 10 {
		<-done
	}

	lastChannel := sm.GetLastChannel()
	if lastChannel == "" {
		t.Error("Expected non-empty channel after concurrent writes")
	}
}

func TestNewManager_ExistingState(t *testing.T) {
	db := testDB(t)

	sm1 := NewManager(db)
	sm1.SetLastChannel("existing-channel")
	sm1.SetLastChatID("existing-chat-id")

	sm2 := NewManager(db)

	if sm2.GetLastChannel() != "existing-channel" {
		t.Errorf("Expected channel 'existing-channel', got '%s'", sm2.GetLastChannel())
	}

	if sm2.GetLastChatID() != "existing-chat-id" {
		t.Errorf("Expected chat ID 'existing-chat-id', got '%s'", sm2.GetLastChatID())
	}
}

func TestNewManager_EmptyDB(t *testing.T) {
	db := testDB(t)
	sm := NewManager(db)

	if sm.GetLastChannel() != "" {
		t.Errorf("Expected empty channel, got '%s'", sm.GetLastChannel())
	}

	if sm.GetLastChatID() != "" {
		t.Errorf("Expected empty chat ID, got '%s'", sm.GetLastChatID())
	}

	if !sm.GetTimestamp().IsZero() {
		t.Error("Expected zero timestamp for new state")
	}
}

func TestNewManager_NilDB(t *testing.T) {
	sm := NewManager(nil)

	// Should not panic
	if sm.GetLastChannel() != "" {
		t.Errorf("Expected empty channel with nil db")
	}

	// SetLastChannel should not error (noop)
	if err := sm.SetLastChannel("test"); err != nil {
		t.Errorf("Expected no error with nil db, got %v", err)
	}
}

// Suppress unused warning for sync.
var _ = sync.Mutex{}
