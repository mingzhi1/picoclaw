// Package store provides a centralised SQLite database for PicoClaw's
// persistent runtime state.  A single "picoclaw.db" is created inside
// each workspace directory, and tables for sessions, cron jobs, and
// application state are initialised on open.
//
// Architecture: callers receive a *sql.DB handle and manage their own
// tables.  This package only owns the connection lifecycle.
package store

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	_ "modernc.org/sqlite"
)

const dbName = "picoclaw.db"

var (
	mu    sync.Mutex
	pools map[string]*sql.DB // workspace → *sql.DB
)

func init() {
	pools = make(map[string]*sql.DB)
}

// Open opens (or creates) the shared picoclaw.db inside workspace.
// Multiple calls with the same workspace return the same *sql.DB.
// Different workspaces get independent database connections.
func Open(workspace string) (*sql.DB, error) {
	mu.Lock()
	defer mu.Unlock()

	if db, ok := pools[workspace]; ok {
		return db, nil
	}

	if err := os.MkdirAll(workspace, 0o755); err != nil {
		return nil, fmt.Errorf("store: mkdir %s: %w", workspace, err)
	}

	dbPath := filepath.Join(workspace, dbName)
	db, err := sql.Open("sqlite",
		dbPath+"?_pragma=journal_mode(wal)&_pragma=busy_timeout(5000)&_pragma=foreign_keys(on)")
	if err != nil {
		return nil, fmt.Errorf("store: open %s: %w", dbPath, err)
	}

	// WAL mode allows concurrent reads; limit max connections to avoid
	// "database is locked" under heavy write contention.
	db.SetMaxOpenConns(4)

	pools[workspace] = db
	return db, nil
}

// Close closes the database for the given workspace.
// Safe to call multiple times or with unknown workspaces.
func Close(workspace string) error {
	mu.Lock()
	defer mu.Unlock()

	db, ok := pools[workspace]
	if !ok {
		return nil
	}
	delete(pools, workspace)
	return db.Close()
}

// CloseAll closes all open databases.  Called on process shutdown.
func CloseAll() {
	mu.Lock()
	defer mu.Unlock()

	for ws, db := range pools {
		db.Close()
		delete(pools, ws)
	}
}

// DBPath returns the full path to the database file.
func DBPath(workspace string) string {
	return filepath.Join(workspace, dbName)
}
