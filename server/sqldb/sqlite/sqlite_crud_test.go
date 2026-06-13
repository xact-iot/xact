package sqlite

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"
	"time"

	"github.com/xact-iot/xact/events"
	"github.com/xact-iot/xact/sqldb"
)

func newSQLiteCRUDTestDB(t *testing.T) *SQLiteDB {
	t.Helper()
	ctx := context.Background()
	dbi, err := NewSQLiteDB(ctx, filepath.Join(t.TempDir(), "xact.db"))
	if err != nil {
		t.Fatalf("NewSQLiteDB: %v", err)
	}
	t.Cleanup(dbi.Close)
	if err := dbi.Migrate(ctx); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	return dbi.(*SQLiteDB)
}

func TestSQLiteUserAndOrganisationLifecycle(t *testing.T) {
	ctx := context.Background()
	db := newSQLiteCRUDTestDB(t)

	roles, err := db.ListRoles(ctx)
	if err != nil {
		t.Fatalf("ListRoles: %v", err)
	}
	if len(roles) == 0 {
		t.Fatal("expected seeded roles")
	}
	perms, err := db.ListPermissions(ctx, "default")
	if err != nil {
		t.Fatalf("ListPermissions: %v", err)
	}
	for _, rp := range perms {
		var ui map[string]map[string]bool
		if err := json.Unmarshal(rp.UI, &ui); err != nil {
			t.Fatalf("decode permissions for %s: %v", rp.Role, err)
		}
		wantProfileChange := rp.Role != "User"
		if ui["profile"]["change"] != wantProfileChange {
			t.Fatalf("%s profile.change = %v, want %v", rp.Role, ui["profile"]["change"], wantProfileChange)
		}
	}

	area := &sqldb.OrgArea{North: 10, South: 1, East: 20, West: 2}
	org := &sqldb.Organisation{Name: "plant", DisplayName: "Plant", Active: true, Area: area}
	if err := db.CreateOrganisation(ctx, org); err != nil {
		t.Fatalf("CreateOrganisation: %v", err)
	}
	gotOrg, err := db.GetOrganisation(ctx, "plant")
	if err != nil {
		t.Fatalf("GetOrganisation: %v", err)
	}
	if gotOrg == nil || gotOrg.Area == nil || gotOrg.Area.North != 10 {
		t.Fatalf("got org = %#v", gotOrg)
	}
	gotOrg.DisplayName = "Plant Updated"
	gotOrg.Active = false
	if err := db.UpdateOrganisation(ctx, "plant", gotOrg); err != nil {
		t.Fatalf("UpdateOrganisation: %v", err)
	}
	orgs, err := db.ListOrganisations(ctx)
	if err != nil {
		t.Fatalf("ListOrganisations: %v", err)
	}
	if len(orgs) < 2 {
		t.Fatalf("orgs = %#v", orgs)
	}

	hash, err := HashPassword("old")
	if err != nil {
		t.Fatalf("HashPassword: %v", err)
	}
	if !CheckPassword(hash, "old") || CheckPassword(hash, "wrong") {
		t.Fatal("CheckPassword mismatch")
	}
	if pw, err := GenerateRandomPassword(); err != nil || len(pw) == 0 {
		t.Fatalf("GenerateRandomPassword = %q, %v", pw, err)
	}

	user := &sqldb.User{FirstName: "Grace", LastName: "Hopper", LoginName: "grace", Email: "grace@example.test", Active: true}
	if err := db.CreateUser(ctx, user, hash); err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	if user.TokenVersion != 1 {
		t.Fatalf("CreateUser token version = %d, want 1", user.TokenVersion)
	}
	if err := db.AssignUserToOrg(ctx, user.ID, "plant", []string{"Admin", "Operator"}); err != nil {
		t.Fatalf("AssignUserToOrg: %v", err)
	}
	if err := db.SetUserOrgRoles(ctx, user.ID, "plant", []string{"Manager"}); err != nil {
		t.Fatalf("SetUserOrgRoles: %v", err)
	}
	if err := db.UpdateLastLogin(ctx, user.ID); err != nil {
		t.Fatalf("UpdateLastLogin: %v", err)
	}
	user.FirstName = "Amazing"
	user.NotificationOptions = json.RawMessage(`{"emailEnabled":true}`)
	if err := db.UpdateUser(ctx, user); err != nil {
		t.Fatalf("UpdateUser: %v", err)
	}
	if err := db.SetUserPassword(ctx, user.ID, hash); err != nil {
		t.Fatalf("SetUserPassword: %v", err)
	}
	active, tokenVersion, err := db.GetUserAuthState(ctx, user.ID)
	if err != nil || !active || tokenVersion < 1 {
		t.Fatalf("GetUserAuthState active=%v version=%d err=%v", active, tokenVersion, err)
	}
	if err := db.BumpUserTokenVersion(ctx, user.ID); err != nil {
		t.Fatalf("BumpUserTokenVersion: %v", err)
	}
	byID, err := db.GetUserByID(ctx, user.ID)
	if err != nil {
		t.Fatalf("GetUserByID: %v", err)
	}
	if byID == nil || byID.FirstName != "Amazing" || len(byID.Orgs) != 1 || byID.Orgs[0].Roles[0] != "Manager" {
		t.Fatalf("by id = %#v", byID)
	}
	byLogin, _, err := db.GetUserByLogin(ctx, "grace@example.test")
	if err != nil || byLogin == nil || byLogin.ID != user.ID {
		t.Fatalf("GetUserByLogin = %#v, %v", byLogin, err)
	}
	users, err := db.ListUsers(ctx)
	if err != nil || len(users) == 0 {
		t.Fatalf("ListUsers len=%d err=%v", len(users), err)
	}

	if err := db.DeleteOrganisation(ctx, "plant"); err != nil {
		t.Fatalf("DeleteOrganisation: %v", err)
	}
	if org, err := db.GetOrganisation(ctx, "plant"); err != nil || org != nil {
		t.Fatalf("deleted org = %#v err=%v", org, err)
	}
}

