package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/xact-iot/xact/sqldb"
	"github.com/xact-iot/xact/sqldb/sqlite"
)

func TestDashboardAPIManagerActionsPreserveWidgets(t *testing.T) {
	ctx := context.Background()
	db, err := sqlite.NewSQLiteDB(ctx, "file:dashboard-manager-test?mode=memory&cache=shared")
	if err != nil {
		t.Fatalf("new sqlite db: %v", err)
	}
	defer db.Close()
	if err := db.Migrate(ctx); err != nil {
		t.Fatalf("migrate sqlite db: %v", err)
	}

	router := newDashboardTestRouter(db)

	issWidgets := []map[string]any{
		{"id": "iss-title", "type": "html-widget", "x": 0, "y": 0, "w": 24, "h": 3, "config": map[string]any{"html": "ISS"}},
		{"id": "iss-altitude", "type": "big-number-widget", "x": 0, "y": 3, "w": 6, "h": 5, "config": map[string]any{"tagPath": "NASA.ISS.orbit.altitude"}},
	}
	tagViewWidgets := []map[string]any{
		{"id": "tag-browser", "type": "tags-manager-widget", "x": 0, "y": 0, "w": 24, "h": 20, "config": map[string]any{}},
	}

	operations := postDashboard(t, router, map[string]any{"name": "Operations", "icon": "mdi:view-dashboard", "sortOrder": 0})
	archive := postDashboard(t, router, map[string]any{"name": "Archive", "icon": "mdi:folder", "sortOrder": 1})
	iss := postDashboard(t, router, map[string]any{"name": "ISS", "description": "ISS telemetry", "icon": "mdi:space-station", "sortOrder": 2, "widgets": issWidgets})
	tagView := postDashboard(t, router, map[string]any{"name": "Tag View", "icon": "mdi:proximity-sensor", "sortOrder": 3, "widgets": tagViewWidgets})

	// Re-arrange top-level categories using the same metadata-only update shape
	// as the dashboard manager. Widgets must be preserved because they are omitted.
	putDashboard(t, router, archive.ID, map[string]any{"name": "Archive", "icon": "mdi:folder", "sortOrder": 0})
	putDashboard(t, router, operations.ID, map[string]any{"name": "Operations", "icon": "mdi:view-dashboard", "sortOrder": 3})

	// Move dashboards into a category. These updates intentionally omit widgets.
	putDashboard(t, router, iss.ID, map[string]any{"name": "ISS", "description": "ISS telemetry", "icon": "mdi:space-station", "parentId": archive.ID, "sortOrder": 1})
	putDashboard(t, router, tagView.ID, map[string]any{"name": "Tag View", "icon": "mdi:proximity-sensor", "parentId": archive.ID, "sortOrder": 2})

	// Rename the category, then send child updates that refer to its stable id.
	putDashboard(t, router, archive.ID, map[string]any{"name": "Mission", "icon": "mdi:folder-star", "sortOrder": 0})
	putDashboard(t, router, iss.ID, map[string]any{"name": "ISS", "description": "ISS telemetry", "icon": "mdi:space-station", "parentId": archive.ID, "sortOrder": 1})
	putDashboard(t, router, tagView.ID, map[string]any{"name": "Tag View", "icon": "mdi:proximity-sensor", "parentId": archive.ID, "sortOrder": 2})

	dashboards := listDashboards(t, router)
	byName := map[string]sqldb.DashboardMeta{}
	for _, p := range dashboards {
		byName[p.Name] = p
	}

	mission, ok := byName["Mission"]
	if !ok {
		t.Fatalf("renamed category Mission not found in list: %#v", dashboards)
	}
	if mission.ParentID != nil {
		t.Fatalf("Mission should be top level, got parent id %v", *mission.ParentID)
	}
	if mission.SortOrder != 0 {
		t.Fatalf("Mission sort order = %d, want 0", mission.SortOrder)
	}

	operationsMeta, ok := byName["Operations"]
	if !ok {
		t.Fatalf("Operations category not found in list: %#v", dashboards)
	}
	if operationsMeta.ParentID != nil {
		t.Fatalf("Operations should remain top level, got parent id %v", *operationsMeta.ParentID)
	}
	if operationsMeta.SortOrder != 3 {
		t.Fatalf("Operations sort order = %d, want 3", operationsMeta.SortOrder)
	}

	for name, wantOrder := range map[string]int{"ISS": 1, "Tag View": 2} {
		got, ok := byName[name]
		if !ok {
			t.Fatalf("%s dashboard not found in list: %#v", name, dashboards)
		}
		if got.ParentID == nil || *got.ParentID != mission.ID {
			t.Fatalf("%s parent id = %v, want Mission id %d", name, got.ParentID, mission.ID)
		}
		if got.SortOrder != wantOrder {
			t.Fatalf("%s sort order = %d, want %d", name, got.SortOrder, wantOrder)
		}
	}

	assertDashboardWidgets(t, router, iss.ID, "ISS", issWidgets)
	assertDashboardWidgets(t, router, tagView.ID, "Tag View", tagViewWidgets)
	assertDashboardWidgets(t, router, archive.ID, "Mission", []map[string]any{})
}

