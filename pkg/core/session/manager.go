package session

import (
	"database/sql"
	"encoding/json"
	"sync"
	"time"

	"github.com/sipeed/picoclaw/pkg/llm/providers"
)

type Session struct {
	Key      string              `json:"key"`
	Messages []providers.Message `json:"messages"`
	Summary  string              `json:"summary,omitempty"`
	Created  time.Time           `json:"created"`
	Updated  time.Time           `json:"updated"`
}

type SessionManager struct {
	sessions map[string]*Session
	mu       sync.RWMutex
	db       *sql.DB
}

const sessionsDDL = `
CREATE TABLE IF NOT EXISTS sessions (
    key       TEXT    PRIMARY KEY,
    messages  TEXT    NOT NULL DEFAULT '[]',
    summary   TEXT    NOT NULL DEFAULT '',
    created   INTEGER NOT NULL,
    updated   INTEGER NOT NULL
);
`

// NewSessionManager creates a new session manager backed by a shared SQLite DB.
// Pass the *sql.DB obtained from store.Open.
// If db is nil, sessions are memory-only (for tests / CLI oneshot).
func NewSessionManager(db *sql.DB) *SessionManager {
	sm := &SessionManager{
		sessions: make(map[string]*Session),
		db:       db,
	}

	if db != nil {
		db.Exec(sessionsDDL)
		sm.loadSessions()
	}

	return sm
}

func (sm *SessionManager) GetOrCreate(key string) *Session {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	session, ok := sm.sessions[key]
	if ok {
		return session
	}

	session = &Session{
		Key:      key,
		Messages: []providers.Message{},
		Created:  time.Now(),
		Updated:  time.Now(),
	}
	sm.sessions[key] = session

	return session
}

func (sm *SessionManager) AddMessage(sessionKey, role, content string) {
	sm.AddFullMessage(sessionKey, providers.Message{
		Role:    role,
		Content: content,
	})
}

// AddFullMessage adds a complete message with tool calls and tool call ID to the session.
// This is used to save the full conversation flow including tool calls and tool results.
func (sm *SessionManager) AddFullMessage(sessionKey string, msg providers.Message) {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	session, ok := sm.sessions[sessionKey]
	if !ok {
		session = &Session{
			Key:      sessionKey,
			Messages: []providers.Message{},
			Created:  time.Now(),
		}
		sm.sessions[sessionKey] = session
	}

	session.Messages = append(session.Messages, msg)
	session.Updated = time.Now()
}

func (sm *SessionManager) GetHistory(key string) []providers.Message {
	sm.mu.RLock()
	defer sm.mu.RUnlock()

	session, ok := sm.sessions[key]
	if !ok {
		return []providers.Message{}
	}

	history := make([]providers.Message, len(session.Messages))
	copy(history, session.Messages)
	return history
}

func (sm *SessionManager) GetSummary(key string) string {
	sm.mu.RLock()
	defer sm.mu.RUnlock()

	session, ok := sm.sessions[key]
	if !ok {
		return ""
	}
	return session.Summary
}

func (sm *SessionManager) SetSummary(key string, summary string) {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	session, ok := sm.sessions[key]
	if ok {
		session.Summary = summary
		session.Updated = time.Now()
	}
}

func (sm *SessionManager) TruncateHistory(key string, keepLast int) {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	session, ok := sm.sessions[key]
	if !ok {
		return
	}

	if keepLast <= 0 {
		session.Messages = []providers.Message{}
		session.Updated = time.Now()
		return
	}

	if len(session.Messages) <= keepLast {
		return
	}

	session.Messages = session.Messages[len(session.Messages)-keepLast:]
	session.Updated = time.Now()
}

// Save persists a single session to SQLite.
func (sm *SessionManager) Save(key string) error {
	if sm.db == nil {
		return nil
	}

	// Snapshot under read lock, then perform slow DB I/O after unlock.
	sm.mu.RLock()
	stored, ok := sm.sessions[key]
	if !ok {
		sm.mu.RUnlock()
		return nil
	}

	snapshot := Session{
		Key:     stored.Key,
		Summary: stored.Summary,
		Created: stored.Created,
		Updated: stored.Updated,
	}
	if len(stored.Messages) > 0 {
		snapshot.Messages = make([]providers.Message, len(stored.Messages))
		copy(snapshot.Messages, stored.Messages)
	} else {
		snapshot.Messages = []providers.Message{}
	}
	sm.mu.RUnlock()

	msgsJSON, err := json.Marshal(snapshot.Messages)
	if err != nil {
		return err
	}

	_, err = sm.db.Exec(`
		INSERT INTO sessions (key, messages, summary, created, updated)
		VALUES (?, ?, ?, ?, ?)
		ON CONFLICT(key) DO UPDATE SET
			messages = excluded.messages,
			summary  = excluded.summary,
			updated  = excluded.updated`,
		snapshot.Key,
		string(msgsJSON),
		snapshot.Summary,
		snapshot.Created.Unix(),
		snapshot.Updated.Unix(),
	)
	return err
}

func (sm *SessionManager) loadSessions() {
	if sm.db == nil {
		return
	}

	rows, err := sm.db.Query(`SELECT key, messages, summary, created, updated FROM sessions`)
	if err != nil {
		return
	}
	defer rows.Close()

	for rows.Next() {
		var key, msgsJSON, summary string
		var createdUnix, updatedUnix int64
		if err := rows.Scan(&key, &msgsJSON, &summary, &createdUnix, &updatedUnix); err != nil {
			continue
		}

		var msgs []providers.Message
		if err := json.Unmarshal([]byte(msgsJSON), &msgs); err != nil {
			msgs = []providers.Message{}
		}

		sm.sessions[key] = &Session{
			Key:      key,
			Messages: msgs,
			Summary:  summary,
			Created:  time.Unix(createdUnix, 0),
			Updated:  time.Unix(updatedUnix, 0),
		}
	}
}

// SetHistory updates the messages of a session.
func (sm *SessionManager) SetHistory(key string, history []providers.Message) {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	session, ok := sm.sessions[key]
	if ok {
		// Create a deep copy to strictly isolate internal state
		// from the caller's slice.
		msgs := make([]providers.Message, len(history))
		copy(msgs, history)
		session.Messages = msgs
		session.Updated = time.Now()
	}
}