func TestSQLiteReportTagCalcAndNotificationProfileLifecycle(t *testing.T) {
	ctx := context.Background()
	db := newSQLiteCRUDTestDB(t)

	tpl := &sqldb.PDFTemplate{Name: "Daily", Description: "Daily report", TemplateJSON: json.RawMessage(`{"pages":[]}`)}
	if err := db.CreatePDFTemplate(ctx, "default", tpl); err != nil {
		t.Fatalf("CreatePDFTemplate: %v", err)
	}
	tpl.Name = "Daily Updated"
	tpl.Variables = json.RawMessage(`[{"name":"shift","type":"custom"}]`)
	if err := db.UpdatePDFTemplate(ctx, "default", tpl.ID, tpl); err != nil {
		t.Fatalf("UpdatePDFTemplate: %v", err)
	}
	gotTpl, err := db.GetPDFTemplate(ctx, "default", tpl.ID)
	if err != nil || gotTpl == nil || gotTpl.Name != "Daily Updated" {
		t.Fatalf("GetPDFTemplate = %#v err=%v", gotTpl, err)
	}
	templates, err := db.ListPDFTemplates(ctx, "default")
	if err != nil || len(templates) == 0 {
		t.Fatalf("ListPDFTemplates len=%d err=%v", len(templates), err)
	}
	if err := db.DeletePDFTemplate(ctx, "default", tpl.ID); err != nil {
		t.Fatalf("DeletePDFTemplate: %v", err)
	}

	calc := &sqldb.TagCalc{Name: "Health", OutputTag: "CUSTOM.Health", Expression: "1+1", IntervalSeconds: 30, Enabled: true}
	if err := db.CreateTagCalc(ctx, "default", calc); err != nil {
		t.Fatalf("CreateTagCalc: %v", err)
	}
	calc.Expression = "2+2"
	calc.Enabled = false
	if err := db.UpdateTagCalc(ctx, "default", calc.ID, calc); err != nil {
		t.Fatalf("UpdateTagCalc: %v", err)
	}
	gotCalc, err := db.GetTagCalc(ctx, "default", calc.ID)
	if err != nil || gotCalc == nil || gotCalc.Expression != "2+2" || gotCalc.Enabled {
		t.Fatalf("GetTagCalc = %#v err=%v", gotCalc, err)
	}
	calcs, err := db.ListTagCalcs(ctx, "default")
	if err != nil || len(calcs) == 0 {
		t.Fatalf("ListTagCalcs len=%d err=%v", len(calcs), err)
	}
	if err := db.DeleteTagCalc(ctx, "default", calc.ID); err != nil {
		t.Fatalf("DeleteTagCalc: %v", err)
	}

	hash, _ := HashPassword("pw")
	user := &sqldb.User{FirstName: "Notify", LastName: "Me", LoginName: "notify", Email: "notify@example.test", Active: true, NotificationOptions: json.RawMessage(`{"emailEnabled":true}`)}
	if err := db.CreateUser(ctx, user, hash); err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	if err := db.AssignUserToOrg(ctx, user.ID, "default", []string{"Operator"}); err != nil {
		t.Fatalf("AssignUserToOrg: %v", err)
	}
	profile := &sqldb.NotificationProfile{Name: "Ops", Description: "Ops team", Roles: []string{"Operator"}, Users: []int{user.ID}, AckRequired: true}
	if err := db.CreateNotificationProfile(ctx, "default", profile); err != nil {
		t.Fatalf("CreateNotificationProfile: %v", err)
	}
	profile.Description = "Updated"
	profile.AckRequired = false
	if err := db.UpdateNotificationProfile(ctx, "default", profile.ID, profile); err != nil {
		t.Fatalf("UpdateNotificationProfile: %v", err)
	}
	gotProfile, err := db.GetNotificationProfile(ctx, "default", profile.ID)
	if err != nil || gotProfile == nil || gotProfile.Description != "Updated" || gotProfile.AckRequired {
		t.Fatalf("GetNotificationProfile = %#v err=%v", gotProfile, err)
	}
	byName, err := db.GetNotificationProfileByName(ctx, "default", "Ops")
	if err != nil || byName == nil || byName.ID != profile.ID {
		t.Fatalf("GetNotificationProfileByName = %#v err=%v", byName, err)
	}
	id, err := db.ResolveNotificationID(ctx, "default", "Ops")
	if err != nil || id != profile.ID {
		t.Fatalf("ResolveNotificationID = %d err=%v", id, err)
	}
	recipients, err := db.GetNotificationRecipients(ctx, "default", profile.ID)
	if err != nil || len(recipients) != 1 || recipients[0].Email != user.Email {
		t.Fatalf("GetNotificationRecipients = %#v err=%v", recipients, err)
	}
	profiles, err := db.ListNotificationProfiles(ctx, "default")
	if err != nil || len(profiles) == 0 {
		t.Fatalf("ListNotificationProfiles len=%d err=%v", len(profiles), err)
	}
	if err := db.DeleteNotificationProfile(ctx, "default", profile.ID); err != nil {
		t.Fatalf("DeleteNotificationProfile: %v", err)
	}
}

