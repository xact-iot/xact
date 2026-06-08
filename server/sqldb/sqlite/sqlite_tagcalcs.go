package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/xact-iot/xact/sqldb"
)

// ListTagCalcs returns all tag calcs for an organisation.
func (db *SQLiteDB) ListTagCalcs(ctx context.Context, org string) ([]sqldb.TagCalc, error) {
	rows, err := db.db.QueryContext(ctx, `
		SELECT id, org_name, name, description, output_tag, expression, interval_seconds, enabled, created_at, updated_at
		FROM tag_calcs
		WHERE org_name = ?
		ORDER BY name
	`, org)
	if err != nil {
		return nil, fmt.Errorf("listing tag calcs: %w", err)
	}
	defer rows.Close()

	var scripts []sqldb.TagCalc
	for rows.Next() {
		s, err := scanTagCalc(rows)
		if err != nil {
			return nil, err
		}
		scripts = append(scripts, s)
	}
	return scripts, rows.Err()
}

// GetTagCalc returns a single tag calc by ID. Returns nil if not found.
func (db *SQLiteDB) GetTagCalc(ctx context.Context, org string, id int) (*sqldb.TagCalc, error) {
	rows, err := db.db.QueryContext(ctx, `
		SELECT id, org_name, name, description, output_tag, expression, interval_seconds, enabled, created_at, updated_at
		FROM tag_calcs
		WHERE org_name = ? AND id = ?
	`, org, id)
	if err != nil {
		return nil, fmt.Errorf("getting tag calc: %w", err)
	}
	defer rows.Close()

	if !rows.Next() {
		return nil, rows.Err()
	}
	s, err := scanTagCalc(rows)
	if err != nil {
		return nil, fmt.Errorf("scanning tag calc: %w", err)
	}
	return &s, nil
}

// CreateTagCalc inserts a new tag calc. Sets s.ID, s.CreatedAt, s.UpdatedAt on success.
func (db *SQLiteDB) CreateTagCalc(ctx context.Context, org string, s *sqldb.TagCalc) error {
	enabled := 0
	if s.Enabled {
		enabled = 1
	}
	now := formatTimestamp(time.Now())
	result, err := db.db.ExecContext(ctx, `
		INSERT INTO tag_calcs (org_name, name, description, output_tag, expression, interval_seconds, enabled, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, org, s.Name, s.Description, s.OutputTag, s.Expression, s.IntervalSeconds, enabled, now, now)
	if err != nil {
		return fmt.Errorf("creating tag calc: %w", err)
	}
	id, _ := result.LastInsertId()
	s.ID = int(id)
	s.OrgName = org
	s.CreatedAt = parseTimestamp(now)
	s.UpdatedAt = parseTimestamp(now)
	return nil
}

// UpdateTagCalc replaces an existing tag calc identified by ID.
func (db *SQLiteDB) UpdateTagCalc(ctx context.Context, org string, id int, s *sqldb.TagCalc) error {
	enabled := 0
	if s.Enabled {
		enabled = 1
	}
	now := formatTimestamp(time.Now())
	result, err := db.db.ExecContext(ctx, `
		UPDATE tag_calcs
		SET name = ?, description = ?, output_tag = ?, expression = ?, interval_seconds = ?, enabled = ?, updated_at = ?
		WHERE org_name = ? AND id = ?
	`, s.Name, s.Description, s.OutputTag, s.Expression, s.IntervalSeconds, enabled, now, org, id)
	if err != nil {
		return fmt.Errorf("updating tag calc: %w", err)
	}
	n, _ := result.RowsAffected()
	if n == 0 {
		return fmt.Errorf("tag calc not found")
	}
	return nil
}

// DeleteTagCalc removes a tag calc by ID.
func (db *SQLiteDB) DeleteTagCalc(ctx context.Context, org string, id int) error {
	result, err := db.db.ExecContext(ctx,
		"DELETE FROM tag_calcs WHERE org_name = ? AND id = ?", org, id)
	if err != nil {
		return fmt.Errorf("deleting tag calc: %w", err)
	}
	n, _ := result.RowsAffected()
	if n == 0 {
		return fmt.Errorf("tag calc not found")
	}
	return nil
}

// scanTagCalc scans a tag calc from rows.
func scanTagCalc(rows *sql.Rows) (sqldb.TagCalc, error) {
	var s sqldb.TagCalc
	var enabledInt int
	var createdAtStr, updatedAtStr string
	if err := rows.Scan(&s.ID, &s.OrgName, &s.Name, &s.Description,
		&s.OutputTag, &s.Expression, &s.IntervalSeconds, &enabledInt,
		&createdAtStr, &updatedAtStr); err != nil {
		return s, fmt.Errorf("scanning tag calc: %w", err)
	}
	s.Enabled = enabledInt != 0
	s.CreatedAt = parseTimestamp(createdAtStr)
	s.UpdatedAt = parseTimestamp(updatedAtStr)
	return s, nil
}

