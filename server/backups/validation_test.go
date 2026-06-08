package backups

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"io"
	"strings"
	"testing"
)

func TestValidateSchemaRejectsUnsafeIdentifiersAndTypes(t *testing.T) {
	tests := []struct {
		name   string
		schema Schema
		want   string
	}{
		{
			name: "table",
			schema: Schema{Tables: map[string]Table{
				`events; DROP TABLE users;`: {Columns: []Column{{Name: "id", Type: "integer"}}},
			}},
			want: "invalid table identifier",
		},
		{
			name: "column",
			schema: Schema{Tables: map[string]Table{
				"events": {Columns: []Column{{Name: "message); DROP", Type: "text"}}},
			}},
			want: "invalid column identifier",
		},
		{
			name: "type",
			schema: Schema{Tables: map[string]Table{
				"events": {Columns: []Column{{Name: "message", Type: "text; DROP TABLE users"}}},
			}},
			want: "invalid column type",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateSchema(&tt.schema)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("ValidateSchema error = %v, want %q", err, tt.want)
			}
		})
	}
}

func TestRestoreRejectsTableDataOutsideSchema(t *testing.T) {
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)

	schema := Schema{Tables: map[string]Table{
		"events": {Columns: []Column{{Name: "id", Type: "integer"}}},
	}}
	data, err := json.Marshal(schema)
	if err != nil {
		t.Fatal(err)
	}
	if err := writeTarFile(t, tw, "schema.json", data); err != nil {
		t.Fatal(err)
	}
	if err := writeTarFile(t, tw, "tables/users.csv", []byte("id\n1\n")); err != nil {
		t.Fatal(err)
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}

	err = Restore(context.Background(), noopAdapter{}, bytes.NewReader(buf.Bytes()))
	if err == nil || !strings.Contains(err.Error(), `table "users" not present in schema`) {
		t.Fatalf("Restore error = %v, want unknown table rejection", err)
	}
}

func TestRestoreReplacingValidatesBeforeClearingTarget(t *testing.T) {
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)

	schema := Schema{Tables: map[string]Table{
		"events": {Columns: []Column{{Name: "id", Type: "integer"}}},
	}}
	data, err := json.Marshal(schema)
	if err != nil {
		t.Fatal(err)
	}
	if err := writeTarFile(t, tw, "schema.json", data); err != nil {
		t.Fatal(err)
	}
	if err := writeTarFile(t, tw, "tables/users.csv", []byte("id\n1\n")); err != nil {
		t.Fatal(err)
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}

	cleared := false
	err = RestoreReplacing(context.Background(), noopAdapter{}, bytes.NewReader(buf.Bytes()), func(context.Context) error {
		cleared = true
		return nil
	})
	if err == nil || !strings.Contains(err.Error(), `table "users" not present in schema`) {
		t.Fatalf("RestoreReplacing error = %v, want unknown table rejection", err)
	}
	if cleared {
		t.Fatalf("target was cleared before archive validation completed")
	}
}

func writeTarFile(t *testing.T, tw *tar.Writer, name string, data []byte) error {
	t.Helper()
	if err := tw.WriteHeader(&tar.Header{Name: name, Mode: 0o600, Size: int64(len(data))}); err != nil {
		return err
	}
	_, err := tw.Write(data)
	return err
}

type noopAdapter struct{}

func (noopAdapter) ListTables(context.Context) ([]string, error)                { return nil, nil }
func (noopAdapter) ExportSchema(context.Context) (*Schema, error)               { return nil, nil }
func (noopAdapter) ExportTable(context.Context, string, io.Writer) error        { return nil }
func (noopAdapter) CreateTable(context.Context, string, Table) error            { return nil }
func (noopAdapter) ImportTable(context.Context, string, Table, io.Reader) error { return nil }
func (noopAdapter) FinalizeTable(context.Context, string, Table) error          { return nil }
