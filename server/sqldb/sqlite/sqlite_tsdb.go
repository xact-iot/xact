package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/xact-iot/xact/sqldb"
)

// InsertMetrics writes one or more timestamped metric values for an organisation.
func (db *SQLiteDB) InsertMetrics(ctx context.Context, orgName string, entries []sqldb.MetricEntry) error {
	if len(entries) == 0 {
		return nil
	}

	tx, err := db.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("beginning transaction: %w", err)
	}
	committed := false
	defer func() {
		if !committed {
			db.clearMetricIDCaches()
		}
		_ = tx.Rollback()
	}()

	orgID, err := db.metricOrgID(ctx, tx, orgName)
	if err != nil {
		return err
	}

	stmt, err := tx.PrepareContext(ctx,
		"INSERT INTO device_metrics (time, org_id, device_id, metric_id, value) VALUES (?, ?, ?, ?, ?)",
	)
	if err != nil {
		return fmt.Errorf("preparing metric data insert: %w", err)
	}
	defer stmt.Close()

	for _, entry := range entries {
		deviceID, err := db.metricDeviceID(ctx, tx, orgID, entry.DeviceName)
		if err != nil {
			return err
		}
		metricID, err := db.metricDefinitionID(ctx, tx, deviceID, entry.MetricName)
		if err != nil {
			return err
		}
		if _, err := stmt.ExecContext(ctx,
			formatTimestamp(entry.Timestamp), orgID, deviceID, metricID, entry.Value,
		); err != nil {
			return fmt.Errorf("inserting metric data point: %w", err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("committing metric data points: %w", err)
	}
	committed = true
	return nil
}

type metricDeviceCacheKey struct {
	orgID int
	name  string
}

type metricDefinitionCacheKey struct {
	deviceID int
	name     string
}

func (db *SQLiteDB) metricOrgID(ctx context.Context, tx *sql.Tx, orgName string) (int, error) {
	if id, ok := db.metricOrgIDs.Load(orgName); ok {
		return id.(int), nil
	}

	var id int
	if err := tx.QueryRowContext(ctx,
		"SELECT id FROM organisations WHERE name = ?", orgName,
	).Scan(&id); err != nil {
		return 0, fmt.Errorf("organisation %q not found: %w", orgName, err)
	}
	db.metricOrgIDs.Store(orgName, id)
	return id, nil
}

func (db *SQLiteDB) metricDeviceID(ctx context.Context, tx *sql.Tx, orgID int, name string) (int, error) {
	key := metricDeviceCacheKey{orgID: orgID, name: name}
	if id, ok := db.metricDeviceIDs.Load(key); ok {
		return id.(int), nil
	}

	if _, err := tx.ExecContext(ctx,
		"INSERT OR IGNORE INTO metric_devices (org_id, name) VALUES (?, ?)",
		orgID, name,
	); err != nil {
		return 0, fmt.Errorf("upserting device %q: %w", name, err)
	}

	var id int
	if err := tx.QueryRowContext(ctx,
		"SELECT id FROM metric_devices WHERE org_id = ? AND name = ?",
		orgID, name,
	).Scan(&id); err != nil {
		return 0, fmt.Errorf("getting device ID for %q: %w", name, err)
	}
	db.metricDeviceIDs.Store(key, id)
	return id, nil
}

func (db *SQLiteDB) metricDefinitionID(ctx context.Context, tx *sql.Tx, deviceID int, name string) (int, error) {
	key := metricDefinitionCacheKey{deviceID: deviceID, name: name}
	if id, ok := db.metricDefinitionIDs.Load(key); ok {
		return id.(int), nil
	}

	if _, err := tx.ExecContext(ctx,
		"INSERT OR IGNORE INTO metric_definitions (device_id, name) VALUES (?, ?)",
		deviceID, name,
	); err != nil {
		return 0, fmt.Errorf("upserting metric %q: %w", name, err)
	}

	var id int
	if err := tx.QueryRowContext(ctx,
		"SELECT id FROM metric_definitions WHERE device_id = ? AND name = ?",
		deviceID, name,
	).Scan(&id); err != nil {
		return 0, fmt.Errorf("getting metric ID for %q: %w", name, err)
	}
	db.metricDefinitionIDs.Store(key, id)
	return id, nil
}

func (db *SQLiteDB) clearMetricIDCaches() {
	db.metricOrgIDs.Clear()
	db.metricDeviceIDs.Clear()
	db.metricDefinitionIDs.Clear()
}

// QueryMetricsRange returns time-ordered series for a device over [start, end].
func (db *SQLiteDB) QueryMetricsRange(ctx context.Context, orgName, deviceName string,
	metrics []string, start, end time.Time) ([]sqldb.MetricSeries, error) {

	if end.IsZero() {
		end = time.Now()
	}

	orgID, err := db.resolveOrgID(ctx, orgName)
	if err != nil {
		return nil, err
	}

	var deviceID int
	err = db.db.QueryRowContext(ctx,
		"SELECT id FROM metric_devices WHERE org_id = ? AND name = ?",
		orgID, deviceName,
	).Scan(&deviceID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("looking up device %q: %w", deviceName, err)
	}

	if len(metrics) == 0 {
		return nil, nil
	}

	query := `
		SELECT dm.time, md.name, dm.value
		FROM device_metrics dm
		JOIN metric_definitions md ON md.id = dm.metric_id
		WHERE dm.org_id = ? AND dm.device_id = ?
		  AND md.name IN ` + inClause(len(metrics)) + `
		  AND dm.time >= ? AND dm.time <= ?
		ORDER BY md.name, dm.time ASC
	`
	args := []any{orgID, deviceID}
	for _, m := range metrics {
		args = append(args, m)
	}
	args = append(args, formatTimestamp(start), formatTimestamp(end))

	rows, err := db.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("querying metrics range: %w", err)
	}
	defer rows.Close()

	return scanMetricSeries(rows)
}

