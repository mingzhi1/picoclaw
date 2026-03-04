package kvcache

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func tempDB(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	return filepath.Join(dir, "test_cache.db")
}

func TestBasicSetGet(t *testing.T) {
	s, err := New(tempDB(t))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	// Miss
	if _, ok := s.Get("foo"); ok {
		t.Fatal("expected miss")
	}

	// Set + Get
	s.Set("foo", []byte("bar"), 0)
	v, ok := s.Get("foo")
	if !ok || string(v) != "bar" {
		t.Fatalf("got %q, ok=%v", v, ok)
	}

	// String helpers
	s.SetString("hello", "world", 0)
	sv, ok := s.GetString("hello")
	if !ok || sv != "world" {
		t.Fatalf("got %q, ok=%v", sv, ok)
	}
}

func TestOverwrite(t *testing.T) {
	s, err := New(tempDB(t))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	s.Set("k", []byte("v1"), 0)
	s.Set("k", []byte("v2"), 0)

	v, ok := s.Get("k")
	if !ok || string(v) != "v2" {
		t.Fatalf("expected v2, got %q", v)
	}
}

func TestDelete(t *testing.T) {
	s, err := New(tempDB(t))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	s.Set("k", []byte("v"), 0)
	s.Delete("k")

	if _, ok := s.Get("k"); ok {
		t.Fatal("expected miss after delete")
	}
}

func TestTTLExpiry(t *testing.T) {
	s, err := New(tempDB(t))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	// Set with 1 second TTL
	s.Set("ephemeral", []byte("data"), 1)

	// Should exist immediately
	if _, ok := s.Get("ephemeral"); !ok {
		t.Fatal("expected hit before expiry")
	}

	// Wait for expiry
	time.Sleep(1100 * time.Millisecond)

	// Should be gone (lazy eviction on Get)
	if _, ok := s.Get("ephemeral"); ok {
		t.Fatal("expected miss after TTL")
	}
}

func TestPersistence(t *testing.T) {
	dbPath := tempDB(t)

	// Write and close.
	s1, err := New(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	s1.Set("persistent", []byte("value"), 0)
	s1.Set("will_expire", []byte("gone"), 1)
	s1.Close()

	// Wait for TTL to pass.
	time.Sleep(1100 * time.Millisecond)

	// Reopen — persistent entry should survive, expired should not.
	s2, err := New(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer s2.Close()

	v, ok := s2.Get("persistent")
	if !ok || string(v) != "value" {
		t.Fatalf("expected persistent value, got %q ok=%v", v, ok)
	}

	if _, ok := s2.Get("will_expire"); ok {
		t.Fatal("expired entry should not survive reopen")
	}
}

func TestCleanup(t *testing.T) {
	s, err := New(tempDB(t))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	s.Set("keep", []byte("a"), 0)
	s.Set("expire1", []byte("b"), 1)
	s.Set("expire2", []byte("c"), 1)

	time.Sleep(1100 * time.Millisecond)

	removed := s.Cleanup()
	if removed != 2 {
		t.Fatalf("expected 2 removed, got %d", removed)
	}
	if s.Len() != 1 {
		t.Fatalf("expected 1 remaining, got %d", s.Len())
	}
}

func TestHasAndKeys(t *testing.T) {
	s, err := New(tempDB(t))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	s.Set("a", []byte("1"), 0)
	s.Set("b", []byte("2"), 0)

	if !s.Has("a") {
		t.Fatal("expected Has(a) = true")
	}
	if s.Has("c") {
		t.Fatal("expected Has(c) = false")
	}

	keys := s.Keys()
	if len(keys) != 2 {
		t.Fatalf("expected 2 keys, got %d", len(keys))
	}
}

func TestNewCreatesDir(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "sub", "deep", "cache.db")

	// Parent dirs don't exist — should fail since SQLite doesn't create dirs.
	// This verifies the user must ensure the directory exists.
	_, err := New(dbPath)
	if err == nil {
		// If the driver auto-creates dirs, that's fine too — just verify it works.
		return
	}

	// Create dirs and retry.
	os.MkdirAll(filepath.Dir(dbPath), 0o755)
	s, err := New(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	s.Set("test", []byte("ok"), 0)
	v, _ := s.Get("test")
	if string(v) != "ok" {
		t.Fatal("unexpected value after dir creation")
	}
}
