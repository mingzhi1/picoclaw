package agent

import (
	"database/sql"
	"strings"
	"testing"

	_ "modernc.org/sqlite"
)

func newTestFactStore(t *testing.T) *FactStore {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:?_pragma=journal_mode(wal)&_pragma=busy_timeout(3000)")
	if err != nil {
		t.Fatalf("open test db: %v", err)
	}
	fs, err := NewFactStore(db)
	if err != nil {
		t.Fatalf("NewFactStore: %v", err)
	}
	t.Cleanup(func() { fs.Close() })
	return fs
}

func TestFactStore_UpsertState(t *testing.T) {
	fs := newTestFactStore(t)

	// First insert.
	f1, err := fs.Upsert("parseConfig", "latency", "850ns", FactState, "")
	if err != nil {
		t.Fatal(err)
	}
	if f1.ID == 0 {
		t.Error("expected non-zero ID")
	}
	if f1.Value != "850ns" {
		t.Errorf("value = %q, want 850ns", f1.Value)
	}

	// Overwrite with new value.
	f2, err := fs.Upsert("parseConfig", "latency", "420ns", FactState, "")
	if err != nil {
		t.Fatal(err)
	}
	if f2.ID == f1.ID {
		t.Error("overwrite should create new ID")
	}
	if f2.Value != "420ns" {
		t.Errorf("value = %q, want 420ns", f2.Value)
	}

	// Old fact should be superseded.
	active, _ := fs.GetByEntity("parseConfig")
	if len(active) != 1 {
		t.Fatalf("expected 1 active fact, got %d", len(active))
	}
	if active[0].Value != "420ns" {
		t.Errorf("active value = %q, want 420ns", active[0].Value)
	}
}

func TestFactStore_UpsertAppend(t *testing.T) {
	fs := newTestFactStore(t)

	// Append type: values accumulate.
	_, err := fs.Upsert("optimizer", "techniques", "sync.Pool", FactAppend, "")
	if err != nil {
		t.Fatal(err)
	}
	_, err = fs.Upsert("optimizer", "techniques", "type switch", FactAppend, "")
	if err != nil {
		t.Fatal(err)
	}

	active, _ := fs.GetByEntity("optimizer")
	if len(active) != 2 {
		t.Fatalf("expected 2 active append facts, got %d", len(active))
	}
}

func TestFactStore_UpsertEvent(t *testing.T) {
	fs := newTestFactStore(t)

	f1, _ := fs.Upsert("bug#42", "status", "open", FactEvent, "topic1")
	f2, _ := fs.Upsert("bug#42", "status", "fixed", FactEvent, "topic1")

	if f2.ID == f1.ID {
		t.Error("event overwrite should create new ID")
	}

	active, _ := fs.GetByEntity("bug#42")
	if len(active) != 1 {
		t.Fatalf("expected 1 active event, got %d", len(active))
	}
	if active[0].Value != "fixed" {
		t.Errorf("value = %q, want fixed", active[0].Value)
	}
}

func TestFactStore_GetActive_ByTopic(t *testing.T) {
	fs := newTestFactStore(t)

	fs.Upsert("config", "port", "8080", FactState, "topicA")
	fs.Upsert("config", "host", "localhost", FactState, "topicB")
	fs.Upsert("config", "env", "prod", FactState, "") // global

	topicA, _ := fs.GetActive("topicA")
	if len(topicA) != 1 || topicA[0].Key != "port" {
		t.Errorf("topicA facts = %d, want 1 with key=port", len(topicA))
	}

	global, _ := fs.GetGlobalFacts()
	if len(global) != 1 || global[0].Key != "env" {
		t.Errorf("global facts = %d, want 1 with key=env", len(global))
	}
}

func TestFactStore_GetHistory(t *testing.T) {
	fs := newTestFactStore(t)

	fs.Upsert("svc", "latency", "850ns", FactState, "")
	fs.Upsert("svc", "latency", "420ns", FactState, "")
	fs.Upsert("svc", "latency", "380ns", FactState, "")

	history, _ := fs.GetHistory("svc", "latency")
	if len(history) != 3 {
		t.Fatalf("expected 3 history entries, got %d", len(history))
	}
	// Most recent first.
	if history[0].Value != "380ns" {
		t.Errorf("history[0] = %q, want 380ns", history[0].Value)
	}
	if history[2].Value != "850ns" {
		t.Errorf("history[2] = %q, want 850ns", history[2].Value)
	}
}

func TestFactStore_FormatForContext(t *testing.T) {
	fs := newTestFactStore(t)

	fs.Upsert("svc", "latency", "380ns", FactState, "")
	fs.Upsert("svc", "status", "running", FactEvent, "")
	fs.Upsert("optimizer", "techniques", "sync.Pool", FactAppend, "")

	ctx := fs.FormatForContext("")
	if ctx == "" {
		t.Error("expected non-empty context")
	}
	if !strings.Contains(ctx, "svc") || !strings.Contains(ctx, "380ns") {
		t.Error("context should contain entity and values")
	}
}
