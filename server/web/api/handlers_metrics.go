package api

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/xact-iot/xact/sqldb"
)

// MetricHandlers holds dependencies for metric REST endpoints.
type MetricHandlers struct {
	DB             sqldb.DB
	orgFromContext func(ctx context.Context) (string, bool)
}

// NewMetricHandlers creates MetricHandlers with a context function for resolving the org.
func NewMetricHandlers(db sqldb.DB, orgFromContext func(ctx context.Context) (string, bool)) *MetricHandlers {
	return &MetricHandlers{DB: db, orgFromContext: orgFromContext}
}

// metricSeriesJSON is the ECharts-compatible wire format: data points are [ms, value] tuples.
type metricSeriesJSON struct {
	Name string      `json:"name"`
	Data [][2]any    `json:"data"`
}

func toJSONSeries(series []sqldb.MetricSeries, maxPoints int) []metricSeriesJSON {
	out := make([]metricSeriesJSON, 0, len(series))
	for _, s := range series {
		data := downsampleMetricPoints(s.Data, maxPoints)
		pts := make([][2]any, len(data))
		for i, p := range data {
			pts[i] = [2]any{p.Timestamp.UnixMilli(), p.Value}
		}
		out = append(out, metricSeriesJSON{Name: s.Name, Data: pts})
	}
	return out
}

func downsampleMetricPoints(points []sqldb.MetricPoint, maxPoints int) []sqldb.MetricPoint {
	if maxPoints <= 0 || len(points) <= maxPoints {
		return points
	}
	if maxPoints == 1 {
		return points[len(points)-1:]
	}

	out := make([]sqldb.MetricPoint, 0, maxPoints)
	lastIdx := len(points) - 1
	prevIdx := -1
	for i := 0; i < maxPoints; i++ {
		idx := (i * lastIdx) / (maxPoints - 1)
		if idx == prevIdx {
			continue
		}
		out = append(out, points[idx])
		prevIdx = idx
	}
	return out
}

// HandleQueryRange handles GET /api/v1/metrics/* (range query).
// Query params: start (RFC3339, required), end (RFC3339, optional), metrics (comma-separated), max_points (optional).
func (h *MetricHandlers) HandleQueryRange(w http.ResponseWriter, r *http.Request) {
	device := chi.URLParam(r, "*")

	org, ok := h.orgFromContext(r.Context())
	if !ok || org == "" {
		http.Error(w, "organisation not found in token", http.StatusUnauthorized)
		return
	}

	q := r.URL.Query()
	startStr := q.Get("start")
	if startStr == "" {
		http.Error(w, "start parameter is required", http.StatusBadRequest)
		return
	}
	start, err := time.Parse(time.RFC3339, startStr)
	if err != nil {
		http.Error(w, "invalid start timestamp", http.StatusBadRequest)
		return
	}

	var end time.Time
	if endStr := q.Get("end"); endStr != "" {
		end, err = time.Parse(time.RFC3339, endStr)
		if err != nil {
			http.Error(w, "invalid end timestamp", http.StatusBadRequest)
			return
		}
	}

	metricsParam := q.Get("metrics")
	if metricsParam == "" {
		http.Error(w, "metrics parameter is required", http.StatusBadRequest)
		return
	}
	metrics := strings.Split(metricsParam, ",")

	maxPoints := 0
	if maxStr := q.Get("max_points"); maxStr != "" {
		maxPoints, err = strconv.Atoi(maxStr)
		if err != nil || maxPoints < 0 {
			http.Error(w, "invalid max_points parameter", http.StatusBadRequest)
			return
		}
	}

	series, err := h.DB.QueryMetricsRange(r.Context(), org, device, metrics, start, end)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	type response struct {
		Device string             `json:"device"`
		Start  string             `json:"start"`
		End    string             `json:"end"`
		Series []metricSeriesJSON `json:"series"`
	}

	endTime := end
	if endTime.IsZero() {
		endTime = time.Now()
	}

	json.NewEncoder(w).Encode(response{
		Device: device,
		Start:  start.UTC().Format(time.RFC3339),
		End:    endTime.UTC().Format(time.RFC3339),
		Series: toJSONSeries(series, maxPoints),
	})
}

// HandleQuerySince handles GET /api/v1/metrics/*/since (incremental update query).
// Query params: after (RFC3339, required), start_metric (required), metrics (comma-separated).
func (h *MetricHandlers) HandleQuerySince(w http.ResponseWriter, r *http.Request) {
	rest := chi.URLParam(r, "*")
	device := strings.TrimSuffix(rest, "/since")

	org, ok := h.orgFromContext(r.Context())
	if !ok || org == "" {
		http.Error(w, "organisation not found in token", http.StatusUnauthorized)
		return
	}

	q := r.URL.Query()
	afterStr := q.Get("after")
	if afterStr == "" {
		http.Error(w, "after parameter is required", http.StatusBadRequest)
		return
	}
	after, err := time.Parse(time.RFC3339, afterStr)
	if err != nil {
		http.Error(w, "invalid after timestamp", http.StatusBadRequest)
		return
	}

	startMetric := q.Get("start_metric")
	if startMetric == "" {
		http.Error(w, "start_metric parameter is required", http.StatusBadRequest)
		return
	}

	metricsParam := q.Get("metrics")
	if metricsParam == "" {
		http.Error(w, "metrics parameter is required", http.StatusBadRequest)
		return
	}
	metrics := strings.Split(metricsParam, ",")

	series, err := h.DB.QueryMetricsSince(r.Context(), org, device, metrics, startMetric, after)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	type response struct {
		Device string             `json:"device"`
		After  string             `json:"after"`
		Series []metricSeriesJSON `json:"series"`
	}

	json.NewEncoder(w).Encode(response{
		Device: device,
		After:  after.UTC().Format(time.RFC3339),
		Series: toJSONSeries(series, 0),
	})
}
