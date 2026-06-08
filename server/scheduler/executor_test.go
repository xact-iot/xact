package scheduler

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/xact-iot/xact/sqldb"
	"github.com/xact-iot/xact/sqldb/sqlite"
)

func TestCommandPayloadDataIncludesFalseValue(t *testing.T) {
	data, err := commandPayloadData("cmd-1", "pumps.0.status", false)
	if err != nil {
		t.Fatalf("commandPayloadData returned error: %v", err)
	}

	var got map[string]any
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("payload is invalid JSON: %v", err)
	}
	if got["id"] != "cmd-1" {
		t.Fatalf("id = %v, want cmd-1", got["id"])
	}
	value, ok := got["pumps.0.status"].(bool)
	if !ok {
		t.Fatalf("pumps.0.status type = %T, want bool; payload %s", got["pumps.0.status"], data)
	}
	if value {
		t.Fatalf("pumps.0.status = true, want false; payload %s", data)
	}
}

func TestShellTaskRunsWhenUnsafeTasksEnabled(t *testing.T) {
	t.Setenv("SCHEDULER_OUTPUT_DIR", t.TempDir())

	_, err := Run(context.Background(), RunContext{
		AllowUnsafeTasks: true,
		Task: sqldb.ScheduledTask{
			TaskType:   "shell",
			TaskConfig: json.RawMessage(`{"command":"true"}`),
		},
	})
	if err != nil {
		t.Fatalf("Run() returned error: %v", err)
	}
}

func TestYaegiTaskRunsWhenUnsafeTasksEnabled(t *testing.T) {
	_, err := Run(context.Background(), RunContext{
		AllowUnsafeTasks: true,
		Task: sqldb.ScheduledTask{
			TaskType:   "yaegi",
			TaskConfig: json.RawMessage(`{"script":"package script\nfunc Run() error { return nil }"}`),
		},
	})
	if err != nil {
		t.Fatalf("Run() returned error: %v", err)
	}
}

func TestResolveSchedulerOutputDirRejectsTraversal(t *testing.T) {
	t.Setenv("SCHEDULER_OUTPUT_DIR", t.TempDir())

	if _, err := resolveSchedulerOutputDir("../outside"); err == nil {
		t.Fatalf("resolveSchedulerOutputDir accepted path traversal")
	}
	if _, err := resolveSchedulerOutputDir("/tmp/outside"); err == nil {
		t.Fatalf("resolveSchedulerOutputDir accepted absolute path")
	}
	if got, err := resolveSchedulerOutputDir("reports"); err != nil || !strings.HasSuffix(got, "reports") {
		t.Fatalf("resolveSchedulerOutputDir = %q, %v; want reports under root", got, err)
	}
}

func TestUnsafeTasksRequireServerFlag(t *testing.T) {
	for _, taskType := range []string{"shell", "yaegi"} {
		t.Run(taskType, func(t *testing.T) {
			_, err := Run(context.Background(), RunContext{
				Task: sqldb.ScheduledTask{
					TaskType:   taskType,
					TaskConfig: json.RawMessage(`{}`),
				},
			})
			if err == nil || !strings.Contains(err.Error(), "disabled by server configuration") {
				t.Fatalf("Run() error = %v, want disabled error", err)
			}
		})
	}
}

func TestRunRejectsUnknownAndInvalidCommandTasks(t *testing.T) {
	if _, err := Run(context.Background(), RunContext{Task: sqldb.ScheduledTask{TaskType: "mystery"}}); err == nil || !strings.Contains(err.Error(), "unknown task type") {
		t.Fatalf("unknown task error = %v", err)
	}
	for name, cfg := range map[string]string{
		"bad json":       `{bad`,
		"missing device": `{"tagPath":"x","value":1}`,
		"missing tag":    `{"deviceName":"pump","value":1}`,
		"missing value":  `{"deviceName":"pump","tagPath":"x"}`,
		"blank value":    `{"deviceName":"pump","tagPath":"x","value":" "}`,
		"no nats":        `{"deviceName":"pump","tagPath":"x","value":false}`,
	} {
		t.Run(name, func(t *testing.T) {
			_, err := Run(context.Background(), RunContext{
				Task: sqldb.ScheduledTask{TaskType: "command", TaskConfig: json.RawMessage(cfg)},
			})
			if err == nil {
				t.Fatal("expected command task error")
			}
		})
	}
}

