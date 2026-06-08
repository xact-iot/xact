package backups

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"database/sql"
	"testing"

	_ "modernc.org/sqlite"
)

func TestSQLiteRestoreConvertsEmptyNullableFieldsToNull(t *testing.T) {
	var archive bytes.Buffer
	gz := gzip.NewWriter(&archive)
	tw := tar.NewWriter(gz)

	schemaJSON := []byte(`{
  "Tables": {
    "dashboards": {
      "Columns": [
        {"Name":"id","Type":"INTEGER","Nullable":false},
        {"Name":"name","Type":"TEXT","Nullable":false},
        {"Name":"parent_id","Type":"INTEGER","Nullable":true}
      ],
      "PrimaryKey": ["id"]
    }
  }
}`)
	if err := writeTarFile(t, tw, "schema.json", schemaJSON); err != nil {
		t.Fatal(err)
	}
	if err := writeTarFile(t, tw, "tables/dashboards.csv", []byte("id,name,parent_id\n1,Root,\n")); err != nil {
		t.Fatal(err)
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}

	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	adapter := &SQLiteAdapter{DB: db}
	if err := Restore(context.Background(), adapter, bytes.NewReader(archive.Bytes())); err != nil {
		t.Fatalf("Restore returned error: %v", err)
	}

	var parent sql.NullInt64
	if err := db.QueryRow(`SELECT parent_id FROM dashboards WHERE id = 1`).Scan(&parent); err != nil {
		t.Fatalf("query restored dashboard: %v", err)
	}
	if parent.Valid {
		t.Fatalf("parent_id restored as %d, want NULL", parent.Int64)
	}
}
