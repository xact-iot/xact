package psql

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/pashagolub/pgxmock/v4"
	"github.com/xact-iot/xact/events"
	"github.com/xact-iot/xact/sqldb"
)

func newMockPostgres(t *testing.T) (*PostgresDB, pgxmock.PgxPoolIface) {
	t.Helper()
	mock, err := pgxmock.NewPool()
	if err != nil {
		t.Fatalf("new pgx mock: %v", err)
	}
	t.Cleanup(func() {
		if err := mock.ExpectationsWereMet(); err != nil {
			t.Fatalf("unmet pgx expectations: %v", err)
		}
		mock.Close()
	})
	return &PostgresDB{pool: mock}, mock
}

func TestPostgresRawDBWithMockPool(t *testing.T) {
	db, _ := newMockPostgres(t)
	if db.RawDB() != nil {
		t.Fatal("mock-backed PostgresDB should not expose a raw sql.DB")
	}
	if db.BackupAdapter() == nil {
		t.Fatal("expected backup adapter")
	}
}

func TestPostgresTagCalcsWithPGXMock(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	db, mock := newMockPostgres(t)

	mock.ExpectQuery("SELECT id, org_name, name, description, output_tag, expression, interval_seconds, enabled, created_at, updated_at").
		WithArgs("default").
		WillReturnRows(pgxmock.NewRows([]string{"id", "org_name", "name", "description", "output_tag", "expression", "interval_seconds", "enabled", "created_at", "updated_at"}).
			AddRow(7, "default", "Health", "desc", "CUSTOM.Health", "A+B", 30, true, now, now))

	calcs, err := db.ListTagCalcs(ctx, "default")
	if err != nil {
		t.Fatalf("ListTagCalcs: %v", err)
	}
	if len(calcs) != 1 || calcs[0].Name != "Health" || calcs[0].OutputTag != "CUSTOM.Health" {
		t.Fatalf("unexpected tag calcs: %#v", calcs)
	}

	mock.ExpectQuery("SELECT id, org_name, name, description, output_tag, expression, interval_seconds, enabled, created_at, updated_at").
		WithArgs("default", 7).
		WillReturnRows(pgxmock.NewRows([]string{"id", "org_name", "name", "description", "output_tag", "expression", "interval_seconds", "enabled", "created_at", "updated_at"}).
			AddRow(7, "default", "Health", "desc", "CUSTOM.Health", "A+B", 30, true, now, now))

	calc, err := db.GetTagCalc(ctx, "default", 7)
	if err != nil {
		t.Fatalf("GetTagCalc: %v", err)
	}
	if calc == nil || calc.ID != 7 {
		t.Fatalf("unexpected tag calc: %#v", calc)
	}

	mock.ExpectQuery("SELECT id, org_name, name, description, output_tag, expression, interval_seconds, enabled, created_at, updated_at").
		WithArgs("default", 99).
		WillReturnError(pgx.ErrNoRows)

	missing, err := db.GetTagCalc(ctx, "default", 99)
	if err != nil {
		t.Fatalf("GetTagCalc missing: %v", err)
	}
	if missing != nil {
		t.Fatalf("expected nil missing calc, got %#v", missing)
	}

	create := &sqldb.TagCalc{Name: "Flow", Description: "flow total", OutputTag: "CUSTOM.Flow", Expression: "sum", IntervalSeconds: 60, Enabled: true}
	mock.ExpectQuery("INSERT INTO tag_calcs").
		WithArgs("default", create.Name, create.Description, create.OutputTag, create.Expression, create.IntervalSeconds, create.Enabled).
		WillReturnRows(pgxmock.NewRows([]string{"id", "created_at", "updated_at"}).AddRow(8, now, now))

	if err := db.CreateTagCalc(ctx, "default", create); err != nil {
		t.Fatalf("CreateTagCalc: %v", err)
	}
	if create.ID != 8 || create.OrgName != "default" {
		t.Fatalf("create did not hydrate tag calc: %#v", create)
	}

	mock.ExpectExec("UPDATE tag_calcs").
		WithArgs("default", 8, create.Name, create.Description, create.OutputTag, create.Expression, create.IntervalSeconds, create.Enabled).
		WillReturnResult(pgxmock.NewResult("UPDATE", 1))
	if err := db.UpdateTagCalc(ctx, "default", 8, create); err != nil {
		t.Fatalf("UpdateTagCalc: %v", err)
	}

	mock.ExpectExec("DELETE FROM tag_calcs").WithArgs("default", 8).WillReturnResult(pgxmock.NewResult("DELETE", 1))
	if err := db.DeleteTagCalc(ctx, "default", 8); err != nil {
		t.Fatalf("DeleteTagCalc: %v", err)
	}

	mock.ExpectExec("UPDATE tag_calcs").WithArgs("default", 404, create.Name, create.Description, create.OutputTag, create.Expression, create.IntervalSeconds, create.Enabled).WillReturnResult(pgxmock.NewResult("UPDATE", 0))
	if err := db.UpdateTagCalc(ctx, "default", 404, create); err == nil || !strings.Contains(err.Error(), "not found") {
		t.Fatalf("expected not found update error, got %v", err)
	}
}

