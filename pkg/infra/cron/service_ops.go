package cron

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"time"

	"github.com/adhocore/gronx"
)

func (cs *CronService) computeNextRun(schedule *CronSchedule, nowMS int64) *int64 {
	if schedule.Kind == "at" {
		if schedule.AtMS != nil && *schedule.AtMS > nowMS {
			return schedule.AtMS
		}
		return nil
	}

	if schedule.Kind == "every" {
		if schedule.EveryMS == nil || *schedule.EveryMS <= 0 {
			return nil
		}
		next := nowMS + *schedule.EveryMS
		return &next
	}

	if schedule.Kind == "cron" {
		if schedule.Expr == "" {
			return nil
		}

		now := time.UnixMilli(nowMS)
		nextTime, err := gronx.NextTickAfter(schedule.Expr, now, false)
		if err != nil {
			log.Printf("[cron] failed to compute next run for expr '%s': %v", schedule.Expr, err)
			return nil
		}

		nextMS := nextTime.UnixMilli()
		return &nextMS
	}

	return nil
}

func (cs *CronService) recomputeNextRuns() {
	now := time.Now().UnixMilli()
	for i := range cs.jobs {
		job := &cs.jobs[i]
		if job.Enabled {
			job.State.NextRunAtMS = cs.computeNextRun(&job.Schedule, now)
		}
	}
}

func (cs *CronService) getNextWakeMS() *int64 {
	var nextWake *int64
	for _, job := range cs.jobs {
		if job.Enabled && job.State.NextRunAtMS != nil {
			if nextWake == nil || *job.State.NextRunAtMS < *nextWake {
				nextWake = job.State.NextRunAtMS
			}
		}
	}
	return nextWake
}

func (cs *CronService) Load() error {
	cs.mu.Lock()
	defer cs.mu.Unlock()
	cs.loadJobs()
	return nil
}

func (cs *CronService) SetOnJob(handler JobHandler) {
	cs.mu.Lock()
	defer cs.mu.Unlock()
	cs.onJob = handler
}

// loadJobs loads all cron jobs from SQLite into memory.
func (cs *CronService) loadJobs() {
	cs.jobs = []CronJob{}

	if cs.db == nil {
		return
	}

	rows, err := cs.db.Query(`
		SELECT id, name, enabled, schedule, payload,
		       next_run_at_ms, last_run_at_ms, last_status, last_error,
		       created_at_ms, updated_at_ms, delete_after_run
		FROM cron_jobs`)
	if err != nil {
		return
	}
	defer rows.Close()

	for rows.Next() {
		var job CronJob
		var schedJSON, payloadJSON string
		var enabled, deleteAfterRun int
		var nextRunMS, lastRunMS *int64

		if err := rows.Scan(
			&job.ID, &job.Name, &enabled, &schedJSON, &payloadJSON,
			&nextRunMS, &lastRunMS, &job.State.LastStatus, &job.State.LastError,
			&job.CreatedAtMS, &job.UpdatedAtMS, &deleteAfterRun,
		); err != nil {
			continue
		}

		job.Enabled = enabled != 0
		job.DeleteAfterRun = deleteAfterRun != 0
		job.State.NextRunAtMS = nextRunMS
		job.State.LastRunAtMS = lastRunMS
		json.Unmarshal([]byte(schedJSON), &job.Schedule)
		json.Unmarshal([]byte(payloadJSON), &job.Payload)

		cs.jobs = append(cs.jobs, job)
	}
}

// saveAllJobsUnsafe persists the in-memory job slice to SQLite via UPSERT.
// Must be called with lock held.
func (cs *CronService) saveAllJobsUnsafe() {
	if cs.db == nil {
		return
	}

	for i := range cs.jobs {
		if err := cs.saveJobUnsafe(&cs.jobs[i]); err != nil {
			log.Printf("[cron] failed to save job %s: %v", cs.jobs[i].ID, err)
		}
	}
}

// saveJobUnsafe persists a single job to SQLite via UPSERT.
func (cs *CronService) saveJobUnsafe(job *CronJob) error {
	if cs.db == nil {
		return nil
	}

	schedJSON, _ := json.Marshal(job.Schedule)
	payloadJSON, _ := json.Marshal(job.Payload)

	enabled := 0
	if job.Enabled {
		enabled = 1
	}
	dar := 0
	if job.DeleteAfterRun {
		dar = 1
	}

	_, err := cs.db.Exec(`
		INSERT INTO cron_jobs (id, name, enabled, schedule, payload,
		    next_run_at_ms, last_run_at_ms, last_status, last_error,
		    created_at_ms, updated_at_ms, delete_after_run)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
		    name=excluded.name, enabled=excluded.enabled,
		    schedule=excluded.schedule, payload=excluded.payload,
		    next_run_at_ms=excluded.next_run_at_ms,
		    last_run_at_ms=excluded.last_run_at_ms,
		    last_status=excluded.last_status,
		    last_error=excluded.last_error,
		    updated_at_ms=excluded.updated_at_ms,
		    delete_after_run=excluded.delete_after_run`,
		job.ID, job.Name, enabled, string(schedJSON), string(payloadJSON),
		job.State.NextRunAtMS, job.State.LastRunAtMS,
		job.State.LastStatus, job.State.LastError,
		job.CreatedAtMS, job.UpdatedAtMS, dar,
	)
	return err
}

