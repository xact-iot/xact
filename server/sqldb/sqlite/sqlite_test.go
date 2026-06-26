package sqlite

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/xact-iot/xact/events"
	"github.com/xact-iot/xact/sqldb"
)

func TestSQLiteOpenPathAddsConcurrencyPragmas(t *testing.T) {
	path := sqliteOpenPath("file:xact.db?mode=rwc")
	if !strings.Contains(path, "mode=rwc") {
		t.Fatalf("open path lost existing query params: %s", path)
	}
	for _, want := range []string{
		"_pragma=busy_timeout%3D10000",
		"_pragma=foreign_keys%28ON%29",
		"_pragma=journal_mode%28WAL%29",
	} {
		if !strings.Contains(path, want) {
			t.Fatalf("open path %q missing %q", path, want)
		}
	}
}

func TestNewSQLiteDBConfiguresPooledWALConnections(t *testing.T) {
	ctx := context.Background()
	dbi, err := NewSQLiteDB(ctx, filepath.Join(t.TempDir(), "xact.db"))
	if err != nil {
		t.Fatalf("NewSQLiteDB: %v", err)
	}
	defer dbi.Close()
	db := dbi.(*SQLiteDB)

	if got := db.RawDB().Stats().MaxOpenConnections; got != 8 {
		t.Fatalf("MaxOpenConnections = %d, want 8", got)
	}

	var journalMode string
	if err := db.RawDB().QueryRowContext(ctx, "PRAGMA journal_mode").Scan(&journalMode); err != nil {
		t.Fatalf("query journal_mode: %v", err)
	}
	if journalMode != "wal" {
		t.Fatalf("journal_mode = %q, want wal", journalMode)
	}

	var foreignKeys int
	if err := db.RawDB().QueryRowContext(ctx, "PRAGMA foreign_keys").Scan(&foreignKeys); err != nil {
		t.Fatalf("query foreign_keys: %v", err)
	}
	if foreignKeys != 1 {
		t.Fatalf("foreign_keys = %d, want 1", foreignKeys)
	}
}

func TestMigrateSeedsStarterDashboards(t *testing.T) {
	ctx := context.Background()
	db, err := NewSQLiteDB(ctx, filepath.Join(t.TempDir(), "xact.db"))
	if err != nil {
		t.Fatalf("NewSQLiteDB: %v", err)
	}
	defer db.Close()

	if err := db.Migrate(ctx); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	if err := db.Migrate(ctx); err != nil {
		t.Fatalf("second Migrate: %v", err)
	}

	dashboards, err := db.ListDashboards(ctx, "default")
	if err != nil {
		t.Fatalf("ListDashboards: %v", err)
	}
	if len(dashboards) != 9 {
		t.Fatalf("starter dashboard count = %d, want 9: %#v", len(dashboards), dashboards)
	}

	welcome := findDashboardMeta(t, dashboards, sqldb.StarterWelcomeName)
	if welcome.IsCategory {
		t.Fatal("Welcome should be a dashboard, not a category")
	}
	if welcome.ParentID != nil {
		t.Fatalf("Welcome parent = %v, want nil", *welcome.ParentID)
	}
	assertDashboardWidgetType(t, db, welcome.ID, "html-widget")

	systemMetrics := findDashboardMeta(t, dashboards, sqldb.StarterSystemMetricsName)
	if systemMetrics.IsCategory {
		t.Fatal("System Metrics should be a dashboard, not a category")
	}
	if systemMetrics.ParentID != nil {
		t.Fatalf("System Metrics parent = %v, want nil", *systemMetrics.ParentID)
	}
	assertDashboardWidgetType(t, db, systemMetrics.ID, "text-widget")

	settings := findDashboardMeta(t, dashboards, sqldb.StarterSettingsCategory)
	if !settings.IsCategory {
		t.Fatal("Settings should be a category")
	}
	if settings.ParentID != nil {
		t.Fatalf("Settings parent = %v, want nil", *settings.ParentID)
	}

	settingsChildren := []struct {
		name       string
		widgetType string
	}{
		{sqldb.StarterTagsManagerName, "tags-manager-widget"},
		{sqldb.StarterOrgUsersPermsName, "tabs-widget"},
		{sqldb.StarterTagCalcsName, "tagcalcs-widget"},
		{sqldb.StarterSchedulerName, "scheduler-widget"},
		{sqldb.StarterNotificationsName, "notifications-widget"},
	}
	for _, child := range settingsChildren {
		dashboard := findDashboardMeta(t, dashboards, child.name)
		if dashboard.IsCategory {
			t.Fatalf("%s should be a dashboard, not a category", child.name)
		}
		if dashboard.ParentID == nil || *dashboard.ParentID != settings.ID {
			t.Fatalf("%s parent = %v, want %d", child.name, dashboard.ParentID, settings.ID)
		}
		assertDashboardWidgetType(t, db, dashboard.ID, child.widgetType)
	}

	help := findDashboardMeta(t, dashboards, sqldb.StarterHelpName)
	if help.IsCategory {
		t.Fatal("Help should be a dashboard, not a category")
	}
	if help.ParentID != nil {
		t.Fatalf("Help parent = %v, want nil", *help.ParentID)
	}
	assertDashboardWidgetType(t, db, help.ID, "manual-widget")
}

