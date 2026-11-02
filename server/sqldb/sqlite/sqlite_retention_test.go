package sqlite

import (
	"context"
	"testing"
	"time"

	"github.com/xact-iot/xact/sqldb"
)

func countDeviceMetrics(t *testing.T, db *SQLiteDB) int {
	t.Helper()
	var n int
	if err := db.db.QueryRow(`SELECT COUNT(*) FROM device_metrics`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func TestSQLitePurgeMetricsBefore(t *testing.T) {
	ctx := context.Background()
	db := newSQLiteCRUDTestDB(t)
	now := time.Now().UTC()

	var points []sqldb.MetricEntry
	for _, device := range []string{"pump-1", "pump-2"} {
		for _, metric := range []string{"temp", "flow"} {
			for i := 0; i < metricsPurgeBatchSize+10; i++ {
				points = append(points, sqldb.MetricEntry{
					DeviceName: device, MetricName: metric,
					Timestamp: now.Add(-48*time.Hour + time.Duration(i)*time.Millisecond), Value: 1,
				})
			}
			points = append(points, sqldb.MetricEntry{DeviceName: device, MetricName: metric, Timestamp: now, Value: 2})
		}
	}
	if err := db.InsertMetrics(ctx, "default", points); err != nil {
		t.Fatalf("InsertMetrics: %v", err)
	}
	// Rows restored from an older backup use Go's time.String() format.
	if _, err := db.db.Exec(`UPDATE device_metrics SET time = ? WHERE rowid = 1`,
		now.Add(-72*time.Hour).Format("2006-01-02 15:04:05.999999999 -0700 MST")); err != nil {
		t.Fatal(err)
	}

	deleted, err := db.PurgeMetricsBefore(ctx, now.Add(-24*time.Hour))
	if err != nil {
		t.Fatalf("PurgeMetricsBefore: %v", err)
	}
	if want := int64(4 * (metricsPurgeBatchSize + 10)); deleted != want {
		t.Fatalf("deleted = %d, want %d", deleted, want)
	}
	if n := countDeviceMetrics(t, db); n != 4 {
		t.Fatalf("remaining rows = %d, want 4", n)
	}
}

func TestSQLiteConfigureMetricsRetentionPurgesInBackground(t *testing.T) {
	ctx := context.Background()
	db := newSQLiteCRUDTestDB(t)
	now := time.Now().UTC()
	if err := db.InsertMetrics(ctx, "default", []sqldb.MetricEntry{
		{DeviceName: "pump-1", MetricName: "temp", Timestamp: now.Add(-48 * time.Hour), Value: 1},
		{DeviceName: "pump-1", MetricName: "temp", Timestamp: now, Value: 2},
	}); err != nil {
		t.Fatalf("InsertMetrics: %v", err)
	}

	if err := db.ConfigureMetricsRetention(ctx, 24*time.Hour); err != nil {
		t.Fatalf("ConfigureMetricsRetention: %v", err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for countDeviceMetrics(t, db) != 1 {
		if time.Now().After(deadline) {
			t.Fatalf("old metric not purged; rows = %d", countDeviceMetrics(t, db))
		}
		time.Sleep(10 * time.Millisecond)
	}
	db.stopMetricsPurger()
}

func TestSQLiteMigrateDropsDuplicateRestoreMetricIndex(t *testing.T) {
	ctx := context.Background()
	db := newSQLiteCRUDTestDB(t)
	for _, stmt := range []string{
		`CREATE INDEX idx_restore_device_metrics_1 ON device_metrics (time)`,
		`CREATE INDEX idx_restore_device_metrics_2 ON device_metrics (org_id, device_id, metric_id, time)`,
	} {
		if _, err := db.db.Exec(stmt); err != nil {
			t.Fatal(err)
		}
	}
	if err := db.Migrate(ctx); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	rows, err := db.db.Query(`SELECT name FROM sqlite_master WHERE type = 'index' AND tbl_name = 'device_metrics' ORDER BY name`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var names []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			t.Fatal(err)
		}
		names = append(names, name)
	}
	want := []string{"idx_device_metrics_lookup", "idx_restore_device_metrics_1"}
	if !equalStrings(names, want) {
		t.Fatalf("indexes = %v, want %v", names, want)
	}
}
