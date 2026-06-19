package reporting

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"math"
	"sort"
	"strings"
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

	// SeriesNames overrides legend labels. Empty entries fall back to the queried series/tag name.
	SeriesNames []string `json:"seriesNames,omitempty"`

	Smooth     bool  `json:"smooth,omitempty"`
	ShowLegend bool  `json:"showLegend,omitempty"`
	FillArea   bool  `json:"fillArea,omitempty"`
	ShowGrid   *bool `json:"showGrid,omitempty"`
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

	seriesNames, values := buildChartValues(cfg, series, allTimes, tsIndex)

	// ── 3. Build X-axis labels (sampled for readability) ─────────────────────
	xLabels := formatTimestamps(allTimes)
	xLabelCount := chartXLabelCount(widthPx, len(xLabels))

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
	yOpt := chartYAxisOption(cfg, values)
	if cfg.FillArea && len(values) > 1 {
		baseValues := chartValuesWithVisibleSeries(values, func(index int) bool { return index == 0 })
		base, err := renderChartPNG(cfg, baseValues, seriesNames, theme, xLabels, xLabelCount, yOpt, widthPx, heightPx, true, false)
		if err != nil {
			return nil, err
		}
		overlayCfg := cfg
		overlayValues := chartValuesWithVisibleSeries(values, func(index int) bool { return index > 0 })
		overlay, err := renderChartPNG(overlayCfg, overlayValues, seriesNames, theme, xLabels, xLabelCount, yOpt, widthPx, heightPx, false, false)
		if err != nil {
			return nil, err
		}
		chromeOnlyValues := chartValuesWithVisibleSeries(values, func(index int) bool { return false })
		chromeOnly, err := renderChartPNG(overlayCfg, chromeOnlyValues, seriesNames, theme, xLabels, xLabelCount, yOpt, widthPx, heightPx, false, false)
		if err != nil {
			return nil, err
		}
		return compositeDeltaPNG(base, overlay, chromeOnly)
	}

	return renderChartPNG(cfg, values, seriesNames, theme, xLabels, xLabelCount, yOpt, widthPx, heightPx, cfg.FillArea, false)
}

