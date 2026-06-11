// Package scheduler manages recurring tasks backed by cron expressions.
// Each node in a cluster loads all enabled tasks and uses NATS KV de-dup
// to ensure each task fires exactly once per scheduled minute across nodes.
package scheduler

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/robfig/cron/v3"
	eventspkg "github.com/xact-iot/xact/events"
	"github.com/xact-iot/xact/sqldb"
)

// Engine manages the lifecycle of all scheduled tasks.
type Engine struct {
	db               sqldb.DB
	nc               *nats.Conn
	cr               *cron.Cron
	jobs             map[string]cron.EntryID // task UUID → cron entry ID
	mu               sync.Mutex
	allowUnsafeTasks bool
}

type EngineOptions struct {
	AllowUnsafeTasks bool
}

// New creates a new scheduler Engine. Call Load() to register tasks.
func New(db sqldb.DB, nc *nats.Conn) *Engine {
	return NewWithOptions(db, nc, EngineOptions{})
}

// NewWithOptions creates a scheduler Engine with explicit execution options.
func NewWithOptions(db sqldb.DB, nc *nats.Conn, opts EngineOptions) *Engine {
	return &Engine{
		db:               db,
		nc:               nc,
		cr:               cron.New(),
		jobs:             make(map[string]cron.EntryID),
		allowUnsafeTasks: opts.AllowUnsafeTasks,
	}
}

func (e *Engine) AllowUnsafeTasks() bool {
	return e != nil && e.allowUnsafeTasks
}

// Load fetches all enabled scheduled tasks for all organisations and registers
// them with the cron runner. Call once at startup.
func (e *Engine) Load(ctx context.Context) error {
	// Load tasks across all orgs by querying directly - we pass "" as the org
	// sentinel and let the DB implementation handle it.
	// Because the sqldb interface is org-scoped, we fetch per-org.
	// The engine maintains a flat job map keyed by UUID regardless of org.
	tasks, err := e.db.ListScheduledTasks(ctx, "")
	if err != nil {
		return fmt.Errorf("scheduler: loading tasks: %w", err)
	}
	e.markInterruptedRuns(ctx, tasks)
	e.cr.Start()
	for _, t := range tasks {
		if err := e.register(t); err != nil {
			log.Printf("scheduler: skipping task %q (%s): %v", t.Name, t.ID, err)
		}
	}
	return nil
}

// LoadForOrg fetches and registers all enabled tasks for a single organisation.
// Used at startup when tasks can be fetched per-org.
func (e *Engine) LoadForOrg(ctx context.Context, org string) error {
	tasks, err := e.db.ListScheduledTasks(ctx, org)
	if err != nil {
		return fmt.Errorf("scheduler: loading tasks for org %q: %w", org, err)
	}
	e.markInterruptedRuns(ctx, tasks)
	for _, t := range tasks {
		if !t.Enabled {
			continue
		}
		if err := e.register(t); err != nil {
			log.Printf("scheduler: skipping task %q (%s): %v", t.Name, t.ID, err)
		}
	}
	return nil
}

func (e *Engine) markInterruptedRuns(ctx context.Context, tasks []sqldb.ScheduledTask) {
	const message = "Interrupted by server restart"
	now := time.Now()
	for _, task := range tasks {
		if task.LastRunStatus != "running" {
			continue
		}
		runAt := now
		if task.LastRunAt != nil {
			runAt = *task.LastRunAt
		}
		if err := e.db.UpdateScheduledTaskStatus(ctx, task.ID, "error", message, runAt); err != nil {
			log.Printf("scheduler: failed to mark interrupted task %q (%s): %v", task.Name, task.ID, err)
		}
		entries, err := e.db.ListScheduleRunLog(ctx, task.ID, 1000)
		if err != nil {
			log.Printf("scheduler: failed to load run history for interrupted task %q (%s): %v", task.Name, task.ID, err)
			continue
		}
		for _, entry := range entries {
			if entry.Status != "running" {
				continue
			}
			if err := e.db.UpdateScheduleRunLog(ctx, entry.ID, now, "error", message, entry.OutputPath); err != nil {
				log.Printf("scheduler: failed to mark interrupted run log %d for task %q (%s): %v", entry.ID, task.Name, task.ID, err)
			}
		}
	}
}

// Reload removes and re-adds a single task by ID. Call after create or update.
func (e *Engine) Reload(ctx context.Context, org, id string) error {
	t, err := e.db.GetScheduledTask(ctx, org, id)
	if err != nil {
		return err
	}
	if t == nil {
		return fmt.Errorf("task %q not found", id)
	}
	e.Remove(id)
	if !t.Enabled {
		return nil
	}
	return e.register(*t)
}