func TestCreateOrganisationSkipsSystemMetricsStarterDashboard(t *testing.T) {
	ctx := context.Background()
	db, err := NewSQLiteDB(ctx, filepath.Join(t.TempDir(), "xact.db"))
	if err != nil {
		t.Fatalf("NewSQLiteDB: %v", err)
	}
	defer db.Close()

	if err := db.Migrate(ctx); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	if err := db.CreateOrganisation(ctx, &sqldb.Organisation{
		Name:        "plant",
		DisplayName: "Plant",
		Active:      true,
	}); err != nil {
		t.Fatalf("CreateOrganisation: %v", err)
	}

	dashboards, err := db.ListDashboards(ctx, "plant")
	if err != nil {
		t.Fatalf("ListDashboards: %v", err)
	}
	if len(dashboards) != 8 {
		t.Fatalf("starter dashboard count = %d, want 8: %#v", len(dashboards), dashboards)
	}
	if hasDashboardNamed(dashboards, sqldb.StarterSystemMetricsName) {
		t.Fatalf("non-default org should not have %q dashboard: %#v", sqldb.StarterSystemMetricsName, dashboards)
	}
}

func TestMigrateRepairsLegacyEventsIDColumn(t *testing.T) {
	ctx := context.Background()
	dbi, err := NewSQLiteDB(ctx, filepath.Join(t.TempDir(), "xact.db"))
	if err != nil {
		t.Fatalf("NewSQLiteDB: %v", err)
	}
	defer dbi.Close()
	db := dbi.(*SQLiteDB)

	if _, err := db.RawDB().ExecContext(ctx, `CREATE TABLE events (
		id              BIGINT NOT NULL,
		timestamp       TEXT NOT NULL,
		server          TEXT NOT NULL DEFAULT '',
		org_name        TEXT NOT NULL DEFAULT '',
		user_id         INTEGER,
		severity        TEXT NOT NULL,
		notification_id INTEGER NOT NULL DEFAULT 0,
		device          TEXT NOT NULL DEFAULT '',
		message         TEXT NOT NULL DEFAULT '',
		params          TEXT
	)`); err != nil {
		t.Fatalf("create legacy events table: %v", err)
	}

	if err := db.Migrate(ctx); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	if err := db.InsertEventEntries(ctx, []events.EventEntry{{
		Timestamp: time.Date(2026, 6, 14, 9, 10, 57, 0, time.UTC),
		Server:    "srv",
		OrgName:   "default",
		Severity:  string(events.Info),
		Device:    "LA_LongBeach.AirQuality.AQ-S-0009",
		Message:   "PM2.5 particulate concentration returned to normal",
	}}); err != nil {
		t.Fatalf("InsertEventEntries after migration: %v", err)
	}

	var id int64
	if err := db.RawDB().QueryRowContext(ctx, `SELECT id FROM events LIMIT 1`).Scan(&id); err != nil {
		t.Fatalf("query inserted event id: %v", err)
	}
	if id == 0 {
		t.Fatal("inserted event id = 0, want generated rowid")
	}
}

