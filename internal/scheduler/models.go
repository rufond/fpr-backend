package scheduler

import (
	"time"

	"github.com/robfig/cron/v3"
)

const (
	RunSourceSchedule = "schedule"
	RunSourceManual   = "manual"

	RunStatusRunning   = "running"
	RunStatusCompleted = "completed"
	RunStatusFailed    = "failed"
	RunStatusNoop      = "noop"
)

type JobInfo struct {
	ID      cron.EntryID `json:"id"`
	Key     string       `json:"key"`
	Name    string       `json:"name"`
	Spec    string       `json:"spec"`
	Running bool         `json:"running"`
	Prev    *time.Time   `json:"prev"`
	Next    *time.Time   `json:"next"`
}

type RunJobRequest struct {
	Key string `json:"key"`
}

type JobRun struct {
	ID         int64          `json:"id" db:"id"`
	JobKey     string         `json:"job_key" db:"job_key"`
	RunSource  string         `json:"run_source" db:"run_source"`
	Status     string         `json:"status" db:"status"`
	Summary    map[string]any `json:"summary" db:"summary"`
	Error      string         `json:"error" db:"error"`
	StartedAt  time.Time      `json:"started_at" db:"started_at"`
	FinishedAt *time.Time     `json:"finished_at" db:"finished_at"`
}

type RunJobResult struct {
	RunID   int64          `json:"run_id"`
	Key     string         `json:"key"`
	Status  string         `json:"status"`
	Summary map[string]any `json:"summary,omitempty"`
	Error   string         `json:"error,omitempty"`
}

type JobResult struct {
	Status  string
	Summary map[string]any
}

func JobCompleted(summary map[string]any) *JobResult {
	return &JobResult{
		Status:  RunStatusCompleted,
		Summary: summary,
	}
}

func JobNoop(summary map[string]any) *JobResult {
	return &JobResult{
		Status:  RunStatusNoop,
		Summary: summary,
	}
}
