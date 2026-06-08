package reporting

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/xact-iot/xact/events"
	"github.com/xact-iot/xact/sqldb"
)

func TestResolveVariablesCoversBuiltinRTDBSQLAndCustom(t *testing.T) {
	vals := ResolveVariables(context.Background(), []Variable{
		{Name: "org", Type: VariableTypeBuiltin, Source: "org_name"},
		{Name: "slug", Type: VariableTypeBuiltin, Source: "org_slug"},
		{Name: "report", Type: VariableTypeBuiltin, Source: "report_name"},
		{Name: "page", Type: VariableTypeBuiltin, Source: "page_no"},
		{Name: "pages", Type: VariableTypeBuiltin, Source: "page_count"},
		{Name: "tag", Type: VariableTypeRTDB, Path: "plant.temp"},
		{Name: "sql", Type: VariableTypeSQL, Query: "select name"},
		{Name: "custom", Type: VariableTypeCustom, DefaultValue: "fallback"},
		{Name: "missing", Type: VariableTypeRTDB, Path: "plant.missing"},
	}, ResolveContext{
		OrgName:        "default",
		OrgDisplayName: "Default Organisation",
		ReportName:     "Daily",
		TagReader: func(path string) (string, bool) {
			return map[string]string{"plant.temp": "21.5"}[path], path == "plant.temp"
		},
		SQLQueryer: func(_ context.Context, query string, orgName string) (string, error) {
			if query != "select name" || orgName != "default" {
				t.Fatalf("unexpected SQL query %q org %q", query, orgName)
			}
			return "sql-value", nil
		},
	})

	want := map[string]string{
		"org": "Default Organisation", "slug": "default", "report": "Daily", "page": "«PAGE_NO»", "pages": "«PAGE_COUNT»",
		"tag": "21.5", "sql": "sql-value", "custom": "fallback", "missing": "",
	}
	for k, v := range want {
		if vals[k] != v {
			t.Fatalf("%s = %q, want %q in %#v", k, vals[k], v, vals)
		}
	}
}

func TestSubstituteTemplateEscapesValuesAndValidatesJSON(t *testing.T) {
	out, err := SubstituteTemplate(json.RawMessage(`{"title":"{{title}}","body":"{{body}}"}`), map[string]string{
		"title": `A "quoted" title`,
		"body":  "line1\nline2",
	})
	if err != nil {
		t.Fatalf("SubstituteTemplate: %v", err)
	}
	if string(out) != `{"title":"A \"quoted\" title","body":"line1\nline2"}` {
		t.Fatalf("substituted JSON = %s", out)
	}

	if _, err := SubstituteTemplate(json.RawMessage(`{"title":"{{title}}"}`), map[string]string{"title": "\n"}); err != nil {
		t.Fatalf("escaped newline should keep JSON valid: %v", err)
	}
	if _, err := SubstituteTemplate(json.RawMessage(`{"title":{{title}}}`), map[string]string{"title": "not-json"}); err == nil {
		t.Fatal("expected invalid substituted JSON error")
	}
}

func TestParseVariablesAndFormattingHelpers(t *testing.T) {
	vars, err := ParseVariables(&sqldb.PDFTemplate{Variables: json.RawMessage(`[{"name":"x","type":"custom","defaultValue":"y"}]`)})
	if err != nil {
		t.Fatalf("ParseVariables: %v", err)
	}
	if len(vars) != 1 || vars[0].Name != "x" || vars[0].DefaultValue != "y" {
		t.Fatalf("vars = %#v", vars)
	}
	if _, err := ParseVariables(&sqldb.PDFTemplate{Variables: json.RawMessage(`{bad`)}); err == nil {
		t.Fatal("expected invalid variables error")
	}

	if parseLookback("7d") != 7*24*time.Hour || parseLookback("") != 24*time.Hour {
		t.Fatal("parseLookback returned unexpected duration")
	}
	times := []time.Time{
		time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
		time.Date(2026, 1, 2, 1, 0, 0, 0, time.UTC),
	}
	labels := formatTimestamps(times)
	if len(labels) != 2 || labels[0] == "" || labels[1] == "" {
		t.Fatalf("labels = %#v", labels)
	}
	if labels := formatTimestamps(nil); labels != nil {
		t.Fatalf("nil times labels = %#v", labels)
	}
}

