package sqlite

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"
	"time"

	"github.com/xact-iot/xact/sqldb"
)

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
	if len(dashboards) != 3 {
		t.Fatalf("starter dashboard count = %d, want 3: %#v", len(dashboards), dashboards)
	}

	dashboard := findDashboardMeta(t, dashboards, sqldb.StarterDashboardName)
	if dashboard.IsCategory {
		t.Fatal("DASHBOARD should be a dashboard, not a category")
	}
	if dashboard.ParentID != nil {
		t.Fatalf("DASHBOARD parent = %v, want nil", *dashboard.ParentID)
	}
	assertDashboardWidgetType(t, db, dashboard.ID, "manual-widget")

	monitoring := findDashboardMeta(t, dashboards, sqldb.StarterMonitoringCategory)
	if !monitoring.IsCategory {
		t.Fatal("MONTORING should be a category")
	}
	if monitoring.ParentID != nil {
		t.Fatalf("MONTORING parent = %v, want nil", *monitoring.ParentID)
	}

	tagView := findDashboardMeta(t, dashboards, sqldb.StarterTagViewName)
	if tagView.IsCategory {
		t.Fatal("Tag View should be a dashboard, not a category")
	}
	if tagView.ParentID == nil || *tagView.ParentID != monitoring.ID {
		t.Fatalf("Tag View parent = %v, want %d", tagView.ParentID, monitoring.ID)
	}
	assertDashboardWidgetType(t, db, tagView.ID, "tags-manager-widget")
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
	if len(widgets) != 1 || widgets[0].Type != want {
		t.Fatalf("widget type = %#v, want single %q", widgets, want)
	}
}
