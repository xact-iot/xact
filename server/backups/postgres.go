package backups

import (
	"context"
	"database/sql"
	"encoding/csv"
	"fmt"
	"io"
	"strings"
	"time"
)

type PostgresAdapter struct {
	DB *sql.DB
}

func (p *PostgresAdapter) hypertables(ctx context.Context) (map[string]string, error) {
	rows, err := p.DB.QueryContext(ctx, `
	SELECT h.hypertable_name, d.column_name
	FROM timescaledb_information.hypertables h
	JOIN timescaledb_information.dimensions d
	  ON d.hypertable_schema = h.hypertable_schema
	 AND d.hypertable_name = h.hypertable_name
	 AND d.dimension_number = 1
	WHERE h.hypertable_schema = 'public'
	`)
	if err != nil {
		rows, err = p.DB.QueryContext(ctx, `
		SELECT hypertable_name, time_column_name
		FROM timescaledb_information.hypertables
		WHERE hypertable_schema = 'public'
		`)
		if err != nil {
			return nil, err
		}
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
	hypertables, err := p.hypertables(ctx)
	if err != nil {
		hypertables = map[string]string{}
	}

	tx, err := p.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	hypertableNames, regularTables := splitHypertables(tables, hypertables)

	for _, table := range hypertableNames {
		quotedTable, err := quoteIdent(table)
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, fmt.Sprintf(`DROP TABLE IF EXISTS %s CASCADE`, quotedTable)); err != nil {
			return err
		}
	}

	if len(regularTables) > 0 {
		quotedTables, err := quoteIdentList("table", regularTables)
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, fmt.Sprintf(
			`DROP TABLE IF EXISTS %s CASCADE`,
			strings.Join(quotedTables, ", "),
		)); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func splitHypertables(tables []string, hypertables map[string]string) ([]string, []string) {
	var hypertableNames []string
	var regularTables []string
	for _, table := range tables {
		if _, ok := hypertables[table]; ok {
			hypertableNames = append(hypertableNames, table)
			continue
		}
		regularTables = append(regularTables, table)
	}
	return hypertableNames, regularTables
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

	indexRows, err := p.DB.QueryContext(ctx, `
		SELECT
			t.relname AS table_name,
			i.indisunique,
			string_agg(a.attname, ',' ORDER BY ord.n) AS columns
		FROM pg_index i
		JOIN pg_class ix ON ix.oid = i.indexrelid
		JOIN pg_class t ON t.oid = i.indrelid
		JOIN pg_namespace ns ON ns.oid = t.relnamespace
		JOIN unnest(i.indkey) WITH ORDINALITY AS ord(attnum, n) ON TRUE
		JOIN pg_attribute a ON a.attrelid = t.oid AND a.attnum = ord.attnum
		WHERE ns.nspname = 'public'
		  AND NOT i.indisprimary
		  AND i.indexprs IS NULL
		  AND i.indpred IS NULL
		GROUP BY t.relname, ix.relname, i.indisunique
		ORDER BY t.relname, ix.relname
	`)
	if err != nil {
		return nil, err
	}
	defer indexRows.Close()
	for indexRows.Next() {
		var tbl string
		var unique bool
		var columnsCSV string
		if err := indexRows.Scan(&tbl, &unique, &columnsCSV); err != nil {
			return nil, err
		}
		t := schema.Tables[tbl]
		t.Indexes = append(t.Indexes, Index{Columns: strings.Split(columnsCSV, ","), Unique: unique})
		schema.Tables[tbl] = t
	}
	if err := indexRows.Err(); err != nil {
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
	columnTypes := columnTypesByName(schema)
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
				value, err := postgresImportValue(columnTypes[header[i]], v)
				if err != nil {
					return fmt.Errorf("table %q column %q: %w", table, header[i], err)
				}
				vals[i] = value
			}
		}
		if _, err := stmt.ExecContext(ctx, vals...); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func columnTypesByName(table Table) map[string]string {
	types := make(map[string]string, len(table.Columns))
	for _, column := range table.Columns {
		types[column.Name] = strings.ToLower(strings.TrimSpace(column.Type))
	}
	return types
}

func postgresImportValue(columnType, value string) (any, error) {
	switch columnType {
	case "timestamp with time zone", "timestamp without time zone":
		return normalizePostgresTimestamp(value)
	default:
		return value, nil
	}
}

func normalizePostgresTimestamp(value string) (string, error) {
	text := strings.TrimSpace(value)
	if text == "" {
		return text, nil
	}
	layouts := []string{
		time.RFC3339Nano,
		"2006-01-02 15:04:05.999999999 -0700 MST",
		"2006-01-02 15:04:05.999999 -0700 MST",
		"2006-01-02 15:04:05 -0700 MST",
		"2006-01-02 15:04:05.999999999 -0700",
		"2006-01-02 15:04:05.999999 -0700",
		"2006-01-02 15:04:05 -0700",
		"2006-01-02 15:04:05.999999999",
		"2006-01-02 15:04:05.999999",
		"2006-01-02 15:04:05",
	}
	for _, layout := range layouts {
		if parsed, err := time.Parse(layout, text); err == nil {
			return parsed.Format(time.RFC3339Nano), nil
		}
	}
	return "", fmt.Errorf("invalid timestamp %q", value)
}

func restorePostgresIDSequence(ctx context.Context, db *sql.DB, tableName string, table Table) error {
	var idColumn *Column
	for i := range table.Columns {
		column := &table.Columns[i]
		if column.Name != "id" {
			continue
		}
		switch strings.ToLower(strings.TrimSpace(column.Type)) {
		case "integer", "bigint":
			idColumn = column
		}
		break
	}
	if idColumn == nil {
		return nil
	}

	quotedTable, err := quoteIdent(tableName)
	if err != nil {
		return err
	}
	quotedColumn, err := quoteIdent(idColumn.Name)
	if err != nil {
		return err
	}
	sequenceName := tableName + "_" + idColumn.Name + "_seq"
	quotedSequence, err := quoteIdent(sequenceName)
	if err != nil {
		return err
	}
	sequenceLiteral := quoteLiteral(sequenceName)

	if _, err := db.ExecContext(ctx, fmt.Sprintf(`CREATE SEQUENCE IF NOT EXISTS %s`, quotedSequence)); err != nil {
		return err
	}
	if _, err := db.ExecContext(ctx, fmt.Sprintf(`ALTER SEQUENCE %s OWNED BY %s.%s`, quotedSequence, quotedTable, quotedColumn)); err != nil {
		return err
	}
	if _, err := db.ExecContext(ctx, fmt.Sprintf(`ALTER TABLE %s ALTER COLUMN %s SET DEFAULT nextval(%s::regclass)`, quotedTable, quotedColumn, sequenceLiteral)); err != nil {
		return err
	}
	_, err = db.ExecContext(ctx, postgresSequenceSetvalSQL(sequenceLiteral, quotedColumn, quotedTable))
	return err
}

func quoteLiteral(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "''") + "'"
}

func postgresSequenceSetvalSQL(sequenceLiteral, quotedColumn, quotedTable string) string {
	return fmt.Sprintf(`
		SELECT setval(%s::regclass,
			COALESCE((SELECT MAX(%s) FROM %s), 1),
			(SELECT MAX(%s) FROM %s) IS NOT NULL
		)
	`, sequenceLiteral, quotedColumn, quotedTable, quotedColumn, quotedTable)
}

// Recreate PostgreSQL-specific table behavior.
func (p *PostgresAdapter) FinalizeTable(
	ctx context.Context,
	name string,
	table Table,
) error {
	if err := ValidateTable(name, table); err != nil {
		return err
	}
	if err := restorePostgresIDSequence(ctx, p.DB, name, table); err != nil {
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
			`SELECT create_hypertable($1::regclass, $2::name, if_not_exists => TRUE, migrate_data => TRUE)`,
			name,
			timecol,
		)

		return err
	}

	return nil
}
