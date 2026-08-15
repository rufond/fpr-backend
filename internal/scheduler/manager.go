package scheduler

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/robfig/cron/v3"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"

	"github.com/rufond/fpr-backend/internal/realtime"
)

type JobFunc func(ctx context.Context, logger zerolog.Logger) (*JobResult, error)

type registeredJob struct {
	id   cron.EntryID
	key  string
	name string
	spec string
	fn   JobFunc
}

type Manager struct {
	cron       *cron.Cron
	repository *Repository

	mu       sync.Mutex
	jobs     map[string]*registeredJob
	running  map[string]bool
	realtime realtime.Publisher

	ctx context.Context
}

func NewManager(repository *Repository, realtimePublisher realtime.Publisher) *Manager {
	cronLogger := CronLogger{}
	if realtimePublisher == nil {
		realtimePublisher = realtime.DiscardPublisher{}
	}

	return &Manager{
		cron: cron.New(
			cron.WithChain(
				cron.Recover(cronLogger),
				cron.SkipIfStillRunning(cronLogger),
			),
			cron.WithLogger(cronLogger),
			cron.WithLocation(time.UTC),
		),
		repository: repository,
		realtime:   realtimePublisher,
		jobs:       map[string]*registeredJob{},
		running:    map[string]bool{},
		ctx:        context.Background(),
	}
}

func (m *Manager) Add(key string, name string, spec string, fn JobFunc) error {
	if key == "" {
		return fmt.Errorf("scheduler job key is empty")
	}

	if fn == nil {
		return fmt.Errorf("scheduler job %s func is nil", key)
	}

	m.mu.Lock()
	if _, ok := m.jobs[key]; ok {
		m.mu.Unlock()
		return fmt.Errorf("scheduler job %s already registered", key)
	}
	m.mu.Unlock()

	id, err := m.cron.AddFunc(spec, func() {
		_, errRun := m.run(m.ctx, key, RunSourceSchedule)
		if errRun != nil {
			if errors.Is(errRun, ErrJobAlreadyRunning) {
				log.Warn().
					Str("job_key", key).
					Msg("scheduler job skipped because it is already running")
				return
			}

			log.Error().
				Err(errRun).
				Str("job_key", key).
				Msg("scheduler job failed")
		}
	})
	if err != nil {
		return fmt.Errorf("register scheduler job %s: %w", key, err)
	}

	m.mu.Lock()
	m.jobs[key] = &registeredJob{
		id:   id,
		key:  key,
		name: name,
		spec: spec,
		fn:   fn,
	}
	m.mu.Unlock()

	return nil
}

func (m *Manager) MustAdd(key string, name string, spec string, fn JobFunc) {
	if err := m.Add(key, name, spec, fn); err != nil {
		log.Panic().
			Err(err).
			Str("job_key", key).
			Msg("register scheduler job")
	}
}

func (m *Manager) Start(ctx context.Context) {
	if ctx != nil {
		m.ctx = ctx
	}

	m.cron.Start()
}

func (m *Manager) Stop() {
	ctx := m.cron.Stop()
	<-ctx.Done()
}

