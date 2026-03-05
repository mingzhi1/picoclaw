package state

import (
	"database/sql"
	"fmt"
	"log"
	"sync"
	"time"
)

// State represents the persistent state for a workspace.
// It includes information about the last active channel/chat.
type State struct {
	// LastChannel is the last channel used for communication
	LastChannel string `json:"last_channel,omitempty"`

	// LastChatID is the last chat ID used for communication
	LastChatID string `json:"last_chat_id,omitempty"`

	// Timestamp is the last time this state was updated
	Timestamp time.Time `json:"timestamp"`
}

const stateDDL = `
CREATE TABLE IF NOT EXISTS state (
    key   TEXT PRIMARY KEY,
    value TEXT NOT NULL DEFAULT ''
);
`

// Manager manages persistent state with SQLite backend.
type Manager struct {
	state *State
	mu    sync.RWMutex
	db    *sql.DB
}

// NewManager creates a new state manager backed by a shared SQLite DB.
// Pass the *sql.DB obtained from store.Open.
func NewManager(db *sql.DB) *Manager {
	sm := &Manager{
		state: &State{},
		db:    db,
	}

	if db != nil {
		if _, err := db.Exec(stateDDL); err != nil {
			log.Printf("[WARN] state: failed to create state table: %v", err)
		}
		if err := sm.load(); err != nil {
			log.Printf("[WARN] state: failed to load state: %v", err)
		}
	}

	return sm
}

// SetLastChannel atomically updates the last channel and saves the state.
func (sm *Manager) SetLastChannel(channel string) error {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	sm.state.LastChannel = channel
	sm.state.Timestamp = time.Now()

	if err := sm.saveKey("last_channel", channel); err != nil {
		return fmt.Errorf("failed to save state: %w", err)
	}
	return sm.saveKey("timestamp", fmt.Sprintf("%d", sm.state.Timestamp.Unix()))
}

// SetLastChatID atomically updates the last chat ID and saves the state.
func (sm *Manager) SetLastChatID(chatID string) error {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	sm.state.LastChatID = chatID
	sm.state.Timestamp = time.Now()

	if err := sm.saveKey("last_chat_id", chatID); err != nil {
		return fmt.Errorf("failed to save state: %w", err)
	}
	return sm.saveKey("timestamp", fmt.Sprintf("%d", sm.state.Timestamp.Unix()))
}

// GetLastChannel returns the last channel from the state.
func (sm *Manager) GetLastChannel() string {
	sm.mu.RLock()
	defer sm.mu.RUnlock()
	return sm.state.LastChannel
}

// GetLastChatID returns the last chat ID from the state.
func (sm *Manager) GetLastChatID() string {
	sm.mu.RLock()
	defer sm.mu.RUnlock()
	return sm.state.LastChatID
}

// GetTimestamp returns the timestamp of the last state update.
func (sm *Manager) GetTimestamp() time.Time {
	sm.mu.RLock()
	defer sm.mu.RUnlock()
	return sm.state.Timestamp
}

// saveKey persists a single key-value pair to SQLite.
func (sm *Manager) saveKey(key, value string) error {
	if sm.db == nil {
		return nil
	}
	_, err := sm.db.Exec(`
		INSERT INTO state (key, value) VALUES (?, ?)
		ON CONFLICT(key) DO UPDATE SET value = excluded.value`,
		key, value)
	return err
}

// load reads state from SQLite.
func (sm *Manager) load() error {
	if sm.db == nil {
		return nil
	}

	rows, err := sm.db.Query(`SELECT key, value FROM state`)
	if err != nil {
		return fmt.Errorf("failed to query state: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var key, value string
		if err := rows.Scan(&key, &value); err != nil {
			continue
		}
		switch key {
		case "last_channel":
			sm.state.LastChannel = value
		case "last_chat_id":
			sm.state.LastChatID = value
		case "timestamp":
			var ts int64
			fmt.Sscanf(value, "%d", &ts)
			if ts > 0 {
				sm.state.Timestamp = time.Unix(ts, 0)
			}
		}
	}
	return rows.Err()
}