// Remove unregisters a task from the cron runner. Call after delete.
func (e *Engine) Remove(id string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if eid, ok := e.jobs[id]; ok {
		e.cr.Remove(eid)
		delete(e.jobs, id)
	}
}

// RunNow executes a task immediately, bypassing the de-dup lock.
// Returns the output path (may be empty for shell/yaegi tasks).
func (e *Engine) RunNow(ctx context.Context, org, id string) (string, error) {
	t, err := e.db.GetScheduledTask(ctx, org, id)
	if err != nil {
		return "", err
	}
	if t == nil {
		return "", fmt.Errorf("task %q not found", id)
	}
	return e.execute(ctx, *t, time.Now())
}

// StartNow starts a task immediately in the background, bypassing the de-dup lock.
func (e *Engine) StartNow(ctx context.Context, org, id string) error {
	t, err := e.db.GetScheduledTask(ctx, org, id)
	if err != nil {
		return err
	}
	if t == nil {
		return fmt.Errorf("task %q not found", id)
	}
	go func(task sqldb.ScheduledTask) {
		if _, err := e.execute(context.Background(), task, time.Now()); err != nil {
			log.Printf("scheduler: manual task %q (%s) failed: %v", task.Name, task.ID, err)
		}
	}(*t)
	return nil
}

// Stop gracefully shuts down the cron runner.
func (e *Engine) Stop() {
	ctx := e.cr.Stop()
	<-ctx.Done()
}

// register adds a task to the cron runner.
func (e *Engine) register(t sqldb.ScheduledTask) error {
	taskCopy := t // capture for closure
	eid, err := e.cr.AddFunc(t.Schedule, func() {
		// Build a lock key from the task ID and the current minute epoch so
		// each cron fire gets a unique, short-lived key.
		fireMinute := time.Now().Unix() / 60
		lockKey := fmt.Sprintf("sched.%s.%d", taskCopy.ID, fireMinute)
		if !TryAcquireForFire(lockKey) {
			return // another node is handling this fire
		}
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
		defer cancel()
		if _, err := e.execute(ctx, taskCopy, time.Now()); err != nil {
			log.Printf("scheduler: task %q (%s) failed: %v", taskCopy.Name, taskCopy.ID, err)
		}
	})
	if err != nil {
		return fmt.Errorf("invalid cron schedule %q: %w", t.Schedule, err)
	}
	e.mu.Lock()
	e.jobs[t.ID] = eid
	e.mu.Unlock()
	return nil
}

// execute runs a task, writes the run log, and updates last-run status on the task.
func (e *Engine) execute(ctx context.Context, t sqldb.ScheduledTask, firedAt time.Time) (string, error) {
	// Mark task as running.
	_ = e.db.UpdateScheduledTaskStatus(ctx, t.ID, "running", "", firedAt)

	// Insert a pending run log entry.
	entry := &sqldb.ScheduleRunLog{
		ScheduleID: t.ID,
		OrgName:    t.OrgName,
		FiredAt:    firedAt,
		Status:     "running",
	}
	_ = e.db.AppendScheduleRunLog(ctx, entry)

	// Run the task.
	outputPath, err := Run(ctx, RunContext{
		DB:               e.db,
		NC:               e.nc,
		Org:              t.OrgName,
		Task:             t,
		FiredAt:          firedAt,
		AllowUnsafeTasks: e.allowUnsafeTasks,
		Progress: func(message string) {
			_ = e.db.UpdateScheduledTaskStatus(ctx, t.ID, "running", message, firedAt)
		},
	})

	completedAt := time.Now()
	status := "ok"
	message := ""
	severity := string(eventspkg.Info)
	if err != nil {
		status = "error"
		message = err.Error()
		severity = string(eventspkg.Error)
	}

	// Update run log entry.
	_ = e.db.UpdateScheduleRunLog(ctx, entry.ID, completedAt, status, message, outputPath)

	// Update task last-run status.
	_ = e.db.UpdateScheduledTaskStatus(ctx, t.ID, status, message, firedAt)

	// Write event so it appears in the events viewer.
	eventMsg := fmt.Sprintf("Scheduled task %q completed successfully", t.Name)
	if err != nil {
		eventMsg = fmt.Sprintf("Scheduled task %q failed: %s", t.Name, message)
	}
	_ = e.db.InsertEventEntries(ctx, []eventspkg.EventEntry{{
		Timestamp: completedAt,
		OrgName:   t.OrgName,
		Severity:  severity,
		Device:    "scheduler",
		Message:   eventMsg,
	}})

	return outputPath, err
}
