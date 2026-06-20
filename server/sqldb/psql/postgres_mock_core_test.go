package psql

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/pashagolub/pgxmock/v4"
	"github.com/xact-iot/xact/sqldb"
)

func TestPostgresDashboardsPermissionsAndConfigWithPGXMock(t *testing.T) {
	ctx := context.Background()
	db, mock := newMockPostgres(t)
	parentID := 1

	mock.ExpectQuery("SELECT p.id, p.name, p.description, p.icon, p.variation").
		WithArgs("default").
		WillReturnRows(pgxmock.NewRows([]string{"id", "name", "description", "icon", "variation", "device_type", "permission", "is_category", "parent_id", "sort_order"}).
			AddRow(2, "Overview", "desc", "icon", "var", "pump", "view", false, &parentID, 10))
	dashboards, err := db.ListDashboards(ctx, "default")
	if err != nil {
		t.Fatalf("ListDashboards: %v", err)
	}
	if len(dashboards) != 1 || dashboards[0].ParentID == nil || *dashboards[0].ParentID != parentID {
		t.Fatalf("unexpected dashboards: %#v", dashboards)
	}

	mock.ExpectQuery("SELECT p.id, p.name, p.description, p.icon, p.variation").
		WithArgs("default", 2).
		WillReturnRows(pgxmock.NewRows([]string{"id", "name", "description", "icon", "variation", "device_type", "permission", "is_category", "parent_id", "sort_order", "widgets"}).
			AddRow(2, "Overview", "desc", "icon", "var", "pump", "view", false, nil, 10, json.RawMessage(`[{"id":"w"}]`)))
	dashboard, err := db.GetDashboard(ctx, "default", 2)
	if err != nil {
		t.Fatalf("GetDashboard: %v", err)
	}
	if dashboard == nil || string(dashboard.Widgets) != `[{"id":"w"}]` {
		t.Fatalf("unexpected dashboard: %#v", dashboard)
	}

	mock.ExpectQuery("SELECT p.id, p.name, p.description, p.icon, p.variation").
		WithArgs("default", 404).
		WillReturnError(pgx.ErrNoRows)
	missing, err := db.GetDashboard(ctx, "default", 404)
	if err != nil {
		t.Fatalf("GetDashboard missing: %v", err)
	}
	if missing != nil {
		t.Fatalf("expected nil missing dashboard, got %#v", missing)
	}

	newDashboard := &sqldb.Dashboard{Name: "New", Description: "new", Icon: "mdi:view", Variation: "wide", DeviceType: "pump", Permission: "edit", IsCategory: false, ParentID: &parentID, SortOrder: 4}
	mock.ExpectQuery("SELECT id FROM organisations").WithArgs("default").WillReturnRows(pgxmock.NewRows([]string{"id"}).AddRow(9))
	mock.ExpectQuery("INSERT INTO dashboards").
		WithArgs(9, newDashboard.Name, newDashboard.Description, newDashboard.Icon, newDashboard.Variation, newDashboard.DeviceType, newDashboard.Permission, newDashboard.IsCategory, newDashboard.ParentID, newDashboard.SortOrder, json.RawMessage("[]")).
		WillReturnRows(pgxmock.NewRows([]string{"id"}).AddRow(12))
	if err := db.CreateDashboard(ctx, "default", newDashboard); err != nil {
		t.Fatalf("CreateDashboard: %v", err)
	}
	if newDashboard.ID != 12 {
		t.Fatalf("create did not hydrate dashboard: %#v", newDashboard)
	}

	newDashboard.Widgets = json.RawMessage(`[{"id":"x"}]`)
	mock.ExpectQuery("SELECT id FROM organisations").WithArgs("default").WillReturnRows(pgxmock.NewRows([]string{"id"}).AddRow(9))
	mock.ExpectExec("UPDATE dashboards SET").
		WithArgs(9, 12, newDashboard.Name, newDashboard.Description, newDashboard.Icon, newDashboard.Variation, newDashboard.DeviceType, newDashboard.Permission, newDashboard.IsCategory, newDashboard.ParentID, newDashboard.SortOrder, newDashboard.Widgets).
		WillReturnResult(pgxmock.NewResult("UPDATE", 1))
	if err := db.UpdateDashboard(ctx, "default", 12, newDashboard); err != nil {
		t.Fatalf("UpdateDashboard: %v", err)
	}

	mock.ExpectQuery("SELECT id FROM organisations").WithArgs("default").WillReturnRows(pgxmock.NewRows([]string{"id"}).AddRow(9))
	mock.ExpectExec("DELETE FROM dashboards").WithArgs(9, 12).WillReturnResult(pgxmock.NewResult("DELETE", 0))
	if err := db.DeleteDashboard(ctx, "default", 12); err == nil || !strings.Contains(err.Error(), "not found") {
		t.Fatalf("expected delete dashboard not found, got %v", err)
	}

	mock.ExpectQuery("SELECT p.role, p.ui, p.server").
		WithArgs("default").
		WillReturnRows(pgxmock.NewRows([]string{"role", "ui", "server"}).AddRow("Admin", json.RawMessage(`{"x":true}`), json.RawMessage(`{"api":true}`)))
	perms, err := db.ListPermissions(ctx, "default")
	if err != nil {
		t.Fatalf("ListPermissions: %v", err)
	}
	if len(perms) != 1 || perms[0].Role != "Admin" {
		t.Fatalf("unexpected permissions: %#v", perms)
	}

	mock.ExpectQuery("SELECT p.role, p.ui, p.server").WithArgs("default", "Admin").WillReturnRows(
		pgxmock.NewRows([]string{"role", "ui", "server"}).AddRow("Admin", json.RawMessage(`{"x":true}`), json.RawMessage(`{"api":true}`)))
	perm, err := db.GetPermissions(ctx, "default", "Admin")
	if err != nil {
		t.Fatalf("GetPermissions: %v", err)
	}
	if perm == nil || string(perm.UI) != `{"x":true}` {
		t.Fatalf("unexpected permission: %#v", perm)
	}

	mock.ExpectQuery("SELECT id FROM organisations").WithArgs("default").WillReturnRows(pgxmock.NewRows([]string{"id"}).AddRow(9))
	mock.ExpectExec("UPDATE permissions SET").
		WithArgs(9, "Admin", json.RawMessage("{}"), json.RawMessage("{}")).
		WillReturnResult(pgxmock.NewResult("UPDATE", 1))
	if err := db.UpdatePermissions(ctx, "default", "Admin", &sqldb.RolePermissions{}); err != nil {
		t.Fatalf("UpdatePermissions: %v", err)
	}

	mock.ExpectQuery("SELECT id FROM organisations").WithArgs("default").WillReturnRows(pgxmock.NewRows([]string{"id"}).AddRow(9))
	mock.ExpectExec("INSERT INTO system_config").WithArgs(9, "app", json.RawMessage(`{"ok":true}`)).WillReturnResult(pgxmock.NewResult("INSERT", 1))
	if err := db.SaveConfig(ctx, "default", "app", json.RawMessage(`{"ok":true}`)); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}

	mock.ExpectQuery("SELECT sc.config").WithArgs("default", "app").WillReturnRows(pgxmock.NewRows([]string{"config"}).AddRow(json.RawMessage(`{"ok":true}`)))
	config, err := db.LoadConfig(ctx, "default", "app")
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if string(config) != `{"ok":true}` {
		t.Fatalf("unexpected config: %s", config)
	}

	mock.ExpectQuery("SELECT id FROM organisations").WithArgs("missing").WillReturnError(pgx.ErrNoRows)
	if _, err := db.resolveOrgID(ctx, "missing"); err == nil || !strings.Contains(err.Error(), "organisation") {
		t.Fatalf("expected org lookup error, got %v", err)
	}
}