func renderChartPNG(cfg ChartConfig, values [][]float64, seriesNames []string, theme charts.ColorPalette, xLabels []string, xLabelCount int, yOpt charts.YAxisOption, widthPx, heightPx int, fillArea bool, transparentChrome bool) ([]byte, error) {
	optFuncs := []charts.OptionFunc{
		charts.ThemeOptionFunc(theme),
		charts.XAxisLabelsOptionFunc(xLabels),
		charts.LegendLabelsOptionFunc(seriesNames),
		func(opt *charts.ChartOption) {
			opt.Width = widthPx
			opt.Height = heightPx
			opt.LineStrokeWidth = 2.4
			opt.Symbol = charts.SymbolNone
			opt.Legend.Symbol = charts.SymbolDot
			opt.XAxis.LabelCount = xLabelCount
			opt.XAxis.LabelCountAdjustment = -1
			opt.XAxis.LabelFontStyle.FontSize = 10
			if cfg.Smooth {
				opt.XAxis.BoundaryGap = charts.Ptr(false)
			}
			if fillArea || transparentChrome {
				opt.XAxis.BoundaryGap = charts.Ptr(false)
			}
			if fillArea {
				opt.FillArea = charts.Ptr(true)
				opt.FillOpacity = 48
			}
			if !cfg.ShowLegend {
				opt.Legend.Show = charts.Ptr(false)
			}
			if transparentChrome {
				opt.Legend.Theme = transparentLegendTheme(theme, len(seriesNames))
			}
		},
	}

	optFuncs = append(optFuncs, charts.YAxisOptionFunc(yOpt))

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

func chartYAxisOption(cfg ChartConfig, values [][]float64) charts.YAxisOption {
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
	yOpt.ValueFormatter = chartIntegerValueFormatter
	yOpt.PreferNiceIntervals = charts.Ptr(true)
	yOpt.SplitLineShow = charts.Ptr(chartBoolDefault(cfg.ShowGrid, true))
	yOpt.SpineLineShow = charts.Ptr(true)
	if cfg.FillArea && len(values) > 1 && (yOpt.Min == nil || yOpt.Max == nil) {
		minVal, maxVal, ok := chartValueRange(values)
		if ok {
			if yOpt.Min == nil {
				if minVal >= 0 {
					minVal = 0
				}
				yOpt.Min = charts.Ptr(minVal)
			}
			if yOpt.Max == nil {
				span := maxVal - *yOpt.Min
				if span <= 0 {
					span = 1
				}
				yOpt.Max = charts.Ptr(maxVal + span*0.05)
			}
		}
	}
	if yOpt.Unit == 0 {
		if unit, ok := chartNiceYAxisUnit(yOpt, values); ok {
			yOpt.Unit = unit
		}
	}
	return yOpt
}

func chartValueRange(values [][]float64) (float64, float64, bool) {
	nullVal := charts.GetNullValue()
	var minVal, maxVal float64
	seen := false
	for _, series := range values {
		for _, v := range series {
			if v == nullVal {
				continue
			}
			if !seen || v < minVal {
				minVal = v
			}
			if !seen || v > maxVal {
				maxVal = v
			}
			seen = true
		}
	}
	return minVal, maxVal, seen
}

func chartIntegerValueFormatter(v float64) string {
	return fmt.Sprintf("%.0f", math.Round(v))
}

func chartBoolDefault(v *bool, fallback bool) bool {
	if v == nil {
		return fallback
	}
	return *v
}

func chartNiceYAxisUnit(yOpt charts.YAxisOption, values [][]float64) (float64, bool) {
	minVal, maxVal, ok := chartValueRange(values)
	if !ok {
		return 0, false
	}
	if yOpt.Min != nil {
		minVal = *yOpt.Min
	}
	if yOpt.Max != nil {
		maxVal = *yOpt.Max
	}
	span := maxVal - minVal
	if span <= 0 {
		return 1, true
	}
	raw := span / 5
	magnitude := math.Pow(10, math.Floor(math.Log10(raw)))
	fraction := raw / magnitude
	switch {
	case fraction <= 1.5:
		return magnitude, true
	case fraction <= 3:
		return 2 * magnitude, true
	case fraction <= 7:
		return 5 * magnitude, true
	default:
		return 10 * magnitude, true
	}
}

func chartSeriesColors(theme charts.ColorPalette, start, count int) []charts.Color {
	colors := make([]charts.Color, count)
	for i := range colors {
		colors[i] = theme.GetSeriesColor(start + i)
	}
	return colors
}

func chartColorsWithVisibleSeries(theme charts.ColorPalette, count int, visible func(int) bool) []charts.Color {
	colors := make([]charts.Color, count)
	for i := range colors {
		if visible(i) {
			colors[i] = theme.GetSeriesColor(i)
		} else {
			colors[i] = charts.ColorTransparent
		}
	}
	return colors
}

func chartValuesWithVisibleSeries(values [][]float64, visible func(int) bool) [][]float64 {
	filtered := make([][]float64, len(values))
	nullVal := charts.GetNullValue()
	for i, series := range values {
		filtered[i] = make([]float64, len(series))
		if visible(i) {
			copy(filtered[i], series)
			continue
		}
		for j := range filtered[i] {
			filtered[i][j] = nullVal
		}
	}
	return filtered
}

func transparentChartChromeTheme(theme charts.ColorPalette) charts.ColorPalette {
	return theme.
		WithBackgroundColor(charts.ColorTransparent).
		WithXAxisColor(charts.ColorTransparent).
		WithYAxisColor(charts.ColorTransparent).
		WithAxisSplitLineColor(charts.ColorTransparent).
		WithXAxisTextColor(charts.ColorTransparent).
		WithYAxisTextColor(charts.ColorTransparent).
		WithTitleTextColor(charts.ColorTransparent).
		WithTitleBorderColor(charts.ColorTransparent).
		WithLegendTextColor(charts.ColorTransparent).
		WithLegendBorderColor(charts.ColorTransparent)
}

func transparentLegendTheme(theme charts.ColorPalette, count int) charts.ColorPalette {
	return transparentChartChromeTheme(theme).WithSeriesColors(chartSolidColors(charts.ColorTransparent, count))
}

func chartSolidColors(color charts.Color, count int) []charts.Color {
	colors := make([]charts.Color, count)
	for i := range colors {
		colors[i] = color
	}
	return colors
}

func compositeDeltaPNG(basePNG, overlayPNG, referencePNG []byte) ([]byte, error) {
	baseImg, err := png.Decode(bytes.NewReader(basePNG))
	if err != nil {
		return nil, fmt.Errorf("decode base chart PNG: %w", err)
	}
	overlayImg, err := png.Decode(bytes.NewReader(overlayPNG))
	if err != nil {
		return nil, fmt.Errorf("decode overlay chart PNG: %w", err)
	}
	referenceImg, err := png.Decode(bytes.NewReader(referencePNG))
	if err != nil {
		return nil, fmt.Errorf("decode reference chart PNG: %w", err)
	}
	bounds := baseImg.Bounds()
	out := image.NewRGBA(bounds)
	draw.Draw(out, bounds, baseImg, bounds.Min, draw.Src)
	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			overlayColor := overlayImg.At(x, y)
			if colorDelta(overlayColor, referenceImg.At(x, y)) == 0 {
				continue
			}
			out.Set(x, y, overlayColor)
		}
	}

	var buf bytes.Buffer
	if err := png.Encode(&buf, out); err != nil {
		return nil, fmt.Errorf("encode composed chart PNG: %w", err)
	}
	return buf.Bytes(), nil
}

