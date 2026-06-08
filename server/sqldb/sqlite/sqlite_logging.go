package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/xact-iot/xact/events"
	"github.com/xact-iot/xact/sqldb"
)

// InsertEventEntries batch-inserts event entries into the events table.
func (db *SQLiteDB) InsertEventEntries(ctx context.Context, entries []events.EventEntry) error {
	if len(entries) == 0 {
		return nil
	}

	tx, err := db.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("beginning transaction: %w", err)
	}
	defer tx.Rollback()

	stmt, err := tx.PrepareContext(ctx, `
		INSERT INTO events (timestamp, server, org_name, user_id, severity, notification_id, device, message, params)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
	`)
	if err != nil {
		return fmt.Errorf("preparing event insert: %w", err)
	}
	defer stmt.Close()

	for _, e := range entries {
		var params []byte
		if len(e.Params) > 0 {
			params, _ = json.Marshal(e.Params)
		}
		if _, err := stmt.ExecContext(ctx,
			formatTimestamp(e.Timestamp), e.Server, e.OrgName, e.UserID,
			e.Severity, e.NotificationID, e.Device, e.Message, params,
		); err != nil {
			return fmt.Errorf("inserting event entry: %w", err)
		}
	}
	return tx.Commit()
}

// QueryEvents returns event entries matching the filter, ordered by timestamp descending.
func (db *SQLiteDB) QueryEvents(ctx context.Context, filter sqldb.EventFilter) ([]events.EventEntry, error) {
	var b strings.Builder
	b.WriteString("SELECT id, timestamp, server, org_name, user_id, severity, notification_id, device, message, params FROM events WHERE 1=1")

	var args []any

	if filter.AfterID > 0 {
		b.WriteString(" AND id > ?")
		args = append(args, filter.AfterID)
	}
	if filter.OrgName != "" {
		b.WriteString(" AND org_name = ?")
		args = append(args, filter.OrgName)
	}
	if filter.UserID != 0 {
		b.WriteString(" AND user_id = ?")
		args = append(args, filter.UserID)
	}
	if filter.Severity != "" {
		b.WriteString(" AND severity = ?")
		args = append(args, filter.Severity)
	}
	if filter.Device != "" {
		b.WriteString(" AND device = ?")
		args = append(args, filter.Device)
	}
	if filter.NotificationID != 0 {
		b.WriteString(" AND notification_id = ?")
		args = append(args, filter.NotificationID)
	}
	if filter.Search != "" {
		b.WriteString(" AND lower(message) LIKE '%' || lower(?) || '%'")
		args = append(args, filter.Search)
	}
	if filter.StartTime != nil {
		b.WriteString(" AND timestamp >= ?")
		args = append(args, formatTimestamp(*filter.StartTime))
	}
	if filter.EndTime != nil {
		b.WriteString(" AND timestamp <= ?")
		args = append(args, formatTimestamp(*filter.EndTime))
	}

	b.WriteString(" ORDER BY timestamp DESC")

	limit := filter.Limit
	if limit <= 0 {
		limit = 100
	}
	b.WriteString(" LIMIT ?")
	args = append(args, limit)

	rows, err := db.db.QueryContext(ctx, b.String(), args...)
	if err != nil {
		return nil, fmt.Errorf("querying events: %w", err)
	}
	defer rows.Close()

	var entries []events.EventEntry
	for rows.Next() {
		var e events.EventEntry
		var tsStr string
		var userID sql.NullInt64
		var params sql.NullString
		if err := rows.Scan(&e.ID, &tsStr, &e.Server, &e.OrgName, &userID,
			&e.Severity, &e.NotificationID, &e.Device, &e.Message, &params); err != nil {
			return nil, fmt.Errorf("scanning event row: %w", err)
		}
		e.Timestamp = parseTimestamp(tsStr)
		if userID.Valid {
			id := int(userID.Int64)
			e.UserID = &id
		}
		if params.Valid && len(params.String) > 0 {
			json.Unmarshal([]byte(params.String), &e.Params)
		}
		entries = append(entries, e)
	}
	return entries, rows.Err()
}

// PurgeEventsBefore deletes event entries older than the given time.
func (db *SQLiteDB) PurgeEventsBefore(ctx context.Context, before time.Time) error {
	_, err := db.db.ExecContext(ctx,
		"DELETE FROM events WHERE timestamp < ?", formatTimestamp(before))
	if err != nil {
		return fmt.Errorf("purging events: %w", err)
	}
	return nil
}
