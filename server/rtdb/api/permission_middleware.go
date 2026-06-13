package api

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
)

// requireUIPermission returns a Chi middleware that enforces a ui permission.
// It returns 403 Forbidden with {"error":"forbidden"} if the check fails.
func (s *Server) requireUIPermission(resource, action string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !s.checkUIPermission(r.Context(), resource, action) {
				w.WriteHeader(http.StatusForbidden)
				json.NewEncoder(w).Encode(map[string]string{"error": "forbidden"})
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// requireAnyUIPermission returns a middleware that allows the request when any
// one of the named actions is granted for the resource.
func (s *Server) requireAnyUIPermission(resource string, actions ...string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			for _, action := range actions {
				if s.checkUIPermission(r.Context(), resource, action) {
					next.ServeHTTP(w, r)
					return
				}
			}
			w.WriteHeader(http.StatusForbidden)
			json.NewEncoder(w).Encode(map[string]string{"error": "forbidden"})
		})
	}
}

func (s *Server) requireSystemAdmin() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !isSystemAdmin(r.Context()) {
				w.WriteHeader(http.StatusForbidden)
				json.NewEncoder(w).Encode(map[string]string{"error": "forbidden"})
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

func isSystemAdmin(ctx context.Context) bool {
	claims, ok := GetClaimsFromContext(ctx)
	if !ok {
		return false
	}
	for _, role := range claims.Roles {
		if strings.EqualFold(role, "SystemAdmin") {
			return true
		}
	}
	return false
}

// checkUIPermission performs the actual DB lookup and OR-merges the user's roles.
// SystemAdmin roles bypass the check immediately. Returns false on DB error (fail closed).
func (s *Server) checkUIPermission(ctx context.Context, resource, action string) bool {
	// SystemAdmin bypass - no DB query needed
	if isSystemAdmin(ctx) {
		return true
	}

	claims, ok := GetClaimsFromContext(ctx)
	if !ok {
		return false
	}

	if s.db == nil {
		return false
	}

	allPerms, err := s.db.ListPermissions(ctx, claims.TenantID)
	if err != nil {
		return false // fail closed
	}

	userRoles := make(map[string]bool, len(claims.Roles))
	for _, role := range claims.Roles {
		userRoles[strings.ToLower(role)] = true
	}

	for _, rp := range allPerms {
		if !userRoles[strings.ToLower(rp.Role)] {
			continue
		}
		var uiMap map[string]map[string]bool
		if err := json.Unmarshal(rp.UI, &uiMap); err != nil {
			continue
		}
		if uiMap[resource][action] {
			return true
		}
	}
	return false
}
