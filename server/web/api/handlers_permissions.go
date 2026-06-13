package api

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/xact-iot/xact/sqldb"
)

// RolesExtractor extracts the user's roles from a request context.
// This avoids a circular import with the rtdb/api package.
type RolesExtractor func(ctx context.Context) ([]string, bool)

// TenantExtractor extracts the current tenant/org from a request context.
type TenantExtractor func(ctx context.Context) (string, bool)

// PermissionHandlers holds dependencies for permission REST endpoints.
type PermissionHandlers struct {
	DB          sqldb.DB
	GetRoles    RolesExtractor
	getTenantID TenantExtractor
}

// NewPermissionHandlers creates a new PermissionHandlers instance.
func NewPermissionHandlers(database sqldb.DB, getRoles RolesExtractor, getTenantID TenantExtractor) *PermissionHandlers {
	return &PermissionHandlers{DB: database, GetRoles: getRoles, getTenantID: getTenantID}
}

// HandleGetMyPermissions returns merged UI permissions for the current user's roles.
// It reads the user's roles from JWT claims, fetches each role's UI permissions,
// and merges them with OR logic (any true wins).
func (h *PermissionHandlers) HandleGetMyPermissions(w http.ResponseWriter, r *http.Request) {
	roles, ok := h.GetRoles(r.Context())
	if !ok {
		http.Error(w, "missing claims", http.StatusUnauthorized)
		return
	}
	org, ok := h.getTenantID(r.Context())
	if !ok {
		http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
		return
	}

	allPerms, err := h.DB.ListPermissions(r.Context(), org)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	userRoles := make(map[string]bool, len(roles))
	for _, role := range roles {
		userRoles[strings.ToLower(role)] = true
	}

	merged := make(map[string]map[string]bool)
	for _, rp := range allPerms {
		if !userRoles[strings.ToLower(rp.Role)] {
			continue
		}
		var uiMap map[string]map[string]bool
		if err := json.Unmarshal(rp.UI, &uiMap); err != nil {
			continue
		}
		for resource, actions := range uiMap {
			for action, allowed := range actions {
				if allowed {
					if merged[resource] == nil {
						merged[resource] = make(map[string]bool)
					}
					merged[resource][action] = true
				}
			}
		}
	}

	json.NewEncoder(w).Encode(merged)
}

// HandleListRolePermissions returns all role permission records for the admin widget.
func (h *PermissionHandlers) HandleListRolePermissions(w http.ResponseWriter, r *http.Request) {
	org, ok := h.getTenantID(r.Context())
	if !ok {
		http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
		return
	}
	perms, err := h.DB.ListPermissions(r.Context(), org)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if perms == nil {
		perms = []sqldb.RolePermissions{}
	}
	json.NewEncoder(w).Encode(perms)
}

// HandleUpdateRolePermissions updates a specific role's permissions.
func (h *PermissionHandlers) HandleUpdateRolePermissions(w http.ResponseWriter, r *http.Request) {
	org, ok := h.getTenantID(r.Context())
	if !ok {
		http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
		return
	}
	role := chi.URLParam(r, "role")
	if role == "" {
		http.Error(w, "role is required", http.StatusBadRequest)
		return
	}

	var req sqldb.RolePermissions
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	req.Role = role

	if err := h.DB.UpdatePermissions(r.Context(), org, role, &req); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	json.NewEncoder(w).Encode(req)
}
