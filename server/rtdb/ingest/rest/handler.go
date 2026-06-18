// Package rest provides the HTTP handler for the REST device data ingest API.
package rest

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	natsgo "github.com/nats-io/nats.go"
	"github.com/xact-iot/xact/openapischema"
	"github.com/xact-iot/xact/rtdb/ingest"
	"github.com/xact-iot/xact/sqldb"
)

// Handler handles POST /api/v1/ingest/{tenant}[/zone/{zone}]/{devicetype}/{devicename}.
// Zone is optional; when absent the path is {tenant}/{devicetype}/{devicename}.
// Authentication is performed via an API key in the Authorization header:
//
//	Authorization: ApiKey <key>
//
// The key must belong to the organisation identified by {tenant}.
type Handler struct {
	nc          *natsgo.Conn
	db          sqldb.DB
	apiKeyCache *apiKeyCache
	CurrentOrg  func(r *http.Request) string
	Audit       func(r *http.Request, orgName, action string, params map[string]any)
}

// New creates a new ingest Handler.
func New(nc *natsgo.Conn, db sqldb.DB) *Handler {
	return &Handler{nc: nc, db: db, apiKeyCache: newAPIKeyCache(apiKeyCacheTTLFromEnv())}
}

func (h *Handler) HandleIngestWithSchema() openapischema.Handler {
	return ingestSchemaHandler(h.HandleIngest)
}

// HandleIngest processes an incoming device data request (zoneless variant).
func (h *Handler) HandleIngest(w http.ResponseWriter, r *http.Request) {
	h.handleIngest(w, r, "")
}

func (h *Handler) HandleIngestWithZoneWithSchema() openapischema.Handler {
	return ingestSchemaHandler(h.HandleIngestWithZone)
}

// HandleIngestWithZone processes an incoming device data request (zoned variant).
func (h *Handler) HandleIngestWithZone(w http.ResponseWriter, r *http.Request) {
	zone := chi.URLParam(r, "zone")
	h.handleIngest(w, r, zone)
}

func ingestSchemaHandler(handler http.HandlerFunc) openapischema.Handler {
	return openapischema.Handler{
		Handler: handler,
		RequestBody: map[string]any{
			"required": true,
			"content": map[string]any{
				"application/json": map[string]any{
					"schema": map[string]any{
						"type":                 "object",
						"additionalProperties": true,
					},
				},
			},
		},
		Responses: openapischema.ResponseSchemas(map[int]any{http.StatusNoContent: nil}),
		Tags:      []string{"ingest"},
	}
}

