package rest

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/xact-iot/xact/rtdb/tree"
	"github.com/xact-iot/xact/sqldb"
	"github.com/xact-iot/xact/sqldb/sqlite"
)

func newTestHandler(t *testing.T) (*Handler, sqldb.DB, *tree.TreeWithOperations) {
	t.Helper()
	ctx := context.Background()
	db, err := sqlite.NewSQLiteDB(ctx, filepath.Join(t.TempDir(), "xact.db"))
	if err != nil {
		t.Fatalf("NewSQLiteDB: %v", err)
	}
	t.Cleanup(db.Close)
	if err := db.Migrate(ctx); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	treeOps := tree.NewTreeWithOperations(nil)
	h := New(nil, db, treeOps)
	h.CurrentOrg = func(*http.Request) string { return "default" }
	return h, db, treeOps
}

func requestWithParams(method, target string, body []byte, params map[string]string) *http.Request {
	req := httptest.NewRequest(method, target, bytes.NewReader(body))
	rctx := chi.NewRouteContext()
	for key, value := range params {
		rctx.URLParams.Add(key, value)
	}
	return req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
}

func TestAPIKeyCacheHitAndClear(t *testing.T) {
	cache := newAPIKeyCache(time.Minute)
	cache.Set("key-1", "default")

	org, ok := cache.Get("key-1")
	if !ok {
		t.Fatal("expected cached key")
	}
	if org != "default" {
		t.Fatalf("org = %q, want default", org)
	}

	cache.Clear()
	if _, ok := cache.Get("key-1"); ok {
		t.Fatal("expected cache miss after clear")
	}
}

func TestAPIKeyCacheExpiry(t *testing.T) {
	cache := newAPIKeyCache(5 * time.Millisecond)
	cache.Set("key-1", "default")
	time.Sleep(10 * time.Millisecond)

	if _, ok := cache.Get("key-1"); ok {
		t.Fatal("expected expired cache entry to miss")
	}
}

func TestAPIKeyCacheDisabled(t *testing.T) {
	cache := newAPIKeyCache(0)
	cache.Set("key-1", "default")

	if _, ok := cache.Get("key-1"); ok {
		t.Fatal("expected disabled cache to miss")
	}
}

func TestAPIKeyCacheTTLFromEnv(t *testing.T) {
	t.Setenv("REST_API_KEY_CACHE_TTL", "250ms")
	if got := apiKeyCacheTTLFromEnv(); got != 250*time.Millisecond {
		t.Fatalf("ttl = %s, want 250ms", got)
	}

	t.Setenv("REST_API_KEY_CACHE_TTL", "-1s")
	if got := apiKeyCacheTTLFromEnv(); got != defaultAPIKeyCacheTTL {
		t.Fatalf("negative ttl = %s, want default %s", got, defaultAPIKeyCacheTTL)
	}

	t.Setenv("REST_API_KEY_CACHE_TTL", "nope")
	if got := apiKeyCacheTTLFromEnv(); got != defaultAPIKeyCacheTTL {
		t.Fatalf("invalid ttl = %s, want default %s", got, defaultAPIKeyCacheTTL)
	}
}

func TestMaskAPIKey(t *testing.T) {
	raw := "0123456789abcdef0123456789abcdef"
	if got := maskAPIKey(raw); got != "01234567...cdef" {
		t.Fatalf("maskAPIKey() = %q", got)
	}
	if got := maskAPIKey("short"); got != "redacted" {
		t.Fatalf("short maskAPIKey() = %q", got)
	}
}

func TestHandleIngestValidationAndAuth(t *testing.T) {
	h, db, _ := newTestHandler(t)
	key, err := db.CreateAPIKey(context.Background(), "default", "ingest")
	if err != nil {
		t.Fatalf("CreateAPIKey: %v", err)
	}

	tests := []struct {
		name       string
		params     map[string]string
		auth       string
		body       []byte
		wantStatus int
	}{
		{
			name:       "missing route params",
			body:       []byte(`{}`),
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "missing api key",
			params:     map[string]string{"tenant": "default", "devicetype": "pump", "devicename": "p1"},
			body:       []byte(`{}`),
			wantStatus: http.StatusUnauthorized,
		},
		{
			name:       "api key for different tenant",
			params:     map[string]string{"tenant": "other", "devicetype": "pump", "devicename": "p1"},
			auth:       "ApiKey " + key.Key,
			body:       []byte(`{}`),
			wantStatus: http.StatusForbidden,
		},
		{
			name:       "invalid json body",
			params:     map[string]string{"tenant": "default", "devicetype": "pump", "devicename": "p1"},
			auth:       "ApiKey " + key.Key,
			body:       []byte(`{`),
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "publish failure after valid request",
			params:     map[string]string{"tenant": "default", "devicetype": "pump", "devicename": "p1"},
			auth:       "ApiKey " + key.Key,
			body:       []byte(`{"temp": 42, "ts": 1700000000000}`),
			wantStatus: http.StatusInternalServerError,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := requestWithParams(http.MethodPost, "/ingest", tt.body, tt.params)
			if tt.auth != "" {
				req.Header.Set("Authorization", tt.auth)
			}
			rr := httptest.NewRecorder()
			h.HandleIngest(rr, req)
			if rr.Code != tt.wantStatus {
				t.Fatalf("status = %d, want %d; body=%q", rr.Code, tt.wantStatus, rr.Body.String())
			}
		})
	}
}

