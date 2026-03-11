// PicoClaw - Ultra-lightweight personal AI agent
// License: MIT
//
// Copyright (c) 2026 PicoClaw contributors

package agent

import (
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/sipeed/picoclaw/pkg/infra/logger"
)

// ---------------------------------------------------------------------------
// Fact Store — persistent entity-attribute-value store with versioning
//
// Three fact types (from design doc):
//   - state:  new value overwrites old (e.g. latency: 850ns → 420ns)
//   - append: values accumulate (e.g. optimizations: sync.Pool, type switch)
//   - event:  state transition (e.g. status: needs_optimization → done)
//
// On overwrite, the old fact is marked superseded (not deleted) for traceability.
// ---------------------------------------------------------------------------

// FactType enumerates fact lifecycle semantics.
type FactType string

const (
	FactState  FactType = "state"
	FactAppend FactType = "append"
	FactEvent  FactType = "event"
)

// Fact represents a single entity-attribute-value triple.
type Fact struct {
	ID           int64
	Entity       string   // e.g. "parseConfig", "docker", "bug#123"
	Key          string   // e.g. "status", "latency", "error_cause"
	Value        string   // current value
	Type         FactType // state | append | event
	TopicID      string   // "" = global fact (always visible)
	SupersededBy *int64   // nil = active, else ID of replacement
	CreatedAt    int64
	UpdatedAt    int64
}

// IsActive returns true if the fact has not been superseded.
func (f *Fact) IsActive() bool { return f.SupersededBy == nil }

// FactStore provides persistent fact management backed by SQLite.
type FactStore struct {
	db *sql.DB
}

// NewFactStore opens (or creates) the facts table in the given database.
func NewFactStore(db *sql.DB) (*FactStore, error) {
	if db == nil {
		return nil, fmt.Errorf("nil database")
	}
	schema := `
	CREATE TABLE IF NOT EXISTS memory_facts (
		id             INTEGER PRIMARY KEY AUTOINCREMENT,
		entity         TEXT NOT NULL,
		key            TEXT NOT NULL,
		value          TEXT NOT NULL,
		type           TEXT NOT NULL DEFAULT 'state',
		topic_id       TEXT NOT NULL DEFAULT '',
		superseded_by  INTEGER,
		created_at     INTEGER NOT NULL,
		updated_at     INTEGER NOT NULL
	);
	CREATE INDEX IF NOT EXISTS idx_facts_entity_key ON memory_facts(entity, key);
	CREATE INDEX IF NOT EXISTS idx_facts_topic ON memory_facts(topic_id);
	CREATE INDEX IF NOT EXISTS idx_facts_active ON memory_facts(superseded_by) WHERE superseded_by IS NULL;
	`
	if _, err := db.Exec(schema); err != nil {
		return nil, fmt.Errorf("fact_store schema: %w", err)
	}
	return &FactStore{db: db}, nil
}