func TestPostgresScheduledTasksWithPGXMock(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 5, 6, 7, 8, 9, 0, time.UTC)
	completed := now.Add(time.Minute)
	db, mock := newMockPostgres(t)

	taskRows := func() *pgxmock.Rows {
		return pgxmock.NewRows([]string{"id", "org_name", "name", "description", "task_type", "task_config", "schedule", "enabled", "last_run_at", "last_run_status", "last_run_message", "created_at", "updated_at"}).
			AddRow("task-1", "default", "Daily", "desc", "report", []byte(`{"template":"daily"}`), "0 0 * * *", true, &now, "ok", "done", now, now)
	}

	mock.ExpectQuery("SELECT id, org_name, name, description, task_type, task_config, schedule").
		WithArgs("default").
		WillReturnRows(taskRows())
	tasks, err := db.ListScheduledTasks(ctx, "default")
	if err != nil {
		t.Fatalf("ListScheduledTasks: %v", err)
	}
	if len(tasks) != 1 || tasks[0].ID != "task-1" || string(tasks[0].TaskConfig) != `{"template":"daily"}` {
		t.Fatalf("unexpected tasks: %#v", tasks)
	}

	mock.ExpectQuery("SELECT id, org_name, name, description, task_type, task_config, schedule").
		WithArgs("default", "task-1").
		WillReturnRows(taskRows())
	task, err := db.GetScheduledTask(ctx, "default", "task-1")
	if err != nil {
		t.Fatalf("GetScheduledTask: %v", err)
	}
	if task == nil || task.LastRunAt == nil || task.LastRunStatus != "ok" {
		t.Fatalf("unexpected task: %#v", task)
	}

	create := &sqldb.ScheduledTask{ID: "11111111-1111-4111-8111-111111111111", Name: "Hourly", Description: "desc", TaskType: "backup", TaskConfig: json.RawMessage(`{"path":"/tmp"}`), Schedule: "0 * * * *", Enabled: true}
	mock.ExpectQuery("INSERT INTO scheduled_tasks").
		WithArgs(create.ID, "default", create.Name, create.Description, create.TaskType, create.TaskConfig, create.Schedule, create.Enabled).
		WillReturnRows(pgxmock.NewRows([]string{"id", "created_at", "updated_at"}).AddRow(create.ID, now, now))
	if err := db.CreateScheduledTask(ctx, "default", create); err != nil {
		t.Fatalf("CreateScheduledTask: %v", err)
	}
	if create.ID != "11111111-1111-4111-8111-111111111111" {
		t.Fatalf("create did not hydrate task: %#v", create)
	}

	mock.ExpectExec("UPDATE scheduled_tasks").
		WithArgs(create.Name, create.Description, create.TaskType, create.TaskConfig, create.Schedule, create.Enabled, "default", "task-2").
		WillReturnResult(pgxmock.NewResult("UPDATE", 1))
	if err := db.UpdateScheduledTask(ctx, "default", "task-2", create); err != nil {
		t.Fatalf("UpdateScheduledTask: %v", err)
	}

	mock.ExpectExec("DELETE FROM scheduled_tasks").WithArgs("default", "task-2").WillReturnResult(pgxmock.NewResult("DELETE", 1))
	if err := db.DeleteScheduledTask(ctx, "default", "task-2"); err != nil {
		t.Fatalf("DeleteScheduledTask: %v", err)
	}

	mock.ExpectExec("UPDATE scheduled_tasks").WithArgs(now, "ok", "done", "task-2").WillReturnResult(pgxmock.NewResult("UPDATE", 1))
	if err := db.UpdateScheduledTaskStatus(ctx, "task-2", "ok", "done", now); err != nil {
		t.Fatalf("UpdateScheduledTaskStatus: %v", err)
	}

	entry := &sqldb.ScheduleRunLog{ScheduleID: "task-2", OrgName: "default", FiredAt: now, Status: "running", Message: "started", OutputPath: "/tmp/out"}
	mock.ExpectQuery("INSERT INTO schedule_run_log").
		WithArgs(entry.ScheduleID, entry.OrgName, entry.FiredAt, entry.Status, entry.Message, entry.OutputPath).
		WillReturnRows(pgxmock.NewRows([]string{"id"}).AddRow(99))
	if err := db.AppendScheduleRunLog(ctx, entry); err != nil {
		t.Fatalf("AppendScheduleRunLog: %v", err)
	}
	if entry.ID != 99 {
		t.Fatalf("append did not hydrate run log: %#v", entry)
	}

	mock.ExpectExec("UPDATE schedule_run_log").
		WithArgs(completed, "ok", "done", "/tmp/out", 99).
		WillReturnResult(pgxmock.NewResult("UPDATE", 1))
	if err := db.UpdateScheduleRunLog(ctx, 99, completed, "ok", "done", "/tmp/out"); err != nil {
		t.Fatalf("UpdateScheduleRunLog: %v", err)
	}

	mock.ExpectQuery("SELECT id, schedule_id, org_name, fired_at, completed_at, status, message, output_path").
		WithArgs("task-2", 10).
		WillReturnRows(pgxmock.NewRows([]string{"id", "schedule_id", "org_name", "fired_at", "completed_at", "status", "message", "output_path"}).
			AddRow(99, "task-2", "default", now, &completed, "ok", "done", "/tmp/out"))
	logs, err := db.ListScheduleRunLog(ctx, "task-2", 10)
	if err != nil {
		t.Fatalf("ListScheduleRunLog: %v", err)
	}
	if len(logs) != 1 || logs[0].CompletedAt == nil || logs[0].Status != "ok" {
		t.Fatalf("unexpected run logs: %#v", logs)
	}
}
