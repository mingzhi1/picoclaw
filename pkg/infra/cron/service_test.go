package cron

import (
	"database/sql"
	"path/filepath"
	"testing"

	_ "modernc.org/sqlite"
)

func testDB(t *testing.T) *sql.DB {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "test.db")
	db, err := sql.Open("sqlite", dbPath+"?_pragma=journal_mode(wal)&_pragma=busy_timeout(3000)")
	if err != nil {
		t.Fatalf("Failed to open test db: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func TestAddJob(t *testing.T) {
	db := testDB(t)
	cs := NewCronService(db, nil)

	_, err := cs.AddJob("test", CronSchedule{Kind: "every", EveryMS: int64Ptr(60000)}, "hello", false, "cli", "direct")
	if err != nil {
		t.Fatalf("AddJob failed: %v", err)
	}

	jobs := cs.ListJobs(true)
	if len(jobs) != 1 {
		t.Fatalf("expected 1 job, got %d", len(jobs))
	}

	if jobs[0].Name != "test" {
		t.Errorf("expected job name 'test', got %q", jobs[0].Name)
	}
}

func TestJobPersistence(t *testing.T) {
	db := testDB(t)
	cs := NewCronService(db, nil)

	_, err := cs.AddJob("persistent", CronSchedule{Kind: "every", EveryMS: int64Ptr(30000)}, "ping", false, "cli", "direct")
	if err != nil {
		t.Fatalf("AddJob failed: %v", err)
	}

	// Load into a new service to verify persistence
	cs2 := NewCronService(db, nil)
	jobs := cs2.ListJobs(true)
	if len(jobs) != 1 {
		t.Fatalf("expected 1 job after reload, got %d", len(jobs))
	}
	if jobs[0].Name != "persistent" {
		t.Errorf("expected job name 'persistent', got %q", jobs[0].Name)
	}
}

func TestRemoveJob(t *testing.T) {
	db := testDB(t)
	cs := NewCronService(db, nil)

	job, err := cs.AddJob("to-remove", CronSchedule{Kind: "every", EveryMS: int64Ptr(60000)}, "hello", false, "cli", "direct")
	if err != nil {
		t.Fatalf("AddJob failed: %v", err)
	}

	if !cs.RemoveJob(job.ID) {
		t.Fatal("RemoveJob returned false")
	}

	jobs := cs.ListJobs(true)
	if len(jobs) != 0 {
		t.Fatalf("expected 0 jobs after remove, got %d", len(jobs))
	}
}

func TestNilDB(t *testing.T) {
	cs := NewCronService(nil, nil)

	_, err := cs.AddJob("test", CronSchedule{Kind: "every", EveryMS: int64Ptr(60000)}, "hello", false, "cli", "direct")
	if err != nil {
		t.Fatalf("AddJob with nil db should succeed: %v", err)
	}

	jobs := cs.ListJobs(true)
	if len(jobs) != 1 {
		t.Fatalf("expected 1 job in memory, got %d", len(jobs))
	}
}

func int64Ptr(v int64) *int64 {
	return &v
}