func newDashboardTestRouter(db sqldb.DB) http.Handler {
	h := NewDashboardHandlers(db, func(context.Context) (string, bool) {
		return "default", true
	})
	r := chi.NewRouter()
	r.Get("/dashboards", h.HandleListDashboards)
	r.Post("/dashboards", h.HandleCreateDashboard)
	r.Get("/dashboards/{id}", h.HandleGetDashboard)
	r.Put("/dashboards/{id}", h.HandleUpdateDashboard)
	r.Delete("/dashboards/{id}", h.HandleDeleteDashboard)
	return r
}

func postDashboard(t *testing.T, handler http.Handler, payload map[string]any) sqldb.Dashboard {
	t.Helper()
	var dashboard sqldb.Dashboard
	doJSON(t, handler, http.MethodPost, "/dashboards", payload, http.StatusCreated, &dashboard)
	return dashboard
}

func putDashboard(t *testing.T, handler http.Handler, id int, payload map[string]any) {
	t.Helper()
	doJSON(t, handler, http.MethodPut, "/dashboards/"+strconv.Itoa(id), payload, http.StatusOK, nil)
}

func listDashboards(t *testing.T, handler http.Handler) []sqldb.DashboardMeta {
	t.Helper()
	var dashboards []sqldb.DashboardMeta
	doJSON(t, handler, http.MethodGet, "/dashboards", nil, http.StatusOK, &dashboards)
	return dashboards
}

func getDashboard(t *testing.T, handler http.Handler, id int) sqldb.Dashboard {
	t.Helper()
	var dashboard sqldb.Dashboard
	doJSON(t, handler, http.MethodGet, "/dashboards/"+strconv.Itoa(id), nil, http.StatusOK, &dashboard)
	return dashboard
}

func assertDashboardWidgets(t *testing.T, handler http.Handler, id int, name string, want []map[string]any) {
	t.Helper()
	dashboard := getDashboard(t, handler, id)
	var got []map[string]any
	if err := json.Unmarshal(dashboard.Widgets, &got); err != nil {
		t.Fatalf("unmarshal %s widgets: %v; raw=%s", name, err, string(dashboard.Widgets))
	}
	gotJSON := marshalJSON(t, got)
	wantJSON := marshalJSON(t, want)
	if !bytes.Equal(gotJSON, wantJSON) {
		t.Fatalf("%s widgets changed\n got: %s\nwant: %s", name, gotJSON, wantJSON)
	}
}

func marshalJSON(t *testing.T, value any) []byte {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("marshal json value: %v", err)
	}
	return data
}

func doJSON(t *testing.T, handler http.Handler, method, path string, payload any, wantStatus int, out any) {
	t.Helper()
	var body bytes.Buffer
	if payload != nil {
		if err := json.NewEncoder(&body).Encode(payload); err != nil {
			t.Fatalf("encode %s %s payload: %v", method, path, err)
		}
	}
	req := httptest.NewRequest(method, path, &body)
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	if rr.Code != wantStatus {
		t.Fatalf("%s %s status = %d, want %d; body=%s", method, path, rr.Code, wantStatus, rr.Body.String())
	}
	if out != nil {
		if err := json.NewDecoder(rr.Body).Decode(out); err != nil {
			t.Fatalf("decode %s %s response: %v; body=%s", method, path, err, rr.Body.String())
		}
	}
}

func TestDashboardAPIManagerActionsRequireExplicitWidgetsToOverwrite(t *testing.T) {
	ctx := context.Background()
	db, err := sqlite.NewSQLiteDB(ctx, "file:dashboard-manager-overwrite-test?mode=memory&cache=shared")
	if err != nil {
		t.Fatalf("new sqlite db: %v", err)
	}
	defer db.Close()
	if err := db.Migrate(ctx); err != nil {
		t.Fatalf("migrate sqlite db: %v", err)
	}
	router := newDashboardTestRouter(db)

	widgets := []map[string]any{{"id": "w1", "type": "text-widget", "config": map[string]any{"text": "keep me"}}}
	dashboard := postDashboard(t, router, map[string]any{"name": "Dashboard", "sortOrder": 0, "widgets": widgets})

	putDashboard(t, router, dashboard.ID, map[string]any{"name": "Dashboard", "sortOrder": 1})
	assertDashboardWidgets(t, router, dashboard.ID, "Dashboard", widgets)

	putDashboard(t, router, dashboard.ID, map[string]any{"name": "Dashboard", "sortOrder": 2, "widgets": []map[string]any{}})
	assertDashboardWidgets(t, router, dashboard.ID, "Dashboard", widgets)

	putDashboard(t, router, dashboard.ID, map[string]any{"widgets": []map[string]any{}})
	assertDashboardWidgets(t, router, dashboard.ID, "Dashboard", []map[string]any{})
}

