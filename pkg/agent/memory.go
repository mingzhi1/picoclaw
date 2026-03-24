// PicoClaw - Ultra-lightweight personal AI agent
// Inspired by and based on nanobot: https://github.com/HKUDS/nanobot
// License: MIT
//
// Copyright (c) 2026 PicoClaw contributors

package agent

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/sipeed/picoclaw/pkg/infra/logger"

	_ "modernc.org/sqlite"
)

// MemoryStore manages persistent memory for the agent using SQLite.
//
// Schema:
//   - long_term: single-row table holding the long-term memory content
//   - daily_notes: one row per day (key = "YYYYMMDD")
//   - memory_entries: individually tagged memory items
//
// The database file is stored at workspace/memory.db.
type MemoryStore struct {
	workspace string
	db        *sql.DB
	mu        sync.Mutex // serialise writes
}

// NewMemoryStore creates a new MemoryStore backed by SQLite.
// It creates the database and tables if they do not exist.
func NewMemoryStore(workspace string) *MemoryStore {
	dbPath := filepath.Join(workspace, "memory.db")

	// Ensure workspace directory exists.
	os.MkdirAll(workspace, 0o755)

	db, err := sql.Open("sqlite", dbPath+"?_pragma=journal_mode(wal)&_pragma=busy_timeout(5000)")
	if err != nil {
		logger.DebugCF("memory", "Failed to open memory DB", map[string]any{"error": err.Error()})
		// Return a store that degrades gracefully (methods return empty / no-op).
		return &MemoryStore{workspace: workspace}
	}

	// Create tables.
	ddl := `
CREATE TABLE IF NOT EXISTS long_term (
	id      INTEGER PRIMARY KEY CHECK (id = 1),
	content TEXT NOT NULL DEFAULT ''
);
INSERT OR IGNORE INTO long_term (id, content) VALUES (1, '');

CREATE TABLE IF NOT EXISTS daily_notes (
	day     TEXT PRIMARY KEY,  -- YYYYMMDD
	content TEXT NOT NULL DEFAULT ''
);

CREATE TABLE IF NOT EXISTS memory_entries (
	id         INTEGER PRIMARY KEY AUTOINCREMENT,
	content    TEXT    NOT NULL,
	tags       TEXT    NOT NULL DEFAULT '',  -- comma-separated (legacy, see memory_entry_tags)
	created_at TEXT    NOT NULL DEFAULT (datetime('now')),
	updated_at TEXT    NOT NULL DEFAULT (datetime('now'))
);

CREATE TABLE IF NOT EXISTS memory_entry_tags (
	entry_id INTEGER NOT NULL,
	tag      TEXT    NOT NULL,
	PRIMARY KEY (tag, entry_id)
);
CREATE INDEX IF NOT EXISTS idx_met_entry ON memory_entry_tags(entry_id);

CREATE TABLE IF NOT EXISTS cot_usage (
	id          INTEGER PRIMARY KEY AUTOINCREMENT,
	intent      TEXT    NOT NULL DEFAULT '',
	tags        TEXT    NOT NULL DEFAULT '',  -- comma-separated tags from message analysis
	cot_prompt  TEXT    NOT NULL DEFAULT '',  -- LLM-generated thinking strategy
	message     TEXT    NOT NULL DEFAULT '',  -- first 200 chars of user message
	feedback    INTEGER NOT NULL DEFAULT 0,   -- -1=bad, 0=neutral, 1=good
	created_at  TEXT    NOT NULL DEFAULT (datetime('now'))
);
`
	if _, err := db.Exec(ddl); err != nil {
		logger.DebugCF("memory", "Failed to initialise memory DB tables", map[string]any{"error": err.Error()})
		db.Close()
		return &MemoryStore{workspace: workspace}
	}

	ms := &MemoryStore{
		workspace: workspace,
		db:        db,
	}

	// Migrate from legacy file-based storage if memory.db was just created.
	ms.migrateFromFiles()
	ms.migrateEntryTags()

	return ms
}

// Close closes the underlying database. Safe to call multiple times.
func (ms *MemoryStore) Close() {
	if ms.db != nil {
		ms.db.Close()
	}
}

// --- Long-term memory -------------------------------------------------------

// ReadLongTerm reads the long-term memory content.
// Returns empty string if the database is unavailable.
func (ms *MemoryStore) ReadLongTerm() string {
	if ms.db == nil {
		return ""
	}
	var content string
	err := ms.db.QueryRow("SELECT content FROM long_term WHERE id = 1").Scan(&content)
	if err != nil {
		return ""
	}
	return content
}