func TestSchedulerHelpers(t *testing.T) {
	if sanitiseFilename(`Daily Report: Pump/1`) != "Daily-Report_-Pump_1" {
		t.Fatalf("sanitiseFilename mismatch")
	}
	if commandRelativePath("default", "pump", "default.pump.controls.start") != "controls.start" {
		t.Fatalf("commandRelativePath full mismatch")
	}
	if commandRelativePath("default", "pump", "pump.controls.start") != "controls.start" {
		t.Fatalf("commandRelativePath device mismatch")
	}
	if newCommandID() == "" {
		t.Fatal("newCommandID returned empty string")
	}
	t.Setenv("SCHEDULER_WORK_DIR", filepath.Join(t.TempDir(), "work"))
	if dir, err := schedulerWorkDir(); err != nil || !strings.HasSuffix(dir, "work") {
		t.Fatalf("schedulerWorkDir = %q, %v", dir, err)
	}
}

func TestEngineLifecycleWithSQLiteTasks(t *testing.T) {
	ctx := context.Background()
	dbi, err := sqlite.NewSQLiteDB(ctx, filepath.Join(t.TempDir(), "xact.db"))
	if err != nil {
		t.Fatalf("NewSQLiteDB: %v", err)
	}
	defer dbi.Close()
	if err := dbi.Migrate(ctx); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	db := dbi
	task := &sqldb.ScheduledTask{
		OrgName:    "default",
		Name:       "Noop",
		Schedule:   "0 0 * * *",
		TaskType:   "shell",
		TaskConfig: json.RawMessage(`{"command":"true"}`),
		Enabled:    false,
	}
	if err := db.CreateScheduledTask(ctx, "default", task); err != nil {
		t.Fatalf("CreateScheduledTask: %v", err)
	}
	e := NewWithOptions(db, nil, EngineOptions{AllowUnsafeTasks: true})
	if !e.AllowUnsafeTasks() {
		t.Fatal("AllowUnsafeTasks false")
	}
	if err := e.LoadForOrg(ctx, "default"); err != nil {
		t.Fatalf("LoadForOrg: %v", err)
	}
	if len(e.jobs) != 0 {
		t.Fatalf("disabled task registered jobs = %#v", e.jobs)
	}
	task.Enabled = true
	task.Schedule = "bad cron"
	if err := db.UpdateScheduledTask(ctx, "default", task.ID, task); err != nil {
		t.Fatalf("UpdateScheduledTask: %v", err)
	}
	if err := e.Reload(ctx, "default", task.ID); err == nil {
		t.Fatal("expected bad cron reload error")
	}
	task.Schedule = "0 0 * * *"
	if err := db.UpdateScheduledTask(ctx, "default", task.ID, task); err != nil {
		t.Fatalf("UpdateScheduledTask good: %v", err)
	}
	if err := e.Reload(ctx, "default", task.ID); err != nil {
		t.Fatalf("Reload: %v", err)
	}
	if len(e.jobs) != 1 {
		t.Fatalf("registered jobs = %#v", e.jobs)
	}
	e.Remove(task.ID)
	if len(e.jobs) != 0 {
		t.Fatalf("jobs after remove = %#v", e.jobs)
	}
	if _, err := e.RunNow(ctx, "default", "missing"); err == nil {
		t.Fatal("expected missing task error")
	}

	task.TaskConfig = json.RawMessage(`{"command":"true"}`)
	task.Enabled = true
	if err := db.UpdateScheduledTask(ctx, "default", task.ID, task); err != nil {
		t.Fatalf("UpdateScheduledTask run now: %v", err)
	}
	t.Setenv("SCHEDULER_OUTPUT_DIR", t.TempDir())
	if _, err := e.RunNow(ctx, "default", task.ID); err != nil {
		t.Fatalf("RunNow: %v", err)
	}
	if err := db.UpdateScheduledTaskStatus(ctx, task.ID, "ok", "", time.Now()); err != nil {
		t.Fatalf("UpdateScheduledTaskStatus: %v", err)
	}
}
