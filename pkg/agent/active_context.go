// PicoClaw - Ultra-lightweight personal AI agent
// License: MIT
//
// Copyright (c) 2026 PicoClaw contributors

package agent

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"sync"

	"github.com/mingzhi1/metaclaw/pkg/infra/logger"
)

// ActiveContext holds the structured per-channel context that Phase 1 uses
// to understand short/ambiguous user messages.
//
// Design choices:
//   - CurrentFiles: last 5 file paths touched by tool calls (read/write/edit/append/list_dir).
//   - RecentErrors: last 3 tool failure messages.
//   - CurrentTask / RecentSummaries are intentionally omitted — they overlap with
//     the recent-M turns in instant memory and would be redundant.
type ActiveContext struct {
	CurrentFiles []string `json:"current_files"` // newest first, max 5
	RecentErrors []string `json:"recent_errors"` // newest first, max 3
}

const activeContextDDL = `
CREATE TABLE IF NOT EXISTS active_context (
    key   TEXT PRIMARY KEY,
    data  TEXT NOT NULL DEFAULT '{}'
);
`

// ActiveContextStore is a thread-safe in-memory map of channel:chatID → ActiveContext.
// It is backed by SQLite for persistence (db may be nil for memory-only mode).
type ActiveContextStore struct {
	mu   sync.RWMutex
	data map[string]*ActiveContext // key = "channel:chatID"
	db   *sql.DB
}

// NewActiveContextStore creates a store. If db is non-nil, the table is created
// and existing rows are loaded into memory.
func NewActiveContextStore(db *sql.DB) *ActiveContextStore {
	s := &ActiveContextStore{
		data: make(map[string]*ActiveContext),
		db:   db,
	}
	if db != nil {
		db.Exec(activeContextDDL)
		s.loadAll()
	}
	return s
}

// Get returns a copy of the ActiveContext for the given key (never nil).
func (s *ActiveContextStore) Get(key string) *ActiveContext {
	s.mu.RLock()
	ac, ok := s.data[key]
	s.mu.RUnlock()

	if !ok || ac == nil {
		return &ActiveContext{}
	}
	// Return a shallow copy to avoid callers mutating the store.
	cp := *ac
	cp.CurrentFiles = append([]string(nil), ac.CurrentFiles...)
	cp.RecentErrors = append([]string(nil), ac.RecentErrors...)
	return &cp
}

// fileExtractingTools is the set of tool names whose arguments may carry file paths.
// Keys are lowercase tool names; values indicate the argument name(s) to inspect.
var fileExtractingTools = map[string][]string{
	"read_file":   {"path", "file_path", "filename"},
	"write_file":  {"path", "file_path", "filename"},
	"edit_file":   {"path", "file_path", "filename"},
	"append_file": {"path", "file_path", "filename"},
	"list_dir":    {"path", "dir_path", "directory"},
}

// Update applies the outcomes of a completed turn to the ActiveContext for key.
// It extracts file paths from tool call arguments and captures error messages.
func (s *ActiveContextStore) Update(key string, input RuntimeInput) {
	if key == "" {
		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	ac, ok := s.data[key]
	if !ok || ac == nil {
		ac = &ActiveContext{}
		s.data[key] = ac
	}

	// Extract file paths from tool calls.
	for _, tc := range input.ToolCalls {
		name := strings.ToLower(tc.Name)
		argFields, relevant := fileExtractingTools[name]
		if !relevant {
			continue
		}
		// tc.Args is stored as JSON string or we can check tc.ArgsRaw if available.
		// Since ToolCallRecord only has Name/Error/Duration, we skip argument extraction
		// here and rely on callers passing a richer input in the future (M5).
		// For now we still handle errors.
		_ = argFields
	}

	// Capture tool errors.
	for _, tc := range input.ToolCalls {
		if tc.Error == "" {
			continue
		}
		msg := fmt.Sprintf("[%s] %s", tc.Name, tc.Error)
		// Prepend (newest first) and cap at 3.
		ac.RecentErrors = prependCapped(ac.RecentErrors, msg, 3)
	}

	s.persist(key, ac)
}

// UpdateWithFiles is an extended update that also receives file paths extracted
// by the loop (call this when tool argument parsing is available).
func (s *ActiveContextStore) UpdateWithFiles(key string, input RuntimeInput, filePaths []string) {
	s.Update(key, input)

	if len(filePaths) == 0 {
		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	ac, ok := s.data[key]
	if !ok || ac == nil {
		ac = &ActiveContext{}
		s.data[key] = ac
	}

	for _, p := range filePaths {
		if p != "" {
			ac.CurrentFiles = prependCapped(ac.CurrentFiles, p, 5)
		}
	}

	s.persist(key, ac)
}

// prependCapped prepends item to slice and caps the result at max length.
// Deduplicates: if item already exists it is moved to the front.
func prependCapped(slice []string, item string, max int) []string {
	// Remove duplicate.
	filtered := make([]string, 0, len(slice))
	for _, s := range slice {
		if s != item {
			filtered = append(filtered, s)
		}
	}
	result := append([]string{item}, filtered...)
	if len(result) > max {
		result = result[:max]
	}
	return result
}

// Format renders the context as a markdown block for injection into a user message.
// Returns empty string when there is nothing to show.
func (ac *ActiveContext) Format() string {
	if len(ac.CurrentFiles) == 0 && len(ac.RecentErrors) == 0 {
		return ""
	}
	var sb strings.Builder
	sb.WriteString("## Current Context\n")
	if len(ac.CurrentFiles) > 0 {
		sb.WriteString("Files in use: ")
		sb.WriteString(strings.Join(ac.CurrentFiles, ", "))
		sb.WriteString("\n")
	}
	if len(ac.RecentErrors) > 0 {
		sb.WriteString("Recent errors:\n")
		for _, e := range ac.RecentErrors {
			sb.WriteString("  - ")
			sb.WriteString(e)
			sb.WriteString("\n")
		}
	}
	return sb.String()
}

// ---------------------------------------------------------------------------
// SQLite persistence
// ---------------------------------------------------------------------------

// persist writes a single key to SQLite (called with lock held).
func (s *ActiveContextStore) persist(key string, ac *ActiveContext) {
	if s.db == nil {
		return
	}
	data, err := json.Marshal(ac)
	if err != nil {
		return
	}
	s.db.Exec(`
		INSERT INTO active_context (key, data) VALUES (?, ?)
		ON CONFLICT(key) DO UPDATE SET data = excluded.data`,
		key, string(data))
}

// loadAll reads all rows from SQLite into memory.
func (s *ActiveContextStore) loadAll() {
	if s.db == nil {
		return
	}
	rows, err := s.db.Query(`SELECT key, data FROM active_context`)
	if err != nil {
		return
	}
	defer rows.Close()
	for rows.Next() {
		var key, raw string
		if err := rows.Scan(&key, &raw); err != nil {
			continue
		}
		var ac ActiveContext
		if err := json.Unmarshal([]byte(raw), &ac); err != nil {
			continue
		}
		s.data[key] = &ac
	}
	logger.DebugCF("active_context", "Loaded from SQLite", map[string]any{"keys": len(s.data)})
}