// WriteLongTerm replaces the long-term memory content.
func (ms *MemoryStore) WriteLongTerm(content string) error {
	if ms.db == nil {
		return fmt.Errorf("memory DB not available")
	}
	ms.mu.Lock()
	defer ms.mu.Unlock()
	_, err := ms.db.Exec("UPDATE long_term SET content = ? WHERE id = 1", content)
	return err
}

// --- Daily notes ------------------------------------------------------------

// todayKey returns today's date as "YYYYMMDD".
func todayKey() string {
	return time.Now().Format("20060102")
}

// ReadToday reads today's daily note.
// Returns empty string if the file doesn't exist or the database is unavailable.
func (ms *MemoryStore) ReadToday() string {
	if ms.db == nil {
		return ""
	}
	var content string
	err := ms.db.QueryRow("SELECT content FROM daily_notes WHERE day = ?", todayKey()).Scan(&content)
	if err != nil {
		return ""
	}
	return content
}

// AppendToday appends content to today's daily note.
// If no note exists for today, a new one is created with a date header.
func (ms *MemoryStore) AppendToday(content string) error {
	if ms.db == nil {
		return fmt.Errorf("memory DB not available")
	}
	ms.mu.Lock()
	defer ms.mu.Unlock()

	key := todayKey()

	var existing string
	err := ms.db.QueryRow("SELECT content FROM daily_notes WHERE day = ?", key).Scan(&existing)
	if err == sql.ErrNoRows || existing == "" {
		// New day 鈥?add header.
		header := fmt.Sprintf("# %s\n\n", time.Now().Format("2006-01-02"))
		content = header + content
		_, err = ms.db.Exec(
			"INSERT OR REPLACE INTO daily_notes (day, content) VALUES (?, ?)",
			key, content,
		)
	} else if err == nil {
		// Append to existing.
		content = existing + "\n" + content
		_, err = ms.db.Exec("UPDATE daily_notes SET content = ? WHERE day = ?", content, key)
	}
	return err
}

// GetRecentDailyNotes returns daily notes from the last N days.
// Contents are joined with "---" separator.
func (ms *MemoryStore) GetRecentDailyNotes(days int) string {
	if ms.db == nil {
		return ""
	}

	cutoff := time.Now().AddDate(0, 0, -(days - 1)).Format("20060102")
	rows, err := ms.db.Query(
		"SELECT content FROM daily_notes WHERE day >= ? ORDER BY day DESC",
		cutoff,
	)
	if err != nil {
		return ""
	}
	defer rows.Close()

	var sb strings.Builder
	first := true
	for rows.Next() {
		var content string
		if err := rows.Scan(&content); err != nil || content == "" {
			continue
		}
		if !first {
			sb.WriteString("\n\n---\n\n")
		}
		sb.WriteString(content)
		first = false
	}
	return sb.String()
}

// --- Tagged memory entries ---------------------------------------------------

// MemoryEntry represents a single tagged memory item.
type MemoryEntry struct {
	ID        int64
	Content   string
	Tags      []string
	CreatedAt string
	UpdatedAt string
}

// normaliseTags lowercases, trims, deduplicates, and sorts tags.
func normaliseTags(tags []string) []string {
	seen := make(map[string]struct{}, len(tags))
	out := make([]string, 0, len(tags))
	for _, t := range tags {
		t = strings.ToLower(strings.TrimSpace(t))
		if t == "" {
			continue
		}
		if _, ok := seen[t]; !ok {
			seen[t] = struct{}{}
			out = append(out, t)
		}
	}
	return out
}

// splitTags splits a stored tag string back into a slice.
func splitTags(s string) []string {
	if s == "" {
		return nil
	}
	return strings.Split(s, ",")
}

