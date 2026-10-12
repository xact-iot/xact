package api

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/xact-iot/xact/rtdb/tree"
	"github.com/xact-iot/xact/sqldb"
)

func busPayload(t *testing.T, value any) json.RawMessage {
	t.Helper()
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func TestManualBusCRUDAndFiltering(t *testing.T) {
	state := busState{Stops: map[string]busStop{}, Routes: map[string]busRoute{}, Schedule: map[string]busSchedule{}}
	stop := busStop{ID: "stop-1", Name: "Market", Lat: 15.3, Lon: -61.4, Enabled: true}
	route := busRoute{ID: "route-1", Name: "Town loop", Path: [][2]float64{{15.3, -61.4}, {15.31, -61.41}}, Enabled: true}
	if err := applyBusMutation(&state, "create_stop", busPayload(t, map[string]any{"record": stop})); err != nil {
		t.Fatal(err)
	}
	if err := applyBusMutation(&state, "create_route", busPayload(t, map[string]any{"record": route})); err != nil {
		t.Fatal(err)
	}
	if err := applyBusMutation(&state, "create_stop", busPayload(t, map[string]any{"record": stop})); err == nil {
		t.Fatal("duplicate stop was accepted")
	}
	stop.Name = "Market Square"
	if err := applyBusMutation(&state, "update_stop", busPayload(t, map[string]any{"record": stop})); err != nil {
		t.Fatal(err)
	}
	route.Name = "Town route"
	if err := applyBusMutation(&state, "update_route", busPayload(t, map[string]any{"record": route})); err != nil {
		t.Fatal(err)
	}
	entry := busSchedule{RouteID: "route-1", TripID: "trip-1", StopID: "stop-1", Sequence: 1, Arrival: "08:00:00", Departure: "08:01:00", ServiceDate: "2026-09-29"}
	if err := applyBusMutation(&state, "create_schedule", busPayload(t, map[string]any{"record": entry})); err != nil {
		t.Fatal(err)
	}
	rows := busRows(state, "list_schedule", busListRequest{RouteID: "route-1", Date: "2026-09-29"})
	if rows["total"] != 1 {
		t.Fatalf("expected one schedule row, got %v", rows["total"])
	}
	if got := rows["rows"].([]any)[0].(busSchedule); got.StopName != "Market Square" || got.ID == "" {
		t.Fatalf("schedule row not enriched: %+v", got)
	}
	if err := applyBusMutation(&state, "delete_stop", busPayload(t, map[string]any{"id": "stop-1"})); err == nil {
		t.Fatal("referenced stop was deleted")
	}
	id := rows["rows"].([]any)[0].(busSchedule).ID
	entry.ID = id
	entry.Arrival = "09:00:00"
	if err := applyBusMutation(&state, "update_schedule", busPayload(t, map[string]any{"record": entry})); err != nil {
		t.Fatal(err)
	}
	if state.Schedule[id].Arrival != "09:00:00" {
		t.Fatal("schedule update was not stored")
	}
	if err := applyBusMutation(&state, "delete_schedule", busPayload(t, map[string]any{"id": id})); err != nil {
		t.Fatal(err)
	}
	if err := applyBusMutation(&state, "delete_stop", busPayload(t, map[string]any{"id": "stop-1"})); err != nil {
		t.Fatal(err)
	}
	if err := applyBusMutation(&state, "delete_route", busPayload(t, map[string]any{"id": "route-1"})); err != nil {
		t.Fatal(err)
	}
	if len(state.Stops) != 0 || len(state.Routes) != 0 || len(state.Schedule) != 0 {
		t.Fatal("delete did not persist in state")
	}
}

func TestManualBusValidationAndPersistence(t *testing.T) {
	state := busState{Stops: map[string]busStop{}, Routes: map[string]busRoute{}, Schedule: map[string]busSchedule{}}
	badRoute := busRoute{ID: "route-1", Name: "Incomplete", Path: [][2]float64{{15, -61}}}
	if err := applyBusMutation(&state, "create_route", busPayload(t, map[string]any{"record": badRoute})); err == nil || !strings.Contains(err.Error(), "two route points") {
		t.Fatalf("expected path validation, got %v", err)
	}
	badStop := busStop{ID: "stop-1", Name: "Invalid", Lat: 91, Lon: 0}
	if err := applyBusMutation(&state, "create_stop", busPayload(t, map[string]any{"record": badStop})); err == nil {
		t.Fatal("invalid coordinates were accepted")
	}
	db := &testDB{}
	server := &Server{db: db}
	state.Stops["valid"] = busStop{ID: "valid", Name: "Valid", Lat: 15, Lon: -61, Enabled: true}
	if err := server.saveBusState(context.Background(), "tenant-a", "default", state); err != nil {
		t.Fatal(err)
	}
	got, err := server.loadBusState(context.Background(), "tenant-a", "default")
	if err != nil {
		t.Fatal(err)
	}
	if got.Stops["valid"].Name != "Valid" || db.configName != "public_bus_default" || db.configOrg != "tenant-a" {
		t.Fatalf("state not stored per tenant and scope: %+v", got)
	}
}

type busTestDB struct{ *testDB }

func (d *busTestDB) GetUserByID(_ context.Context, _ int) (*sqldb.User, error) { return d.user, nil }

func TestPublicBusRequestRoute(t *testing.T) {
	server := NewServer(ServerConfig{ProxyPath: "/xact"}, tree.NewTreeWithOperations(nil), nil, nil, "test-secret", newTestDB("admin", "password"), "")
	req := httptest.NewRequest(http.MethodPost, "/xact/api/v1/applications/public_bus/request/get_status", strings.NewReader(`{"scope":"default","payload":{}}`))
	rec := httptest.NewRecorder()
	server.Router().ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("bus route should exist and require authentication, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestManualBusHTTPPermissionsAndRevision(t *testing.T) {
	db := &busTestDB{newRoleTestDB("admin", "password", "SystemAdmin")}
	db.organisations = []sqldb.Organisation{{Name: "default", Area: &sqldb.OrgArea{North: 15.7, South: 15.1, East: -61.1, West: -61.8}}}
	server := &Server{db: db}
	call := func(op string, revision *int, payload any, roles []string) *httptest.ResponseRecorder {
		body := busPayload(t, map[string]any{"scope": "default", "revision": revision, "payload": payload})
		req := httptest.NewRequest(http.MethodPost, "/api/v1/applications/public_bus/request/"+op, strings.NewReader(string(body)))
		route := chi.NewRouteContext()
		route.URLParams.Add("operation", op)
		ctx := context.WithValue(req.Context(), chi.RouteCtxKey, route)
		ctx = context.WithValue(ctx, claimsContextKey, &JWTClaims{UserID: "1", TenantID: "default", Roles: roles})
		rec := httptest.NewRecorder()
		server.handlePublicBusRequest(rec, req.WithContext(ctx))
		return rec
	}
	stop := busStop{ID: "stop-1", Name: "Market", Lat: 15.3, Lon: -61.4, Enabled: true}
	rec := call("create_stop", nil, map[string]any{"record": stop}, []string{"SystemAdmin"})
	if rec.Code != http.StatusOK {
		t.Fatalf("create status %d: %s", rec.Code, rec.Body.String())
	}
	stale := 0
	rec = call("create_stop", &stale, map[string]any{"record": busStop{ID: "stop-2", Name: "Other", Lat: 15, Lon: -61}}, []string{"SystemAdmin"})
	if rec.Code != http.StatusConflict {
		t.Fatalf("expected revision conflict, got %d", rec.Code)
	}
	rec = call("list_stops", nil, map[string]any{}, []string{"SystemAdmin"})
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "Market") {
		t.Fatalf("list failed: %d %s", rec.Code, rec.Body.String())
	}
	rec = call("get_status", nil, map[string]any{}, []string{"SystemAdmin"})
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"area":{"north":15.7,"south":15.1,"east":-61.1,"west":-61.8}`) {
		t.Fatalf("organisation bounds missing from bus status: %d %s", rec.Code, rec.Body.String())
	}
	db.user.Orgs = []sqldb.UserOrg{{OrgID: 1, OrgName: "default", Roles: []string{"Viewer"}}}
	rec = call("create_stop", nil, map[string]any{"record": stop}, []string{"Viewer"})
	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected permission denial, got %d", rec.Code)
	}
}

func TestBusImportRequestBodyLimit(t *testing.T) {
	if maxBusGTFSRequestBytes <= int64(base64.StdEncoding.EncodedLen(maxBusGTFSZipBytes))+1024 {
		t.Fatal("import request cap does not fit a 25 MiB ZIP after base64 encoding")
	}
	handler := limitRequestBodyWithImport(8, 36)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, err := io.Copy(io.Discard, r.Body); err != nil {
			w.WriteHeader(http.StatusRequestEntityTooLarge)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	for _, tc := range []struct {
		method, path, body string
		want               int
	}{
		{http.MethodPost, "/xact/api/v1/applications/public_bus/request/import_start", strings.Repeat("x", 24), http.StatusNoContent},
		{http.MethodPost, "/xact/api/v1/applications/public_bus/upload", strings.Repeat("x", 24), http.StatusNoContent},
		{http.MethodPost, "/xact/api/v1/applications/public_bus/upload", strings.Repeat("x", 37), http.StatusRequestEntityTooLarge},
		{http.MethodPost, "/xact/api/v1/applications/public_bus/request/import_start", strings.Repeat("x", 37), http.StatusRequestEntityTooLarge},
		{http.MethodPost, "/xact/api/v1/applications/public_bus/request/create_stop", strings.Repeat("x", 24), http.StatusRequestEntityTooLarge},
		{http.MethodGet, "/xact/api/v1/applications/public_bus/request/import_start", strings.Repeat("x", 24), http.StatusRequestEntityTooLarge},
	} {
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, httptest.NewRequest(tc.method, tc.path, strings.NewReader(tc.body)))
		if rec.Code != tc.want {
			t.Fatalf("%s %s (%d bytes): got %d, want %d", tc.method, tc.path, len(tc.body), rec.Code, tc.want)
		}
	}
}

func TestBusGTFSArchiveExpandedLimits(t *testing.T) {
	file := func(name string, size uint64) *zip.File {
		return &zip.File{FileHeader: zip.FileHeader{Name: name, UncompressedSize64: size}}
	}
	for _, tc := range []struct {
		name  string
		files []*zip.File
		want  string
	}{
		{"unused shapes are ignored", []*zip.File{file("shapes.txt", 600<<20), file("stops.txt", 100<<20)}, ""},
		{"large used tables fit", []*zip.File{file("stops.txt", 100<<20), file("stop_times.txt", 100<<20)}, ""},
		{"single table cap", []*zip.File{file("stop_times.txt", 129<<20)}, "stop_times.txt exceeds 128 MiB"},
		{"total used table cap", []*zip.File{file("stops.txt", 100<<20), file("trips.txt", 100<<20), file("stop_times.txt", 100<<20)}, "GTFS tables expand beyond 256 MiB"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := validateBusGTFSArchiveSizes(tc.files)
			if tc.want == "" && err != nil || tc.want != "" && (err == nil || !strings.Contains(err.Error(), tc.want)) {
				t.Fatalf("validateBusGTFSArchiveSizes() = %v, want %q", err, tc.want)
			}
		})
	}
}

func TestParseBusGTFSWithLargeUnusedShapes(t *testing.T) {
	var buffer bytes.Buffer
	archive := zip.NewWriter(&buffer)
	for name, body := range map[string]string{
		"stops.txt":      "stop_id,stop_name,stop_lat,stop_lon\nA,Market,15.3,-61.4\n",
		"routes.txt":     "route_id,route_short_name,route_long_name,route_type\nR,1,Town loop,3\n",
		"trips.txt":      "route_id,service_id,trip_id,trip_headsign\nR,weekday,T,Town\n",
		"stop_times.txt": "trip_id,arrival_time,departure_time,stop_id,stop_sequence\nT,08:00:00,08:01:00,A,1\n",
	} {
		entry, err := archive.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := entry.Write([]byte(body)); err != nil {
			t.Fatal(err)
		}
	}
	shapes, err := archive.Create("shapes.txt")
	if err != nil {
		t.Fatal(err)
	}
	chunk := bytes.Repeat([]byte("x"), 1<<20)
	for i := 0; i < 70; i++ {
		if _, err := shapes.Write(chunk); err != nil {
			t.Fatal(err)
		}
	}
	if err := archive.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := parseBusGTFS(base64.StdEncoding.EncodeToString(buffer.Bytes())); err != nil {
		t.Fatalf("GTFS with unused shapes larger than 64 MiB was rejected: %v", err)
	}
}

func TestParseBusGTFSOverFiveMiB(t *testing.T) {
	var buffer bytes.Buffer
	archive := zip.NewWriter(&buffer)
	for name, body := range map[string]string{
		"stops.txt":      "stop_id,stop_name,stop_lat,stop_lon\nA,Market,15.3,-61.4\n",
		"routes.txt":     "route_id,route_short_name,route_long_name,route_type\nR,1,Town loop,3\n",
		"trips.txt":      "route_id,service_id,trip_id,trip_headsign\nR,weekday,T,Town\n",
		"stop_times.txt": "trip_id,arrival_time,departure_time,stop_id,stop_sequence\nT,08:00:00,08:01:00,A,1\n",
	} {
		entry, err := archive.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := entry.Write([]byte(body)); err != nil {
			t.Fatal(err)
		}
	}
	padding, err := archive.CreateHeader(&zip.FileHeader{Name: "padding.bin", Method: zip.Store})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := padding.Write(bytes.Repeat([]byte("x"), 16<<20)); err != nil {
		t.Fatal(err)
	}
	if err := archive.Close(); err != nil {
		t.Fatal(err)
	}
	if buffer.Len() <= 5<<20 || buffer.Len() > maxBusGTFSZipBytes {
		t.Fatalf("test ZIP size = %d", buffer.Len())
	}
	if _, err := parseBusGTFS(base64.StdEncoding.EncodeToString(buffer.Bytes())); err != nil {
		t.Fatalf("16 MiB GTFS ZIP was rejected: %v", err)
	}
}

func TestParseBusGTFS(t *testing.T) {
	var buffer bytes.Buffer
	archive := zip.NewWriter(&buffer)
	files := map[string]string{
		"stops.txt":          "stop_id,stop_name,stop_lat,stop_lon\nA,Market,15.3,-61.4\nB,Station,15.4,-61.5\n",
		"routes.txt":         "route_id,route_short_name,route_long_name,route_type\nR,1,Town loop,3\nX,2,Rail,2\n",
		"trips.txt":          "route_id,service_id,trip_id,trip_headsign\nR,weekday,T,Town\nX,weekday,rail,Rail\n",
		"stop_times.txt":     "trip_id,arrival_time,departure_time,stop_id,stop_sequence\nT,08:00:00,08:01:00,A,1\nT,08:10:00,08:11:00,B,2\n",
		"calendar.txt":       "service_id,monday,tuesday,wednesday,thursday,friday,saturday,sunday,start_date,end_date\nweekday,1,1,1,1,1,0,0,20260901,20260930\n",
		"calendar_dates.txt": "service_id,date,exception_type\nweekday,20260930,2\n",
	}
	for name, body := range files {
		writer, err := archive.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := writer.Write([]byte(body)); err != nil {
			t.Fatal(err)
		}
	}
	if err := archive.Close(); err != nil {
		t.Fatal(err)
	}
	feed, err := parseBusGTFS(base64.StdEncoding.EncodeToString(buffer.Bytes()))
	if err != nil {
		t.Fatal(err)
	}
	if len(feed.Stops) != 2 || len(feed.Routes) != 1 || len(feed.Schedule) != 2 || len(feed.Routes["R"].Path) != 2 || feed.Trips != 1 {
		t.Fatalf("unexpected import: %+v", feed)
	}
	if !busServiceActive(feed.Calendars["weekday"], "2026-09-29") || busServiceActive(feed.Calendars["weekday"], "2026-09-30") {
		t.Fatal("GTFS calendar exceptions were not applied")
	}
	imported := busState{Stops: feed.Stops, Routes: feed.Routes, Schedule: feed.Schedule, Calendars: feed.Calendars}
	if busRows(imported, "list_schedule", busListRequest{Date: "2026-09-29"})["total"] != 2 || busRows(imported, "list_schedule", busListRequest{Date: "2026-09-30"})["total"] != 0 {
		t.Fatal("imported schedule date filtering is incorrect")
	}
	if _, err := parseBusGTFS("not a zip"); err == nil {
		t.Fatal("invalid ZIP was accepted")
	}
	db := &busTestDB{newRoleTestDB("admin", "password", "SystemAdmin")}
	server := &Server{db: db}
	existing := busState{Stops: map[string]busStop{"manual": {ID: "manual", Name: "Manual", Lat: 15, Lon: -61, Origin: "manual"}}, Routes: map[string]busRoute{}, Schedule: map[string]busSchedule{}, Calendars: map[string]busCalendar{}}
	if err := server.saveBusState(context.Background(), "default", "default", existing); err != nil {
		t.Fatal(err)
	}
	body := busPayload(t, map[string]any{"scope": "default", "payload": map[string]any{"archive_name": "feed.zip", "archive_base64": base64.StdEncoding.EncodeToString(buffer.Bytes())}})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/applications/public_bus/request/import_start", strings.NewReader(string(body)))
	route := chi.NewRouteContext()
	route.URLParams.Add("operation", "import_start")
	ctx := context.WithValue(req.Context(), chi.RouteCtxKey, route)
	ctx = context.WithValue(ctx, claimsContextKey, &JWTClaims{UserID: "1", TenantID: "default", Roles: []string{"SystemAdmin"}})
	rec := httptest.NewRecorder()
	server.handlePublicBusRequest(rec, req.WithContext(ctx))
	if rec.Code != http.StatusOK {
		t.Fatalf("import failed: %d %s", rec.Code, rec.Body.String())
	}
	stored, err := server.loadBusState(context.Background(), "default", "default")
	if err != nil {
		t.Fatal(err)
	}
	if stored.Stops["manual"].Name != "Manual" || stored.Stops["A"].Name != "Market" || len(stored.Schedule) != 2 || stored.ImportID == "" {
		t.Fatalf("import did not preserve manual records: %+v", stored)
	}
}