func TestPostgresNotificationProfilesWithPGXMock(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 2, 3, 4, 5, 6, 0, time.UTC)
	db, mock := newMockPostgres(t)
	roles := []byte(`["Admin","Operator"]`)
	users := []byte(`[10,11]`)

	profileRows := func() *pgxmock.Rows {
		return pgxmock.NewRows([]string{"id", "org_name", "name", "description", "roles", "users", "ack_required", "created_at", "updated_at"}).
			AddRow(3, "default", "Ops", "ops alerts", roles, users, true, now, now)
	}

	mock.ExpectQuery("SELECT id, org_name, name, description, roles, users, ack_required, created_at, updated_at").
		WithArgs("default").
		WillReturnRows(profileRows())
	profiles, err := db.ListNotificationProfiles(ctx, "default")
	if err != nil {
		t.Fatalf("ListNotificationProfiles: %v", err)
	}
	if len(profiles) != 1 || profiles[0].Roles[1] != "Operator" || profiles[0].Users[0] != 10 {
		t.Fatalf("unexpected profiles: %#v", profiles)
	}

	mock.ExpectQuery("SELECT id, org_name, name, description, roles, users, ack_required, created_at, updated_at").
		WithArgs("default", 3).
		WillReturnRows(profileRows())
	profile, err := db.GetNotificationProfile(ctx, "default", 3)
	if err != nil {
		t.Fatalf("GetNotificationProfile: %v", err)
	}
	if profile == nil || !profile.AckRequired {
		t.Fatalf("unexpected profile: %#v", profile)
	}

	mock.ExpectQuery("SELECT id FROM notification_profiles").
		WithArgs("default", "Ops").
		WillReturnRows(pgxmock.NewRows([]string{"id"}).AddRow(3))
	id, err := db.ResolveNotificationID(ctx, "default", "Ops")
	if err != nil || id != 3 {
		t.Fatalf("ResolveNotificationID = %d, %v", id, err)
	}

	create := &sqldb.NotificationProfile{Name: "Tech", Description: "tech alerts", Roles: []string{"Technician"}, Users: []int{42}, AckRequired: true}
	mock.ExpectQuery("INSERT INTO notification_profiles").
		WithArgs("default", create.Name, create.Description, []byte(`["Technician"]`), []byte(`[42]`), true).
		WillReturnRows(pgxmock.NewRows([]string{"id", "created_at", "updated_at"}).AddRow(4, now, now))
	if err := db.CreateNotificationProfile(ctx, "default", create); err != nil {
		t.Fatalf("CreateNotificationProfile: %v", err)
	}
	if create.ID != 4 || create.OrgName != "default" {
		t.Fatalf("create did not hydrate profile: %#v", create)
	}

	update := &sqldb.NotificationProfile{Name: "Tech", Description: "changed"}
	mock.ExpectExec("UPDATE notification_profiles").
		WithArgs("default", 4, update.Name, update.Description, []byte("[]"), []byte("[]"), false).
		WillReturnResult(pgxmock.NewResult("UPDATE", 1))
	if err := db.UpdateNotificationProfile(ctx, "default", 4, update); err != nil {
		t.Fatalf("UpdateNotificationProfile: %v", err)
	}

	mock.ExpectExec("DELETE FROM notification_profiles").WithArgs("default", 4).WillReturnResult(pgxmock.NewResult("DELETE", 0))
	if err := db.DeleteNotificationProfile(ctx, "default", 4); err == nil || !strings.Contains(err.Error(), "not found") {
		t.Fatalf("expected delete not found error, got %v", err)
	}
}

