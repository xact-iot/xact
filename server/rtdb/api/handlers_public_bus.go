package api

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/xact-iot/xact/applications"
)

type busStop struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Code        string   `json:"code,omitempty"`
	Lat         float64  `json:"lat"`
	Lon         float64  `json:"lon"`
	DisplayName string   `json:"display_name,omitempty"`
	Enabled     bool     `json:"enabled"`
	Routes      []string `json:"routes,omitempty"`
	Origin      string   `json:"origin,omitempty"`
}
type busRoute struct {
	ID          string       `json:"id"`
	Name        string       `json:"name"`
	ShortName   string       `json:"short_name,omitempty"`
	Type        string       `json:"type,omitempty"`
	Path        [][2]float64 `json:"path,omitempty"`
	DisplayName string       `json:"display_name,omitempty"`
	Enabled     bool         `json:"enabled"`
	Origin      string       `json:"origin,omitempty"`
}
type busSchedule struct {
	ID          string `json:"id"`
	RouteID     string `json:"route_id"`
	TripID      string `json:"trip_id"`
	Headsign    string `json:"headsign,omitempty"`
	StopID      string `json:"stop_id"`
	StopName    string `json:"stop_name,omitempty"`
	Sequence    int    `json:"sequence"`
	Arrival     string `json:"arrival"`
	Departure   string `json:"departure"`
	ServiceID   string `json:"service_id,omitempty"`
	ServiceDate string `json:"service_date"`
	Pickup      bool   `json:"pickup,omitempty"`
	Origin      string `json:"origin,omitempty"`
}
type busCalendar struct {
	Start      string         `json:"start"`
	End        string         `json:"end"`
	Weekdays   [7]bool        `json:"weekdays"`
	Exceptions map[string]int `json:"exceptions,omitempty"`
}

func busServiceActive(calendar busCalendar, date string) bool {
	if exception, ok := calendar.Exceptions[date]; ok {
		return exception == 1
	}
	if calendar.Start == "" || date < calendar.Start || date > calendar.End {
		return false
	}
	parsed, err := time.Parse("2006-01-02", date)
	if err != nil {
		return false
	}
	return calendar.Weekdays[(int(parsed.Weekday())+6)%7]
}

type busState struct {
	Revision     int                        `json:"revision"`
	Stops        map[string]busStop         `json:"stops"`
	Routes       map[string]busRoute        `json:"routes"`
	Schedule     map[string]busSchedule     `json:"schedule"`
	Assignments  map[string]json.RawMessage `json:"assignments,omitempty"`
	Calendars    map[string]busCalendar     `json:"calendars,omitempty"`
	ImportID     string                     `json:"import_id,omitempty"`
	ImportName   string                     `json:"import_name,omitempty"`
	ImportStops  int                        `json:"import_stops,omitempty"`
	ImportRoutes int                        `json:"import_routes,omitempty"`
	ImportTrips  int                        `json:"import_trips,omitempty"`
}
type busRequest struct {
	Scope    string          `json:"scope"`
	Revision *int            `json:"revision,omitempty"`
	Payload  json.RawMessage `json:"payload"`
}
type busListRequest struct {
	Query   string `json:"query"`
	RouteID string `json:"route_id"`
	StopID  string `json:"stop_id"`
	Date    string `json:"date"`
	Offset  int    `json:"offset"`
	Limit   int    `json:"limit"`
}
type busRecordRequest struct {
	ID      string          `json:"id"`
	Record  json.RawMessage `json:"record"`
	Kind    string          `json:"kind"`
	Name    string          `json:"name"`
	Enabled bool            `json:"enabled"`
}

