package api

import (
	"context"
	"encoding/json"
	"github.com/go-chi/chi/v5"
	"github.com/xact-iot/xact/applications"
	"github.com/xact-iot/xact/rtdb/nats"
	"net/http"
	"strconv"
	"strings"
	"time"
)

func (s *Server) applicationManifests() ([]applications.Manifest, error) {
	return applications.Load(s.pluginDir)
}
func (s *Server) handleApplications(w http.ResponseWriter, r *http.Request) {
	all, e := s.applicationManifests()
	if e != nil {
		http.Error(w, "application registration unavailable", 503)
		return
	}
	if all == nil {
		all = []applications.Manifest{}
	}
	for i := range all {
		all[i].Services = nil
	}
	w.Header().Set("Cache-Control", "no-store")
	json.NewEncoder(w).Encode(all)
}

// Reload live organisation roles; application authorization must not trust roles
// captured at login after an administrator has revoked them.
func (s *Server) applicationContext(ctx context.Context, c *JWTClaims) (context.Context, *JWTClaims, bool) {
	if c == nil || s.db == nil || c.TokenType == "agent" {
		return ctx, nil, false
	}
	id, e := strconv.Atoi(c.UserID)
	if e != nil {
		return ctx, nil, false
	}
	user, e := s.db.GetUserByID(ctx, id)
	if e != nil || user == nil || !user.Active {
		return ctx, nil, false
	}
	fresh := *c
	fresh.Roles = nil
	for _, o := range user.Orgs {
		if o.OrgName == c.TenantID {
			fresh.Roles = append(fresh.Roles, o.Roles...)
		}
		if claimsHasSystemAdmin(c) {
			for _, role := range o.Roles {
				if strings.EqualFold(role, "SystemAdmin") {
					fresh.Roles = append(fresh.Roles, "SystemAdmin")
				}
			}
		}
	}
	if len(fresh.Roles) == 0 {
		return ctx, nil, false
	}
	return context.WithValue(ctx, claimsContextKey, &fresh), &fresh, true
}
func (s *Server) handleApplicationSession(w http.ResponseWriter, r *http.Request) {
	c, ok := GetClaimsFromContext(r.Context())
	if !ok {
		unauthorized(w)
		return
	}
	ctx, c, ok := s.applicationContext(r.Context(), c)
	if !ok {
		unauthorized(w)
		return
	}
	all, e := s.applicationManifests()
	if e != nil {
		http.Error(w, "application registration unavailable", 503)
		return
	}
	for _, m := range all {
		if m.ID != chi.URLParam(r, "application") {
			continue
		}
		grants := map[string]bool{}
		for _, p := range m.Resource.Permissions {
			grants[p.Name] = s.checkUIPermission(ctx, m.Resource.ID, p.Name)
		}
		expires := time.Time{}
		if c.ExpiresAt != nil {
			expires = c.ExpiresAt.Time
		}
		w.Header().Set("Cache-Control", "no-store")
		json.NewEncoder(w).Encode(map[string]any{"application_id": m.ID, "user_id": c.UserID, "tenant": c.TenantID, "roles": c.Roles, "resource": m.Resource.ID, "permissions": grants, "widgets": m.Widgets, "expires_at": expires, "inbox_prefix": nats.BrowserInboxPrefix(requestBearer(r))})
		return
	}
	http.NotFound(w, r)
}
func (s *Server) applicationSubjects(ctx context.Context, c *JWTClaims) []string {
	ctx, c, ok := s.applicationContext(ctx, c)
	if !ok {
		return nil
	}
	all, e := s.applicationManifests()
	if e != nil {
		return nil
	}
	out := []string{}
	for _, m := range all {
		allowed := false
		for _, p := range m.Resource.Permissions {
			if s.checkUIPermission(ctx, m.Resource.ID, p.Name) {
				allowed = true
				break
			}
		}
		if allowed {
			for _, op := range m.Operations {
				out = append(out, applications.Subject(c.TenantID, m.ID, "*", op))
			}
		}
	}
	return out
}