// AddEntry inserts a new tagged memory entry. Returns the new entry ID.
func (ms *MemoryStore) AddEntry(content string, tags []string) (int64, error) {
	if ms.db == nil {
		return 0, fmt.Errorf("memory DB not available")
	}
	ms.mu.Lock()
	defer ms.mu.Unlock()

	normTags := normaliseTags(tags)

	tx, err := ms.db.Begin()
	if err != nil {
		return 0, err
	}
	defer tx.Rollback() //nolint:errcheck

	res, err := tx.Exec(
		"INSERT INTO memory_entries (content, tags) VALUES (?, ?)",
		content, strings.Join(normTags, ","),
	)
	if err != nil {
		return 0, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return 0, err
	}

	for _, tag := range normTags {
		if _, err := tx.Exec(
			"INSERT OR IGNORE INTO memory_entry_tags (entry_id, tag) VALUES (?, ?)",
			id, tag,
		); err != nil {
			return 0, err
		}
	}

	return id, tx.Commit()
}

// UpdateEntry updates the content and tags of an existing entry.
func (ms *MemoryStore) UpdateEntry(id int64, content string, tags []string) error {
	if ms.db == nil {
		return fmt.Errorf("memory DB not available")
	}
	ms.mu.Lock()
	defer ms.mu.Unlock()

	normTags := normaliseTags(tags)

	tx, err := ms.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback() //nolint:errcheck

	if _, err := tx.Exec(
		"UPDATE memory_entries SET content = ?, tags = ?, updated_at = datetime('now') WHERE id = ?",
		content, strings.Join(normTags, ","), id,
	); err != nil {
		return err
	}

	if _, err := tx.Exec("DELETE FROM memory_entry_tags WHERE entry_id = ?", id); err != nil {
		return err
	}
	for _, tag := range normTags {
		if _, err := tx.Exec(
			"INSERT OR IGNORE INTO memory_entry_tags (entry_id, tag) VALUES (?, ?)",
			id, tag,
		); err != nil {
			return err
		}
	}

	return tx.Commit()
}

// DeleteEntry removes a memory entry by ID.
func (ms *MemoryStore) DeleteEntry(id int64) error {
	if ms.db == nil {
		return fmt.Errorf("memory DB not available")
	}
	ms.mu.Lock()
	defer ms.mu.Unlock()

	tx, err := ms.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback() //nolint:errcheck

	if _, err := tx.Exec("DELETE FROM memory_entry_tags WHERE entry_id = ?", id); err != nil {
		return err
	}
	if _, err := tx.Exec("DELETE FROM memory_entries WHERE id = ?", id); err != nil {
		return err
	}
	return tx.Commit()
}

// GetEntry retrieves a single memory entry by ID.
func (ms *MemoryStore) GetEntry(id int64) (*MemoryEntry, error) {
	if ms.db == nil {
		return nil, fmt.Errorf("memory DB not available")
	}
	var e MemoryEntry
	var tagsStr string
	err := ms.db.QueryRow(
		"SELECT id, content, tags, created_at, updated_at FROM memory_entries WHERE id = ?", id,
	).Scan(&e.ID, &e.Content, &tagsStr, &e.CreatedAt, &e.UpdatedAt)
	if err != nil {
		return nil, err
	}
	e.Tags = splitTags(tagsStr)
	return &e, nil
}