func (s *Server) loadBusState(ctx context.Context, tenant, scope string) (busState, error) {
	state := busState{Stops: map[string]busStop{}, Routes: map[string]busRoute{}, Schedule: map[string]busSchedule{}, Assignments: map[string]json.RawMessage{}, Calendars: map[string]busCalendar{}}
	raw, err := s.db.LoadConfig(ctx, tenant, "public_bus_"+scope)
	if err != nil || len(raw) == 0 {
		return state, err
	}
	if err := json.Unmarshal(raw, &state); err != nil {
		return state, err
	}
	if state.Stops == nil {
		state.Stops = map[string]busStop{}
	}
	if state.Routes == nil {
		state.Routes = map[string]busRoute{}
	}
	if state.Schedule == nil {
		state.Schedule = map[string]busSchedule{}
	}
	if state.Assignments == nil {
		state.Assignments = map[string]json.RawMessage{}
	}
	if state.Calendars == nil {
		state.Calendars = map[string]busCalendar{}
	}
	return state, nil
}
func (s *Server) saveBusState(ctx context.Context, tenant, scope string, state busState) error {
	raw, err := json.Marshal(state)
	if err != nil {
		return err
	}
	return s.db.SaveConfig(ctx, tenant, "public_bus_"+scope, raw)
}
func busError(w http.ResponseWriter, code int, message string) {
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(map[string]any{"ok": false, "error": map[string]string{"message": message}})
}
func busID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return ""
	}
	return hex.EncodeToString(b[:])
}
func validBusTime(value string) bool {
	parts := strings.Split(value, ":")
	if len(parts) != 3 {
		return false
	}
	h, e1 := strconv.Atoi(parts[0])
	m, e2 := strconv.Atoi(parts[1])
	sec, e3 := strconv.Atoi(parts[2])
	return e1 == nil && e2 == nil && e3 == nil && h >= 0 && h <= 47 && m >= 0 && m <= 59 && sec >= 0 && sec <= 59
}
func validBusDate(value string) bool {
	if len(value) != 10 || value[4] != '-' || value[7] != '-' {
		return false
	}
	_, err := time.Parse("2006-01-02", value)
	return err == nil
}
func validateStop(v busStop) error {
	if strings.TrimSpace(v.ID) == "" || strings.TrimSpace(v.Name) == "" {
		return errors.New("Stop ID and name are required")
	}
	if len(v.ID) > 128 || len(v.Name) > 256 {
		return errors.New("Stop ID or name is too long")
	}
	if math.IsNaN(v.Lat) || math.IsInf(v.Lat, 0) || v.Lat < -90 || v.Lat > 90 || math.IsNaN(v.Lon) || math.IsInf(v.Lon, 0) || v.Lon < -180 || v.Lon > 180 {
		return errors.New("Stop coordinates are invalid")
	}
	return nil
}
func validateRoute(v busRoute) error {
	if strings.TrimSpace(v.ID) == "" || strings.TrimSpace(v.Name) == "" {
		return errors.New("Route ID and name are required")
	}
	if len(v.ID) > 128 || len(v.Name) > 256 {
		return errors.New("Route ID or name is too long")
	}
	if len(v.Path) < 2 || len(v.Path) > 1000 {
		return errors.New("Place at least two route points on the map")
	}
	for _, p := range v.Path {
		if math.IsNaN(p[0]) || math.IsInf(p[0], 0) || p[0] < -90 || p[0] > 90 || math.IsNaN(p[1]) || math.IsInf(p[1], 0) || p[1] < -180 || p[1] > 180 {
			return errors.New("Route coordinates are invalid")
		}
	}
	return nil
}
func validateSchedule(v busSchedule, state busState) error {
	if v.RouteID == "" || v.TripID == "" || v.StopID == "" || v.Sequence < 1 || !validBusTime(v.Arrival) || !validBusTime(v.Departure) || !validBusDate(v.ServiceDate) {
		return errors.New("Route, trip, stop, sequence, date and valid arrival/departure times are required")
	}
	if _, ok := state.Routes[v.RouteID]; !ok {
		return errors.New("Select an existing route")
	}
	if _, ok := state.Stops[v.StopID]; !ok {
		return errors.New("Select an existing stop")
	}
	return nil
}
func busRows(state busState, operation string, filter busListRequest) map[string]any {
	if filter.Offset < 0 {
		filter.Offset = 0
	}
	if filter.Limit <= 0 || filter.Limit > 200 {
		filter.Limit = 50
	}
	query := strings.ToLower(strings.TrimSpace(filter.Query))
	rows := make([]any, 0)
	switch operation {
	case "list_stops":
		keys := make([]string, 0, len(state.Stops))
		for id := range state.Stops {
			keys = append(keys, id)
		}
		sort.Strings(keys)
		for _, id := range keys {
			v := state.Stops[id]
			if filter.RouteID != "" {
				found := false
				for _, entry := range state.Schedule {
					if entry.RouteID == filter.RouteID && entry.StopID == id {
						found = true
						break
					}
				}
				if !found {
					continue
				}
			}
			if query != "" && !strings.Contains(strings.ToLower(v.Name+" "+v.ID+" "+v.Code), query) {
				continue
			}
			routes := map[string]bool{}
			for _, entry := range state.Schedule {
				if entry.StopID == id {
					routes[entry.RouteID] = true
				}
			}
			v.Routes = []string{}
			for route := range routes {
				v.Routes = append(v.Routes, route)
			}
			sort.Strings(v.Routes)
			rows = append(rows, v)
		}
	case "list_routes":
		keys := make([]string, 0, len(state.Routes))
		for id := range state.Routes {
			keys = append(keys, id)
		}
		sort.Strings(keys)
		for _, id := range keys {
			v := state.Routes[id]
			if query != "" && !strings.Contains(strings.ToLower(v.Name+" "+v.ID+" "+v.ShortName), query) {
				continue
			}
			rows = append(rows, v)
		}
	case "list_schedule":
		keys := make([]string, 0, len(state.Schedule))
		for id := range state.Schedule {
			keys = append(keys, id)
		}
		sort.Strings(keys)
		for _, id := range keys {
			v := state.Schedule[id]
			if filter.RouteID != "" && v.RouteID != filter.RouteID || filter.StopID != "" && v.StopID != filter.StopID || filter.Date != "" && ((v.ServiceDate != "" && v.ServiceDate != filter.Date) || (v.ServiceDate == "" && !busServiceActive(state.Calendars[v.ServiceID], filter.Date))) {
				continue
			}
			if query != "" && !strings.Contains(strings.ToLower(v.RouteID+" "+v.TripID+" "+v.Headsign+" "+v.StopID), query) {
				continue
			}
			v.StopName = state.Stops[v.StopID].Name
			rows = append(rows, v)
		}
	}
	if operation == "list_schedule" {
		sort.Slice(rows, func(i, j int) bool {
			a, b := rows[i].(busSchedule), rows[j].(busSchedule)
			if a.RouteID != b.RouteID {
				return a.RouteID < b.RouteID
			}
			if a.TripID != b.TripID {
				return a.TripID < b.TripID
			}
			if a.ServiceDate != b.ServiceDate {
				return a.ServiceDate < b.ServiceDate
			}
			return a.Sequence < b.Sequence
		})
	}
	total := len(rows)
	if filter.Offset > total {
		filter.Offset = total
	}
	end := filter.Offset + filter.Limit
	if end > total {
		end = total
	}
	return map[string]any{"rows": rows[filter.Offset:end], "total": total}
}
func applyBusMutation(state *busState, operation string, payload json.RawMessage) error {
	var input busRecordRequest
	if err := json.Unmarshal(payload, &input); err != nil {
		return errors.New("Invalid bus record")
	}
	switch operation {
	case "create_stop", "update_stop":
		var v busStop
		if err := json.Unmarshal(input.Record, &v); err != nil {
			return errors.New("Invalid stop")
		}
		if err := validateStop(v); err != nil {
			return err
		}
		_, exists := state.Stops[v.ID]
		if operation == "create_stop" && exists {
			return errors.New("Stop ID already exists")
		}
		if operation == "update_stop" && !exists {
			return errors.New("Stop not found")
		}
		v.Origin = "manual"
		state.Stops[v.ID] = v
	case "delete_stop":
		if _, ok := state.Stops[input.ID]; !ok {
			return errors.New("Stop not found")
		}
		for _, v := range state.Schedule {
			if v.StopID == input.ID {
				return errors.New("Remove schedule entries for this stop first")
			}
		}
		delete(state.Stops, input.ID)
	case "create_route", "update_route":
		var v busRoute
		if err := json.Unmarshal(input.Record, &v); err != nil {
			return errors.New("Invalid route")
		}
		if err := validateRoute(v); err != nil {
			return err
		}
		_, exists := state.Routes[v.ID]
		if operation == "create_route" && exists {
			return errors.New("Route ID already exists")
		}
		if operation == "update_route" && !exists {
			return errors.New("Route not found")
		}
		v.Origin = "manual"
		state.Routes[v.ID] = v
	case "delete_route":
		if _, ok := state.Routes[input.ID]; !ok {
			return errors.New("Route not found")
		}
		for _, v := range state.Schedule {
			if v.RouteID == input.ID {
				return errors.New("Remove schedule entries for this route first")
			}
		}
		delete(state.Routes, input.ID)
	case "create_schedule", "update_schedule":
		var v busSchedule
		if err := json.Unmarshal(input.Record, &v); err != nil {
			return errors.New("Invalid schedule entry")
		}
		if err := validateSchedule(v, *state); err != nil {
			return err
		}
		if operation == "create_schedule" {
			v.ID = busID()
			if v.ID == "" {
				return errors.New("Could not create schedule ID")
			}
		} else {
			if v.ID == "" {
				v.ID = input.ID
			}
			if _, ok := state.Schedule[v.ID]; !ok {
				return errors.New("Schedule entry not found")
			}
		}
		v.Origin = "manual"
		state.Schedule[v.ID] = v
	case "delete_schedule":
		if _, ok := state.Schedule[input.ID]; !ok {
			return errors.New("Schedule entry not found")
		}
		delete(state.Schedule, input.ID)
	case "set_assignment":
		var value struct {
			ID         string `json:"id"`
			ReporterID string `json:"reporter_id"`
			VehicleID  string `json:"vehicle_id"`
			Trip       struct {
				TripID      string `json:"trip_id"`
				ServiceDate string `json:"service_date"`
			} `json:"trip"`
			ValidFrom string `json:"valid_from"`
			ValidTo   string `json:"valid_to"`
		}
		if err := json.Unmarshal(payload, &value); err != nil || value.ID == "" || value.ReporterID == "" || value.VehicleID == "" || value.Trip.TripID == "" || !validBusDate(value.Trip.ServiceDate) {
			return errors.New("Complete the phone assignment fields")
		}
		from, e1 := time.Parse(time.RFC3339, value.ValidFrom)
		to, e2 := time.Parse(time.RFC3339, value.ValidTo)
		if e1 != nil || e2 != nil || !to.After(from) {
			return errors.New("Enter a valid assignment time range")
		}
		state.Assignments[value.ID] = append(json.RawMessage(nil), payload...)
	case "set_override":
		switch input.Kind {
		case "stop":
			v, ok := state.Stops[input.ID]
			if !ok {
				return errors.New("Stop not found")
			}
			v.DisplayName = input.Name
			v.Enabled = input.Enabled
			state.Stops[input.ID] = v
		case "route":
			v, ok := state.Routes[input.ID]
			if !ok {
				return errors.New("Route not found")
			}
			v.DisplayName = input.Name
			v.Enabled = input.Enabled
			state.Routes[input.ID] = v
		default:
			return errors.New("Invalid override")
		}
	default:
		return errors.New("Unsupported bus operation")
	}
	return nil
}
func (s *Server) handlePublicBusRequest(w http.ResponseWriter, r *http.Request) {
	if s.db == nil {
		busError(w, http.StatusServiceUnavailable, "Persistent storage is unavailable")
		return
	}
	ctx, claims, ok := s.applicationContext(r.Context(), mustClaims(r.Context()))
	if !ok {
		unauthorized(w)
		return
	}
	op := chi.URLParam(r, "operation")
	read := op == "get_status" || op == "list_stops" || op == "list_routes" || op == "list_schedule"
	permission := "manage"
	if read {
		permission = "read"
	}
	if !s.checkUIPermission(ctx, "public-bus-config", permission) {
		busError(w, http.StatusForbidden, "Bus configuration permission denied")
		return
	}
	maxBodyBytes := int64(8 << 20)
	if op == "import_start" {
		maxBodyBytes = maxBusGTFSRequestBytes
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxBodyBytes)
	var req busRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || !applications.ValidToken(req.Scope) {
		busError(w, http.StatusBadRequest, "Invalid bus request")
		return
	}
	s.busMu.Lock()
	defer s.busMu.Unlock()
	state, err := s.loadBusState(ctx, claims.TenantID, req.Scope)
	if err != nil {
		busError(w, http.StatusInternalServerError, "Could not load bus configuration")
		return
	}
	var data any
	switch op {
	case "get_status":
		assignments := []json.RawMessage{}
		for _, v := range state.Assignments {
			assignments = append(assignments, v)
		}
		datasets := []any{}
		jobs := []any{}
		if state.ImportID != "" {
			datasets = append(datasets, map[string]any{"id": state.ImportID, "version": state.ImportName, "routes": state.ImportRoutes, "stops": state.ImportStops, "trips": state.ImportTrips})
			jobs = append(jobs, map[string]any{"id": state.ImportID, "dataset_id": state.ImportID, "status": "completed"})
		}
		status := map[string]any{"active": len(state.Stops)+len(state.Routes)+len(state.Schedule) > 0, "environment": "Manual", "state": map[string]any{"revision": state.Revision, "active_dataset": state.ImportID, "previous_dataset": ""}, "datasets": datasets, "feeds": []any{}, "jobs": jobs, "sources": []any{}, "assignments": assignments}
		if org, err := s.db.GetOrganisation(ctx, claims.TenantID); err == nil && org != nil && org.Area != nil {
			status["area"] = org.Area
		}
		data = status
	case "list_stops", "list_routes", "list_schedule":
		var filter busListRequest
		if err := json.Unmarshal(req.Payload, &filter); err != nil {
			busError(w, http.StatusBadRequest, "Invalid list filter")
			return
		}
		data = busRows(state, op, filter)
	case "import_start":
		if req.Revision != nil && *req.Revision != state.Revision {
			busError(w, http.StatusConflict, "Bus configuration changed. Refresh and try again.")
			return
		}
		var input struct {
			ArchiveName   string `json:"archive_name"`
			ArchiveBase64 string `json:"archive_base64"`
		}
		if err := json.Unmarshal(req.Payload, &input); err != nil || !strings.HasSuffix(strings.ToLower(input.ArchiveName), ".zip") {
			busError(w, http.StatusBadRequest, "Choose a GTFS ZIP file")
			return
		}
		feed, err := parseBusGTFS(input.ArchiveBase64)
		if err != nil {
			busError(w, http.StatusBadRequest, err.Error())
			return
		}
		for id, v := range state.Stops {
			if v.Origin == "gtfs" {
				delete(state.Stops, id)
			}
		}
		for id, v := range state.Routes {
			if v.Origin == "gtfs" {
				delete(state.Routes, id)
			}
		}
		for id, v := range state.Schedule {
			if v.Origin == "gtfs" {
				delete(state.Schedule, id)
			}
		}
		for id, v := range feed.Stops {
			if _, exists := state.Stops[id]; !exists {
				state.Stops[id] = v
			}
		}
		for id, v := range feed.Routes {
			if _, exists := state.Routes[id]; !exists {
				state.Routes[id] = v
			}
		}
		for id, v := range feed.Schedule {
			if _, exists := state.Schedule[id]; !exists {
				state.Schedule[id] = v
			}
		}
		for _, entry := range state.Schedule {
			if entry.Origin != "manual" {
				continue
			}
			if _, ok := state.Stops[entry.StopID]; !ok {
				busError(w, http.StatusConflict, "Import would remove a stop used by a manual schedule entry")
				return
			}
			if _, ok := state.Routes[entry.RouteID]; !ok {
				busError(w, http.StatusConflict, "Import would remove a route used by a manual schedule entry")
				return
			}
		}
		state.Calendars = feed.Calendars
		state.ImportID = busID()
		if state.ImportID == "" {
			busError(w, http.StatusInternalServerError, "Could not create import ID")
			return
		}
		state.ImportName = input.ArchiveName
		state.ImportStops = len(feed.Stops)
		state.ImportRoutes = len(feed.Routes)
		state.ImportTrips = feed.Trips
		state.Revision++
		if err := s.saveBusState(ctx, claims.TenantID, req.Scope, state); err != nil {
			busError(w, http.StatusInternalServerError, "Could not save imported bus data")
			return
		}
		data = map[string]any{"dataset_id": state.ImportID, "stops": state.ImportStops, "routes": state.ImportRoutes, "trips": state.ImportTrips}
	case "create_stop", "update_stop", "delete_stop", "create_route", "update_route", "delete_route", "create_schedule", "update_schedule", "delete_schedule", "set_override", "set_assignment":
		if req.Revision != nil && *req.Revision != state.Revision {
			busError(w, http.StatusConflict, "Bus configuration changed. Refresh and try again.")
			return
		}
		if err := applyBusMutation(&state, op, req.Payload); err != nil {
			busError(w, http.StatusBadRequest, err.Error())
			return
		}
		state.Revision++
		if err := s.saveBusState(ctx, claims.TenantID, req.Scope, state); err != nil {
			busError(w, http.StatusInternalServerError, "Could not save bus configuration")
			return
		}
		data = map[string]any{"saved": true}
	default:
		busError(w, http.StatusNotImplemented, fmt.Sprintf("Bus operation %s is not available", op))
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "revision": state.Revision, "data": data})
}
func mustClaims(ctx context.Context) *JWTClaims {
	claims, _ := GetClaimsFromContext(ctx)
	return claims
}
