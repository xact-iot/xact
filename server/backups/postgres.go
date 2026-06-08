package backups

import (
	"context"
	"database/sql"
	"encoding/csv"
	"fmt"
	"io"
	"strings"
)

type PostgresAdapter struct {
	DB *sql.DB
}

func (p *PostgresAdapter) hypertables(ctx context.Context) (map[string]string, error) {

	rows, err := p.DB.QueryContext(ctx, `
	SELECT hypertable_name, time_column_name
	FROM timescaledb_information.hypertables
	`)

	if err != nil {
		return nil, err
	}

	defer rows.Close()

	result := map[string]string{}

	for rows.Next() {

		var name string
		var timecol string

		if err := rows.Scan(&name, &timecol); err != nil {
			return nil, err
		}

		result[name] = timecol
	}

	return result, rows.Err()
}

func (p *PostgresAdapter) ListTables(ctx context.Context) ([]string, error) {

	rows, err := p.DB.QueryContext(ctx, `
	SELECT tablename
	FROM pg_tables
	WHERE schemaname='public'
	`)

	if err != nil {
		return nil, err
	}

	defer rows.Close()

	var tables []string

	for rows.Next() {

		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, err
		}

		tables = append(tables, name)
	}

	return tables, rows.Err()
}

func (p *PostgresAdapter) DropPublicTables(ctx context.Context) error {
	tables, err := p.ListTables(ctx)
	if err != nil {
		return err
	}
	if len(tables) == 0 {
		return nil
	}
	quotedTables, err := quoteIdentList("table", tables)
	if err != nil {
		return err
	}

	tx, err := p.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if _, err := tx.ExecContext(ctx, fmt.Sprintf(
		`DROP TABLE IF EXISTS %s CASCADE`,
		strings.Join(quotedTables, ", "),
	)); err != nil {
		return err
	}
	return tx.Commit()
}

func (p *PostgresAdapter) ExportTable(
	ctx context.Context,
	table string,
	w io.Writer,
) error {

	quotedTable, err := quoteIdent(table)
	if err != nil {
		return err
	}
	rows, err := p.DB.QueryContext(ctx,
		fmt.Sprintf(`SELECT * FROM %s`, quotedTable))

	if err != nil {
		return err
	}

	defer rows.Close()

	return WriteCSV(rows, w)
}

func (p *PostgresAdapter) ExportSchema(ctx context.Context) (*Schema, error) {
	colRows, err := p.DB.QueryContext(ctx, `
		SELECT table_name, column_name, data_type, is_nullable
		FROM information_schema.columns
		WHERE table_schema = 'public'
		ORDER BY table_name, ordinal_position
	`)
	if err != nil {
		return nil, err
	}
	defer colRows.Close()

	schema := &Schema{Tables: make(map[string]Table)}
	for colRows.Next() {
		var tbl, col, dtype, nullable string
		if err := colRows.Scan(&tbl, &col, &dtype, &nullable); err != nil {
			return nil, err
		}
		t := schema.Tables[tbl]
		t.Columns = append(t.Columns, Column{Name: col, Type: dtype, Nullable: nullable == "YES"})
		schema.Tables[tbl] = t
	}
	if err := colRows.Err(); err != nil {
		return nil, err
	}

	pkRows, err := p.DB.QueryContext(ctx, `
		SELECT kcu.table_name, kcu.column_name
		FROM information_schema.table_constraints tc
		JOIN information_schema.key_column_usage kcu
		  ON tc.constraint_name = kcu.constraint_name AND tc.table_schema = kcu.table_schema
		WHERE tc.constraint_type = 'PRIMARY KEY' AND tc.table_schema = 'public'
		ORDER BY kcu.table_name, kcu.ordinal_position
	`)
	if err != nil {
		return nil, err
	}
	defer pkRows.Close()

	for pkRows.Next() {
		var tbl, col string
		if err := pkRows.Scan(&tbl, &col); err != nil {
			return nil, err
		}
		t := schema.Tables[tbl]
		t.PrimaryKey = append(t.PrimaryKey, col)
		schema.Tables[tbl] = t
	}
	if err := pkRows.Err(); err != nil {
		return nil, err
	}

	hypertables, _ := p.hypertables(ctx)
	for tbl, timeCol := range hypertables {
		t := schema.Tables[tbl]
		if t.Extensions == nil {
			t.Extensions = make(map[string]any)
		}
		t.Extensions["timescaledb"] = map[string]any{"hypertable": true, "time_column": timeCol}
		schema.Tables[tbl] = t
	}

	return schema, nil
}