func TestHandleIngestWithZoneAndCachedAPIKey(t *testing.T) {
	h, _, _ := newTestHandler(t)
	h.apiKeyCache.Set("cached-key", "default")
	req := requestWithParams(http.MethodPost, "/ingest", []byte(`{"temp": 42}`), map[string]string{
		"tenant": "default", "zone": "north", "devicetype": "pump", "devicename": "p1",
	})
	req.Header.Set("Authorization", "ApiKey cached-key")
	rr := httptest.NewRecorder()

	h.HandleIngestWithZone(rr, req)
	if rr.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d; body=%q", rr.Code, http.StatusInternalServerError, rr.Body.String())
	}
}

func TestHandleDeleteDeviceWithAPIKey(t *testing.T) {
	h, db, treeOps := newTestHandler(t)
	key, err := db.CreateAPIKey(context.Background(), "default", "ingest")
	if err != nil {
		t.Fatalf("CreateAPIKey: %v", err)
	}
	if err := treeOps.CreateNode("default/pump/p1", ""); err != nil {
		t.Fatalf("CreateNode: %v", err)
	}

	req := requestWithParams(http.MethodDelete, "/ingest", nil, map[string]string{
		"tenant": "default", "devicetype": "pump", "devicename": "p1",
	})
	req.Header.Set("Authorization", "ApiKey "+key.Key)
	rr := httptest.NewRecorder()

	h.HandleDeleteDevice(rr, req)
	if rr.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want %d; body=%q", rr.Code, http.StatusNoContent, rr.Body.String())
	}
	if _, err := treeOps.FindNode("default.pump.p1"); err == nil {
		t.Fatal("device still exists after delete")
	}
}

func TestHandleDeleteDeviceWithZoneAndCachedAPIKey(t *testing.T) {
	h, _, treeOps := newTestHandler(t)
	h.apiKeyCache.Set("cached-key", "default")
	if err := treeOps.CreateNode("default/north/pump/p1", ""); err != nil {
		t.Fatalf("CreateNode: %v", err)
	}

	req := requestWithParams(http.MethodDelete, "/ingest", nil, map[string]string{
		"tenant": "default", "zone": "north", "devicetype": "pump", "devicename": "p1",
	})
	req.Header.Set("Authorization", "ApiKey cached-key")
	rr := httptest.NewRecorder()

	h.HandleDeleteDeviceWithZone(rr, req)
	if rr.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want %d; body=%q", rr.Code, http.StatusNoContent, rr.Body.String())
	}
	if _, err := treeOps.FindNode("default.north.pump.p1"); err == nil {
		t.Fatal("zoned device still exists after delete")
	}
}

func TestHandleDeleteDeviceValidationAndAuth(t *testing.T) {
	h, db, _ := newTestHandler(t)
	key, err := db.CreateAPIKey(context.Background(), "default", "ingest")
	if err != nil {
		t.Fatalf("CreateAPIKey: %v", err)
	}

	tests := []struct {
		name       string
		params     map[string]string
		auth       string
		wantStatus int
	}{
		{
			name:       "missing route params",
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "missing api key",
			params:     map[string]string{"tenant": "default", "devicetype": "pump", "devicename": "p1"},
			wantStatus: http.StatusUnauthorized,
		},
		{
			name:       "api key for different tenant",
			params:     map[string]string{"tenant": "other", "devicetype": "pump", "devicename": "p1"},
			auth:       "ApiKey " + key.Key,
			wantStatus: http.StatusForbidden,
		},
		{
			name:       "missing device",
			params:     map[string]string{"tenant": "default", "devicetype": "pump", "devicename": "p1"},
			auth:       "ApiKey " + key.Key,
			wantStatus: http.StatusInternalServerError,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := requestWithParams(http.MethodDelete, "/ingest", nil, tt.params)
			if tt.auth != "" {
				req.Header.Set("Authorization", tt.auth)
			}
			rr := httptest.NewRecorder()
			h.HandleDeleteDevice(rr, req)
			if rr.Code != tt.wantStatus {
				t.Fatalf("status = %d, want %d; body=%q", rr.Code, tt.wantStatus, rr.Body.String())
			}
		})
	}
}

