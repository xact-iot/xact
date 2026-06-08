package psql

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/xact-iot/xact/sqldb"
)

func (db *PostgresDB) ListTagCalcs(ctx context.Context, org string) ([]sqldb.TagCalc, error) {
	rows, err := db.pool.Query(ctx, `
		SELECT id, org_name, name, description, output_tag, expression, interval_seconds, enabled, created_at, updated_at
		FROM tag_calcs
		WHERE org_name = $1
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

func (db *PostgresDB) GetTagCalc(ctx context.Context, org string, id int) (*sqldb.TagCalc, error) {
	row := db.pool.QueryRow(ctx, `
		SELECT id, org_name, name, description, output_tag, expression, interval_seconds, enabled, created_at, updated_at
		FROM tag_calcs
		WHERE org_name = $1 AND id = $2
	`, org, id)

	s, err := scanTagCalcRow(row)
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("getting tag calc: %w", err)
	}
	return &s, nil
}

func (db *PostgresDB) CreateTagCalc(ctx context.Context, org string, s *sqldb.TagCalc) error {
	err := db.pool.QueryRow(ctx, `
		INSERT INTO tag_calcs (org_name, name, description, output_tag, expression, interval_seconds, enabled)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		RETURNING id, created_at, updated_at
	`, org, s.Name, s.Description, s.OutputTag, s.Expression, s.IntervalSeconds, s.Enabled).
		Scan(&s.ID, &s.CreatedAt, &s.UpdatedAt)
	if err != nil {
		return fmt.Errorf("creating tag calc: %w", err)
	}
	s.OrgName = org
	return nil
}

func (db *PostgresDB) UpdateTagCalc(ctx context.Context, org string, id int, s *sqldb.TagCalc) error {
	tag, err := db.pool.Exec(ctx, `
		UPDATE tag_calcs
		SET name = $3, description = $4, output_tag = $5, expression = $6, interval_seconds = $7, enabled = $8, updated_at = NOW()
		WHERE org_name = $1 AND id = $2
	`, org, id, s.Name, s.Description, s.OutputTag, s.Expression, s.IntervalSeconds, s.Enabled)
	if err != nil {
		return fmt.Errorf("updating tag calc: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("tag calc not found")
	}
	return nil
}

func (db *PostgresDB) DeleteTagCalc(ctx context.Context, org string, id int) error {
	tag, err := db.pool.Exec(ctx, `
		DELETE FROM tag_calcs WHERE org_name = $1 AND id = $2
	`, org, id)
	if err != nil {
		return fmt.Errorf("deleting tag calc: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("tag calc not found")
	}
	return nil
}

func scanTagCalc(rows pgx.Rows) (sqldb.TagCalc, error) {
	var s sqldb.TagCalc
	if err := rows.Scan(&s.ID, &s.OrgName, &s.Name, &s.Description, &s.OutputTag, &s.Expression, &s.IntervalSeconds, &s.Enabled, &s.CreatedAt, &s.UpdatedAt); err != nil {
		return s, fmt.Errorf("scanning tag calc: %w", err)
	}
	return s, nil
}

func scanTagCalcRow(row pgx.Row) (sqldb.TagCalc, error) {
	var s sqldb.TagCalc
	if err := row.Scan(&s.ID, &s.OrgName, &s.Name, &s.Description, &s.OutputTag, &s.Expression, &s.IntervalSeconds, &s.Enabled, &s.CreatedAt, &s.UpdatedAt); err != nil {
		return s, err
	}
	return s, nil
}