func (h *Handler) handleIngest(w http.ResponseWriter, r *http.Request, zone string) {
	tenant := chi.URLParam(r, "tenant")
	deviceType := chi.URLParam(r, "devicetype")
	deviceName := chi.URLParam(r, "devicename")

	if tenant == "" || deviceType == "" || deviceName == "" {
		http.Error(w, "tenant, devicetype, and devicename are required", http.StatusBadRequest)
		return
	}

	// Authenticate via API key and verify it belongs to the requested tenant.
	orgName, err := h.resolveAPIKey(r)
	if err != nil || orgName == "" {
		fmt.Printf("api invalid or missing API key\n")
		http.Error(w, "invalid or missing API key", http.StatusUnauthorized)
		return
	}
	if orgName != tenant {
		fmt.Printf("api for another org\n")
		http.Error(w, "API key does not belong to the requested organisation", http.StatusForbidden)
		return
	}

	// Read and parse the payload (1 MB limit).
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 1<<20))
	if err != nil {
		http.Error(w, "failed to read request body", http.StatusBadRequest)
		return
	}

	tagData, err := ingest.ParsePayload(body)
	if err != nil {
		http.Error(w, "invalid payload: "+err.Error(), http.StatusBadRequest)
		return
	}

	if err := ingest.PublishIngest(h.nc, tenant, zone, deviceType, deviceName, tagData); err != nil {
		if errors.Is(err, ingest.ErrIngestBusy) {
			http.Error(w, "ingest queue full", http.StatusServiceUnavailable)
			return
		}
		http.Error(w, "failed to write data: "+err.Error(), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// resolveAPIKey extracts the key from the Authorization header and returns
// the owning organisation name, or ("", nil) when no header is present.
func (h *Handler) resolveAPIKey(r *http.Request) (string, error) {
	authHeader := r.Header.Get("Authorization")
	if authHeader == "" {
		return "", nil
	}
	key := strings.TrimSpace(strings.TrimPrefix(authHeader, "ApiKey "))
	if key == "" {
		return "", nil
	}
	if orgName, ok := h.apiKeyCache.Get(key); ok {
		return orgName, nil
	}
	orgName, err := h.db.GetAPIKeyOrg(r.Context(), key)
	if err != nil {
		return "", err
	}
	if orgName != "" {
		h.apiKeyCache.Set(key, orgName)
	}
	return orgName, nil
}

// ── API key management handlers ───────────────────────────────────────────────
// These are mounted under /api/v1/api-keys and use the current JWT org.

type createAPIKeyRequest struct {
	Name string `json:"name"`
}

func (h *Handler) HandleListAPIKeysWithSchema() openapischema.Handler {
	return openapischema.WithSchema(h.HandleListAPIKeys, nil, []sqldb.APIKey{}, "api-keys")
}

// HandleListAPIKeys returns all API keys for an organisation.
func (h *Handler) HandleListAPIKeys(w http.ResponseWriter, r *http.Request) {
	orgName := h.requestOrg(r)
	if orgName == "" {
		http.Error(w, "organisation not found in token", http.StatusUnauthorized)
		return
	}
	keys, err := h.db.ListAPIKeys(r.Context(), orgName)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if keys == nil {
		keys = []sqldb.APIKey{}
	}
	for i := range keys {
		keys[i].Key = maskAPIKey(keys[i].Key)
	}
	json.NewEncoder(w).Encode(keys)
}

func (h *Handler) HandleCreateAPIKeyWithSchema() openapischema.Handler {
	return openapischema.Handler{
		Handler:     h.HandleCreateAPIKey,
		RequestBody: openapischema.JSONRequestBody(createAPIKeyRequest{}),
		Responses:   openapischema.ResponseSchema(http.StatusCreated, sqldb.APIKey{}),
		Tags:        []string{"api-keys"},
	}
}

// HandleCreateAPIKey generates a new API key for an organisation.
// The raw key value is only returned in this response.
func (h *Handler) HandleCreateAPIKey(w http.ResponseWriter, r *http.Request) {
	orgName := h.requestOrg(r)
	if orgName == "" {
		http.Error(w, "organisation not found in token", http.StatusUnauthorized)
		return
	}

	var req createAPIKeyRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	if req.Name == "" {
		http.Error(w, "name is required", http.StatusBadRequest)
		return
	}

	key, err := h.db.CreateAPIKey(r.Context(), orgName, req.Name)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	h.apiKeyCache.Clear()
	h.audit(r, orgName, "API key created", map[string]any{
		"id":      key.ID,
		"name":    key.Name,
		"prefix":  key.KeyPrefix,
		"last4":   key.KeyLast4,
		"created": key.CreatedAt,
	})

	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(key)
}

func (h *Handler) HandleDeleteAPIKeyWithSchema() openapischema.Handler {
	return openapischema.WithResponses(h.HandleDeleteAPIKey, map[int]any{http.StatusNoContent: nil}, "api-keys")
}

// HandleDeleteAPIKey removes an API key by numeric ID.
func (h *Handler) HandleDeleteAPIKey(w http.ResponseWriter, r *http.Request) {
	orgName := h.requestOrg(r)
	if orgName == "" {
		http.Error(w, "organisation not found in token", http.StatusUnauthorized)
		return
	}

	var id int
	if n, err := fmt.Sscan(chi.URLParam(r, "id"), &id); err != nil || n == 0 || id <= 0 {
		http.Error(w, "invalid key id", http.StatusBadRequest)
		return
	}

	if err := h.db.DeleteAPIKey(r.Context(), orgName, id); err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	h.apiKeyCache.Clear()
	h.audit(r, orgName, "API key deleted", map[string]any{"id": id})
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) requestOrg(r *http.Request) string {
	if h.CurrentOrg != nil {
		if org := strings.TrimSpace(h.CurrentOrg(r)); org != "" {
			return org
		}
	}
	return strings.TrimSpace(chi.URLParam(r, "name"))
}

func (h *Handler) audit(r *http.Request, orgName, action string, params map[string]any) {
	if h.Audit != nil {
		h.Audit(r, orgName, action, params)
	}
}

const defaultAPIKeyCacheTTL = 30 * time.Second

type apiKeyCache struct {
	mu      sync.RWMutex
	ttl     time.Duration
	entries map[string]apiKeyCacheEntry
}

type apiKeyCacheEntry struct {
	orgName   string
	expiresAt time.Time
}

func newAPIKeyCache(ttl time.Duration) *apiKeyCache {
	return &apiKeyCache{
		ttl:     ttl,
		entries: make(map[string]apiKeyCacheEntry),
	}
}

func (c *apiKeyCache) Get(key string) (string, bool) {
	if c == nil || c.ttl <= 0 {
		return "", false
	}
	now := time.Now()
	c.mu.RLock()
	entry, ok := c.entries[key]
	c.mu.RUnlock()
	if !ok {
		return "", false
	}
	if !now.Before(entry.expiresAt) {
		c.mu.Lock()
		if current, ok := c.entries[key]; ok && !now.Before(current.expiresAt) {
			delete(c.entries, key)
		}
		c.mu.Unlock()
		return "", false
	}
	return entry.orgName, true
}

func (c *apiKeyCache) Set(key, orgName string) {
	if c == nil || c.ttl <= 0 || key == "" || orgName == "" {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.entries[key] = apiKeyCacheEntry{
		orgName:   orgName,
		expiresAt: time.Now().Add(c.ttl),
	}
}

func (c *apiKeyCache) Clear() {
	if c == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.entries = make(map[string]apiKeyCacheEntry)
}

func apiKeyCacheTTLFromEnv() time.Duration {
	raw := strings.TrimSpace(os.Getenv("REST_API_KEY_CACHE_TTL"))
	if raw == "" {
		return defaultAPIKeyCacheTTL
	}
	ttl, err := time.ParseDuration(raw)
	if err != nil || ttl < 0 {
		return defaultAPIKeyCacheTTL
	}
	return ttl
}

func maskAPIKey(key string) string {
	key = strings.TrimSpace(key)
	if strings.Contains(key, "...") {
		return key
	}
	if len(key) < 16 {
		return "redacted"
	}
	return key[:8] + "..." + key[len(key)-4:]
}