func (p *PostgresAdapter) CreateTable(ctx context.Context, name string, table Table) error {
	if err := ValidateTable(name, table); err != nil {
		return err
	}
	quotedTable, err := quoteIdent(name)
	if err != nil {
		return err
	}
	cols := make([]string, len(table.Columns))
	for i, c := range table.Columns {
		null := "NOT NULL"
		if c.Nullable {
			null = ""
		}
		quotedColumn, err := quoteIdent(c.Name)
		if err != nil {
			return err
		}
		cols[i] = fmt.Sprintf(`%s %s %s`, quotedColumn, c.Type, null)
	}
	if len(table.PrimaryKey) > 0 {
		quotedPK, err := quoteIdentList("primary key column", table.PrimaryKey)
		if err != nil {
			return err
		}
		cols = append(cols, fmt.Sprintf("PRIMARY KEY (%s)", strings.Join(quotedPK, ", ")))
	}
	_, err = p.DB.ExecContext(ctx,
		fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s (%s)`, quotedTable, strings.Join(cols, ", ")))
	return err
}

func (p *PostgresAdapter) ImportTable(ctx context.Context, table string, schema Table, r io.Reader) error {
	reader := csv.NewReader(r)
	header, err := reader.Read()
	if err != nil {
		return err
	}
	if len(header) == 0 {
		return fmt.Errorf("table %q CSV header is empty", table)
	}
	nullable := nullableColumns(schema)
	quotedTable, err := quoteIdent(table)
	if err != nil {
		return err
	}
	placeholders := make([]string, len(header))
	quotedCols, err := quoteIdentList("column", header)
	if err != nil {
		return err
	}
	for i := range header {
		placeholders[i] = fmt.Sprintf("$%d", i+1)
	}
	query := fmt.Sprintf(`INSERT INTO %s (%s) VALUES (%s) ON CONFLICT DO NOTHING`,
		quotedTable, strings.Join(quotedCols, ", "), strings.Join(placeholders, ", "))

	tx, err := p.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	stmt, err := tx.PrepareContext(ctx, query)
	if err != nil {
		tx.Rollback()
		return err
	}
	defer stmt.Close()
	for {
		row, err := reader.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
		if len(row) != len(header) {
			return fmt.Errorf("table %q CSV row has %d fields, want %d", table, len(row), len(header))
		}
		vals := make([]any, len(row))
		for i, v := range row {
			if v == "" && nullable[header[i]] {
				vals[i] = nil
			} else {
				vals[i] = v
			}
		}
		if _, err := stmt.ExecContext(ctx, vals...); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// Recreate hypertables
func (p *PostgresAdapter) FinalizeTable(
	ctx context.Context,
	name string,
	table Table,
) error {
	if err := ValidateTable(name, table); err != nil {
		return err
	}

	ext, ok := table.Extensions["timescaledb"]
	if !ok {
		return nil
	}

	cfg, ok := ext.(map[string]any)
	if !ok {
		return fmt.Errorf("table %q has invalid timescaledb extension metadata", name)
	}

	if cfg["hypertable"] == true {

		timecol, ok := cfg["time_column"].(string)
		if !ok {
			return fmt.Errorf("table %q timescaledb time_column is invalid", name)
		}

		_, err := p.DB.ExecContext(ctx,
			`SELECT create_hypertable($1::regclass, $2::name, if_not_exists => TRUE)`,
			name,
			timecol,
		)

		return err
	}

	return nil
}
