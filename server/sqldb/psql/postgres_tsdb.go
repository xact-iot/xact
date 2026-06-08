package psql

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/xact-iot/xact/sqldb"
)

// InsertMetrics writes one or more timestamped metric values for an organisation.
func (db *PostgresDB) InsertMetrics(ctx context.Context, orgName string, entries []sqldb.MetricEntry) error {
	if len(entries) == 0 {
		return nil
	}

	tx, err := db.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("beginning transaction: %w", err)
	}
	committed := false
	defer func() {
		if !committed {
			db.clearMetricIDCaches()
		}
		_ = tx.Rollback(ctx)
	}()

	orgID, err := db.metricOrgID(ctx, tx, orgName)
	if err != nil {
		return err
	}

	rows := make([][]any, 0, len(entries))
	for _, entry := range entries {
		deviceID, err := db.metricDeviceID(ctx, tx, orgID, entry.DeviceName)
		if err != nil {
			return err
		}
		metricID, err := db.metricDefinitionID(ctx, tx, deviceID, entry.MetricName)
		if err != nil {
			return err
		}
		rows = append(rows, []any{entry.Timestamp, orgID, deviceID, metricID, entry.Value})
	}

	if _, err := tx.CopyFrom(ctx,
		pgx.Identifier{"device_metrics"},
		[]string{"time", "org_id", "device_id", "metric_id", "value"},
		pgx.CopyFromRows(rows),
	); err != nil {
		return fmt.Errorf("copying metric data points: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
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

func (db *PostgresDB) metricOrgID(ctx context.Context, tx pgx.Tx, orgName string) (int, error) {
	if id, ok := db.metricOrgIDs.Load(orgName); ok {
		return id.(int), nil
	}

	var id int
	if err := tx.QueryRow(ctx,
		"SELECT id FROM organisations WHERE name = $1", orgName,
	).Scan(&id); err != nil {
		return 0, fmt.Errorf("organisation %q not found: %w", orgName, err)
	}
	db.metricOrgIDs.Store(orgName, id)
	return id, nil
}

func (db *PostgresDB) metricDeviceID(ctx context.Context, tx pgx.Tx, orgID int, name string) (int, error) {
	key := metricDeviceCacheKey{orgID: orgID, name: name}
	if id, ok := db.metricDeviceIDs.Load(key); ok {
		return id.(int), nil
	}

	var id int
	if err := tx.QueryRow(ctx, `
		INSERT INTO metric_devices (org_id, name) VALUES ($1, $2)
		ON CONFLICT (org_id, name) DO UPDATE SET name = EXCLUDED.name
		RETURNING id
	`, orgID, name).Scan(&id); err != nil {
		return 0, fmt.Errorf("upserting device %q: %w", name, err)
	}
	db.metricDeviceIDs.Store(key, id)
	return id, nil
}

func (db *PostgresDB) metricDefinitionID(ctx context.Context, tx pgx.Tx, deviceID int, name string) (int, error) {
	key := metricDefinitionCacheKey{deviceID: deviceID, name: name}
	if id, ok := db.metricDefinitionIDs.Load(key); ok {
		return id.(int), nil
	}

	var id int
	if err := tx.QueryRow(ctx, `
		INSERT INTO metric_definitions (device_id, name) VALUES ($1, $2)
		ON CONFLICT (device_id, name) DO UPDATE SET name = EXCLUDED.name
		RETURNING id
	`, deviceID, name).Scan(&id); err != nil {
		return 0, fmt.Errorf("upserting metric %q: %w", name, err)
	}
	db.metricDefinitionIDs.Store(key, id)
	return id, nil
}

func (db *PostgresDB) clearMetricIDCaches() {
	db.metricOrgIDs.Clear()
	db.metricDeviceIDs.Clear()
	db.metricDefinitionIDs.Clear()
}

// QueryMetricsRange returns time-ordered series for a device over [start, end].
func (db *PostgresDB) QueryMetricsRange(ctx context.Context, orgName, deviceName string,
	metrics []string, start, end time.Time) ([]sqldb.MetricSeries, error) {

	if end.IsZero() {
		end = time.Now()
	}

	orgID, err := db.resolveOrgID(ctx, orgName)
	if err != nil {
		return nil, err
	}

	var deviceID int
	if err := db.pool.QueryRow(ctx,
		"SELECT id FROM metric_devices WHERE org_id = $1 AND name = $2",
		orgID, deviceName,
	).Scan(&deviceID); err == pgx.ErrNoRows {
		return nil, nil
	} else if err != nil {
		return nil, fmt.Errorf("looking up device %q: %w", deviceName, err)
	}

	rows, err := db.pool.Query(ctx, `
		SELECT dm.time, md.name, dm.value
		FROM device_metrics dm
		JOIN metric_definitions md ON md.id = dm.metric_id
		WHERE dm.org_id = $1 AND dm.device_id = $2
		  AND md.name = ANY($3)
		  AND dm.time >= $4 AND dm.time <= $5
		ORDER BY md.name, dm.time ASC
	`, orgID, deviceID, metrics, start, end)
	if err != nil {
		return nil, fmt.Errorf("querying metrics range: %w", err)
	}
	defer rows.Close()

	return scanMetricSeries(rows)
}

// QueryMetricsByTagPaths returns time-ordered series whose device+metric path matches
// any of the given tagPaths. Intermediate RTDB nodes are handled via LIKE matching:
// a stored "device.metric" matches tagPath if the path starts with "device." and
// ends with ".metric" (or equals "device.metric" exactly).
func (db *PostgresDB) QueryMetricsByTagPaths(ctx context.Context, orgName string,
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

	rows, err := db.pool.Query(ctx, `
		SELECT dm.time, md.name, dm.value
		FROM device_metrics dm
		JOIN metric_definitions md ON md.id = dm.metric_id
		JOIN metric_devices d ON d.id = dm.device_id
		WHERE dm.org_id = $1
		  AND dm.time >= $2 AND dm.time <= $3
		  AND EXISTS (
		    SELECT 1 FROM unnest($4::text[]) AS p(path)
		    WHERE p.path = d.name || '.' || md.name
		       OR (p.path LIKE (d.name || '.%') AND p.path LIKE ('%.' || md.name))
		  )
		ORDER BY md.name, dm.time ASC
	`, orgID, start, end, tagPaths)
	if err != nil {
		return nil, fmt.Errorf("querying metrics by tag paths: %w", err)
	}
	defer rows.Close()

	return scanMetricSeries(rows)
}

// QueryMetricsSince returns series for the listed metrics with time > startTime.
func (db *PostgresDB) QueryMetricsSince(ctx context.Context, orgName, deviceName string,
	metrics []string, startMetric string, startTime time.Time) ([]sqldb.MetricSeries, error) {

	orgID, err := db.resolveOrgID(ctx, orgName)
	if err != nil {
		return nil, err
	}

	var deviceID int
	if err := db.pool.QueryRow(ctx,
		"SELECT id FROM metric_devices WHERE org_id = $1 AND name = $2",
		orgID, deviceName,
	).Scan(&deviceID); err == pgx.ErrNoRows {
		return nil, nil
	} else if err != nil {
		return nil, fmt.Errorf("looking up device %q: %w", deviceName, err)
	}

	rows, err := db.pool.Query(ctx, `
		SELECT dm.time, md.name, dm.value
		FROM device_metrics dm
		JOIN metric_definitions md ON md.id = dm.metric_id
		WHERE dm.org_id = $1 AND dm.device_id = $2
		  AND md.name = ANY($3)
		  AND dm.time > $4
		ORDER BY md.name, dm.time ASC
	`, orgID, deviceID, metrics, startTime)
	if err != nil {
		return nil, fmt.Errorf("querying metrics since: %w", err)
	}
	defer rows.Close()

	return scanMetricSeries(rows)
}

// ConfigureMetricsRetention updates the TimescaleDB data retention policy.
// Logs a warning and returns nil if TimescaleDB is not installed.
func (db *PostgresDB) ConfigureMetricsRetention(ctx context.Context, retention time.Duration) error {
	days := int(retention.Hours() / 24)

	if _, err := db.pool.Exec(ctx,
		"SELECT remove_retention_policy('device_metrics', if_exists => true)",
	); err != nil {
		log.Printf("metrics retention: remove policy (TimescaleDB may not be installed): %v", err)
		return nil
	}

	if _, err := db.pool.Exec(ctx,
		"SELECT add_retention_policy('device_metrics', make_interval(days => $1))", days,
	); err != nil {
		log.Printf("metrics retention: add policy: %v", err)
		return nil
	}

	return nil
}

// scanMetricSeries reads rows of (time, name, value) and groups them by name.
func scanMetricSeries(rows pgx.Rows) ([]sqldb.MetricSeries, error) {
	seriesMap := make(map[string]*sqldb.MetricSeries)
	var order []string

	for rows.Next() {
		var t time.Time
		var name string
		var value float32
		if err := rows.Scan(&t, &name, &value); err != nil {
			return nil, fmt.Errorf("scanning metric row: %w", err)
		}
		if _, exists := seriesMap[name]; !exists {
			seriesMap[name] = &sqldb.MetricSeries{Name: name}
			order = append(order, name)
		}
		seriesMap[name].Data = append(seriesMap[name].Data, sqldb.MetricPoint{
			Timestamp: t,
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
