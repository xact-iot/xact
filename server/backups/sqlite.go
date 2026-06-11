package backups

import (
	"context"
	"database/sql"
	"encoding/csv"
	"fmt"
	"io"
	"strings"
)

type SQLiteAdapter struct {
	DB *sql.DB
}

func (s *SQLiteAdapter) ListTables(ctx context.Context) ([]string, error) {

	rows, err := s.DB.QueryContext(ctx, `
	SELECT name
	FROM sqlite_master
	WHERE type='table'
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

func (s *SQLiteAdapter) DropUserTables(ctx context.Context) error {
	rows, err := s.DB.QueryContext(ctx, `
	SELECT name
	FROM sqlite_master
	WHERE type='table' AND name NOT LIKE 'sqlite_%'
	`)
	if err != nil {
		return err
	}

	var tables []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			rows.Close()
			return err
		}
		tables = append(tables, name)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return err
	}
	rows.Close()

	if len(tables) == 0 {
		return nil
	}

	if _, err := s.DB.ExecContext(ctx, `PRAGMA foreign_keys = OFF`); err != nil {
		return err
	}
	defer s.DB.ExecContext(ctx, `PRAGMA foreign_keys = ON`) //nolint:errcheck

	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	for _, table := range tables {
		quotedTable, err := quoteIdent(table)
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, fmt.Sprintf(`DROP TABLE IF EXISTS %s`, quotedTable)); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *SQLiteAdapter) ImportTable(
	ctx context.Context,
	table string,
	schema Table,
	r io.Reader,
) error {

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
	quotedCols, err := quoteIdentList("column", header)
	if err != nil {
		return err
	}

	placeholders := strings.Repeat("?,", len(header))
	placeholders = placeholders[:len(placeholders)-1]

	query := fmt.Sprintf(
		"INSERT INTO %s (%s) VALUES (%s)",
		quotedTable,
		strings.Join(quotedCols, ", "),
		placeholders,
	)

	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	stmt, err := tx.PrepareContext(ctx, query)
	if err != nil {
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

		vals := make([]interface{}, len(row))

		for i, value := range row {
			if value == "" && nullable[header[i]] {
				vals[i] = nil
			} else {
				vals[i] = value
			}
		}

		if _, err := stmt.ExecContext(ctx, vals...); err != nil {
			return err
		}
	}

	return tx.Commit()
}

func (s *SQLiteAdapter) ExportSchema(ctx context.Context) (*Schema, error) {
	tables, err := s.ListTables(ctx)
	if err != nil {
		return nil, err
	}
	schema := &Schema{Tables: make(map[string]Table)}
	for _, tbl := range tables {
		quotedTable, err := quoteIdent(tbl)
		if err != nil {
			return nil, err
		}
		rows, err := s.DB.QueryContext(ctx, fmt.Sprintf(`PRAGMA table_info(%s)`, quotedTable))
		if err != nil {
			return nil, err
		}
		var t Table
		for rows.Next() {
			var cid int
			var name, colType string
			var notNull int
			var dflt sql.NullString
			var pk int
			if err := rows.Scan(&cid, &name, &colType, &notNull, &dflt, &pk); err != nil {
				rows.Close()
				return nil, err
			}
			t.Columns = append(t.Columns, Column{Name: name, Type: colType, Nullable: notNull == 0})
			if pk > 0 {
				t.PrimaryKey = append(t.PrimaryKey, name)
			}
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			return nil, err
		}
		rows.Close()
		indexes, err := s.tableIndexes(ctx, tbl)
		if err != nil {
			return nil, err
		}
		t.Indexes = indexes
		schema.Tables[tbl] = t
	}
	return schema, nil
}

func (s *SQLiteAdapter) tableIndexes(ctx context.Context, table string) ([]Index, error) {
	quotedTable, err := quoteIdent(table)
	if err != nil {
		return nil, err
	}
	rows, err := s.DB.QueryContext(ctx, fmt.Sprintf(`PRAGMA index_list(%s)`, quotedTable))
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var indexes []Index
	for rows.Next() {
		var seq int
		var name string
		var unique int
		var origin string
		var partial int
		if err := rows.Scan(&seq, &name, &unique, &origin, &partial); err != nil {
			return nil, err
		}
		if origin == "pk" || partial != 0 {
			continue
		}
		columns, err := s.indexColumns(ctx, name)
		if err != nil {
			return nil, err
		}
		if len(columns) == 0 {
			continue
		}
		indexes = append(indexes, Index{Columns: columns, Unique: unique != 0})
	}
	return indexes, rows.Err()
}

func (s *SQLiteAdapter) indexColumns(ctx context.Context, indexName string) ([]string, error) {
	quotedIndex, err := quoteIdent(indexName)
	if err != nil {
		return nil, err
	}
	rows, err := s.DB.QueryContext(ctx, fmt.Sprintf(`PRAGMA index_info(%s)`, quotedIndex))
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var columns []string
	for rows.Next() {
		var seqno, cid int
		var name string
		if err := rows.Scan(&seqno, &cid, &name); err != nil {
			return nil, err
		}
		columns = append(columns, name)
	}
	return columns, rows.Err()
}

func (s *SQLiteAdapter) ExportTable(ctx context.Context, table string, w io.Writer) error {
	quotedTable, err := quoteIdent(table)
	if err != nil {
		return err
	}
	rows, err := s.DB.QueryContext(ctx, fmt.Sprintf(`SELECT * FROM %s`, quotedTable))
	if err != nil {
		return err
	}
	defer rows.Close()
	return WriteCSV(rows, w)
}

func (s *SQLiteAdapter) CreateTable(ctx context.Context, name string, table Table) error {
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
		cols = append(cols, fmt.Sprintf(`PRIMARY KEY (%s)`, strings.Join(quotedPK, ", ")))
	}
	if _, err = s.DB.ExecContext(ctx,
		fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s (%s)`, quotedTable, strings.Join(cols, ", ")),
	); err != nil {
		return err
	}
	for i, index := range table.Indexes {
		if len(index.Columns) == 0 {
			continue
		}
		quotedCols, err := quoteIdentList("index column", index.Columns)
		if err != nil {
			return err
		}
		unique := ""
		if index.Unique {
			unique = "UNIQUE "
		}
		indexName := fmt.Sprintf("idx_restore_%s_%d", name, i+1)
		quotedIndex, err := quoteIdent(indexName)
		if err != nil {
			return err
		}
		if _, err := s.DB.ExecContext(ctx, fmt.Sprintf(
			`CREATE %sINDEX IF NOT EXISTS %s ON %s (%s)`,
			unique,
			quotedIndex,
			quotedTable,
			strings.Join(quotedCols, ", "),
		)); err != nil {
			return err
		}
	}
	return nil
}

func (s *SQLiteAdapter) FinalizeTable(_ context.Context, _ string, _ Table) error {
	return nil // no extensions to apply for SQLite
}
