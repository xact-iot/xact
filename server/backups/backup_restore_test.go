package backups

import (
	"bytes"
	"context"
	"database/sql"
	"io"
	"strings"
	"testing"
)

func TestCSVValueStringPreservesByteBackedDashboardJSON(t *testing.T) {
	widgets := []byte(`[{"id":"yt","type":"html-widget","config":{"html":"<div style=\"width:100%;height:100%;overflow:hidden;\"><iframe src=\"https://www.youtube.com/embed/vytmBNhc9ig?autoplay=1&mute=1&controls=0&rel=0&playsinline=1&iv_load_policy=3\" allow=\"autoplay; encrypted-media; picture-in-picture\" allowfullscreen></iframe></div>"}}]`)

	if got := csvValueString(widgets); got != string(widgets) {
		t.Fatalf("csvValueString([]byte) = %q, want original JSON", got)
	}

	raw := sql.RawBytes(widgets)
	if got := csvValueString(raw); got != string(widgets) {
		t.Fatalf("csvValueString(sql.RawBytes) = %q, want original JSON", got)
	}
}

func TestBackupWithProgressReportsTables(t *testing.T) {
	var archive bytes.Buffer
	var events []ProgressEvent
	err := BackupWithProgress(context.Background(), progressBackupAdapter{}, &archive, func(ev ProgressEvent) {
		events = append(events, ev)
	})
	if err != nil {
		t.Fatalf("BackupWithProgress: %v", err)
	}
	if len(events) != 4 {
		t.Fatalf("events = %#v, want export/archive for two backed-up tables", events)
	}
	if events[0] != (ProgressEvent{Phase: "exporting", Table: "alpha", Index: 1, Total: 2}) {
		t.Fatalf("first event = %#v", events[0])
	}
	if events[3] != (ProgressEvent{Phase: "archiving", Table: "beta", Index: 2, Total: 2}) {
		t.Fatalf("last event = %#v", events[3])
	}
}

type progressBackupAdapter struct{}

func (progressBackupAdapter) ExportSchema(context.Context) (*Schema, error) {
	return &Schema{Tables: map[string]Table{
		"alpha":        {Columns: []Column{{Name: "id", Type: "TEXT"}}},
		"org_api_keys": {Columns: []Column{{Name: "id", Type: "TEXT"}}},
		"beta":         {Columns: []Column{{Name: "id", Type: "TEXT"}}},
	}}, nil
}

func (progressBackupAdapter) ListTables(context.Context) ([]string, error) {
	return []string{"alpha", "org_api_keys", "beta"}, nil
}

func (progressBackupAdapter) ExportTable(_ context.Context, table string, w io.Writer) error {
	_, err := io.Copy(w, strings.NewReader("id\n"+table+"\n"))
	return err
}

func (progressBackupAdapter) CreateTable(context.Context, string, Table) error            { return nil }
func (progressBackupAdapter) ImportTable(context.Context, string, Table, io.Reader) error { return nil }
func (progressBackupAdapter) FinalizeTable(context.Context, string, Table) error          { return nil }