func TestInsertMetricsBatchedAndCached(t *testing.T) {
	ctx := context.Background()
	db, err := NewSQLiteDB(ctx, filepath.Join(t.TempDir(), "xact.db"))
	if err != nil {
		t.Fatalf("NewSQLiteDB: %v", err)
	}
	defer db.Close()

	if err := db.Migrate(ctx); err != nil {
		t.Fatalf("Migrate: %v", err)
	}

	start := time.Date(2026, 5, 28, 12, 0, 0, 0, time.UTC)
	entries := []sqldb.MetricEntry{
		{DeviceName: "pump-1", MetricName: "pressure", Timestamp: start, Value: 12.5},
		{DeviceName: "pump-1", MetricName: "pressure", Timestamp: start.Add(time.Second), Value: 12.7},
		{DeviceName: "pump-1", MetricName: "flow", Timestamp: start.Add(2 * time.Second), Value: 9.1},
	}
	if err := db.InsertMetrics(ctx, "default", entries); err != nil {
		t.Fatalf("InsertMetrics batch: %v", err)
	}
	if err := db.InsertMetrics(ctx, "default", []sqldb.MetricEntry{
		{DeviceName: "pump-1", MetricName: "pressure", Timestamp: start.Add(3 * time.Second), Value: 13.2},
	}); err != nil {
		t.Fatalf("InsertMetrics cached path: %v", err)
	}

	series, err := db.QueryMetricsRange(ctx, "default", "pump-1", []string{"pressure", "flow"}, start, start.Add(4*time.Second))
	if err != nil {
		t.Fatalf("QueryMetricsRange: %v", err)
	}
	if len(series) != 2 {
		t.Fatalf("series count = %d, want 2: %#v", len(series), series)
	}
	counts := map[string]int{}
	for _, s := range series {
		counts[s.Name] = len(s.Data)
	}
	if counts["pressure"] != 3 {
		t.Fatalf("pressure points = %d, want 3", counts["pressure"])
	}
	if counts["flow"] != 1 {
		t.Fatalf("flow points = %d, want 1", counts["flow"])
	}
}

func TestAPIKeysAreHashedAndListedMasked(t *testing.T) {
	t.Setenv("API_KEY_HASH_SECRET", "test-api-key-hash-secret")
	ctx := context.Background()
	dbi, err := NewSQLiteDB(ctx, filepath.Join(t.TempDir(), "xact.db"))
	if err != nil {
		t.Fatalf("NewSQLiteDB: %v", err)
	}
	defer dbi.Close()
	db := dbi.(*SQLiteDB)

	if err := db.Migrate(ctx); err != nil {
		t.Fatalf("Migrate: %v", err)
	}

	key, err := db.CreateAPIKey(ctx, "default", "ingest")
	if err != nil {
		t.Fatalf("CreateAPIKey: %v", err)
	}
	if key.Key == "" || key.KeyPrefix == "" || key.KeyLast4 == "" {
		t.Fatalf("created key missing raw/prefix/last4: %#v", key)
	}

	org, err := db.GetAPIKeyOrg(ctx, key.Key)
	if err != nil {
		t.Fatalf("GetAPIKeyOrg: %v", err)
	}
	if org != "default" {
		t.Fatalf("org = %q, want default", org)
	}

	keys, err := db.ListAPIKeys(ctx, "default")
	if err != nil {
		t.Fatalf("ListAPIKeys: %v", err)
	}
	if len(keys) != 1 {
		t.Fatalf("key count = %d, want 1", len(keys))
	}
	if keys[0].Key == key.Key {
		t.Fatal("list response exposed raw API key")
	}
	if keys[0].Key != sqldb.MaskAPIKey(key.KeyPrefix, key.KeyLast4) {
		t.Fatalf("listed key = %q, want masked value", keys[0].Key)
	}

	var storedKey, storedHash string
	if err := db.RawDB().QueryRowContext(ctx,
		"SELECT key, key_hash FROM org_api_keys WHERE id = ?", key.ID,
	).Scan(&storedKey, &storedHash); err != nil {
		t.Fatalf("query stored key: %v", err)
	}
	if storedKey == key.Key {
		t.Fatal("database stored raw API key")
	}
	if storedHash == "" {
		t.Fatal("database did not store API key hash")
	}
}

