// PicoClaw - Ultra-lightweight personal AI agent
// License: MIT

package topic

import (
	"database/sql"
	"fmt"
	"os"
	"time"

	"github.com/google/uuid"
	_ "modernc.org/sqlite"
)

const schemaDDL = `
CREATE TABLE IF NOT EXISTS topics (
    id           TEXT    PRIMARY KEY,
    title        TEXT    NOT NULL,
    status       TEXT    NOT NULL DEFAULT 'active',
    summary      TEXT    NOT NULL DEFAULT '',
    total_tokens INTEGER NOT NULL DEFAULT 0,
    turn_count   INTEGER NOT NULL DEFAULT 0,
    created_at   INTEGER NOT NULL,
    updated_at   INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_topics_status ON topics(status);
CREATE INDEX IF NOT EXISTS idx_topics_updated ON topics(updated_at DESC);
`

// Store provides persistent Topic storage backed by SQLite.
// It reuses the same database file as TurnStore (turns.db).
type Store struct {
	db *sql.DB
}

// NewStore opens or creates a topic Store using the given database file path.
func NewStore(dbPath string) (*Store, error) {
	if err := os.MkdirAll(pathDir(dbPath), 0o755); err != nil {
		return nil, fmt.Errorf("topic store: mkdir: %w", err)
	}
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, fmt.Errorf("topic store: open: %w", err)
	}
	db.SetMaxOpenConns(1)
	if _, err := db.Exec(schemaDDL); err != nil {
		db.Close()
		return nil, fmt.Errorf("topic store: schema: %w", err)
	}
	return &Store{db: db}, nil
}

// Close releases the database connection.
func (s *Store) Close() error { return s.db.Close() }

// Create inserts a new active Topic and returns it.
func (s *Store) Create(title string) (*Topic, error) {
	id := uuid.New().String()
	now := time.Now()
	_, err := s.db.Exec(
		`INSERT INTO topics (id, title, status, created_at, updated_at)
		 VALUES (?, ?, 'active', ?, ?)`,
		id, title, now.Unix(), now.Unix(),
	)
	if err != nil {
		return nil, fmt.Errorf("topic store create: %w", err)
	}
	return &Topic{
		ID: id, Title: title, Status: StatusActive,
		CreatedAt: now, UpdatedAt: now,
	}, nil
}

// Get retrieves a single Topic by ID. Returns nil, nil when not found.
func (s *Store) Get(id string) (*Topic, error) {
	row := s.db.QueryRow(
		`SELECT id, title, status, summary, total_tokens, turn_count, created_at, updated_at
		 FROM topics WHERE id = ?`, id)
	return scanTopic(row)
}

// Activated marks one topic as active and all others as idle (single-active invariant).
func (s *Store) Activate(id string) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	now := time.Now().Unix()
	if _, err := tx.Exec(
		`UPDATE topics SET status='idle', updated_at=? WHERE status='active'`, now,
	); err != nil {
		return err
	}
	if _, err := tx.Exec(
		`UPDATE topics SET status='active', updated_at=? WHERE id=?`, now, id,
	); err != nil {
		return err
	}
	return tx.Commit()
}

// SetStatus updates a topic's lifecycle status.
func (s *Store) SetStatus(id string, status Status) error {
	_, err := s.db.Exec(
		`UPDATE topics SET status=?, updated_at=? WHERE id=?`,
		string(status), time.Now().Unix(), id)
	return err
}

// SetSummary stores a generated summary for a topic.
func (s *Store) SetSummary(id, summary string) error {
	_, err := s.db.Exec(
		`UPDATE topics SET summary=?, updated_at=? WHERE id=?`,
		summary, time.Now().Unix(), id)
	return err
}

// AddTokens increments a topic's running token and turn counters.
func (s *Store) AddTokens(id string, tokens int) error {
	_, err := s.db.Exec(
		`UPDATE topics SET total_tokens=total_tokens+?, turn_count=turn_count+1, updated_at=? WHERE id=?`,
		tokens, time.Now().Unix(), id)
	return err
}

// ActiveTopic returns the currently active topic, or nil if none exists.
func (s *Store) ActiveTopic() (*Topic, error) {
	row := s.db.QueryRow(
		`SELECT id, title, status, summary, total_tokens, turn_count, created_at, updated_at
		 FROM topics WHERE status='active' ORDER BY updated_at DESC LIMIT 1`)
	t, err := scanTopic(row)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return t, err
}

// RecentTopics returns the N most recent non-resolved topics for Analyser context.
func (s *Store) RecentTopics(limit int) ([]*Topic, error) {
	rows, err := s.db.Query(
		`SELECT id, title, status, summary, total_tokens, turn_count, created_at, updated_at
		 FROM topics WHERE status IN ('active','idle')
		 ORDER BY updated_at DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var topics []*Topic
	for rows.Next() {
		t, err := scanTopicRow(rows)
		if err != nil {
			return nil, err
		}
		topics = append(topics, t)
	}
	return topics, rows.Err()
}

// StalTopics returns idle topics that have exceeded the idle threshold.
func (s *Store) StaleTopics(idleThreshold time.Duration) ([]*Topic, error) {
	cutoff := time.Now().Add(-idleThreshold).Unix()
	rows, err := s.db.Query(
		`SELECT id, title, status, summary, total_tokens, turn_count, created_at, updated_at
		 FROM topics WHERE status='idle' AND updated_at < ?`, cutoff)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var topics []*Topic
	for rows.Next() {
		t, err := scanTopicRow(rows)
		if err != nil {
			return nil, err
		}
		topics = append(topics, t)
	}
	return topics, rows.Err()
}

// scanTopic reads a single row from a *sql.Row.
func scanTopic(row *sql.Row) (*Topic, error) {
	var t Topic
	var createdAt, updatedAt int64
	err := row.Scan(&t.ID, &t.Title, &t.Status, &t.Summary,
		&t.TotalTokens, &t.TurnCount, &createdAt, &updatedAt)
	if err != nil {
		return nil, err
	}
	t.CreatedAt = time.Unix(createdAt, 0)
	t.UpdatedAt = time.Unix(updatedAt, 0)
	return &t, nil
}

func scanTopicRow(rows *sql.Rows) (*Topic, error) {
	var t Topic
	var createdAt, updatedAt int64
	err := rows.Scan(&t.ID, &t.Title, &t.Status, &t.Summary,
		&t.TotalTokens, &t.TurnCount, &createdAt, &updatedAt)
	if err != nil {
		return nil, err
	}
	t.CreatedAt = time.Unix(createdAt, 0)
	t.UpdatedAt = time.Unix(updatedAt, 0)
	return &t, nil
}

func pathDir(p string) string {
	for i := len(p) - 1; i >= 0; i-- {
		if p[i] == '/' || p[i] == '\\' {
			return p[:i]
		}
	}
	return "."
}
