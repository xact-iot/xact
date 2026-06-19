package reporting

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"image"
	"image/png"
	"strings"
	"testing"
	"time"

	"github.com/go-analyze/charts"
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
	if strings.Contains(labels[0], "\n") || !strings.Contains(labels[0], "/") {
		t.Fatalf("expected intraday labels to include inline date, got %#v", labels)
	}
	if labels := formatTimestamps(nil); labels != nil {
		t.Fatalf("nil times labels = %#v", labels)
	}
}

func TestChartValuesInterpolateInternalTimestampGaps(t *testing.T) {
	t0 := time.Date(2026, 1, 1, 10, 0, 0, 0, time.UTC)
	times := []time.Time{
		t0,
		t0.Add(5 * time.Minute),
		t0.Add(10 * time.Minute),
		t0.Add(15 * time.Minute),
	}
	idx := map[time.Time]int{}
	for i, ts := range times {
		idx[ts] = i
	}

	_, values := buildChartValues(ChartConfig{}, []sqldb.MetricSeries{
		{Name: "a", Data: []sqldb.MetricPoint{
			{Timestamp: times[0], Value: 10},
			{Timestamp: times[2], Value: 30},
		}},
	}, times, idx)

	if len(values) != 1 || len(values[0]) != 4 {
		t.Fatalf("values = %#v", values)
	}
	if values[0][1] != 20 {
		t.Fatalf("interpolated value = %v, want 20", values[0][1])
	}
	if values[0][3] != charts.GetNullValue() {
		t.Fatalf("trailing value = %v, want null sentinel", values[0][3])
	}
}

func TestChartSeriesNamesOverrideLegendLabels(t *testing.T) {
	if got := chartSeriesName(ChartConfig{SeriesNames: []string{"Clean water"}}, "plant.clean_water", 0); got != "Clean water" {
		t.Fatalf("series label = %q, want custom name", got)
	}
	if got := chartSeriesName(ChartConfig{SeriesNames: []string{""}}, "plant.clean_water", 0); got != "plant.clean_water" {
		t.Fatalf("blank series label = %q, want fallback", got)
	}
	if got := chartSeriesName(ChartConfig{}, "plant.clean_water", 1); got != "plant.clean_water" {
		t.Fatalf("missing series label = %q, want fallback", got)
	}
}

func TestChartXLabelCountUsesMoreThanEndpoints(t *testing.T) {
	if got := chartXLabelCount(640, 200); got != 3 {
		t.Fatalf("label count = %d, want 3", got)
	}
	if got := chartXLabelCount(2000, 200); got != 10 {
		t.Fatalf("wide label count = %d, want capped 10", got)
	}
	if got := chartXLabelCount(640, 3); got != 3 {
		t.Fatalf("small label count = %d, want 3", got)
	}
}

func TestChartIntegerValueFormatterRoundsLabels(t *testing.T) {
	if got := chartIntegerValueFormatter(94.21); got != "94" {
		t.Fatalf("formatted label = %q, want 94", got)
	}
	if got := chartIntegerValueFormatter(10.5); got != "11" {
		t.Fatalf("formatted rounded label = %q, want 11", got)
	}
}

func TestChartNiceYAxisUnitUsesHumanFriendlySteps(t *testing.T) {
	unit, ok := chartNiceYAxisUnit(charts.YAxisOption{Min: charts.Ptr(0.0), Max: charts.Ptr(42.0)}, [][]float64{{0, 42}})
	if !ok || unit != 10 {
		t.Fatalf("unit for 0..42 = %v, %v; want 10, true", unit, ok)
	}
	unit, ok = chartNiceYAxisUnit(charts.YAxisOption{Min: charts.Ptr(0.0), Max: charts.Ptr(94.0)}, [][]float64{{0, 94}})
	if !ok || unit != 20 {
		t.Fatalf("unit for 0..94 = %v, %v; want 20, true", unit, ok)
	}
}

func TestFilledMultiSeriesChartUsesSharedYAxisRange(t *testing.T) {
	showGrid := false
	yOpt := chartYAxisOption(ChartConfig{FillArea: true, ShowGrid: &showGrid}, [][]float64{
		{10, 20},
		{80, 90},
	})
	if yOpt.Min == nil || *yOpt.Min != 0 {
		t.Fatalf("y min = %v, want 0", yOpt.Min)
	}
	if yOpt.Max == nil || *yOpt.Max <= 90 {
		t.Fatalf("y max = %v, want padded value above 90", yOpt.Max)
	}
	if yOpt.Unit != 20 {
		t.Fatalf("y unit = %v, want 20", yOpt.Unit)
	}
	if yOpt.SplitLineShow == nil || *yOpt.SplitLineShow {
		t.Fatalf("grid option = %v, want explicit false", yOpt.SplitLineShow)
	}
	if yOpt.SpineLineShow == nil || !*yOpt.SpineLineShow {
		t.Fatalf("spine option = %v, want explicit true", yOpt.SpineLineShow)
	}
}

func TestChartYAxisShowsGridByDefault(t *testing.T) {
	yOpt := chartYAxisOption(ChartConfig{}, [][]float64{{10, 42}})
	if yOpt.SplitLineShow == nil || !*yOpt.SplitLineShow {
		t.Fatalf("grid option = %v, want default true", yOpt.SplitLineShow)
	}
}