func TestPostgresNotificationRecipientsWithPGXMock(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 2, 3, 4, 5, 6, 0, time.UTC)
	db, mock := newMockPostgres(t)

	mock.ExpectQuery("SELECT id, org_name, name, description, roles, users, ack_required, created_at, updated_at").
		WithArgs("default", 3).
		WillReturnRows(pgxmock.NewRows([]string{"id", "org_name", "name", "description", "roles", "users", "ack_required", "created_at", "updated_at"}).
			AddRow(3, "default", "Ops", "ops alerts", []byte(`["Admin"]`), []byte(`[12]`), false, now, now))
	mock.ExpectQuery("SELECT DISTINCT u.id").
		WithArgs("default", []string{"Admin"}, []int{12}).
		WillReturnRows(pgxmock.NewRows([]string{"id", "first_name", "last_name", "email", "notification_options"}).
			AddRow(12, "Ada", "Lovelace", "ada@example.test", json.RawMessage(`{"email":true}`)))

	recipients, err := db.GetNotificationRecipients(ctx, "default", 3)
	if err != nil {
		t.Fatalf("GetNotificationRecipients: %v", err)
	}
	if len(recipients) != 1 || recipients[0].Email != "ada@example.test" {
		t.Fatalf("unexpected recipients: %#v", recipients)
	}
}

func TestPostgresPDFTemplatesWithPGXMock(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 3, 4, 5, 6, 7, 0, time.UTC)
	db, mock := newMockPostgres(t)

	mock.ExpectQuery("SELECT id, org_name, name, description, template_json, variables, created_at, updated_at").
		WithArgs("default").
		WillReturnRows(pgxmock.NewRows([]string{"id", "org_name", "name", "description", "template_json", "variables", "created_at", "updated_at"}).
			AddRow("tpl-1", "default", "Daily", "daily report", []byte(`{"blocks":[]}`), []byte(`["site"]`), now, now))
	templates, err := db.ListPDFTemplates(ctx, "default")
	if err != nil {
		t.Fatalf("ListPDFTemplates: %v", err)
	}
	if len(templates) != 1 || string(templates[0].TemplateJSON) != `{"blocks":[]}` {
		t.Fatalf("unexpected templates: %#v", templates)
	}

	mock.ExpectQuery("SELECT id, org_name, name, description, template_json, variables, created_at, updated_at").
		WithArgs("default", "tpl-1").
		WillReturnRows(pgxmock.NewRows([]string{"id", "org_name", "name", "description", "template_json", "variables", "created_at", "updated_at"}).
			AddRow("tpl-1", "default", "Daily", "daily report", []byte(`{"blocks":[]}`), []byte(`["site"]`), now, now))
	template, err := db.GetPDFTemplate(ctx, "default", "tpl-1")
	if err != nil {
		t.Fatalf("GetPDFTemplate: %v", err)
	}
	if template == nil || template.ID != "tpl-1" {
		t.Fatalf("unexpected template: %#v", template)
	}

	create := &sqldb.PDFTemplate{Name: "Weekly", Description: "weekly"}
	mock.ExpectQuery("INSERT INTO pdf_templates").
		WithArgs("default", create.Name, create.Description, []byte("{}"), []byte("[]")).
		WillReturnRows(pgxmock.NewRows([]string{"id", "created_at", "updated_at"}).AddRow("tpl-2", now, now))
	if err := db.CreatePDFTemplate(ctx, "default", create); err != nil {
		t.Fatalf("CreatePDFTemplate: %v", err)
	}
	if create.ID != "tpl-2" {
		t.Fatalf("create did not hydrate template: %#v", create)
	}

	update := &sqldb.PDFTemplate{Name: "Weekly", Description: "changed", TemplateJSON: json.RawMessage(`{"x":1}`), Variables: json.RawMessage(`["x"]`)}
	mock.ExpectExec("UPDATE pdf_templates").
		WithArgs("default", "tpl-2", update.Name, update.Description, []byte(`{"x":1}`), []byte(`["x"]`)).
		WillReturnResult(pgxmock.NewResult("UPDATE", 1))
	if err := db.UpdatePDFTemplate(ctx, "default", "tpl-2", update); err != nil {
		t.Fatalf("UpdatePDFTemplate: %v", err)
	}

	mock.ExpectExec("DELETE FROM pdf_templates").WithArgs("default", "tpl-2").WillReturnResult(pgxmock.NewResult("DELETE", 1))
	if err := db.DeletePDFTemplate(ctx, "default", "tpl-2"); err != nil {
		t.Fatalf("DeletePDFTemplate: %v", err)
	}
}