// Upsert inserts or updates a fact based on its type semantics.
//   - state/event: supersede existing active fact with same entity+key, insert new
//   - append: insert new without superseding
func (fs *FactStore) Upsert(entity, key, value string, factType FactType, topicID string) (*Fact, error) {
	entity = strings.TrimSpace(entity)
	key = strings.TrimSpace(key)
	if entity == "" || key == "" {
		return nil, fmt.Errorf("entity and key must not be empty")
	}
	if factType == "" {
		factType = FactState
	}

	now := time.Now().Unix()

	tx, err := fs.db.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	// For state/event: supersede existing active fact with same entity+key.
	if factType == FactState || factType == FactEvent {
		// We'll update after insert so we know the new ID.
	}

	// Insert new fact.
	res, err := tx.Exec(
		`INSERT INTO memory_facts (entity, key, value, type, topic_id, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		entity, key, value, string(factType), topicID, now, now,
	)
	if err != nil {
		return nil, err
	}
	newID, _ := res.LastInsertId()

	// Supersede old active facts (state/event only).
	if factType == FactState || factType == FactEvent {
		_, err = tx.Exec(
			`UPDATE memory_facts SET superseded_by = ?, updated_at = ?
			 WHERE entity = ? AND key = ? AND superseded_by IS NULL AND id != ?`,
			newID, now, entity, key, newID,
		)
		if err != nil {
			return nil, err
		}
	}

	if err := tx.Commit(); err != nil {
		return nil, err
	}

	return &Fact{
		ID:        newID,
		Entity:    entity,
		Key:       key,
		Value:     value,
		Type:      factType,
		TopicID:   topicID,
		CreatedAt: now,
		UpdatedAt: now,
	}, nil
}

// GetActive returns all active (non-superseded) facts, optionally filtered by topicID.
// Pass "" to get facts for all topics.
func (fs *FactStore) GetActive(topicID string) ([]Fact, error) {
	var rows *sql.Rows
	var err error
	if topicID != "" {
		rows, err = fs.db.Query(
			`SELECT id, entity, key, value, type, topic_id, created_at, updated_at
			 FROM memory_facts WHERE superseded_by IS NULL AND topic_id = ?
			 ORDER BY entity, key`, topicID)
	} else {
		rows, err = fs.db.Query(
			`SELECT id, entity, key, value, type, topic_id, created_at, updated_at
			 FROM memory_facts WHERE superseded_by IS NULL
			 ORDER BY entity, key`)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanFacts(rows)
}

// GetGlobalFacts returns all active facts with empty topicID (always visible).
func (fs *FactStore) GetGlobalFacts() ([]Fact, error) {
	rows, err := fs.db.Query(
		`SELECT id, entity, key, value, type, topic_id, created_at, updated_at
		 FROM memory_facts WHERE superseded_by IS NULL AND topic_id = ''
		 ORDER BY entity, key`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanFacts(rows)
}

// GetByEntity returns all active facts for a given entity.
func (fs *FactStore) GetByEntity(entity string) ([]Fact, error) {
	rows, err := fs.db.Query(
		`SELECT id, entity, key, value, type, topic_id, created_at, updated_at
		 FROM memory_facts WHERE superseded_by IS NULL AND entity = ?
		 ORDER BY key`, entity)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanFacts(rows)
}

// GetHistory returns the full chain for a given entity+key (active + superseded),
// ordered most recent first.
func (fs *FactStore) GetHistory(entity, key string) ([]Fact, error) {
	rows, err := fs.db.Query(
		`SELECT id, entity, key, value, type, topic_id, created_at, updated_at
		 FROM memory_facts WHERE entity = ? AND key = ?
		 ORDER BY id DESC`, entity, key)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanFacts(rows)
}

// FormatForContext formats active facts into a string for context injection.
// Includes global facts + facts matching topicID.
func (fs *FactStore) FormatForContext(topicID string) string {
	var facts []Fact
	global, err := fs.GetGlobalFacts()
	if err != nil {
		logger.WarnCF("fact_store", "GetGlobalFacts failed", map[string]any{"error": err.Error()})
	} else {
		facts = append(facts, global...)
	}
	if topicID != "" {
		topicFacts, err := fs.GetActive(topicID)
		if err != nil {
			logger.WarnCF("fact_store", "GetActive failed", map[string]any{"error": err.Error()})
		} else {
			facts = append(facts, topicFacts...)
		}
	}
	if len(facts) == 0 {
		return ""
	}

	var sb strings.Builder
	sb.WriteString("# Active Facts\n\n")
	currentEntity := ""
	for _, f := range facts {
		if f.Entity != currentEntity {
			if currentEntity != "" {
				sb.WriteString("\n")
			}
			fmt.Fprintf(&sb, "**%s**:\n", f.Entity)
			currentEntity = f.Entity
		}
		fmt.Fprintf(&sb, "  - %s: %s", f.Key, f.Value)
		if f.Type == FactAppend {
			sb.WriteString(" [+]")
		}
		sb.WriteString("\n")
	}
	return sb.String()
}

// scanFacts reads Fact rows from a query result.
func scanFacts(rows *sql.Rows) ([]Fact, error) {
	var facts []Fact
	for rows.Next() {
		var f Fact
		var factType string
		if err := rows.Scan(&f.ID, &f.Entity, &f.Key, &f.Value, &factType,
			&f.TopicID, &f.CreatedAt, &f.UpdatedAt); err != nil {
			return nil, err
		}
		f.Type = FactType(factType)
		facts = append(facts, f)
	}
	return facts, rows.Err()
}

// Close closes the underlying database connection.
func (fs *FactStore) Close() error {
	if fs.db != nil {
		return fs.db.Close()
	}
	return nil
}