func (cs *CronService) AddJob(
	name string,
	schedule CronSchedule,
	message string,
	deliver bool,
	channel, to string,
) (*CronJob, error) {
	cs.mu.Lock()
	defer cs.mu.Unlock()

	now := time.Now().UnixMilli()

	// One-time tasks (at) should be deleted after execution
	deleteAfterRun := (schedule.Kind == "at")

	job := CronJob{
		ID:       generateID(),
		Name:     name,
		Enabled:  true,
		Schedule: schedule,
		Payload: CronPayload{
			Kind:    "agent_turn",
			Message: message,
			Deliver: deliver,
			Channel: channel,
			To:      to,
		},
		State: CronJobState{
			NextRunAtMS: cs.computeNextRun(&schedule, now),
		},
		CreatedAtMS:    now,
		UpdatedAtMS:    now,
		DeleteAfterRun: deleteAfterRun,
	}

	cs.jobs = append(cs.jobs, job)
	if err := cs.saveJobUnsafe(&job); err != nil {
		return nil, err
	}

	return &job, nil
}

func (cs *CronService) UpdateJob(job *CronJob) error {
	cs.mu.Lock()
	defer cs.mu.Unlock()

	for i := range cs.jobs {
		if cs.jobs[i].ID == job.ID {
			cs.jobs[i] = *job
			cs.jobs[i].UpdatedAtMS = time.Now().UnixMilli()
			return cs.saveJobUnsafe(&cs.jobs[i])
		}
	}
	return fmt.Errorf("job not found")
}

func (cs *CronService) RemoveJob(jobID string) bool {
	cs.mu.Lock()
	defer cs.mu.Unlock()

	return cs.removeJobUnsafe(jobID)
}

func (cs *CronService) removeJobUnsafe(jobID string) bool {
	before := len(cs.jobs)
	var jobs []CronJob
	for _, job := range cs.jobs {
		if job.ID != jobID {
			jobs = append(jobs, job)
		}
	}
	cs.jobs = jobs
	removed := len(cs.jobs) < before

	if removed && cs.db != nil {
		if _, err := cs.db.Exec(`DELETE FROM cron_jobs WHERE id = ?`, jobID); err != nil {
			log.Printf("[cron] failed to delete job from db: %v", err)
		}
	}

	return removed
}

func (cs *CronService) EnableJob(jobID string, enabled bool) *CronJob {
	cs.mu.Lock()
	defer cs.mu.Unlock()

	for i := range cs.jobs {
		job := &cs.jobs[i]
		if job.ID == jobID {
			job.Enabled = enabled
			job.UpdatedAtMS = time.Now().UnixMilli()

			if enabled {
				job.State.NextRunAtMS = cs.computeNextRun(&job.Schedule, time.Now().UnixMilli())
			} else {
				job.State.NextRunAtMS = nil
			}

			if err := cs.saveJobUnsafe(job); err != nil {
				log.Printf("[cron] failed to save job after enable: %v", err)
			}
			return job
		}
	}

	return nil
}

func (cs *CronService) ListJobs(includeDisabled bool) []CronJob {
	cs.mu.RLock()
	defer cs.mu.RUnlock()

	if includeDisabled {
		return cs.jobs
	}

	var enabled []CronJob
	for _, job := range cs.jobs {
		if job.Enabled {
			enabled = append(enabled, job)
		}
	}

	return enabled
}

func (cs *CronService) Status() map[string]any {
	cs.mu.RLock()
	defer cs.mu.RUnlock()

	var enabledCount int
	for _, job := range cs.jobs {
		if job.Enabled {
			enabledCount++
		}
	}

	return map[string]any{
		"enabled":      cs.running,
		"jobs":         len(cs.jobs),
		"nextWakeAtMS": cs.getNextWakeMS(),
	}
}

func generateID() string {
	// Use crypto/rand for better uniqueness under concurrent access
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		// Fallback to time-based if crypto/rand fails
		return fmt.Sprintf("%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(b)
}
