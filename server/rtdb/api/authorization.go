package api

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/xact-iot/xact/sqldb"
)

func currentUserID(r *http.Request) (int, bool) {
	claims, ok := GetClaimsFromContext(r.Context())
	if !ok {
		return 0, false
	}
	id, err := strconv.Atoi(claims.UserID)
	if err != nil {
		return 0, false
	}
	return id, true
}

func claimsCanAccessOrg(claims *JWTClaims, org string) bool {
	if claims == nil || strings.TrimSpace(org) == "" {
		return false
	}
	if claimsHasSystemAdmin(claims) {
		return true
	}
	if org == claims.TenantID {
		return true
	}
	for _, allowed := range claims.AllowedOrgs {
		if allowed == org {
			return true
		}
	}
	return false
}

func claimsHasSystemAdmin(claims *JWTClaims) bool {
	if claims == nil {
		return false
	}
	for _, role := range claims.Roles {
		if role == "SystemAdmin" {
			return true
		}
	}
	return false
}

func (s *Server) canAccessOrg(r *http.Request, org string) bool {
	claims, ok := GetClaimsFromContext(r.Context())
	if !ok {
		return false
	}
	return claimsCanAccessOrg(claims, org)
}

func (s *Server) allowedOrgSet(r *http.Request) (map[string]bool, bool) {
	claims, ok := GetClaimsFromContext(r.Context())
	if !ok {
		return nil, false
	}
	if claimsHasSystemAdmin(claims) {
		return nil, true
	}
	allowed := make(map[string]bool, len(claims.AllowedOrgs)+1)
	if claims.TenantID != "" {
		allowed[claims.TenantID] = true
	}
	for _, org := range claims.AllowedOrgs {
		if org != "" {
			allowed[org] = true
		}
	}
	return allowed, false
}

func (s *Server) requireTargetOrgParam(param string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			org := chi.URLParam(r, param)
			if !s.canAccessOrg(r, org) {
				w.WriteHeader(http.StatusForbidden)
				json.NewEncoder(w).Encode(map[string]string{"error": "forbidden"})
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

func (s *Server) requireRequestPathInTenant(r *http.Request, path string) bool {
	claims, ok := GetClaimsFromContext(r.Context())
	if !ok {
		return false
	}
	return claimsPathInTenant(claims, path)
}

func (s *Server) rtdbPathForRequest(r *http.Request, path string, allowRoot bool) (string, bool) {
	claims, ok := GetClaimsFromContext(r.Context())
	if !ok || claims.TenantID == "" {
		return "", false
	}
	return rtdbPathInTenant(claims.TenantID, path, allowRoot)
}

func rtdbPathInTenant(org, path string, allowRoot bool) (string, bool) {
	org = strings.Trim(strings.TrimSpace(org), ". /")
	if org == "" {
		return "", false
	}
	path = cleanRTDBPath(path)
	if path == "" {
		if allowRoot {
			return org, true
		}
		return "", false
	}
	if path == org || strings.HasPrefix(path, org+"/") {
		return path, true
	}
	return org + "/" + path, true
}

func currentOrgFromRequest(r *http.Request) string {
	claims, ok := GetClaimsFromContext(r.Context())
	if !ok {
		return ""
	}
	return claims.TenantID
}

func cleanRTDBPath(path string) string {
	path = strings.Trim(strings.TrimSpace(path), ". /")
	path = strings.ReplaceAll(path, ".", "/")
	parts := strings.Split(path, "/")
	cleaned := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			cleaned = append(cleaned, part)
		}
	}
	return strings.Join(cleaned, "/")
}

func claimsPathInTenant(claims *JWTClaims, path string) bool {
	if claimsHasSystemAdmin(claims) {
		return true
	}
	first := firstPathComponent(path)
	return first != "" && first == claims.TenantID
}

func firstPathComponent(path string) string {
	path = strings.Trim(strings.TrimSpace(path), ". /")
	if path == "" {
		return ""
	}
	path = strings.ReplaceAll(path, "/", ".")
	for _, part := range strings.Split(path, ".") {
		if part != "" {
			return part
		}
	}
	return ""
}

func filterUsersByAllowedOrgs(users []sqldb.User, allowed map[string]bool, systemAdmin bool) []sqldb.User {
	if systemAdmin {
		return users
	}
	filtered := make([]sqldb.User, 0, len(users))
	for _, user := range users {
		orgs := make([]sqldb.UserOrg, 0, len(user.Orgs))
		for _, org := range user.Orgs {
			if allowed[org.OrgName] {
				orgs = append(orgs, org)
			}
		}
		if len(orgs) == 0 {
			continue
		}
		user.Orgs = orgs
		filtered = append(filtered, user)
	}
	return filtered
}