func TestChartSeriesColorsCanStartAfterFilledSeries(t *testing.T) {
	theme := charts.GetTheme(charts.ThemeLight)
	colors := chartSeriesColors(theme, 1, 2)
	if len(colors) != 2 {
		t.Fatalf("colors len = %d, want 2", len(colors))
	}
	if colors[0] != theme.GetSeriesColor(1) || colors[1] != theme.GetSeriesColor(2) {
		t.Fatalf("colors = %#v, want theme colors 1 and 2", colors)
	}
}

func TestChartColorsWithVisibleSeriesKeepsShape(t *testing.T) {
	theme := charts.GetTheme(charts.ThemeLight)
	colors := chartColorsWithVisibleSeries(theme, 3, func(index int) bool { return index == 1 })
	if len(colors) != 3 {
		t.Fatalf("colors len = %d, want 3", len(colors))
	}
	if colors[1] != theme.GetSeriesColor(1) {
		t.Fatalf("visible color = %#v, want original second series color", colors[1])
	}
	if colors[0] != charts.ColorTransparent || colors[2] != charts.ColorTransparent {
		t.Fatalf("hidden series should be transparent: %#v", colors)
	}
}

func TestChartValuesWithVisibleSeriesPreservesShape(t *testing.T) {
	values := [][]float64{{1, 2}, {3, 4}}
	filtered := chartValuesWithVisibleSeries(values, func(index int) bool { return index == 1 })
	if len(filtered) != 2 || len(filtered[0]) != 2 || len(filtered[1]) != 2 {
		t.Fatalf("filtered shape = %#v", filtered)
	}
	if filtered[0][0] != charts.GetNullValue() || filtered[0][1] != charts.GetNullValue() {
		t.Fatalf("hidden values = %#v, want null sentinels", filtered[0])
	}
	if filtered[1][0] != 3 || filtered[1][1] != 4 {
		t.Fatalf("visible values = %#v, want original second series", filtered[1])
	}
	if values[0][0] != 1 {
		t.Fatalf("source values were mutated: %#v", values)
	}
}

func TestRenderChartFilledMultiSeries(t *testing.T) {
	t0 := time.Date(2026, 1, 1, 10, 0, 0, 0, time.UTC)
	pngBytes, err := RenderChart(ChartConfig{Title: "Filled", FillArea: true, Colors: []string{"#0066cc", "#ff0000"}}, []sqldb.MetricSeries{
		{Name: "primary", Data: []sqldb.MetricPoint{
			{Timestamp: t0, Value: 10},
			{Timestamp: t0.Add(time.Minute), Value: 20},
		}},
		{Name: "overlay", Data: []sqldb.MetricPoint{
			{Timestamp: t0, Value: 80},
			{Timestamp: t0.Add(time.Minute), Value: 90},
		}},
	}, 640, 320)
	if err != nil {
		t.Fatalf("RenderChart: %v", err)
	}
	if !bytes.HasPrefix(pngBytes, []byte{0x89, 'P', 'N', 'G'}) {
		t.Fatalf("rendered chart is not a PNG, prefix=%q", pngBytes[:4])
	}
	img, err := png.Decode(bytes.NewReader(pngBytes))
	if err != nil {
		t.Fatalf("decode chart PNG: %v", err)
	}
	plotLeft := firstPixelX(img.Bounds(), func(x, y int) bool {
		r, g, b, _ := img.At(x, y).RGBA()
		return b > r+10000 && b > g+10000
	})
	redLeft := firstPixelX(img.Bounds(), func(x, y int) bool {
		r, g, b, _ := img.At(x, y).RGBA()
		return r > g+10000 && r > b+10000
	})
	if plotLeft < 0 || redLeft < 0 {
		t.Fatalf("expected blue plot and red overlay pixels, got plotLeft=%d redLeft=%d", plotLeft, redLeft)
	}
	if redLeft < plotLeft {
		t.Fatalf("red overlay starts at x=%d before plot starts at x=%d", redLeft, plotLeft)
	}
}

func firstPixelX(bounds image.Rectangle, match func(x, y int) bool) int {
	for x := bounds.Min.X; x < bounds.Max.X; x++ {
		for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
			if match(x, y) {
				return x
			}
		}
	}
	return -1
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

func TestGeneratePDFClampsOversizedRows(t *testing.T) {
	template := TemplateDoc{
		Config: DocConfig{
			PageSize:      "A4",
			Orientation:   "P",
			DocumentTitle: "Oversized Row",
			Margins:       Margins{Left: 36, Top: 36, Right: 36, Bottom: 36},
		},
		Elements: []Element{
			{ID: "table", Type: "table", AllBorders: true, Rows: [][]CellProps{
				{{Text: "This row is taller than the usable page", CellHeight: 2000}},
				{{Text: "The renderer should still make progress"}},
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
	if len(pdf) < 1000 || !bytes.HasPrefix(pdf, []byte("%PDF")) {
		t.Fatalf("pdf length/header = %d/%q", len(pdf), pdf[:4])
	}
}