func TestAgentTokensAreEncryptedRetrievableListedMaskedAndResolvable(t *testing.T) {
	t.Setenv("API_KEY_HASH_SECRET", "test-agent-token-hash-secret")
	t.Setenv("AGENT_TOKEN_ENCRYPTION_SECRET", "test-agent-token-encryption-secret")
	ctx := context.Background()
	dbi, err := NewSQLiteDB(ctx, filepath.Join(t.TempDir(), "xact.db"))
	if err != nil {
		t.Fatalf("NewSQLiteDB: %v", err)
	}
	defer dbi.Close()
	db := dbi.(*SQLiteDB)

	if err := db.Migrate(ctx); err != nil {
		t.Fatalf("Migrate: %v", err)
	}

	token, err := db.CreateAgentToken(ctx, "default", 1, "local-agent", []string{"Admin", "Technician"}, nil)
	if err != nil {
		t.Fatalf("CreateAgentToken: %v", err)
	}
	if token.Token == "" || token.TokenPrefix == "" || token.TokenLast4 == "" {
		t.Fatalf("created token missing raw/prefix/last4: %#v", token)
	}

	resolved, err := db.ResolveAgentToken(ctx, token.Token)
	if err != nil {
		t.Fatalf("ResolveAgentToken: %v", err)
	}
	if resolved == nil {
		t.Fatal("ResolveAgentToken returned nil")
	}
	if resolved.OrgName != "default" || resolved.Name != "local-agent" {
		t.Fatalf("resolved token = %#v", resolved)
	}
	if got := strings.Join(resolved.Roles, ","); got != "Admin,Technician" {
		t.Fatalf("roles = %q, want Admin,Technician", got)
	}

	retrieved, err := db.GetAgentToken(ctx, "default", token.ID, 1, false)
	if err != nil {
		t.Fatalf("GetAgentToken: %v", err)
	}
	if retrieved == nil || retrieved.Token != token.Token {
		t.Fatalf("retrieved token = %#v, want raw token", retrieved)
	}

	tokens, err := db.ListAgentTokens(ctx, "default", 1, false)
	if err != nil {
		t.Fatalf("ListAgentTokens: %v", err)
	}
	if len(tokens) != 1 {
		t.Fatalf("token count = %d, want 1", len(tokens))
	}
	if tokens[0].Token == token.Token {
		t.Fatal("list response exposed raw agent token")
	}
	if tokens[0].Token != sqldb.MaskAPIKey(token.TokenPrefix, token.TokenLast4) {
		t.Fatalf("listed token = %q, want masked value", tokens[0].Token)
	}

	var storedSecret, storedHash string
	if err := db.RawDB().QueryRowContext(ctx,
		"SELECT token_secret, token_hash FROM org_agent_tokens WHERE id = ?", token.ID,
	).Scan(&storedSecret, &storedHash); err != nil {
		t.Fatalf("query stored token: %v", err)
	}
	if storedSecret == token.Token {
		t.Fatal("database stored raw agent token")
	}
	if storedSecret == "" {
		t.Fatal("database did not store encrypted agent token")
	}
	if storedHash == "" {
		t.Fatal("database did not store agent token lookup hash")
	}

	sysadminToken, err := db.CreateAgentToken(ctx, "default", 1, "sysadmin-agent", []string{"SystemAdmin"}, nil)
	if err != nil {
		t.Fatalf("CreateAgentToken SystemAdmin: %v", err)
	}
	sysadminResolved, err := db.ResolveAgentToken(ctx, sysadminToken.Token)
	if err != nil {
		t.Fatalf("ResolveAgentToken SystemAdmin: %v", err)
	}
	if sysadminResolved == nil || strings.Join(sysadminResolved.Roles, ",") != "SystemAdmin" {
		t.Fatalf("SystemAdmin token roles = %#v", sysadminResolved)
	}
}

func findDashboardMeta(t *testing.T, dashboards []sqldb.DashboardMeta, name string) sqldb.DashboardMeta {
	t.Helper()
	for _, dashboard := range dashboards {
		if dashboard.Name == name {
			return dashboard
		}
	}
	t.Fatalf("dashboard %q not found in %#v", name, dashboards)
	return sqldb.DashboardMeta{}
}

func hasDashboardNamed(dashboards []sqldb.DashboardMeta, name string) bool {
	for _, dashboard := range dashboards {
		if dashboard.Name == name {
			return true
		}
	}
	return false
}

func assertDashboardWidgetType(t *testing.T, db sqldb.DB, dashboardID int, want string) {
	t.Helper()
	dashboard, err := db.GetDashboard(context.Background(), "default", dashboardID)
	if err != nil {
		t.Fatalf("GetDashboard(%d): %v", dashboardID, err)
	}
	if dashboard == nil {
		t.Fatalf("dashboard %d not found", dashboardID)
	}
	var widgets []struct {
		Type string `json:"type"`
	}
	if err := json.Unmarshal(dashboard.Widgets, &widgets); err != nil {
		t.Fatalf("unmarshal widgets: %v; raw=%s", err, string(dashboard.Widgets))
	}
	for _, widget := range widgets {
		if widget.Type == want {
			return
		}
	}
	t.Fatalf("widget type = %#v, want one %q", widgets, want)
}