func TestDashboardAPISupportsDuplicateNamesWithDifferentIDs(t *testing.T) {
	ctx := context.Background()
	db, err := sqlite.NewSQLiteDB(ctx, "file:dashboard-manager-duplicate-name-test?mode=memory&cache=shared")
	if err != nil {
		t.Fatalf("new sqlite db: %v", err)
	}
	defer db.Close()
	if err := db.Migrate(ctx); err != nil {
		t.Fatalf("migrate sqlite db: %v", err)
	}
	router := newDashboardTestRouter(db)

	dashboard := postDashboard(t, router, map[string]any{"name": "ISS", "sortOrder": 0, "isCategory": false})
	category := postDashboard(t, router, map[string]any{"name": "ISS", "icon": "mdi:folder", "sortOrder": 1, "isCategory": true})
	if dashboard.ID == category.ID {
		t.Fatalf("duplicate-name rows should have distinct IDs, got %d", dashboard.ID)
	}

	putDashboard(t, router, category.ID, map[string]any{"name": "ISS", "isCategory": true, "sortOrder": 2})
	gotDashboard := getDashboard(t, router, dashboard.ID)
	gotCategory := getDashboard(t, router, category.ID)
	if gotDashboard.IsCategory {
		t.Fatalf("dashboard row was changed into a category")
	}
	if !gotCategory.IsCategory {
		t.Fatalf("category row did not persist isCategory")
	}
}

func TestDashboardAPIEscapedDashboardName(t *testing.T) {
	ctx := context.Background()
	db, err := sqlite.NewSQLiteDB(ctx, "file:dashboard-manager-escaped-name-test?mode=memory&cache=shared")
	if err != nil {
		t.Fatalf("new sqlite db: %v", err)
	}
	defer db.Close()
	if err := db.Migrate(ctx); err != nil {
		t.Fatalf("migrate sqlite db: %v", err)
	}
	router := newDashboardTestRouter(db)

	name := "Orgs, Users, Permissions"
	widgets := []map[string]any{
		{
			"id":   "ufe27ej1",
			"type": "tabs-widget",
			"x":    1,
			"y":    0,
			"w":    23,
			"h":    46,
			"config": map[string]any{
				"tabs": []map[string]any{
					{"id": "tpxdyik", "label": "Organisations", "widgetType": "organisations-widget", "widgetConfig": map[string]any{}},
					{"id": "tdbfspi", "label": "Users", "widgetType": "users-widget", "widgetConfig": map[string]any{}},
					{"id": "tphcmax", "label": "Permissions", "widgetType": "permissions-widget", "widgetConfig": map[string]any{}},
				},
				"activeTabId": "tpxdyik",
			},
		},
	}

	dashboard := postDashboard(t, router, map[string]any{"name": name, "sortOrder": 0, "widgets": widgets})

	got := getDashboard(t, router, dashboard.ID)
	if got.Name != name {
		t.Fatalf("GET dashboard name = %q, want %q", got.Name, name)
	}

	putDashboard(t, router, dashboard.ID, map[string]any{"name": name, "sortOrder": 4})
	assertDashboardWidgets(t, router, dashboard.ID, name, widgets)

	updatedWidgets := []map[string]any{{"id": "saved", "type": "html-widget", "config": map[string]any{"html": "saved"}}}
	putDashboard(t, router, dashboard.ID, map[string]any{"name": name, "widgets": updatedWidgets})
	assertDashboardWidgets(t, router, dashboard.ID, name, updatedWidgets)
}

func TestDashboardAPIDeleteAndValidation(t *testing.T) {
	ctx := context.Background()
	db, err := sqlite.NewSQLiteDB(ctx, "file:dashboard-delete-test?mode=memory&cache=shared")
	if err != nil {
		t.Fatalf("new sqlite db: %v", err)
	}
	defer db.Close()
	if err := db.Migrate(ctx); err != nil {
		t.Fatalf("migrate sqlite db: %v", err)
	}
	router := newDashboardTestRouter(db)

	dashboard := postDashboard(t, router, map[string]any{"name": "Temporary", "sortOrder": 0})
	doJSON(t, router, http.MethodDelete, "/dashboards/bad", nil, http.StatusBadRequest, nil)
	doJSON(t, router, http.MethodDelete, "/dashboards/"+strconv.Itoa(dashboard.ID), nil, http.StatusNoContent, nil)
	doJSON(t, router, http.MethodGet, "/dashboards/"+strconv.Itoa(dashboard.ID), nil, http.StatusNotFound, nil)
	doJSON(t, router, http.MethodDelete, "/dashboards/"+strconv.Itoa(dashboard.ID), nil, http.StatusInternalServerError, nil)
}
