package reporting

import (
	"fmt"
	"math"
	"sort"
	"time"

	"github.com/go-analyze/charts"

	"github.com/xact-iot/xact/sqldb"
)

// ChartConfig holds rendering configuration for a time-series chart element.
type ChartConfig struct {
	// Metrics is a list of full tag paths below the org node, e.g. "sensors.temp1.flow_rate".
	// Multiple devices can be mixed. All {{variable}} tokens are substituted before
	// GeneratePDF is called, so "{{device}}.temperature" expands to a real path.
	// Each path is split on the last dot: everything before = device name, last = metric name.
	Metrics []string `json:"metrics,omitempty"`

	// Lookback is the time window to fetch: "1h", "6h", "24h", "7d", "30d".
	// Defaults to "24h" when empty.
	Lookback string `json:"lookback,omitempty"`

	Title  string `json:"title,omitempty"`
	YLabel string `json:"yLabel,omitempty"`

	// YMin / YMax pin the Y-axis range. Omit to auto-scale.
	YMin *float64 `json:"yMin,omitempty"`
	YMax *float64 `json:"yMax,omitempty"`

	// Colors is a list of hex colours (#rrggbb) applied to series in order.
	Colors []string `json:"colors,omitempty"`

	Smooth     bool `json:"smooth,omitempty"`
	ShowLegend bool `json:"showLegend,omitempty"`
	FillArea   bool `json:"fillArea,omitempty"`
}

// RenderChart converts MetricSeries data into a PNG chart image.
// widthPx and heightPx are the output image dimensions in pixels.
func RenderChart(cfg ChartConfig, series []sqldb.MetricSeries, widthPx, heightPx int) ([]byte, error) {
	if len(series) == 0 {
		return nil, fmt.Errorf("no data")
	}

	// ── 1. Collect all unique timestamps (sorted) ────────────────────────────
	tsSet := map[time.Time]struct{}{}
	for _, s := range series {
		for _, pt := range s.Data {
			tsSet[pt.Timestamp.UTC().Truncate(time.Second)] = struct{}{}
		}
	}
	allTimes := make([]time.Time, 0, len(tsSet))
	for t := range tsSet {
		allTimes = append(allTimes, t)
	}
	sort.Slice(allTimes, func(i, j int) bool { return allTimes[i].Before(allTimes[j]) })

	// ── 2. Build values matrix (series × time points) ────────────────────────
	tsIndex := make(map[time.Time]int, len(allTimes))
	for i, t := range allTimes {
		tsIndex[t] = i
	}

	seriesNames := make([]string, len(series))
	nullVal := charts.GetNullValue()
	values := make([][]float64, len(series))
	for si, s := range series {
		seriesNames[si] = s.Name
		vals := make([]float64, len(allTimes))
		for i := range vals {
			vals[i] = nullVal
		}
		for _, pt := range s.Data {
			t := pt.Timestamp.UTC().Truncate(time.Second)
			if idx, ok := tsIndex[t]; ok {
				vals[idx] = float64(pt.Value)
			}
		}
		values[si] = vals
	}

	// ── 3. Build X-axis labels (sampled for readability) ─────────────────────
	xLabels := formatTimestamps(allTimes)
	const maxLabels = 12
	if len(xLabels) > maxLabels {
		step := int(math.Ceil(float64(len(xLabels)) / float64(maxLabels)))
		sparse := make([]string, len(xLabels))
		for i, lbl := range xLabels {
			if i%step == 0 || i == len(xLabels)-1 {
				sparse[i] = lbl
			}
		}
		xLabels = sparse
	}

	// ── 4. Build theme (apply custom series colours if provided) ─────────────
	theme := charts.GetTheme(charts.ThemeLight)
	if len(cfg.Colors) > 0 {
		colors := make([]charts.Color, len(cfg.Colors))
		for i, c := range cfg.Colors {
			colors[i] = charts.ParseColor(c)
		}
		theme = theme.WithSeriesColors(colors)
	}

	// ── 5. Assemble options and render ───────────────────────────────────────
	optFuncs := []charts.OptionFunc{
		charts.ThemeOptionFunc(theme),
		charts.XAxisLabelsOptionFunc(xLabels),
		charts.LegendLabelsOptionFunc(seriesNames),
		func(opt *charts.ChartOption) {
			opt.Width = widthPx
			opt.Height = heightPx
			opt.LineStrokeWidth = 1.5
			if cfg.Smooth {
				opt.XAxis.BoundaryGap = charts.Ptr(false)
			}
			if cfg.FillArea {
				opt.FillArea = charts.Ptr(true)
				opt.FillOpacity = 100
				opt.XAxis.BoundaryGap = charts.Ptr(false)
			}
			if !cfg.ShowLegend {
				opt.Legend.Show = charts.Ptr(false)
			}
		},
	}

	if cfg.Title != "" {
		optFuncs = append(optFuncs, charts.TitleTextOptionFunc(cfg.Title))
	}

	yOpt := charts.YAxisOption{}
	if cfg.YMin != nil {
		yOpt.Min = cfg.YMin
	}
	if cfg.YMax != nil {
		yOpt.Max = cfg.YMax
	}
	if cfg.YLabel != "" {
		yOpt.Title = cfg.YLabel
	}
	if yOpt.Min != nil || yOpt.Max != nil || yOpt.Title != "" {
		optFuncs = append(optFuncs, charts.YAxisOptionFunc(yOpt))
	}

	p, err := charts.LineRender(values, optFuncs...)
	if err != nil {
		return nil, fmt.Errorf("rendering chart: %w", err)
	}
	buf, err := p.Bytes()
	if err != nil {
		return nil, fmt.Errorf("encoding chart PNG: %w", err)
	}
	return buf, nil
}

// parseLookback converts a lookback string to a duration.
// Recognised values: "1h", "3h", "6h", "12h", "24h", "7d", "30d". Defaults to 24 hours.
func parseLookback(s string) time.Duration {
	switch s {
	case "1h":
		return time.Hour
	case "3h":
		return 3 * time.Hour
	case "6h":
		return 6 * time.Hour
	case "12h":
		return 12 * time.Hour
	case "7d":
		return 7 * 24 * time.Hour
	case "30d":
		return 30 * 24 * time.Hour
	default:
		return 24 * time.Hour
	}
}

// formatTimestamps picks a human-readable format based on the overall time span.
func formatTimestamps(times []time.Time) []string {
	if len(times) == 0 {
		return nil
	}
	span := times[len(times)-1].Sub(times[0])

	var format string
	switch {
	case span <= 24*time.Hour:
		format = "15:04"
	case span <= 30*24*time.Hour:
		format = "01/02 15:04"
	default:
		format = "2006-01-02"
	}

	out := make([]string, len(times))
	for i, t := range times {
		out[i] = t.Local().Format(format)
	}
	return out
}
