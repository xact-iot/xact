package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/xact-iot/xact/sqldb"
)

func newAPIRequestWithClaims(method, target string, body []byte, claims *JWTClaims) *http.Request {
	req := httptest.NewRequest(method, target, bytes.NewReader(body))
	if claims != nil {
		req = req.WithContext(context.WithValue(req.Context(), claimsContextKey, claims))
	}
	return req
}

func requestWithRouteParam(req *http.Request, key, value string) *http.Request {
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add(key, value)
	return req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
}

func TestHandleMyOrgs(t *testing.T) {
	db := &testDB{
		userOrgs: []sqldb.UserOrg{{OrgID: 1, OrgName: "default", Roles: []string{"Admin"}}},
		organisations: []sqldb.Organisation{
			{ID: 1, Name: "default", DisplayName: "Default", Logo: "logo"},
			{ID: 2, Name: "ops", DisplayName: "Operations"},
		},
	}
	s := &Server{db: db}

	rr := httptest.NewRecorder()
	s.handleMyOrgs(rr, newAPIRequestWithClaims(http.MethodGet, "/my-orgs", nil, nil))
	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("unauthorized status = %d", rr.Code)
	}

	rr = httptest.NewRecorder()
	s.handleMyOrgs(rr, newAPIRequestWithClaims(http.MethodGet, "/my-orgs", nil, &JWTClaims{UserID: "1", Roles: []string{"Admin"}}))
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d; body=%q", rr.Code, rr.Body.String())
	}
	var body struct {
		Orgs []orgSummary `json:"orgs"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode my orgs: %v", err)
	}
	if len(body.Orgs) != 1 || body.Orgs[0].Name != "default" || body.Orgs[0].DisplayName != "Default" {
		t.Fatalf("unexpected my orgs: %#v", body.Orgs)
	}

	rr = httptest.NewRecorder()
	s.handleMyOrgs(rr, newAPIRequestWithClaims(http.MethodGet, "/my-orgs", nil, &JWTClaims{UserID: "1", Roles: []string{"SystemAdmin"}}))
	if rr.Code != http.StatusOK {
		t.Fatalf("system admin status = %d; body=%q", rr.Code, rr.Body.String())
	}
	body.Orgs = nil
	if err := json.Unmarshal(rr.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode system admin orgs: %v", err)
	}
	if len(body.Orgs) != 2 {
		t.Fatalf("system admin orgs = %#v", body.Orgs)
	}
}

func TestHandleSwitchOrg(t *testing.T) {
	db := &testDB{
		userOrgs:    []sqldb.UserOrg{{OrgID: 1, OrgName: "default", Roles: []string{"Admin"}}, {OrgID: 2, OrgName: "ops", Roles: []string{"Operator"}}},
		authActive:  true,
		authVersion: 9,
	}
	s := &Server{db: db, jwtSecret: []byte("secret")}
	claims := &JWTClaims{UserID: "1", Username: "alice", TenantID: "default", Roles: []string{"Admin"}}

	rr := httptest.NewRecorder()
	s.handleSwitchOrg(rr, newAPIRequestWithClaims(http.MethodPost, "/switch", []byte(`{`), claims))
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("bad body status = %d", rr.Code)
	}

	rr = httptest.NewRecorder()
	s.handleSwitchOrg(rr, newAPIRequestWithClaims(http.MethodPost, "/switch", []byte(`{"org":"other"}`), claims))
	if rr.Code != http.StatusForbidden {
		t.Fatalf("forbidden status = %d; body=%q", rr.Code, rr.Body.String())
	}

	rr = httptest.NewRecorder()
	s.handleSwitchOrg(rr, newAPIRequestWithClaims(http.MethodPost, "/switch", []byte(`{"org":"ops"}`), claims))
	if rr.Code != http.StatusOK {
		t.Fatalf("switch status = %d; body=%q", rr.Code, rr.Body.String())
	}
	var resp LoginResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode switch response: %v", err)
	}
	if resp.Token == "" || resp.User.TenantID != "ops" || len(resp.User.Roles) != 1 || resp.User.Roles[0] != "Operator" {
		t.Fatalf("unexpected switch response: %#v", resp)
	}
}

func TestHandleCommandValidationAndAudit(t *testing.T) {
	s := &Server{}
	claims := &JWTClaims{UserID: "3", TenantID: "default", Roles: []string{"Admin"}, AllowedOrgs: []string{"default"}}

	rr := httptest.NewRecorder()
	s.handleCommand(rr, newAPIRequestWithClaims(http.MethodPost, "/command", []byte(`{}`), claims))
	if rr.Code != http.StatusServiceUnavailable {
		t.Fatalf("nil nats status = %d", rr.Code)
	}

	s.nc = nil
	if got := commandRelativePath("default", "Pump1", "default.Pump1.Status.Run"); got != "Status.Run" {
		t.Fatalf("relative full path = %q", got)
	}
	if got := commandRelativePath("default", "Pump1", "Pump1/Status/Run"); got != "Status.Run" {
		t.Fatalf("relative slash path = %q", got)
	}
	if validCommandSubjectComponent("bad name") || validCommandSubjectComponent("bad*name") {
		t.Fatal("invalid command subject component accepted")
	}
	if newCommandID() == "" {
		t.Fatal("newCommandID returned empty id")
	}

	s.auditCommand(context.Background(), nil, "", "", "", "", "", "")
}

func TestPluginHandlersForMapLayersAndThemes(t *testing.T) {
	root := t.TempDir()
	for _, dir := range []string{"map-layer", "themes"} {
		if err := os.MkdirAll(filepath.Join(root, dir), 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", dir, err)
		}
		if err := os.WriteFile(filepath.Join(root, dir, "alpha.js"), []byte("export default {};"), 0o644); err != nil {
			t.Fatalf("write plugin: %v", err)
		}
		if err := os.WriteFile(filepath.Join(root, dir, "ignore.txt"), []byte("nope"), 0o644); err != nil {
			t.Fatalf("write ignored plugin: %v", err)
		}
	}
	s := &Server{pluginDir: root}

	for name, handle := range map[string]http.HandlerFunc{
		"map-layer": s.handleListMapLayerPlugins,
		"themes":    s.handleListThemePlugins,
	} {
		t.Run("list "+name, func(t *testing.T) {
			rr := httptest.NewRecorder()
			handle(rr, httptest.NewRequest(http.MethodGet, "/plugins", nil))
			if rr.Code != http.StatusOK {
				t.Fatalf("status = %d", rr.Code)
			}
			var plugins []widgetPluginMeta
			if err := json.Unmarshal(rr.Body.Bytes(), &plugins); err != nil {
				t.Fatalf("decode plugins: %v", err)
			}
			if len(plugins) != 1 || plugins[0].Name != "alpha" || !strings.Contains(plugins[0].URL, name) {
				t.Fatalf("plugins = %#v", plugins)
			}
		})
	}

	for name, handle := range map[string]http.HandlerFunc{
		"map-layer": s.handleServeMapLayerPlugin,
		"themes":    s.handleServeThemePlugin,
	} {
		t.Run("serve "+name, func(t *testing.T) {
			req := requestWithRouteParam(httptest.NewRequest(http.MethodGet, "/plugins/alpha.js", nil), "filename", "alpha.js")
			rr := httptest.NewRecorder()
			handle(rr, req)
			if rr.Code != http.StatusOK || !strings.Contains(rr.Body.String(), "export default") {
				t.Fatalf("serve status/body = %d %q", rr.Code, rr.Body.String())
			}
		})
	}
}

func TestNATSBrowserConfigAndTimezone(t *testing.T) {
	s := &Server{}
	s.SetNATSBrowserConfig(NATSBrowserConfig{Username: "u", Password: "p", NATSWSPath: "/ws"})
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/nats", nil)
	req.Host = "example.test:8443"
	s.handleNATSConfig(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d", rr.Code)
	}
	var cfg NATSBrowserConfig
	if err := json.Unmarshal(rr.Body.Bytes(), &cfg); err != nil {
		t.Fatalf("decode nats cfg: %v", err)
	}
	if cfg.NATSWSURL != "" {
		t.Fatalf("NATSWSURL = %q", cfg.NATSWSURL)
	}
	if cfg.NATSWSPath != "/ws" {
		t.Fatalf("NATSWSPath = %q", cfg.NATSWSPath)
	}

	t.Setenv("NATS_WS_PORT", "9443")
	s.config.TLS.Enabled = true
	if got := s.directNATSWebSocketURL(req); got != "wss://example.test:9443" {
		t.Fatalf("direct url = %q", got)
	}

	s.config.TLS.Enabled = false
	req.Header.Set("X-Forwarded-Proto", "https")
	if got := s.directNATSWebSocketURL(req); got != "wss://example.test:9443" {
		t.Fatalf("forwarded direct url = %q", got)
	}

	t.Setenv("TZ", "America/New_York")
	if got := serverTimezone(); got != "America/New_York" {
		t.Fatalf("serverTimezone = %q", got)
	}
}

func TestPermissionMiddlewareAndRespondWithError(t *testing.T) {
	s := &Server{db: &testDB{}}
	claims := &JWTClaims{UserID: "1", TenantID: "default", Roles: []string{"Admin"}}
	req := newAPIRequestWithClaims(http.MethodGet, "/protected", nil, claims)

	if !s.checkUIPermission(req.Context(), "nodes", "read") {
		t.Fatal("expected Admin nodes/read permission")
	}
	if s.checkUIPermission(req.Context(), "nodes", "delete") {
		t.Fatal("unexpected Admin nodes/delete permission")
	}
	if !s.checkUIPermission(req.Context(), "profile", "change") {
		t.Fatal("expected Admin profile/change permission")
	}
	if !s.checkUIPermission(newAPIRequestWithClaims(http.MethodGet, "/", nil, &JWTClaims{TenantID: "default", Roles: []string{"admin"}}).Context(), "nodes", "read") {
		t.Fatal("expected lowercase admin role to match Admin permissions")
	}
	if !s.checkUIPermission(newAPIRequestWithClaims(http.MethodGet, "/", nil, &JWTClaims{Roles: []string{"SystemAdmin"}}).Context(), "anything", "anything") {
		t.Fatal("SystemAdmin should bypass permission checks")
	}
	if !s.checkUIPermission(newAPIRequestWithClaims(http.MethodGet, "/", nil, &JWTClaims{Roles: []string{"systemadmin"}}).Context(), "anything", "anything") {
		t.Fatal("lowercase systemadmin should bypass permission checks")
	}

	nextCalled := false
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		nextCalled = true
		w.WriteHeader(http.StatusNoContent)
	})
	rr := httptest.NewRecorder()
	s.requireAnyUIPermission("nodes", "delete", "read")(next).ServeHTTP(rr, req)
	if rr.Code != http.StatusNoContent || !nextCalled {
		t.Fatalf("allowed middleware status/called = %d/%t", rr.Code, nextCalled)
	}

	rr = httptest.NewRecorder()
	s.requireAnyUIPermission("nodes", "delete", "destroy")(next).ServeHTTP(rr, req)
	if rr.Code != http.StatusForbidden {
		t.Fatalf("forbidden middleware status = %d", rr.Code)
	}

	rr = httptest.NewRecorder()
	respondWithError(rr, "bad token")
	if rr.Code != http.StatusOK || !strings.Contains(rr.Body.String(), "bad token") || rr.Header().Get("Content-Type") != "application/json" {
		t.Fatalf("respondWithError status/header/body = %d %q %q", rr.Code, rr.Header().Get("Content-Type"), rr.Body.String())
	}
}

func TestNATSConfigProxyModeDoesNotInferURL(t *testing.T) {
	s := &Server{config: ServerConfig{StaticServeMode: "proxy"}}
	s.SetNATSBrowserConfig(NATSBrowserConfig{Username: "u"})
	rr := httptest.NewRecorder()
	s.handleNATSConfig(rr, httptest.NewRequest(http.MethodGet, "/nats", nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d", rr.Code)
	}
	var cfg NATSBrowserConfig
	if err := json.Unmarshal(rr.Body.Bytes(), &cfg); err != nil {
		t.Fatalf("decode config: %v", err)
	}
	if cfg.NATSWSURL != "" {
		t.Fatalf("proxy mode inferred URL %q", cfg.NATSWSURL)
	}
}
