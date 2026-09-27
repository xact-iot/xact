package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/xact-iot/xact/sqldb"
	"github.com/xact-iot/xact/sqldb/sqlite"
)

func TestPublicDashboardEndpointsEnforcePublicationAndSanitizeWidgets(t *testing.T) {
	ctx := context.Background()
	db, err := sqlite.NewSQLiteDB(ctx, "file:public-dashboard-test?mode=memory&cache=shared")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := db.Migrate(ctx); err != nil {
		t.Fatal(err)
	}

	private := &sqldb.Dashboard{Name: "Private", Widgets: json.RawMessage(`[{"id":"a","type":"text-widget","config":{"text":"secret"}}]`)}
	if err := db.CreateDashboard(ctx, "default", private); err != nil {
		t.Fatal(err)
	}
	public := &sqldb.Dashboard{Name: "Public", IsPublic: true, Widgets: json.RawMessage(`[{"id":"b","type":"area-map-widget","x":0,"y":0,"w":12,"h":12,"config":{"tomtomApiKey":"secret-key","showTraffic":true,"layers":[]}}]`)}
	if err := db.CreateDashboard(ctx, "default", public); err != nil {
		t.Fatal(err)
	}

	h := NewDashboardHandlers(db, func(context.Context) (string, bool) { return "", false })
	r := chi.NewRouter()
	r.Get("/public", h.HandleListPublicDashboards)
	r.Get("/public/{org}", h.HandleListPublicDashboards)
	r.Get("/public/{org}/{id}", h.HandleGetPublicDashboard)

	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/public", nil))
	if rr.Code != http.StatusOK || strings.Contains(rr.Body.String(), "Private") || !strings.Contains(rr.Body.String(), "Public") {
		t.Fatalf("default public list: %d %s", rr.Code, rr.Body.String())
	}
	rr = httptest.NewRecorder()
	r.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/public/default/"+strconv.Itoa(private.ID), nil))
	if rr.Code != http.StatusNotFound {
		t.Fatalf("private dashboard status = %d", rr.Code)
	}

	rr = httptest.NewRecorder()
	r.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/public/default/"+strconv.Itoa(public.ID), nil))
	if rr.Code != http.StatusOK || strings.Contains(rr.Body.String(), "secret-key") || strings.Contains(rr.Body.String(), "showTraffic\\\":true") {
		t.Fatalf("public dashboard response: %d %s", rr.Code, rr.Body.String())
	}
	if rr.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("public response must not be cached")
	}

	public.IsPublic = false
	if err := db.UpdateDashboard(ctx, "default", public.ID, public); err != nil {
		t.Fatal(err)
	}
	rr = httptest.NewRecorder()
	r.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/public/default/"+strconv.Itoa(public.ID), nil))
	if rr.Code != http.StatusNotFound {
		t.Fatalf("revoked dashboard status = %d", rr.Code)
	}
}

func TestPublishingRejectsUnsafeWidgetsAndPermission(t *testing.T) {
	ctx := context.Background()
	db, err := sqlite.NewSQLiteDB(ctx, "file:public-validation-test?mode=memory&cache=shared")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := db.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	router := newDashboardTestRouter(db)
	for _, payload := range []string{
		`{"name":"Unsafe","isPublic":true,"widgets":[{"id":"x","type":"html-widget","config":{}}]}`,
		`{"name":"Restricted","isPublic":true,"permission":"site-a","widgets":[]}`,
	} {
		rr := httptest.NewRecorder()
		router.ServeHTTP(rr, httptest.NewRequest(http.MethodPost, "/dashboards", strings.NewReader(payload)))
		if rr.Code != http.StatusBadRequest {
			t.Fatalf("unexpected publication status %d: %s", rr.Code, rr.Body.String())
		}
	}
}

func TestPublicDashboardRejectsUnsafeWidgets(t *testing.T) {
	for _, kind := range []string{"html-widget", "users-widget", "unknown-plugin"} {
		dashboard := &sqldb.Dashboard{IsPublic: true, Widgets: json.RawMessage(`[{"id":"w","type":"` + kind + `","config":{}}]`)}
		if _, err := sqldb.PublicDashboard(dashboard); err == nil {
			t.Fatalf("%s should be rejected", kind)
		}
	}
}