func TestGeneratorSmallHelpers(t *testing.T) {
	if fontStyle(true, true, true) != "BIU" || fontStyle(false, false, false) != "" {
		t.Fatal("fontStyle mismatch")
	}
	if borderStr(&Borders{Left: 1, Right: 1, Top: 1, Bottom: 1}) != "LRTB" {
		t.Fatal("borderStr mismatch")
	}
	if gridBorder(false, false, &Borders{Left: 1}) != "L" || gridBorder(true, false, nil) != "1" || gridBorder(false, false, nil) != "" {
		t.Fatal("gridBorder mismatch")
	}
	if r, g, b := hexToRGB("#0f8"); r != 0 || g != 255 || b != 136 {
		t.Fatalf("short hex = %d,%d,%d", r, g, b)
	}
	if r, g, b := hexToRGB("#336699"); r != 51 || g != 102 || b != 153 {
		t.Fatalf("long hex = %d,%d,%d", r, g, b)
	}
	if coalesce("", "first", "second") != "first" || ptOrDefault(0, 12) != 12 || ptOrDefault(5, 12) != 5 {
		t.Fatal("coalesce/ptOrDefault mismatch")
	}
	uid := 42
	entry := events.EventEntry{Timestamp: time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC), Severity: "WARN", UserID: &uid, Device: "pump", Message: "high", Params: map[string]any{"b": 2}}
	if eventsColValue(entry, "user") != "uid:42" || eventsColValue(entry, "device") != "pump" || eventsColValue(entry, "missing") != "" {
		t.Fatal("eventsColValue mismatch")
	}
	if !strings.Contains(eventsColValue(entry, "params"), "b=2") {
		t.Fatal("eventsColValue params missing")
	}
}

func TestGeneratePDFRendersSupportedElements(t *testing.T) {
	now := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	template := TemplateDoc{
		Config: DocConfig{
			PageSize:      "A4",
			Orientation:   "P",
			DocumentTitle: "Coverage Report",
			Margins:       Margins{Left: 36, Top: 36, Right: 36, Bottom: 36},
		},
		Elements: []Element{
			{ID: "title", Type: "title", AllBorders: true, Rows: [][]CellProps{{{Text: "Coverage", Bold: true, Align: "C", BgColor: "#eef6ff"}}}},
			{ID: "table", Type: "table", AllBorders: true, ColWidths: []float64{1, 2}, RowHeights: []float64{1.2}, Rows: [][]CellProps{
				{{Text: "Metric", Bold: true}, {Text: "Value", Bold: true}},
				{{Text: "Temperature"}, {Text: "21.5", Borders: &Borders{Left: 1, Right: 1, Top: 1, Bottom: 1}}},
				{{Text: "Logo"}, {Text: "image fallback", ImageData: "data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAwMCAO+/p9sAAAAASUVORK5CYII=", ImageFit: "contain", ImagePad: 2}},
			}},
			{ID: "spacer", Type: "spacer", Height: 12},
			{ID: "image", Type: "image", Width: 12, Height: 12, ImageData: "data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAwMCAO+/p9sAAAAASUVORK5CYII="},
			{ID: "events", Type: "events", Size: 7, EventsConfig: &EventsConfig{Lookback: "1h", Limit: 2, Columns: []string{"timestamp", "severity", "user", "device", "message", "params"}}},
			{ID: "chart", Type: "chart", Height: 80, ChartConfig: &ChartConfig{Metrics: []string{"pump.temp"}, Lookback: "1h", Title: "Pump", ShowLegend: true, Colors: []string{"#336699"}}},
			{ID: "footer", Type: "footer", Text: "Page «PAGE_NO» of «PAGE_COUNT»", FooterBorders: &Borders{Top: 1}},
		},
	}
	raw, err := json.Marshal(template)
	if err != nil {
		t.Fatalf("marshal template: %v", err)
	}

	pdf, err := GeneratePDF(context.Background(), raw, GenerateContext{
		OrgName: "default",
		TagPathsQueryer: func(_ context.Context, org string, paths []string, start, end time.Time) ([]sqldb.MetricSeries, error) {
			if org != "default" || len(paths) != 1 || paths[0] != "pump.temp" || !start.Before(end) {
				t.Fatalf("unexpected chart query org=%q paths=%#v start=%v end=%v", org, paths, start, end)
			}
			return []sqldb.MetricSeries{{Name: "temp", Data: []sqldb.MetricPoint{{Timestamp: now, Value: 1}, {Timestamp: now.Add(time.Minute), Value: 2}}}}, nil
		},
		EventsQueryer: func(_ context.Context, filter sqldb.EventFilter) ([]events.EventEntry, error) {
			if filter.OrgName != "default" || filter.Limit != 2 || filter.StartTime == nil || filter.EndTime == nil {
				t.Fatalf("unexpected event filter: %#v", filter)
			}
			uid := 42
			return []events.EventEntry{{Timestamp: now, Severity: string(events.Warn), UserID: &uid, Device: "pump", Message: "hot", Params: map[string]any{"temp": 90}}}, nil
		},
	})
	if err != nil {
		t.Fatalf("GeneratePDF: %v", err)
	}
	if len(pdf) < 1000 || !bytes.HasPrefix(pdf, []byte("%PDF")) {
		t.Fatalf("pdf length/header = %d/%q", len(pdf), pdf[:4])
	}
}

