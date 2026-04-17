// PicoClaw - Ultra-lightweight personal AI agent
// License: MIT
//
// Copyright (c) 2026 PicoClaw contributors

package agent

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/mingzhi1/metaclaw/pkg/infra/logger"
)

// --- Migration from legacy files --------------------------------------------

// migrateFromFiles imports data from the old file-based storage
// (memory/MEMORY.md and memory/YYYYMM/YYYYMMDD.md) into SQLite.
// It only runs if the long_term content is empty (fresh DB) AND the
// legacy directory exists. After a successful migration the legacy
// directory is renamed to memory_backup.
func (ms *MemoryStore) migrateFromFiles() {
	if ms.db == nil {
		return
	}

	memoryDir := filepath.Join(ms.workspace, "memory")

	// Check if the legacy directory exists.
	info, err := os.Stat(memoryDir)
	if err != nil || !info.IsDir() {
		return // nothing to migrate
	}

	// Only migrate if the DB is empty (fresh).
	longTerm := ms.ReadLongTerm()
	if longTerm != "" {
		return // already has data
	}

	logger.DebugCF("memory", "Migrating legacy file-based memory to SQLite", nil)

	// 1. Long-term memory.
	memoryFile := filepath.Join(memoryDir, "MEMORY.md")
	if data, err := os.ReadFile(memoryFile); err == nil && len(data) > 0 {
		ms.WriteLongTerm(string(data))
	}

	// 2. Daily notes — walk YYYYMM/YYYYMMDD.md files.
	entries, err := os.ReadDir(memoryDir)
	if err != nil {
		return
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		monthDir := filepath.Join(memoryDir, entry.Name())
		dayFiles, err := os.ReadDir(monthDir)
		if err != nil {
			continue
		}
		for _, df := range dayFiles {
			name := df.Name()
			if !strings.HasSuffix(name, ".md") {
				continue
			}
			day := strings.TrimSuffix(name, ".md") // YYYYMMDD
			if len(day) != 8 {
				continue
			}
			data, err := os.ReadFile(filepath.Join(monthDir, name))
			if err != nil || len(data) == 0 {
				continue
			}
			ms.mu.Lock()
			ms.db.Exec(
				"INSERT OR IGNORE INTO daily_notes (day, content) VALUES (?, ?)",
				day, string(data),
			)
			ms.mu.Unlock()
		}
	}

	// Rename legacy dir so we don't migrate again.
	backupDir := filepath.Join(ms.workspace, "memory_backup")
	if err := os.Rename(memoryDir, backupDir); err != nil {
		logger.DebugCF("memory", "Could not rename legacy memory dir", map[string]any{"error": err.Error()})
	} else {
		logger.DebugCF("memory", "Legacy memory migrated and backed up", map[string]any{"backup": backupDir})
	}
}

// migrateEntryTags populates memory_entry_tags from the legacy comma-separated
// tags column. Runs once — skips if the junction table already has rows.
func (ms *MemoryStore) migrateEntryTags() {
	if ms.db == nil {
		return
	}

	var count int
	if err := ms.db.QueryRow("SELECT COUNT(*) FROM memory_entry_tags").Scan(&count); err != nil || count > 0 {
		return
	}

	var entryCount int
	if err := ms.db.QueryRow("SELECT COUNT(*) FROM memory_entries WHERE tags != ''").Scan(&entryCount); err != nil || entryCount == 0 {
		return
	}

	logger.DebugCF("memory", "Migrating entry tags to junction table", map[string]any{"entries": entryCount})

	rows, err := ms.db.Query("SELECT id, tags FROM memory_entries WHERE tags != ''")
	if err != nil {
		return
	}
	defer rows.Close()

	tx, err := ms.db.Begin()
	if err != nil {
		return
	}
	defer tx.Rollback() //nolint:errcheck

	migrated := 0
	for rows.Next() {
		var id int64
		var tagsStr string
		if err := rows.Scan(&id, &tagsStr); err != nil {
			continue
		}
		for _, tag := range splitTags(tagsStr) {
			tag = strings.ToLower(strings.TrimSpace(tag))
			if tag == "" {
				continue
			}
			tx.Exec("INSERT OR IGNORE INTO memory_entry_tags (entry_id, tag) VALUES (?, ?)", id, tag)
			migrated++
		}
	}

	if err := tx.Commit(); err != nil {
		logger.DebugCF("memory", "Failed to migrate entry tags", map[string]any{"error": err.Error()})
		return
	}
	logger.DebugCF("memory", "Entry tags migrated", map[string]any{"tag_rows": migrated})
}