// SearchByTag returns all entries that contain the given tag.
// Tag matching is case-insensitive (tags are stored lowercase).
func (ms *MemoryStore) SearchByTag(tag string) ([]MemoryEntry, error) {
	if ms.db == nil {
		return nil, fmt.Errorf("memory DB not available")
	}
	tag = strings.ToLower(strings.TrimSpace(tag))
	if tag == "" {
		return nil, nil
	}

	rows, err := ms.db.Query(
		`SELECT e.id, e.content, e.tags, e.created_at, e.updated_at
		 FROM memory_entry_tags t
		 JOIN memory_entries e ON e.id = t.entry_id
		 WHERE t.tag = ?
		 ORDER BY e.updated_at DESC`,
		tag,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	return scanEntries(rows)
}

// SearchByTags returns entries that contain ALL of the given tags.
func (ms *MemoryStore) SearchByTags(tags []string) ([]MemoryEntry, error) {
	if ms.db == nil {
		return nil, fmt.Errorf("memory DB not available")
	}
	tags = normaliseTags(tags)
	if len(tags) == 0 {
		return nil, nil
	}

	placeholders := make([]string, len(tags))
	args := make([]any, len(tags)+1)
	for i, tag := range tags {
		placeholders[i] = "?"
		args[i] = tag
	}
	args[len(tags)] = len(tags)

	query := fmt.Sprintf(
		`SELECT e.id, e.content, e.tags, e.created_at, e.updated_at
		 FROM memory_entry_tags t
		 JOIN memory_entries e ON e.id = t.entry_id
		 WHERE t.tag IN (%s)
		 GROUP BY e.id
		 HAVING COUNT(DISTINCT t.tag) = ?
		 ORDER BY e.updated_at DESC`,
		strings.Join(placeholders, ", "),
	)

	rows, err := ms.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	return scanEntries(rows)
}

// SearchByAnyTag returns entries that contain ANY of the given tags (OR logic).
// Results are deduplicated and ordered by updated_at DESC, limited to 20 entries.
func (ms *MemoryStore) SearchByAnyTag(tags []string) ([]MemoryEntry, error) {
	if ms.db == nil {
		return nil, fmt.Errorf("memory DB not available")
	}
	tags = normaliseTags(tags)
	if len(tags) == 0 {
		return nil, nil
	}

	placeholders := make([]string, len(tags))
	args := make([]any, len(tags))
	for i, tag := range tags {
		placeholders[i] = "?"
		args[i] = tag
	}

	query := fmt.Sprintf(
		`SELECT DISTINCT e.id, e.content, e.tags, e.created_at, e.updated_at
		 FROM memory_entry_tags t
		 JOIN memory_entries e ON e.id = t.entry_id
		 WHERE t.tag IN (%s)
		 ORDER BY e.updated_at DESC LIMIT 20`,
		strings.Join(placeholders, ", "),
	)

	rows, err := ms.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	return scanEntries(rows)
}

// ListAllTags returns all unique tags used across memory entries.
func (ms *MemoryStore) ListAllTags() ([]string, error) {
	if ms.db == nil {
		return nil, fmt.Errorf("memory DB not available")
	}

	rows, err := ms.db.Query("SELECT DISTINCT tag FROM memory_entry_tags ORDER BY tag")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []string
	for rows.Next() {
		var tag string
		if err := rows.Scan(&tag); err != nil {
			continue
		}
		result = append(result, tag)
	}
	return result, rows.Err()
}

// ListEntries returns the most recent N entries (all tags), ordered newest first.
func (ms *MemoryStore) ListEntries(limit int) ([]MemoryEntry, error) {
	if ms.db == nil {
		return nil, fmt.Errorf("memory DB not available")
	}
	if limit <= 0 {
		limit = 50
	}

	rows, err := ms.db.Query(
		"SELECT id, content, tags, created_at, updated_at FROM memory_entries ORDER BY updated_at DESC LIMIT ?",
		limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	return scanEntries(rows)
}

// scanEntries is a helper to scan rows into MemoryEntry slices.
func scanEntries(rows *sql.Rows) ([]MemoryEntry, error) {
	var entries []MemoryEntry
	for rows.Next() {
		var e MemoryEntry
		var tagsStr string
		if err := rows.Scan(&e.ID, &e.Content, &tagsStr, &e.CreatedAt, &e.UpdatedAt); err != nil {
			return entries, err
		}
		e.Tags = splitTags(tagsStr)
		entries = append(entries, e)
	}
	return entries, rows.Err()
}

// --- Composite context ------------------------------------------------------

// GetMemoryContext returns formatted memory context for the agent prompt.
// Includes long-term memory, recent daily notes, and recent tagged entries.
func (ms *MemoryStore) GetMemoryContext() string {
	longTerm := ms.ReadLongTerm()
	recentNotes := ms.GetRecentDailyNotes(3)

	var sb strings.Builder
	hasContent := false

	if longTerm != "" {
		sb.WriteString("## Long-term Memory\n\n")
		sb.WriteString(longTerm)
		hasContent = true
	}

	if recentNotes != "" {
		if hasContent {
			sb.WriteString("\n\n---\n\n")
		}
		sb.WriteString("## Recent Daily Notes\n\n")
		sb.WriteString(recentNotes)
		hasContent = true
	}

	// Include recent tagged memory entries.
	entries, _ := ms.ListEntries(10)
	if len(entries) > 0 {
		if hasContent {
			sb.WriteString("\n\n---\n\n")
		}
		sb.WriteString("## Tagged Memories\n\n")
		for _, e := range entries {
			tagLabel := ""
			if len(e.Tags) > 0 {
				tagLabel = " [" + strings.Join(e.Tags, ", ") + "]"
			}
			fmt.Fprintf(&sb, "- (#%d%s) %s\n", e.ID, tagLabel, e.Content)
		}
		hasContent = true
	}

	if !hasContent {
		return ""
	}
	return sb.String()
}
