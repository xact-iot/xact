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

func TestBackupOmitsAPIKeyTableData(t *testing.T) {
	var buf bytes.Buffer
	if err := Backup(context.Background(), apiKeyBackupAdapter{}, &buf); err != nil {
		t.Fatalf("Backup returned error: %v", err)
	}

	entries := tarEntries(t, buf.Bytes())
	if !entries["schema.json"] {
		t.Fatalf("backup missing schema.json")
	}
	if !entries["tables/organisations.csv"] {
		t.Fatalf("backup missing organisations table data")
	}
	if entries["tables/org_api_keys.csv"] {
		t.Fatalf("backup included org_api_keys table data")
	}
}

func TestRestoreIgnoresAPIKeyTableDataFromOlderBackup(t *testing.T) {
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)

	schema := Schema{Tables: map[string]Table{
		"org_api_keys": {Columns: []Column{{Name: "id", Type: "integer"}}},
	}}
	data, err := json.Marshal(schema)
	if err != nil {
		t.Fatal(err)
	}
	if err := writeTarFile(t, tw, "schema.json", data); err != nil {
		t.Fatal(err)
	}
	if err := writeTarFile(t, tw, "tables/org_api_keys.csv", []byte("id\n1\n")); err != nil {
		t.Fatal(err)
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}

	adapter := &apiKeyRestoreAdapter{}
	if err := Restore(context.Background(), adapter, bytes.NewReader(buf.Bytes())); err != nil {
		t.Fatalf("Restore returned error: %v", err)
	}
	if adapter.importedAPIKeys {
		t.Fatalf("restore imported org_api_keys data")
	}
}

type apiKeyBackupAdapter struct{}

func (apiKeyBackupAdapter) ExportSchema(context.Context) (*Schema, error) {
	return &Schema{Tables: map[string]Table{
		"organisations": {Columns: []Column{{Name: "id", Type: "integer"}}},
		"org_api_keys":  {Columns: []Column{{Name: "id", Type: "integer"}}},
	}}, nil
}
func (apiKeyBackupAdapter) ListTables(context.Context) ([]string, error) {
	return []string{"organisations", "org_api_keys"}, nil
}
func (apiKeyBackupAdapter) ExportTable(_ context.Context, table string, w io.Writer) error {
	_, err := io.Copy(w, strings.NewReader("id\n1\n"))
	return err
}
func (apiKeyBackupAdapter) CreateTable(context.Context, string, Table) error            { return nil }
func (apiKeyBackupAdapter) ImportTable(context.Context, string, Table, io.Reader) error { return nil }
func (apiKeyBackupAdapter) FinalizeTable(context.Context, string, Table) error          { return nil }

type apiKeyRestoreAdapter struct {
	importedAPIKeys bool
}

func (*apiKeyRestoreAdapter) ExportSchema(context.Context) (*Schema, error)        { return nil, nil }
func (*apiKeyRestoreAdapter) ListTables(context.Context) ([]string, error)         { return nil, nil }
func (*apiKeyRestoreAdapter) ExportTable(context.Context, string, io.Writer) error { return nil }
func (*apiKeyRestoreAdapter) CreateTable(context.Context, string, Table) error     { return nil }
func (a *apiKeyRestoreAdapter) ImportTable(_ context.Context, table string, _ Table, _ io.Reader) error {
	if table == "org_api_keys" {
		a.importedAPIKeys = true
	}
	return nil
}
func (*apiKeyRestoreAdapter) FinalizeTable(context.Context, string, Table) error { return nil }

func tarEntries(t *testing.T, data []byte) map[string]bool {
	t.Helper()
	gz, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	defer gz.Close()

	tr := tar.NewReader(gz)
	entries := map[string]bool{}
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		entries[hdr.Name] = true
	}
	return entries
}