// QueryMetricsByTagPaths returns time-ordered series whose device+metric path matches
// any of the given tagPaths.
func (db *SQLiteDB) QueryMetricsByTagPaths(ctx context.Context, orgName string,
	tagPaths []string, start, end time.Time) ([]sqldb.MetricSeries, error) {

	if len(tagPaths) == 0 {
		return nil, nil
	}
	if end.IsZero() {
		end = time.Now()
	}

	orgID, err := db.resolveOrgID(ctx, orgName)
	if err != nil {
		return nil, err
	}

	// Build OR conditions, one per tag path.
	var orConditions []string
	var pathArgs []any
	for _, p := range tagPaths {
		orConditions = append(orConditions,
			`(d.name || '.' || md.name = ? OR (? LIKE d.name || '.%' AND ? LIKE '%.' || md.name))`)
		pathArgs = append(pathArgs, p, p, p)
	}

	query := `
		SELECT dm.time, md.name, dm.value
		FROM device_metrics dm
		JOIN metric_definitions md ON md.id = dm.metric_id
		JOIN metric_devices d ON d.id = dm.device_id
		WHERE dm.org_id = ?
		  AND dm.time >= ? AND dm.time <= ?
		  AND (` + strings.Join(orConditions, " OR ") + `)
		ORDER BY md.name, dm.time ASC
	`
	args := []any{orgID, formatTimestamp(start), formatTimestamp(end)}
	args = append(args, pathArgs...)

	rows, err := db.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("querying metrics by tag paths: %w", err)
	}
	defer rows.Close()

	return scanMetricSeries(rows)
}

// QueryMetricsSince returns series for the listed metrics with time > startTime.
func (db *SQLiteDB) QueryMetricsSince(ctx context.Context, orgName, deviceName string,
	metrics []string, startMetric string, startTime time.Time) ([]sqldb.MetricSeries, error) {

	orgID, err := db.resolveOrgID(ctx, orgName)
	if err != nil {
		return nil, err
	}

	var deviceID int
	err = db.db.QueryRowContext(ctx,
		"SELECT id FROM metric_devices WHERE org_id = ? AND name = ?",
		orgID, deviceName,
	).Scan(&deviceID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("looking up device %q: %w", deviceName, err)
	}

	if len(metrics) == 0 {
		return nil, nil
	}

	query := `
		SELECT dm.time, md.name, dm.value
		FROM device_metrics dm
		JOIN metric_definitions md ON md.id = dm.metric_id
		WHERE dm.org_id = ? AND dm.device_id = ?
		  AND md.name IN ` + inClause(len(metrics)) + `
		  AND dm.time > ?
		ORDER BY md.name, dm.time ASC
	`
	args := []any{orgID, deviceID}
	for _, m := range metrics {
		args = append(args, m)
	}
	args = append(args, formatTimestamp(startTime))

	rows, err := db.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("querying metrics since: %w", err)
	}
	defer rows.Close()

	return scanMetricSeries(rows)
}

// ConfigureMetricsRetention is a no-op for SQLite (no TimescaleDB).
func (db *SQLiteDB) ConfigureMetricsRetention(_ context.Context, _ time.Duration) error {
	return nil
}

// scanMetricSeries reads rows of (time TEXT, name TEXT, value REAL) and groups by name.
func scanMetricSeries(rows *sql.Rows) ([]sqldb.MetricSeries, error) {
	seriesMap := make(map[string]*sqldb.MetricSeries)
	var order []string

	for rows.Next() {
		var tsStr, name string
		var value float32
		if err := rows.Scan(&tsStr, &name, &value); err != nil {
			return nil, fmt.Errorf("scanning metric row: %w", err)
		}
		if _, exists := seriesMap[name]; !exists {
			seriesMap[name] = &sqldb.MetricSeries{Name: name}
			order = append(order, name)
		}
		seriesMap[name].Data = append(seriesMap[name].Data, sqldb.MetricPoint{
			Timestamp: parseTimestamp(tsStr),
			Value:     value,
		})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterating metric rows: %w", err)
	}

	result := make([]sqldb.MetricSeries, 0, len(order))
	for _, name := range order {
		result = append(result, *seriesMap[name])
	}
	return result, nil
}
