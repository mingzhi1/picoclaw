// Package kvcache provides a lightweight persistent KV cache.
//
// Architecture: write-through cache with in-memory map + SQLite backend.
//   - Reads:  served from memory (map + sync.RWMutex) — O(1), zero I/O
//   - Writes: go to both memory and SQLite (write-through)
//   - Startup: all non-expired entries are loaded from SQLite into the map
//   - TTL: lazy eviction on Get; bulk Cleanup for background sweep
//
// This reuses the project's existing sqlite3 driver — zero new dependencies.
package kvcache

import (
	"database/sql"
	"sync"
	"time"

	_ "modernc.org/sqlite"
)

// entry holds one cached value with optional expiry.
type entry struct {
	Value     []byte
	ExpiresAt int64 // unix seconds; 0 = no expiry
}

// Store is a persistent KV cache backed by SQLite + in-memory map.
type Store struct {
	mu    sync.RWMutex
	items map[string]entry
	db    *sql.DB
}

// New opens (or creates) a KV cache at dbPath.
// All non-expired entries are loaded into memory on open.
func New(dbPath string) (*Store, error) {
	db, err := sql.Open("sqlite", dbPath+"?_pragma=journal_mode(wal)&_pragma=busy_timeout(3000)")
	if err != nil {
		return nil, err
	}

	if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS kv (
		key        TEXT PRIMARY KEY,
		value      BLOB    NOT NULL,
		expires_at INTEGER NOT NULL DEFAULT 0
	)`); err != nil {
		db.Close()
		return nil, err
	}

	s := &Store{
		items: make(map[string]entry),
		db:    db,
	}

	if err := s.loadAll(); err != nil {
		db.Close()
		return nil, err
	}

	return s, nil
}

// loadAll reads non-expired entries from SQLite into the in-memory map.
func (s *Store) loadAll() error {
	now := time.Now().Unix()

	// Delete expired rows first, then load the rest.
	if _, err := s.db.Exec(
		`DELETE FROM kv WHERE expires_at > 0 AND expires_at <= ?`, now,
	); err != nil {
		return err
	}

	rows, err := s.db.Query(`SELECT key, value, expires_at FROM kv`)
	if err != nil {
		return err
	}
	defer rows.Close()

	for rows.Next() {
		var k string
		var v []byte
		var exp int64
		if err := rows.Scan(&k, &v, &exp); err != nil {
			continue
		}
		s.items[k] = entry{Value: v, ExpiresAt: exp}
	}
	return rows.Err()
}

// Get returns the value for key, or (nil, false) if missing/expired.
func (s *Store) Get(key string) ([]byte, bool) {
	s.mu.RLock()
	e, ok := s.items[key]
	s.mu.RUnlock()

	if !ok {
		return nil, false
	}

	// Lazy expiry check.
	if e.ExpiresAt > 0 && time.Now().Unix() >= e.ExpiresAt {
		s.Delete(key)
		return nil, false
	}

	return e.Value, true
}

// GetString is a convenience wrapper that returns a string.
func (s *Store) GetString(key string) (string, bool) {
	v, ok := s.Get(key)
	if !ok {
		return "", false
	}
	return string(v), true
}

// Set stores a value. ttlSeconds=0 means no expiry.
func (s *Store) Set(key string, value []byte, ttlSeconds int64) {
	var expiresAt int64
	if ttlSeconds > 0 {
		expiresAt = time.Now().Unix() + ttlSeconds
	}

	e := entry{Value: value, ExpiresAt: expiresAt}

	// Memory first (fast path for concurrent readers).
	s.mu.Lock()
	s.items[key] = e
	s.mu.Unlock()

	// Persist to SQLite (best-effort; memory is source of truth while running).
	s.db.Exec(
		`INSERT INTO kv (key, value, expires_at) VALUES (?, ?, ?)
		 ON CONFLICT(key) DO UPDATE SET value=excluded.value, expires_at=excluded.expires_at`,
		key, value, expiresAt,
	)
}

// SetString is a convenience wrapper.
func (s *Store) SetString(key, value string, ttlSeconds int64) {
	s.Set(key, []byte(value), ttlSeconds)
}

// Delete removes a key from both memory and SQLite.
func (s *Store) Delete(key string) {
	s.mu.Lock()
	delete(s.items, key)
	s.mu.Unlock()

	s.db.Exec(`DELETE FROM kv WHERE key = ?`, key)
}

// Has returns true if the key exists and is not expired.
func (s *Store) Has(key string) bool {
	_, ok := s.Get(key)
	return ok
}

// Len returns the number of (possibly stale) entries in memory.
func (s *Store) Len() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.items)
}

// Keys returns all non-expired keys. Performs lazy eviction.
func (s *Store) Keys() []string {
	now := time.Now().Unix()
	s.mu.RLock()
	defer s.mu.RUnlock()

	keys := make([]string, 0, len(s.items))
	for k, e := range s.items {
		if e.ExpiresAt > 0 && now >= e.ExpiresAt {
			continue // skip expired, will be cleaned up later
		}
		keys = append(keys, k)
	}
	return keys
}

// Cleanup removes all expired entries from both memory and SQLite.
// Safe to call from a background goroutine or cron job.
func (s *Store) Cleanup() int {
	now := time.Now().Unix()
	var expired []string

	s.mu.Lock()
	for k, e := range s.items {
		if e.ExpiresAt > 0 && now >= e.ExpiresAt {
			expired = append(expired, k)
			delete(s.items, k)
		}
	}
	s.mu.Unlock()

	if len(expired) > 0 {
		s.db.Exec(`DELETE FROM kv WHERE expires_at > 0 AND expires_at <= ?`, now)
	}

	return len(expired)
}

// Close flushes and closes the underlying database.
func (s *Store) Close() error {
	if s.db != nil {
		return s.db.Close()
	}
	return nil
}