func TestAPIKeyManagementHandlers(t *testing.T) {
	h, _, _ := newTestHandler(t)
	var audits []map[string]any
	h.Audit = func(_ *http.Request, orgName, action string, params map[string]any) {
		audits = append(audits, map[string]any{"org": orgName, "action": action, "params": params})
	}

	rr := httptest.NewRecorder()
	h.HandleCreateAPIKey(rr, httptest.NewRequest(http.MethodPost, "/keys", bytes.NewBufferString(`{`)))
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("invalid create status = %d", rr.Code)
	}

	rr = httptest.NewRecorder()
	h.HandleCreateAPIKey(rr, httptest.NewRequest(http.MethodPost, "/keys", bytes.NewBufferString(`{"name":""}`)))
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("missing name status = %d", rr.Code)
	}

	rr = httptest.NewRecorder()
	h.HandleCreateAPIKey(rr, httptest.NewRequest(http.MethodPost, "/keys", bytes.NewBufferString(`{"name":"rest"}`)))
	if rr.Code != http.StatusCreated {
		t.Fatalf("create status = %d; body=%q", rr.Code, rr.Body.String())
	}
	var created sqldb.APIKey
	if err := json.Unmarshal(rr.Body.Bytes(), &created); err != nil {
		t.Fatalf("decode created key: %v", err)
	}
	if created.ID == 0 || created.Key == "" || len(audits) != 1 {
		t.Fatalf("unexpected created key/audits: %#v %#v", created, audits)
	}

	rr = httptest.NewRecorder()
	h.HandleListAPIKeys(rr, httptest.NewRequest(http.MethodGet, "/keys", nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("list status = %d; body=%q", rr.Code, rr.Body.String())
	}
	var keys []sqldb.APIKey
	if err := json.Unmarshal(rr.Body.Bytes(), &keys); err != nil {
		t.Fatalf("decode keys: %v", err)
	}
	if len(keys) != 1 || keys[0].Key == created.Key {
		t.Fatalf("expected masked listed key, got %#v", keys)
	}

	rr = httptest.NewRecorder()
	h.HandleDeleteAPIKey(rr, requestWithParams(http.MethodDelete, "/keys/nope", nil, map[string]string{"id": "nope"}))
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("invalid delete status = %d", rr.Code)
	}

	rr = httptest.NewRecorder()
	h.HandleDeleteAPIKey(rr, requestWithParams(http.MethodDelete, "/keys/999", nil, map[string]string{"id": "999"}))
	if rr.Code != http.StatusNotFound {
		t.Fatalf("missing delete status = %d", rr.Code)
	}

	rr = httptest.NewRecorder()
	h.HandleDeleteAPIKey(rr, requestWithParams(http.MethodDelete, "/keys/1", nil, map[string]string{"id": "1"}))
	if rr.Code != http.StatusNoContent {
		t.Fatalf("delete status = %d; body=%q", rr.Code, rr.Body.String())
	}
	if len(audits) != 2 || audits[1]["action"] != "API key deleted" {
		t.Fatalf("unexpected audits: %#v", audits)
	}
}

func TestAPIKeyHandlersRequireOrg(t *testing.T) {
	h, _, _ := newTestHandler(t)
	h.CurrentOrg = func(*http.Request) string { return "" }
	req := httptest.NewRequest(http.MethodGet, "/keys", nil)

	for name, handle := range map[string]func(http.ResponseWriter, *http.Request){
		"list":   h.HandleListAPIKeys,
		"create": h.HandleCreateAPIKey,
		"delete": h.HandleDeleteAPIKey,
	} {
		t.Run(name, func(t *testing.T) {
			rr := httptest.NewRecorder()
			handle(rr, req)
			if rr.Code != http.StatusUnauthorized {
				t.Fatalf("status = %d, want %d", rr.Code, http.StatusUnauthorized)
			}
		})
	}
}
