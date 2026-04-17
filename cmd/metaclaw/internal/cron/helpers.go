package cron

import (
	"database/sql"
	"fmt"
	"time"

	"github.com/mingzhi1/metaclaw/pkg/infra/cron"
	"github.com/mingzhi1/metaclaw/pkg/infra/store"
)

func openCronDB(workspace string) *sql.DB {
	db, err := store.Open(workspace)
	if err != nil {
		fmt.Printf("Error opening store: %v\n", err)
		return nil
	}
	return db
}

func cronListCmd(workspace string) {
	db := openCronDB(workspace)
	cs := cron.NewCronService(db, nil)
	jobs := cs.ListJobs(true) // Show all jobs, including disabled

	if len(jobs) == 0 {
		fmt.Println("No scheduled jobs.")
		return
	}

	fmt.Println("\nScheduled Jobs:")
	fmt.Println("----------------")
	for _, job := range jobs {
		var schedule string
		if job.Schedule.Kind == "every" && job.Schedule.EveryMS != nil {
			schedule = fmt.Sprintf("every %ds", *job.Schedule.EveryMS/1000)
		} else if job.Schedule.Kind == "cron" {
			schedule = job.Schedule.Expr
		} else {
			schedule = "one-time"
		}

		nextRun := "scheduled"
		if job.State.NextRunAtMS != nil {
			nextTime := time.UnixMilli(*job.State.NextRunAtMS)
			nextRun = nextTime.Format("2006-01-02 15:04")
		}

		status := "enabled"
		if !job.Enabled {
			status = "disabled"
		}

		fmt.Printf("  %s (%s)\n", job.Name, job.ID)
		fmt.Printf("    Schedule: %s\n", schedule)
		fmt.Printf("    Status: %s\n", status)
		fmt.Printf("    Next run: %s\n", nextRun)
	}
}

func cronRemoveCmd(workspace, jobID string) {
	db := openCronDB(workspace)
	cs := cron.NewCronService(db, nil)
	if cs.RemoveJob(jobID) {
		fmt.Printf("✓ Removed job %s\n", jobID)
	} else {
		fmt.Printf("✗ Job %s not found\n", jobID)
	}
}

func cronSetJobEnabled(workspace, jobID string, enabled bool) {
	db := openCronDB(workspace)
	cs := cron.NewCronService(db, nil)
	job := cs.EnableJob(jobID, enabled)
	if job != nil {
		fmt.Printf("✓ Job '%s' enabled\n", job.Name)
	} else {
		fmt.Printf("✗ Job %s not found\n", jobID)
	}
}