func TestPostgresOrganisationsWithPGXMock(t *testing.T) {
	ctx := context.Background()
	db, mock := newMockPostgres(t)
	areaText := "(2.000000,1.000000),(-2.000000,-1.000000)"

	mock.ExpectQuery("SELECT id, name, display_name, active, logo_data, favicon, area::text FROM organisations ORDER BY name").
		WillReturnRows(pgxmock.NewRows([]string{"id", "name", "display_name", "active", "logo_data", "favicon", "area"}).
			AddRow(1, "default", "Default", true, "logo", "favicon", &areaText))
	orgs, err := db.ListOrganisations(ctx)
	if err != nil {
		t.Fatalf("ListOrganisations: %v", err)
	}
	if len(orgs) != 1 || orgs[0].Area == nil || orgs[0].Area.East != 2 {
		t.Fatalf("unexpected orgs: %#v", orgs)
	}

	mock.ExpectQuery("SELECT id, name, display_name, active, logo_data, favicon, area::text FROM organisations WHERE name =").
		WithArgs("default").
		WillReturnRows(pgxmock.NewRows([]string{"id", "name", "display_name", "active", "logo_data", "favicon", "area"}).
			AddRow(1, "default", "Default", true, "logo", "favicon", nil))
	org, err := db.GetOrganisation(ctx, "default")
	if err != nil {
		t.Fatalf("GetOrganisation: %v", err)
	}
	if org == nil || org.Name != "default" || org.Area != nil {
		t.Fatalf("unexpected org: %#v", org)
	}

	update := &sqldb.Organisation{DisplayName: "Ops", Active: true, Logo: "l", Favicon: "f", Area: &sqldb.OrgArea{North: 1, South: -1, East: 2, West: -2}}
	mock.ExpectExec("UPDATE organisations SET").
		WithArgs("default", update.DisplayName, update.Active, update.Logo, update.Favicon, formatBOX(update.Area)).
		WillReturnResult(pgxmock.NewResult("UPDATE", 1))
	if err := db.UpdateOrganisation(ctx, "default", update); err != nil {
		t.Fatalf("UpdateOrganisation: %v", err)
	}

	if err := db.DeleteOrganisation(ctx, "default"); err == nil || !strings.Contains(err.Error(), "default") {
		t.Fatalf("expected default delete error, got %v", err)
	}
	mock.ExpectExec("DELETE FROM organisations").WithArgs("ops").WillReturnResult(pgxmock.NewResult("DELETE", 0))
	if err := db.DeleteOrganisation(ctx, "ops"); err == nil || !strings.Contains(err.Error(), "not found") {
		t.Fatalf("expected org not found error, got %v", err)
	}
}

func TestPostgresEventLoggingWithPGXMock(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 4, 5, 6, 7, 8, 0, time.UTC)
	db, mock := newMockPostgres(t)
	userID := 2

	if err := db.InsertEventEntries(ctx, nil); err != nil {
		t.Fatalf("InsertEventEntries empty: %v", err)
	}

	event := events.EventEntry{
		Timestamp:      now,
		Server:         "srv",
		OrgName:        "default",
		UserID:         &userID,
		Severity:       "warning",
		NotificationID: 5,
		Device:         "pump",
		Message:        "high temp",
		Params:         map[string]any{"temp": float64(99)},
	}
	mock.ExpectExec("INSERT INTO events").
		WithArgs(event.Timestamp, event.Server, event.OrgName, event.UserID, event.Severity, event.NotificationID, event.Device, event.Message, []byte(`{"temp":99}`)).
		WillReturnResult(pgxmock.NewResult("INSERT", 1))
	if err := db.InsertEventEntries(ctx, []events.EventEntry{event}); err != nil {
		t.Fatalf("InsertEventEntries: %v", err)
	}

	start := now.Add(-time.Hour)
	end := now.Add(time.Hour)
	filter := sqldb.EventFilter{AfterID: 10, OrgName: "default", UserID: 2, Severity: "warning", Device: "pump", NotificationID: 5, Search: "temp", StartTime: &start, EndTime: &end, Limit: 25}
	mock.ExpectQuery("SELECT id, timestamp, server, org_name, user_id, severity, notification_id, device, message, params FROM events").
		WithArgs(int64(10), "default", 2, "warning", "pump", 5, "temp", start, end, 25).
		WillReturnRows(pgxmock.NewRows([]string{"id", "timestamp", "server", "org_name", "user_id", "severity", "notification_id", "device", "message", "params"}).
			AddRow(int64(11), now, "srv", "default", &userID, "warning", 5, "pump", "high temp", []byte(`{"temp":99}`)))
	entries, err := db.QueryEvents(ctx, filter)
	if err != nil {
		t.Fatalf("QueryEvents: %v", err)
	}
	if len(entries) != 1 || entries[0].Params["temp"] != float64(99) {
		t.Fatalf("unexpected events: %#v", entries)
	}

	mock.ExpectExec("DELETE FROM events WHERE timestamp <").WithArgs(start).WillReturnResult(pgxmock.NewResult("DELETE", 3))
	if err := db.PurgeEventsBefore(ctx, start); err != nil {
		t.Fatalf("PurgeEventsBefore: %v", err)
	}
}
