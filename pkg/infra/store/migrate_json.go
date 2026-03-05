package store

import (
	"database/sql"
	"encoding/json"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// MigrateFromJSON performs a one-time import of legacy JSON data into SQLite.
// It checks for existing JSON files (sessions/*.json, cron/jobs.json, state/state.json)
// and imports them into the corresponding SQLite tables.
// After successful import, it renames the source files to *.json.migrated.
//
// This should be called once after Open() during gateway startup.
func MigrateFromJSON(db *sql.DB, workspace string) {
	migrateSessionsJSON(db, workspace)
	migrateCronJSON(db, workspace)
	migrateStateJSON(db, workspace)
	migrateActiveContextJSON(db, workspace)
}

// --- Sessions migration ---

type jsonSession struct {
	Key      string            `json:"key"`
	Messages json.RawMessage   `json:"messages"`
	Summary  string            `json:"summary"`
	Created  time.Time         `json:"created"`
	Updated  time.Time         `json:"updated"`
}

func migrateSessionsJSON(db *sql.DB, workspace string) {
	sessionsDir := filepath.Join(workspace, "sessions")
	entries, err := os.ReadDir(sessionsDir)
	if err != nil {
		return // no sessions dir
	}

	migrated := 0
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			continue
		}

		filePath := filepath.Join(sessionsDir, entry.Name())
		data, err := os.ReadFile(filePath)
		if err != nil {
			continue
		}

		var sess jsonSession
		if err := json.Unmarshal(data, &sess); err != nil {
			continue
		}

		// Skip if key is empty
		if sess.Key == "" {
			continue
		}

		// Check if already migrated
		var count int
		db.QueryRow(`SELECT COUNT(*) FROM sessions WHERE key = ?`, sess.Key).Scan(&count)
		if count > 0 {
			continue
		}

		msgsJSON := string(sess.Messages)
		if msgsJSON == "" {
			msgsJSON = "[]"
		}

		_, err = db.Exec(`
			INSERT OR IGNORE INTO sessions (key, messages, summary, created, updated)
			VALUES (?, ?, ?, ?, ?)`,
			sess.Key, msgsJSON, sess.Summary,
			sess.Created.Unix(), sess.Updated.Unix(),
		)
		if err != nil {
			log.Printf("[migrate] session %s: %v", sess.Key, err)
			continue
		}

		// Rename to .migrated
		os.Rename(filePath, filePath+".migrated")
		migrated++
	}

	if migrated > 0 {
		log.Printf("[migrate] Imported %d sessions from JSON to SQLite", migrated)
	}
}

// --- Cron migration ---

type jsonCronStore struct {
	Version int               `json:"version"`
	Jobs    []json.RawMessage `json:"jobs"`
}