func TestSQLiteReadsPostgresTextBooleans(t *testing.T) {
	ctx := context.Background()
	db := newSQLiteCRUDTestDB(t)

	if _, err := db.RawDB().ExecContext(ctx, `UPDATE organisations SET active = 'true' WHERE name = 'default'`); err != nil {
		t.Fatalf("set org text bool: %v", err)
	}
	org, err := db.GetOrganisation(ctx, "default")
	if err != nil {
		t.Fatalf("GetOrganisation with restored values: %v", err)
	}
	if org == nil || !org.Active {
		t.Fatalf("org active = %#v, want true", org)
	}
	if _, err := db.ListOrganisations(ctx); err != nil {
		t.Fatalf("ListOrganisations with restored values: %v", err)
	}
	nullableOrg, err := scanOrganisation(db.RawDB().QueryRowContext(ctx, `SELECT 1, 'restored', NULL, 'true', NULL, NULL, NULL`))
	if err != nil {
		t.Fatalf("scanOrganisation with restored nulls: %v", err)
	}
	if nullableOrg.DisplayName != "" || nullableOrg.Logo != "" || nullableOrg.Favicon != "" || nullableOrg.Area != nil || !nullableOrg.Active {
		t.Fatalf("nullable org = %#v, want empty nullable fields and active true", nullableOrg)
	}

	hash, _ := HashPassword("pw")
	user := &sqldb.User{FirstName: "Text", LastName: "Bool", LoginName: "textbool", Email: "textbool@example.test", Active: true}
	if err := db.CreateUser(ctx, user, hash); err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	if err := db.AssignUserToOrg(ctx, user.ID, "default", []string{"Operator"}); err != nil {
		t.Fatalf("AssignUserToOrg: %v", err)
	}
	if _, err := db.RawDB().ExecContext(ctx, `UPDATE users SET active = 'true' WHERE id = ?`, user.ID); err != nil {
		t.Fatalf("set user text bool: %v", err)
	}
	if active, _, err := db.GetUserAuthState(ctx, user.ID); err != nil || !active {
		t.Fatalf("GetUserAuthState active=%v err=%v, want true", active, err)
	}
	if byLogin, _, err := db.GetUserByLogin(ctx, "textbool"); err != nil || byLogin == nil {
		t.Fatalf("GetUserByLogin with text bool = %#v err=%v", byLogin, err)
	}
	if _, err := db.ListUsers(ctx); err != nil {
		t.Fatalf("ListUsers with text bool: %v", err)
	}

	calc := &sqldb.TagCalc{Name: "Text Bool Calc", OutputTag: "CUSTOM.TextBool", Expression: "1", IntervalSeconds: 30, Enabled: true}
	if err := db.CreateTagCalc(ctx, "default", calc); err != nil {
		t.Fatalf("CreateTagCalc: %v", err)
	}
	if _, err := db.RawDB().ExecContext(ctx, `UPDATE tag_calcs SET enabled = 'false' WHERE id = ?`, calc.ID); err != nil {
		t.Fatalf("set tag calc text bool: %v", err)
	}
	gotCalc, err := db.GetTagCalc(ctx, "default", calc.ID)
	if err != nil {
		t.Fatalf("GetTagCalc with text bool: %v", err)
	}
	if gotCalc == nil || gotCalc.Enabled {
		t.Fatalf("tag calc enabled = %#v, want false", gotCalc)
	}
	if _, err := db.ListTagCalcs(ctx, "default"); err != nil {
		t.Fatalf("ListTagCalcs with text bool: %v", err)
	}

	profile := &sqldb.NotificationProfile{Name: "Text Bool Profile", Roles: []string{"Operator"}, AckRequired: true}
	if err := db.CreateNotificationProfile(ctx, "default", profile); err != nil {
		t.Fatalf("CreateNotificationProfile: %v", err)
	}
	if _, err := db.RawDB().ExecContext(ctx, `UPDATE notification_profiles SET ack_required = 'true' WHERE id = ?`, profile.ID); err != nil {
		t.Fatalf("set notification text bool: %v", err)
	}
	gotProfile, err := db.GetNotificationProfile(ctx, "default", profile.ID)
	if err != nil {
		t.Fatalf("GetNotificationProfile with text bool: %v", err)
	}
	if gotProfile == nil || !gotProfile.AckRequired {
		t.Fatalf("profile ackRequired = %#v, want true", gotProfile)
	}
	if recipients, err := db.GetNotificationRecipients(ctx, "default", profile.ID); err != nil || len(recipients) != 1 {
		t.Fatalf("GetNotificationRecipients with text bool = %#v err=%v", recipients, err)
	}

	dashboards, err := db.ListDashboards(ctx, "default")
	if err != nil {
		t.Fatalf("ListDashboards before text bool update: %v", err)
	}
	categoryID := 0
	for _, dashboard := range dashboards {
		if dashboard.IsCategory {
			categoryID = dashboard.ID
			break
		}
	}
	if categoryID == 0 {
		t.Fatal("no seeded category dashboard found")
	}
	if _, err := db.RawDB().ExecContext(ctx, `UPDATE dashboards SET is_category = 'true' WHERE id = ?`, categoryID); err != nil {
		t.Fatalf("set dashboard text bool: %v", err)
	}
	dashboard, err := db.GetDashboard(ctx, "default", categoryID)
	if err != nil {
		t.Fatalf("GetDashboard with text bool: %v", err)
	}
	if dashboard == nil || !dashboard.IsCategory {
		t.Fatalf("dashboard isCategory = %#v, want true", dashboard)
	}
	if _, err := db.ListDashboards(ctx, "default"); err != nil {
		t.Fatalf("ListDashboards with text bool: %v", err)
	}

	task := &sqldb.ScheduledTask{Name: "Text Bool Task", TaskType: "report", TaskConfig: json.RawMessage(`{}`), Schedule: "0 0 * * *", Enabled: true}
	if err := db.CreateScheduledTask(ctx, "default", task); err != nil {
		t.Fatalf("CreateScheduledTask: %v", err)
	}
	if _, err := db.RawDB().ExecContext(ctx, `UPDATE scheduled_tasks SET enabled = 'true' WHERE id = ?`, task.ID); err != nil {
		t.Fatalf("set task text bool: %v", err)
	}
	gotTask, err := db.GetScheduledTask(ctx, "default", task.ID)
	if err != nil {
		t.Fatalf("GetScheduledTask with text bool: %v", err)
	}
	if gotTask == nil || !gotTask.Enabled {
		t.Fatalf("task enabled = %#v, want true", gotTask)
	}
	if _, err := db.ListScheduledTasks(ctx, "default"); err != nil {
		t.Fatalf("ListScheduledTasks with text bool: %v", err)
	}
}

