package psql

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/xact-iot/xact/events"
	"github.com/xact-iot/xact/sqldb"
)

// InsertEventEntries batch-inserts event entries into the events table.
func (db *PostgresDB) InsertEventEntries(ctx context.Context, entries []events.EventEntry) error {
	if len(entries) == 0 {
		return nil
	}

	var b strings.Builder
	b.WriteString("INSERT INTO events (timestamp, server, org_name, user_id, severity, notification_id, device, message, params) VALUES ")

	args := make([]any, 0, len(entries)*9)
	for i, e := range entries {
		if i > 0 {
			b.WriteString(", ")
		}
		base := i*9 + 1
		fmt.Fprintf(&b, "($%d, $%d, $%d, $%d, $%d, $%d, $%d, $%d, $%d)",
			base, base+1, base+2, base+3, base+4, base+5, base+6, base+7, base+8)

		var params []byte
		if len(e.Params) > 0 {
			params, _ = json.Marshal(e.Params)
		}

		args = append(args, e.Timestamp, e.Server, e.OrgName, e.UserID, e.Severity, e.NotificationID, e.Device, e.Message, params)
	}

	_, err := db.pool.Exec(ctx, b.String(), args...)
	if err != nil {
		return fmt.Errorf("inserting event entries: %w", err)
	}
	return nil
}

// QueryEvents returns event entries matching the filter, ordered by timestamp descending.
func (db *PostgresDB) QueryEvents(ctx context.Context, filter sqldb.EventFilter) ([]events.EventEntry, error) {
	var b strings.Builder
	b.WriteString("SELECT id, timestamp, server, org_name, user_id, severity, notification_id, device, message, params FROM events WHERE 1=1")

	var args []any
	argN := 1

	if filter.AfterID > 0 {
		fmt.Fprintf(&b, " AND id > $%d", argN)
		args = append(args, filter.AfterID)
		argN++
	}
	if filter.OrgName != "" {
		fmt.Fprintf(&b, " AND org_name = $%d", argN)
		args = append(args, filter.OrgName)
		argN++
	}
	if filter.UserID != 0 {
		fmt.Fprintf(&b, " AND user_id = $%d", argN)
		args = append(args, filter.UserID)
		argN++
	}
	if filter.Severity != "" {
		fmt.Fprintf(&b, " AND severity = $%d", argN)
		args = append(args, filter.Severity)
		argN++
	}
	if filter.Device != "" {
		fmt.Fprintf(&b, " AND device = $%d", argN)
		args = append(args, filter.Device)
		argN++
	}
	if filter.NotificationID != 0 {
		fmt.Fprintf(&b, " AND notification_id = $%d", argN)
		args = append(args, filter.NotificationID)
		argN++
	}
	if filter.Search != "" {
		fmt.Fprintf(&b, " AND message ILIKE '%%' || $%d || '%%'", argN)
		args = append(args, filter.Search)
		argN++
	}
	if filter.StartTime != nil {
		fmt.Fprintf(&b, " AND timestamp >= $%d", argN)
		args = append(args, *filter.StartTime)
		argN++
	}
	if filter.EndTime != nil {
		fmt.Fprintf(&b, " AND timestamp <= $%d", argN)
		args = append(args, *filter.EndTime)
		argN++
	}

	b.WriteString(" ORDER BY timestamp DESC")

	limit := filter.Limit
	if limit <= 0 {
		limit = 100
	}
	fmt.Fprintf(&b, " LIMIT $%d", argN)
	args = append(args, limit)

	rows, err := db.pool.Query(ctx, b.String(), args...)
	if err != nil {
		return nil, fmt.Errorf("querying events: %w", err)
	}
	defer rows.Close()

	var entries []events.EventEntry
	for rows.Next() {
		var e events.EventEntry
		var params []byte
		if err := rows.Scan(&e.ID, &e.Timestamp, &e.Server, &e.OrgName, &e.UserID, &e.Severity, &e.NotificationID, &e.Device, &e.Message, &params); err != nil {
			return nil, fmt.Errorf("scanning event row: %w", err)
		}
		if len(params) > 0 {
			json.Unmarshal(params, &e.Params)
		}
		entries = append(entries, e)
	}
	return entries, rows.Err()
}

// PurgeEventsBefore deletes event entries older than the given time.
func (db *PostgresDB) PurgeEventsBefore(ctx context.Context, before time.Time) error {
	_, err := db.pool.Exec(ctx, "DELETE FROM events WHERE timestamp < $1", before)
	if err != nil {
		return fmt.Errorf("purging events: %w", err)
	}
	return nil
}
