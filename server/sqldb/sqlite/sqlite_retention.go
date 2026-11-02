package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"time"
)

const (
	metricsPurgeInterval  = time.Hour
	metricsPurgeBatchSize = 5000
)

// ConfigureMetricsRetention starts a background loop that deletes
// device_metrics rows older than retention, once at start and then hourly.
// SQLite has no TimescaleDB retention policy, so the application does it.
// A non-positive retention stops any running purger.
func (db *SQLiteDB) ConfigureMetricsRetention(_ context.Context, retention time.Duration) error {
	db.stopMetricsPurger()
	if retention <= 0 {
		return nil
	}

	stop := make(chan struct{})
	done := make(chan struct{})
	db.metricsPurgeMu.Lock()
	db.metricsPurgeStop = stop
	db.metricsPurgeDone = done
	db.metricsPurgeMu.Unlock()

	go func() {
		defer close(done)
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		go func() {
			select {
			case <-stop:
				cancel()
			case <-ctx.Done():
			}
		}()

		ticker := time.NewTicker(metricsPurgeInterval)
		defer ticker.Stop()
		for {
			start := time.Now()
			deleted, err := db.PurgeMetricsBefore(ctx, start.Add(-retention))
			if err != nil && ctx.Err() == nil {
				log.Printf("SQLite metrics retention: purge failed after deleting %d rows: %v", deleted, err)
			} else if deleted > 0 {
				log.Printf("SQLite metrics retention: deleted %d rows older than %s in %s",
					deleted, retention, time.Since(start).Round(time.Millisecond))
			}
			select {
			case <-ticker.C:
			case <-stop:
				return
			}
		}
	}()
	return nil
}

func (db *SQLiteDB) stopMetricsPurger() {
	db.metricsPurgeMu.Lock()
	stop, done := db.metricsPurgeStop, db.metricsPurgeDone
	db.metricsPurgeStop, db.metricsPurgeDone = nil, nil
	db.metricsPurgeMu.Unlock()
	if stop != nil {
		close(stop)
		<-done
	}
}

// PurgeMetricsBefore deletes device_metrics rows with time before cutoff and
// returns the number of rows deleted. It works one metric at a time through
// idx_device_metrics_lookup and in small batches, so each write transaction
// stays short and ingest is not blocked behind one huge delete.
func (db *SQLiteDB) PurgeMetricsBefore(ctx context.Context, cutoff time.Time) (int64, error) {
	type metricKey struct{ orgID, deviceID, metricID int64 }

	rows, err := db.db.QueryContext(ctx, `
		SELECT d.org_id, md.device_id, md.id
		FROM metric_definitions md
		JOIN metric_devices d ON d.id = md.device_id`)
	if err != nil {
		return 0, fmt.Errorf("listing metrics: %w", err)
	}
	var keys []metricKey
	for rows.Next() {
		var k metricKey
		if err := rows.Scan(&k.orgID, &k.deviceID, &k.metricID); err != nil {
			rows.Close()
			return 0, fmt.Errorf("listing metrics: %w", err)
		}
		keys = append(keys, k)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return 0, fmt.Errorf("listing metrics: %w", err)
	}

	before := formatTimestamp(cutoff)
	var total int64
	for _, k := range keys {
		for {
			if err := ctx.Err(); err != nil {
				return total, err
			}
			res, err := db.db.ExecContext(ctx, `
				DELETE FROM device_metrics WHERE rowid IN (
					SELECT rowid FROM device_metrics INDEXED BY idx_device_metrics_lookup
					WHERE org_id = ? AND device_id = ? AND metric_id = ? AND time < ?
					LIMIT ?)`,
				k.orgID, k.deviceID, k.metricID, before, metricsPurgeBatchSize)
			if err != nil {
				return total, fmt.Errorf("purging metrics: %w", err)
			}
			n, _ := res.RowsAffected()
			total += n
			if n < metricsPurgeBatchSize {
				break
			}
		}
	}
	return total, nil
}

// dropRedundantRestoreIndexes drops idx_restore_<table>_N indexes created by a
// backup restore whose columns match keepIndex. Restoring a PostgreSQL backup
// recreates its indexes under restore names, and Migrate then adds its own, so
// large tables like device_metrics end up maintaining the same index twice.
func (db *SQLiteDB) dropRedundantRestoreIndexes(ctx context.Context, table, keepIndex string) error {
	keepCols, err := db.sqliteIndexColumns(ctx, keepIndex)
	if err != nil || len(keepCols) == 0 {
		return err
	}
	rows, err := db.db.QueryContext(ctx,
		`SELECT name FROM sqlite_master WHERE type = 'index' AND tbl_name = ? AND name LIKE ?`,
		table, "idx_restore_"+table+"_%")
	if err != nil {
		return err
	}
	var candidates []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			rows.Close()
			return err
		}
		candidates = append(candidates, name)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}

	for _, name := range candidates {
		cols, err := db.sqliteIndexColumns(ctx, name)
		if err != nil {
			return err
		}
		if !equalStrings(cols, keepCols) {
			continue
		}
		log.Printf("SQLite migration: dropping index %s (duplicates %s)", name, keepIndex)
		if _, err := db.db.ExecContext(ctx, fmt.Sprintf(`DROP INDEX IF EXISTS "%s"`, name)); err != nil {
			return err
		}
	}
	return nil
}

// sqliteIndexColumns returns the column names of an index in key order.
// Expression columns are returned as "".
func (db *SQLiteDB) sqliteIndexColumns(ctx context.Context, index string) ([]string, error) {
	rows, err := db.db.QueryContext(ctx, `SELECT name FROM pragma_index_info(?) ORDER BY seqno`, index)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var cols []string
	for rows.Next() {
		var name sql.NullString
		if err := rows.Scan(&name); err != nil {
			return nil, err
		}
		cols = append(cols, name.String)
	}
	return cols, rows.Err()
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