func TestSQLiteEventsAndMetricQueries(t *testing.T) {
	ctx := context.Background()
	db := newSQLiteCRUDTestDB(t)

	now := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	if err := db.InsertEventEntries(ctx, []events.EventEntry{
		{Timestamp: now, Server: "srv", OrgName: "default", Severity: string(events.Warn), Device: "pump", Message: "hot", Params: map[string]any{"temp": 90}},
		{Timestamp: now.Add(time.Minute), Server: "srv", OrgName: "other", Severity: string(events.Info), Device: "tank", Message: "ok"},
	}); err != nil {
		t.Fatalf("InsertEventEntries: %v", err)
	}
	found, err := db.QueryEvents(ctx, sqldb.EventFilter{OrgName: "default", Severity: string(events.Warn), Search: "hot", Limit: 10})
	if err != nil || len(found) != 1 || found[0].Params["temp"] == nil {
		t.Fatalf("QueryEvents = %#v err=%v", found, err)
	}
	if err := db.PurgeEventsBefore(ctx, now.Add(30*time.Second)); err != nil {
		t.Fatalf("PurgeEventsBefore: %v", err)
	}

	points := []sqldb.MetricEntry{
		{DeviceName: "pump-1", MetricName: "temp", Timestamp: now, Value: 1},
		{DeviceName: "pump-1", MetricName: "flow", Timestamp: now.Add(time.Second), Value: 2},
		{DeviceName: "area.pump-2", MetricName: "temp", Timestamp: now.Add(2 * time.Second), Value: 3},
		{DeviceName: "LA_LongBeach.AirQuality.AQ-S-0001.air", MetricName: "aqi", Timestamp: now.Add(3 * time.Second), Value: 42},
		{DeviceName: "LA_LongBeach.AirQuality.AQ-S-0140.air", MetricName: "aqi", Timestamp: now.Add(4 * time.Second), Value: 84},
	}
	if err := db.InsertMetrics(ctx, "default", points); err != nil {
		t.Fatalf("InsertMetrics: %v", err)
	}
	byPaths, err := db.QueryMetricsByTagPaths(ctx, "default", []string{"pump-1.temp", "area.pump-2.temp"}, now.Add(-time.Second), now.Add(time.Minute))
	if err != nil || len(byPaths) != 1 || byPaths[0].Name != "temp" || len(byPaths[0].Data) != 2 {
		t.Fatalf("QueryMetricsByTagPaths = %#v err=%v", byPaths, err)
	}
	airQualityPath, err := db.QueryMetricsByTagPaths(ctx, "default", []string{"LA_LongBeach.AirQuality.AQ-S-0001.air.aqi"}, now.Add(-time.Second), now.Add(time.Minute))
	if err != nil || len(airQualityPath) != 1 || airQualityPath[0].Name != "aqi" || len(airQualityPath[0].Data) != 1 || airQualityPath[0].Data[0].Value != 42 {
		t.Fatalf("QueryMetricsByTagPaths exact AirQuality path = %#v err=%v", airQualityPath, err)
	}
	since, err := db.QueryMetricsSince(ctx, "default", "pump-1", []string{"temp", "flow"}, "temp", now)
	if err != nil || len(since) != 1 || since[0].Name != "flow" {
		t.Fatalf("QueryMetricsSince = %#v err=%v", since, err)
	}
	if err := db.ConfigureMetricsRetention(ctx, time.Hour); err != nil {
		t.Fatalf("ConfigureMetricsRetention: %v", err)
	}
}