func TestGeneratePDFEmbedsTableCellImage(t *testing.T) {
	template := TemplateDoc{
		Config: DocConfig{
			PageSize:      "A4",
			Orientation:   "P",
			DocumentTitle: "Cell Image",
			Margins:       Margins{Left: 36, Top: 36, Right: 36, Bottom: 36},
		},
		Elements: []Element{
			{ID: "table", Type: "table", AllBorders: true, RowHeights: []float64{4}, Rows: [][]CellProps{
				{{Text: "image fallback", ImageData: "data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAwMCAO+/p9sAAAAASUVORK5CYII=", ImageFit: "contain", ImagePad: 2}},
			}},
		},
	}
	raw, err := json.Marshal(template)
	if err != nil {
		t.Fatalf("marshal template: %v", err)
	}
	pdf, err := GeneratePDF(context.Background(), raw, GenerateContext{OrgName: "default"})
	if err != nil {
		t.Fatalf("GeneratePDF: %v", err)
	}
	if !bytes.Contains(pdf, []byte("/Subtype /Image")) {
		t.Fatalf("generated PDF did not contain an embedded image object")
	}
}

func TestGeneratePDFPageBreaksBeforeChartFooterOverlap(t *testing.T) {
	now := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	template := TemplateDoc{
		Config: DocConfig{
			PageSize:      "A4",
			Orientation:   "P",
			DocumentTitle: "Chart Page Break",
			Margins:       Margins{Left: 36, Top: 36, Right: 36, Bottom: 36},
		},
		Elements: []Element{
			{ID: "spacer", Type: "spacer", Height: 650},
			{ID: "chart", Type: "chart", Height: 220, ChartConfig: &ChartConfig{Metrics: []string{"pump.pm25"}, Lookback: "1h", Title: "PM2.5"}},
			{ID: "footer", Type: "footer", Text: "Page «PAGE_NO» of «PAGE_COUNT»", FooterBorders: &Borders{Top: 1}},
		},
	}
	raw, err := json.Marshal(template)
	if err != nil {
		t.Fatalf("marshal template: %v", err)
	}
	pdf, err := GeneratePDF(context.Background(), raw, GenerateContext{
		OrgName: "default",
		TagPathsQueryer: func(_ context.Context, _ string, _ []string, _, _ time.Time) ([]sqldb.MetricSeries, error) {
			return []sqldb.MetricSeries{{Name: "pm25", Data: []sqldb.MetricPoint{
				{Timestamp: now, Value: 1},
				{Timestamp: now.Add(time.Minute), Value: 2},
			}}}, nil
		},
	})
	if err != nil {
		t.Fatalf("GeneratePDF: %v", err)
	}
	if !bytes.Contains(pdf, []byte("/Count 2")) {
		t.Fatalf("expected chart overflow prevention to create a second page")
	}
}

func TestGeneratePDFKeepsTitleWithFollowingTable(t *testing.T) {
	rows := [][]CellProps{
		{{Text: "Device", Bold: true}, {Text: "AQI", Bold: true}},
	}
	for i := 0; i < 37; i++ {
		rows = append(rows, []CellProps{{Text: fmt.Sprintf("AQ-%03d", i)}, {Text: "0"}})
	}

	template := TemplateDoc{
		Config: DocConfig{
			PageSize:      "A4",
			Orientation:   "P",
			DocumentTitle: "Keep Table Title",
			Margins:       Margins{Left: 36, Top: 36, Right: 36, Bottom: 36},
		},
		Elements: []Element{
			{ID: "spacer", Type: "spacer", Height: 717},
			{ID: "title", Type: "title", Rows: [][]CellProps{{{Text: "5. Highest Attention Monitor Snapshot", Bold: true, BgColor: "#d00019", TextColor: "#ffffff"}}}},
			{ID: "table", Type: "table", AllBorders: true, Rows: rows},
			{ID: "footer", Type: "footer", Text: "Page «PAGE_NO» of «PAGE_COUNT»", FooterBorders: &Borders{Top: 1}},
		},
	}
	raw, err := json.Marshal(template)
	if err != nil {
		t.Fatalf("marshal template: %v", err)
	}
	pdf, err := GeneratePDF(context.Background(), raw, GenerateContext{OrgName: "default"})
	if err != nil {
		t.Fatalf("GeneratePDF: %v", err)
	}
	if !bytes.Contains(pdf, []byte("/Count 3")) {
		t.Fatalf("expected title/table keep-together pagination to create a third page")
	}
}
