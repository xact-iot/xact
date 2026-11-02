package backups

import (
	"bytes"
	"context"
	"database/sql"
	"path/filepath"
	"strings"
	"testing"

	_ "modernc.org/sqlite"
)

func TestSQLiteAdapterBackupDropAndRestoreReplacing(t *testing.T) {
	ctx := context.Background()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.ExecContext(ctx, `CREATE TABLE devices (id INTEGER PRIMARY KEY, name TEXT NOT NULL, note TEXT)`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO devices (id, name, note) VALUES (1, 'pump', NULL)`); err != nil {
		t.Fatal(err)
	}
	adapter := &SQLiteAdapter{DB: db}

	tables, err := adapter.ListTables(ctx)
	if err != nil {
		t.Fatalf("ListTables: %v", err)
	}
	if len(tables) != 1 || tables[0] != "devices" {
		t.Fatalf("tables = %#v", tables)
	}
	schema, err := adapter.ExportSchema(ctx)
	if err != nil {
		t.Fatalf("ExportSchema: %v", err)
	}
	if schema.Tables["devices"].PrimaryKey[0] != "id" {
		t.Fatalf("schema = %#v", schema.Tables["devices"])
	}
	var csv bytes.Buffer
	if err := adapter.ExportTable(ctx, "devices", &csv); err != nil {
		t.Fatalf("ExportTable: %v", err)
	}
	if !strings.Contains(csv.String(), "id,name,note") || !strings.Contains(csv.String(), "pump") {
		t.Fatalf("csv = %q", csv.String())
	}

	var archive bytes.Buffer
	if err := Backup(ctx, adapter, &archive); err != nil {
		t.Fatalf("Backup: %v", err)
	}
	cleared := false
	if err := RestoreReplacing(ctx, adapter, bytes.NewReader(archive.Bytes()), func(ctx context.Context) error {
		cleared = true
		return adapter.DropUserTables(ctx)
	}); err != nil {
		t.Fatalf("RestoreReplacing: %v", err)
	}
	if !cleared {
		t.Fatal("clear target was not called")
	}
	var name string
	var note sql.NullString
	if err := db.QueryRowContext(ctx, `SELECT name, note FROM devices WHERE id = 1`).Scan(&name, &note); err != nil {
		t.Fatalf("query restored row: %v", err)
	}
	if name != "pump" || note.Valid {
		t.Fatalf("restored name/note = %q/%#v", name, note)
	}
}

func TestRestoreRejectsInvalidArchives(t *testing.T) {
	ctx := context.Background()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	adapter := &SQLiteAdapter{DB: db}
	if err := RestoreReplacing(ctx, adapter, strings.NewReader("not gzip"), nil); err == nil {
		t.Fatal("expected nil clearTarget error")
	}
	if err := Restore(ctx, adapter, strings.NewReader("not gzip")); err == nil {
		t.Fatal("expected invalid gzip error")
	}
}

func TestSQLiteAdapterExportSchemaSkipsExpressionIndexes(t *testing.T) {
	ctx := context.Background()
	// File-backed: nested PRAGMA queries use a second pooled connection,
	// which would see an empty database with :memory:.
	db, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	for _, stmt := range []string{
		`CREATE TABLE users (id INTEGER PRIMARY KEY, email TEXT NOT NULL, login TEXT NOT NULL)`,
		`CREATE UNIQUE INDEX idx_users_email_ci ON users(lower(email))`,
		`CREATE INDEX idx_users_login ON users(login)`,
	} {
		if _, err := db.ExecContext(ctx, stmt); err != nil {
			t.Fatal(err)
		}
	}
	adapter := &SQLiteAdapter{DB: db}

	schema, err := adapter.ExportSchema(ctx)
	if err != nil {
		t.Fatalf("ExportSchema: %v", err)
	}
	indexes := schema.Tables["users"].Indexes
	if len(indexes) != 1 || len(indexes[0].Columns) != 1 || indexes[0].Columns[0] != "login" {
		t.Fatalf("indexes = %#v", indexes)
	}
	var archive bytes.Buffer
	if err := Backup(ctx, adapter, &archive); err != nil {
		t.Fatalf("Backup: %v", err)
	}
}