func colorDelta(a, b color.Color) uint32 {
	ar, ag, ab, aa := a.RGBA()
	br, bg, bb, ba := b.RGBA()
	return absDiff16(ar, br) + absDiff16(ag, bg) + absDiff16(ab, bb) + absDiff16(aa, ba)
}

func absDiff16(a, b uint32) uint32 {
	if a > b {
		return a - b
	}
	return b - a
}

func buildChartValues(cfg ChartConfig, series []sqldb.MetricSeries, allTimes []time.Time, tsIndex map[time.Time]int) ([]string, [][]float64) {
	seriesNames := make([]string, len(series))
	nullVal := charts.GetNullValue()
	values := make([][]float64, len(series))
	for si, s := range series {
		seriesNames[si] = chartSeriesName(cfg, s.Name, si)
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
		values[si] = interpolateInternalNulls(vals, allTimes)
	}
	return seriesNames, values
}

func chartSeriesName(cfg ChartConfig, fallback string, index int) string {
	if index < len(cfg.SeriesNames) {
		if name := strings.TrimSpace(cfg.SeriesNames[index]); name != "" {
			return name
		}
	}
	return fallback
}

func interpolateInternalNulls(vals []float64, times []time.Time) []float64 {
	if len(vals) != len(times) || len(vals) < 3 {
		return vals
	}
	nullVal := charts.GetNullValue()
	prev := -1
	for i, v := range vals {
		if v == nullVal {
			continue
		}
		if prev >= 0 && i-prev > 1 {
			prevTime := times[prev]
			nextTime := times[i]
			total := nextTime.Sub(prevTime).Seconds()
			if total > 0 {
				for j := prev + 1; j < i; j++ {
					frac := times[j].Sub(prevTime).Seconds() / total
					vals[j] = vals[prev] + (v-vals[prev])*frac
				}
			}
		}
		prev = i
	}
	return vals
}

func chartXLabelCount(widthPx, labelCount int) int {
	if labelCount <= 0 {
		return 0
	}
	target := widthPx / 170
	if target < 3 {
		target = 3
	}
	if target > 10 {
		target = 10
	}
	if target > labelCount {
		target = labelCount
	}
	return target
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

// formatTimestamps picks compact report labels. The chart renderer measures
// axis labels as single text runs, so keep the date inline to avoid collisions.
func formatTimestamps(times []time.Time) []string {
	if len(times) == 0 {
		return nil
	}
	span := times[len(times)-1].Sub(times[0])

	out := make([]string, len(times))
	for i, t := range times {
		local := t.Local()
		switch {
		case span <= 48*time.Hour:
			out[i] = local.Format("15:04 01/02")
		case span <= 30*24*time.Hour:
			out[i] = local.Format("01/02 15:04")
		default:
			out[i] = local.Format("2006-01-02")
		}
	}
	return out
}