func (m *Manager) Jobs() []JobInfo {
	entries := m.cron.Entries()

	entryByID := make(map[cron.EntryID]cron.Entry, len(entries))
	for _, entry := range entries {
		entryByID[entry.ID] = entry
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	result := make([]JobInfo, 0, len(m.jobs))

	for _, job := range m.jobs {
		info := JobInfo{
			ID:      job.id,
			Key:     job.key,
			Name:    job.name,
			Spec:    job.spec,
			Running: m.running[job.key],
		}

		if entry, ok := entryByID[job.id]; ok {
			if !entry.Prev.IsZero() {
				info.Prev = new(entry.Prev.UTC())
			}

			if !entry.Next.IsZero() {
				info.Next = new(entry.Next.UTC())
			}
		}

		result = append(result, info)
	}

	return result
}

func (m *Manager) RunNow(ctx context.Context, key string) (*RunJobResult, error) {
	return m.run(ctx, key, RunSourceManual)
}

func (m *Manager) run(
	ctx context.Context,
	key string,
	runSource string,
) (result *RunJobResult, err error) {
	if ctx == nil {
		ctx = context.Background()
	}

	job, errLock := m.lockJob(key)
	if errLock != nil {
		return nil, errLock
	}

	previousRun, errPreviousRun := m.repository.LatestFinishedRun(ctx, key)
	previousRunKnown := errPreviousRun == nil
	if errPreviousRun != nil {
		log.Warn().Err(errPreviousRun).Str("job_key", key).Msg("read previous scheduler run for realtime diagnostics")
	}

	runFinished := false

	defer func() {
		m.unlockJob(key)

		if result == nil {
			return
		}

		scopes := []string{realtime.ScopeScheduler}
		if runFinished && schedulerDiagnosticsChanged(previousRun, result.Status, previousRunKnown) {
			scopes = append(scopes, realtime.ScopeDiagnostics)
		}

		m.realtime.Publish(realtime.Update{Scopes: scopes})
	}()

	runID, errCreateRun := m.repository.CreateRun(ctx, key, runSource)
	if errCreateRun != nil {
		return nil, errCreateRun
	}

	writer := NewRunWriter(
		5*time.Second,
		func(messages []map[string]any) error {
			return m.repository.UpdateRunMessages(context.Background(), runID, messages)
		},
	)

	logger := log.Logger.With().
		Str("scheduler_job", key).
		Int64("scheduler_run_id", runID).
		Logger().
		Output(writer)

	status := RunStatusCompleted
	errorText := ""
	summary := map[string]any(nil)

	var jobResult *JobResult

	defer func() {
		if value := recover(); value != nil {
			err = panicToError(value)
		}

		if err != nil {
			status = RunStatusFailed
			errorText = err.Error()

			logger.Error().
				Err(err).
				Msg("scheduler job failed")
		} else {
			if jobResult == nil {
				jobResult = JobCompleted(nil)
			}

			status = jobResult.Status
			if status == "" {
				status = RunStatusCompleted
			}

			summary = jobResult.Summary
		}

		if flushErr := writer.Flush(); flushErr != nil {
			log.Error().
				Err(flushErr).
				Int64("scheduler_run_id", runID).
				Str("job_key", key).
				Msg("flush scheduler job run messages")
		}

		if finishErr := m.repository.FinishRun(
			context.Background(),
			runID,
			status,
			writer.Messages(),
			summary,
			errorText,
		); finishErr != nil {
			log.Error().
				Err(finishErr).
				Int64("scheduler_run_id", runID).
				Str("job_key", key).
				Msg("finish scheduler job run")
		} else {
			runFinished = true
		}

		m.logRunFinished(key, runID, runSource, status, summary, errorText)

		result = &RunJobResult{
			RunID:   runID,
			Key:     key,
			Status:  status,
			Summary: summary,
			Error:   errorText,
		}
	}()

	jobResult, err = job.fn(ctx, logger)
	if err != nil {
		return result, err
	}

	return result, nil
}

func schedulerDiagnosticsChanged(previousRun *JobRun, currentStatus string, previousRunKnown bool) bool {
	if !previousRunKnown {
		return true
	}

	if currentStatus == RunStatusFailed {
		return true
	}

	return previousRun != nil && previousRun.Status == RunStatusFailed
}

func (m *Manager) logRunFinished(
	key string,
	runID int64,
	runSource string,
	status string,
	summary map[string]any,
	errorText string,
) {
	event := log.Info()

	if status == RunStatusFailed {
		event = log.Error()
	}

	if status == RunStatusNoop && runSource == RunSourceSchedule {
		event = log.Debug()
	}

	event = event.
		Str("job_key", key).
		Int64("run_id", runID).
		Str("run_source", runSource).
		Str("status", status)

	if errorText != "" {
		event = event.Str("error", errorText)
	}

	if summary != nil {
		event = event.Interface("summary", summary)
	}

	event.Msg("scheduler job finished")
}

func (m *Manager) lockJob(key string) (*registeredJob, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	job, ok := m.jobs[key]
	if !ok {
		return nil, ErrJobNotFound
	}

	if m.running[key] {
		return nil, ErrJobAlreadyRunning
	}

	m.running[key] = true

	return job, nil
}

func (m *Manager) unlockJob(key string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.running[key] = false
}