func migrateCronJSON(db *sql.DB, workspace string) {
	cronFile := filepath.Join(workspace, "cron", "jobs.json")
	data, err := os.ReadFile(cronFile)
	if err != nil {
		return // no cron file
	}

	var store jsonCronStore
	if err := json.Unmarshal(data, &store); err != nil {
		return
	}

	if len(store.Jobs) == 0 {
		return
	}

	// Check if cron_jobs table already has data
	var count int
	db.QueryRow(`SELECT COUNT(*) FROM cron_jobs`).Scan(&count)
	if count > 0 {
		return // already has data
	}

	migrated := 0
	for _, jobJSON := range store.Jobs {
		var job struct {
			ID             string          `json:"id"`
			Name           string          `json:"name"`
			Enabled        bool            `json:"enabled"`
			Schedule       json.RawMessage `json:"schedule"`
			Payload        json.RawMessage `json:"payload"`
			State          struct {
				NextRunAtMS *int64 `json:"nextRunAtMs"`
				LastRunAtMS *int64 `json:"lastRunAtMs"`
				LastStatus  string `json:"lastStatus"`
				LastError   string `json:"lastError"`
			} `json:"state"`
			CreatedAtMS    int64 `json:"createdAtMs"`
			UpdatedAtMS    int64 `json:"updatedAtMs"`
			DeleteAfterRun bool  `json:"deleteAfterRun"`
		}
		if err := json.Unmarshal(jobJSON, &job); err != nil {
			continue
		}

		enabled := 0
		if job.Enabled {
			enabled = 1
		}
		dar := 0
		if job.DeleteAfterRun {
			dar = 1
		}

		_, err = db.Exec(`
			INSERT OR IGNORE INTO cron_jobs (id, name, enabled, schedule, payload,
			    next_run_at_ms, last_run_at_ms, last_status, last_error,
			    created_at_ms, updated_at_ms, delete_after_run)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			job.ID, job.Name, enabled, string(job.Schedule), string(job.Payload),
			job.State.NextRunAtMS, job.State.LastRunAtMS,
			job.State.LastStatus, job.State.LastError,
			job.CreatedAtMS, job.UpdatedAtMS, dar,
		)
		if err != nil {
			log.Printf("[migrate] cron job %s: %v", job.ID, err)
			continue
		}
		migrated++
	}

	if migrated > 0 {
		os.Rename(cronFile, cronFile+".migrated")
		log.Printf("[migrate] Imported %d cron jobs from JSON to SQLite", migrated)
	}
}

// --- State migration ---

func migrateStateJSON(db *sql.DB, workspace string) {
	// Try new location first, then old
	stateFile := filepath.Join(workspace, "state", "state.json")
	data, err := os.ReadFile(stateFile)
	if err != nil {
		stateFile = filepath.Join(workspace, "state.json")
		data, err = os.ReadFile(stateFile)
		if err != nil {
			return // no state file
		}
	}

	var state struct {
		LastChannel string    `json:"last_channel"`
		LastChatID  string    `json:"last_chat_id"`
		Timestamp   time.Time `json:"timestamp"`
	}
	if err := json.Unmarshal(data, &state); err != nil {
		return
	}

	// Check if state table already has data
	var count int
	db.QueryRow(`SELECT COUNT(*) FROM state`).Scan(&count)
	if count > 0 {
		return
	}

	migrated := 0
	if state.LastChannel != "" {
		db.Exec(`INSERT OR IGNORE INTO state (key, value) VALUES (?, ?)`,
			"last_channel", state.LastChannel)
		migrated++
	}
	if state.LastChatID != "" {
		db.Exec(`INSERT OR IGNORE INTO state (key, value) VALUES (?, ?)`,
			"last_chat_id", state.LastChatID)
		migrated++
	}
	if !state.Timestamp.IsZero() {
		db.Exec(`INSERT OR IGNORE INTO state (key, value) VALUES (?, ?)`,
			"timestamp", strings.TrimRight(state.Timestamp.Format("2006010215040500"), "0"))
		migrated++
	}

	if migrated > 0 {
		os.Rename(stateFile, stateFile+".migrated")
		log.Printf("[migrate] Imported state from JSON to SQLite (%s)", stateFile)
	}
}

// --- Active Context migration ---

func migrateActiveContextJSON(db *sql.DB, workspace string) {
	acFile := filepath.Join(workspace, "active_context.json")
	data, err := os.ReadFile(acFile)
	if err != nil {
		return // no active context file
	}

	// Ensure table exists.
	db.Exec(`CREATE TABLE IF NOT EXISTS active_context (
		key  TEXT PRIMARY KEY,
		data TEXT NOT NULL DEFAULT '{}'
	)`)

	// Check if table already has data.
	var count int
	db.QueryRow(`SELECT COUNT(*) FROM active_context`).Scan(&count)
	if count > 0 {
		return
	}

	var store struct {
		Contexts map[string]json.RawMessage `json:"contexts"`
	}
	if err := json.Unmarshal(data, &store); err != nil {
		return
	}

	migrated := 0
	for key, raw := range store.Contexts {
		_, err := db.Exec(`INSERT OR IGNORE INTO active_context (key, data) VALUES (?, ?)`,
			key, string(raw))
		if err != nil {
			log.Printf("[migrate] active_context %s: %v", key, err)
			continue
		}
		migrated++
	}

	if migrated > 0 {
		os.Rename(acFile, acFile+".migrated")
		log.Printf("[migrate] Imported %d active contexts from JSON to SQLite", migrated)
	}
}
