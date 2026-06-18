package api

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/xact-iot/xact/events"
	"github.com/xact-iot/xact/sqldb"
	"github.com/xact-iot/xact/sqldb/psql"
)

// LoginRequest represents a login request
type LoginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

type BootstrapAdminStatusResponse struct {
	SetupRequired bool `json:"setupRequired"`
	PasswordSet   bool `json:"passwordSet"`
}

type SetBootstrapAdminPasswordRequest struct {
	Password string `json:"password"`
}

// LoginResponse represents a login response
type LoginResponse struct {
	Token     string `json:"token"`
	TokenType string `json:"token_type"`
	ExpiresIn int    `json:"expires_in"`
	User      User   `json:"user"`
}

// User represents user information returned in auth responses
type User struct {
	ID          string   `json:"id"`
	Username    string   `json:"username"`
	TenantID    string   `json:"tenant_id"`
	Roles       []string `json:"roles"`
	AllowedOrgs []string `json:"allowed_orgs"`
}

type orgSummary struct {
	Name        string `json:"name"`
	DisplayName string `json:"displayName"`
	Logo        string `json:"logo,omitempty"`
	Favicon     string `json:"favicon,omitempty"`
}

type MyOrgsResponse struct {
	Orgs []orgSummary `json:"orgs"`
}

func (s *Server) handleLoginWithSchema() openAPIHandler {
	return handlerWithSchema(s.handleLogin, LoginRequest{}, LoginResponse{}, "auth")
}

// handleLogin validates credentials against the database and issues a JWT.
func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	var req LoginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Username == "" {
		http.Error(w, `{"error":"username and password required"}`, http.StatusBadRequest)
		return
	}

	if s.db == nil {
		http.Error(w, `{"error":"database not available"}`, http.StatusServiceUnavailable)
		return
	}

	// Find user by login name or email
	user, hash, err := s.db.GetUserByLogin(r.Context(), req.Username)
	if err != nil {
		http.Error(w, `{"error":"internal error"}`, http.StatusInternalServerError)
		return
	}
	if user != nil && user.LoginName == "admin" && sqldb.IsBootstrapAdminPasswordUnset(hash) {
		w.WriteHeader(http.StatusForbidden)
		json.NewEncoder(w).Encode(map[string]string{"error": "admin password setup required"})
		return
	}
	// Verify password - prefer the auth plugin if one is loaded.
	var authenticated bool
	if s.authPlugin != nil {
		authenticated = s.authPlugin.Authenticate(req.Username, req.Password)
	} else {
		authenticated = psql.CheckPassword(hash, req.Password)
	}
	if user == nil || !authenticated {
		userID := 0
		if user != nil {
			userID = user.ID
		}
		s.auditSecurityEvent(r.Context(), "default", userID, events.Warn, "auth", "Login failed", map[string]any{
			"username":   req.Username,
			"remoteAddr": r.RemoteAddr,
		})
		w.WriteHeader(http.StatusUnauthorized)
		json.NewEncoder(w).Encode(map[string]string{"error": "invalid credentials"})
		return
	}

	resp, err := s.loginResponseForUser(r.Context(), user)
	if err != nil {
		http.Error(w, `{"error":"failed to generate token"}`, http.StatusInternalServerError)
		return
	}

	// Update last login asynchronously
	go s.db.UpdateLastLogin(r.Context(), user.ID) //nolint:errcheck

	json.NewEncoder(w).Encode(resp)
}

func (s *Server) handleBootstrapAdminStatusWithSchema() openAPIHandler {
	return handlerWithSchema(s.handleBootstrapAdminStatus, nil, BootstrapAdminStatusResponse{}, "auth")
}

func (s *Server) handleBootstrapAdminStatus(w http.ResponseWriter, r *http.Request) {
	if s.db == nil {
		http.Error(w, `{"error":"database not available"}`, http.StatusServiceUnavailable)
		return
	}

	user, hash, err := s.db.GetUserByLogin(r.Context(), "admin")
	if err != nil {
		http.Error(w, `{"error":"internal error"}`, http.StatusInternalServerError)
		return
	}
	setupRequired := user != nil && sqldb.IsBootstrapAdminPasswordUnset(hash)
	json.NewEncoder(w).Encode(BootstrapAdminStatusResponse{
		SetupRequired: setupRequired,
		PasswordSet:   user != nil && !setupRequired,
	})
}

func (s *Server) handleSetBootstrapAdminPasswordWithSchema() openAPIHandler {
	return handlerWithSchema(s.handleSetBootstrapAdminPassword, SetBootstrapAdminPasswordRequest{}, LoginResponse{}, "auth")
}

func (s *Server) handleSetBootstrapAdminPassword(w http.ResponseWriter, r *http.Request) {
	if s.db == nil {
		http.Error(w, `{"error":"database not available"}`, http.StatusServiceUnavailable)
		return
	}

	var req SetBootstrapAdminPasswordRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"invalid request body"}`, http.StatusBadRequest)
		return
	}
	if len(strings.TrimSpace(req.Password)) < 8 {
		http.Error(w, `{"error":"password must be at least 8 characters"}`, http.StatusBadRequest)
		return
	}

	user, hash, err := s.db.GetUserByLogin(r.Context(), "admin")
	if err != nil {
		http.Error(w, `{"error":"internal error"}`, http.StatusInternalServerError)
		return
	}
	if user == nil {
		http.Error(w, `{"error":"admin user not found"}`, http.StatusNotFound)
		return
	}
	if !sqldb.IsBootstrapAdminPasswordUnset(hash) {
		w.WriteHeader(http.StatusConflict)
		json.NewEncoder(w).Encode(map[string]string{"error": "admin password is already set"})
		return
	}

	passwordHash, err := psql.HashPassword(req.Password)
	if err != nil {
		http.Error(w, `{"error":"failed to hash password"}`, http.StatusInternalServerError)
		return
	}
	if err := s.db.SetUserPassword(r.Context(), user.ID, passwordHash); err != nil {
		http.Error(w, `{"error":"failed to set password"}`, http.StatusInternalServerError)
		return
	}

	user, _, err = s.db.GetUserByLogin(r.Context(), "admin")
	if err != nil || user == nil {
		http.Error(w, `{"error":"failed to load admin user"}`, http.StatusInternalServerError)
		return
	}
	resp, err := s.loginResponseForUser(r.Context(), user)
	if err != nil {
		http.Error(w, `{"error":"failed to generate token"}`, http.StatusInternalServerError)
		return
	}
	go s.db.UpdateLastLogin(r.Context(), user.ID) //nolint:errcheck
	json.NewEncoder(w).Encode(resp)
}

func (s *Server) loginResponseForUser(ctx context.Context, user *sqldb.User) (LoginResponse, error) {
	// Check if user is a SystemAdmin in any org
	isSystemAdmin := false
	for _, org := range user.Orgs {
		for _, role := range org.Roles {
			if strings.EqualFold(role, "SystemAdmin") {
				isSystemAdmin = true
				break
			}
		}
		if isSystemAdmin {
			break
		}
	}

	// Build allowed orgs list
	var allowedOrgs []string
	if isSystemAdmin {
		// SystemAdmin has access to all orgs
		allOrgs, err := s.db.ListOrganisations(ctx)
		if err == nil {
			for _, o := range allOrgs {
				allowedOrgs = append(allowedOrgs, o.Name)
			}
		}
	} else {
		for _, org := range user.Orgs {
			allowedOrgs = append(allowedOrgs, org.OrgName)
		}
	}

	// Select preferred org: "default" if available, otherwise first in list
	tenantID, roles := selectOrg("default", user.Orgs)

	tokenString, err := s.buildJWT(strconv.Itoa(user.ID), user.LoginName, tenantID, roles, allowedOrgs, user.TokenVersion)
	if err != nil {
		return LoginResponse{}, err
	}

	return LoginResponse{
		Token:     tokenString,
		TokenType: "Bearer",
		ExpiresIn: int((24 * time.Hour).Seconds()),
		User: User{
			ID:          strconv.Itoa(user.ID),
			Username:    user.LoginName,
			TenantID:    tenantID,
			Roles:       roles,
			AllowedOrgs: allowedOrgs,
		},
	}, nil
}

// handleMyOrgs returns the live list of organisations the current user can access.
// For SystemAdmin users this is all orgs; for others it is their assigned orgs.
// This is intentionally re-queried from the DB rather than read from the JWT so
// that orgs created after login are immediately visible.
func (s *Server) handleMyOrgsWithSchema() openAPIHandler {
	return handlerWithSchema(s.handleMyOrgs, nil, MyOrgsResponse{}, "auth")
}

func (s *Server) handleMyOrgs(w http.ResponseWriter, r *http.Request) {
	claims, ok := GetClaimsFromContext(r.Context())
	if !ok {
		http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
		return
	}
	if s.db == nil {
		http.Error(w, `{"error":"database not available"}`, http.StatusServiceUnavailable)
		return
	}

	userID, err := strconv.Atoi(claims.UserID)
	if err != nil {
		http.Error(w, `{"error":"invalid user"}`, http.StatusInternalServerError)
		return
	}

	// Check if user is SystemAdmin in any of their known orgs
	isSystemAdmin := false
	for _, role := range claims.Roles {
		if role == "SystemAdmin" {
			isSystemAdmin = true
			break
		}
	}
	// Also check the AllowedOrgs from the JWT in case they have SystemAdmin elsewhere
	if !isSystemAdmin {
		orgs, err := s.db.GetUserOrgs(r.Context(), userID)
		if err == nil {
			for _, org := range orgs {
				for _, role := range org.Roles {
					if role == "SystemAdmin" {
						isSystemAdmin = true
						break
					}
				}
			}
		}
	}

	var orgNames []string
	var orgsByName = map[string]sqldb.Organisation{}
	if isSystemAdmin {
		allOrgs, err := s.db.ListOrganisations(r.Context())
		if err != nil {
			http.Error(w, `{"error":"internal error"}`, http.StatusInternalServerError)
			return
		}
		for _, o := range allOrgs {
			orgNames = append(orgNames, o.Name)
			orgsByName[o.Name] = o
		}
	} else {
		orgs, err := s.db.GetUserOrgs(r.Context(), userID)
		if err != nil {
			http.Error(w, `{"error":"internal error"}`, http.StatusInternalServerError)
			return
		}
		for _, o := range orgs {
			orgNames = append(orgNames, o.OrgName)
			if org, err := s.db.GetOrganisation(r.Context(), o.OrgName); err == nil && org != nil {
				orgsByName[o.OrgName] = *org
			}
		}
	}

	if orgNames == nil {
		orgNames = []string{}
	}
	orgs := make([]orgSummary, 0, len(orgNames))
	for _, name := range orgNames {
		org := orgsByName[name]
		orgs = append(orgs, orgSummary{
			Name:        name,
			DisplayName: org.DisplayName,
			Logo:        org.Logo,
			Favicon:     org.Favicon,
		})
	}
	json.NewEncoder(w).Encode(MyOrgsResponse{Orgs: orgs})
}

// SwitchOrgRequest is the body for the switch-org endpoint.
type SwitchOrgRequest struct {
	Org string `json:"org"`
}

func (s *Server) handleSwitchOrgWithSchema() openAPIHandler {
	return handlerWithSchema(s.handleSwitchOrg, SwitchOrgRequest{}, LoginResponse{}, "auth")
}

// handleSwitchOrg issues a new JWT for a different organisation.
func (s *Server) handleSwitchOrg(w http.ResponseWriter, r *http.Request) {
	claims, ok := GetClaimsFromContext(r.Context())
	if !ok {
		http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
		return
	}

	var req SwitchOrgRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Org == "" {
		http.Error(w, `{"error":"org required"}`, http.StatusBadRequest)
		return
	}

	if s.db == nil {
		http.Error(w, `{"error":"database not available"}`, http.StatusServiceUnavailable)
		return
	}

	userID, err := strconv.Atoi(claims.UserID)
	if err != nil {
		http.Error(w, `{"error":"invalid user"}`, http.StatusInternalServerError)
		return
	}

	// Re-query the live allowed-org list from the DB so that orgs created after
	// login are immediately accessible (the JWT's AllowedOrgs can be stale).
	isSystemAdmin := false
	for _, role := range claims.Roles {
		if role == "SystemAdmin" {
			isSystemAdmin = true
			break
		}
	}

	var allowedOrgs []string
	if isSystemAdmin {
		allOrgs, err := s.db.ListOrganisations(r.Context())
		if err != nil {
			http.Error(w, `{"error":"internal error"}`, http.StatusInternalServerError)
			return
		}
		for _, o := range allOrgs {
			allowedOrgs = append(allowedOrgs, o.Name)
		}
	} else {
		userOrgs, err := s.db.GetUserOrgs(r.Context(), userID)
		if err != nil {
			http.Error(w, `{"error":"internal error"}`, http.StatusInternalServerError)
			return
		}
		for _, o := range userOrgs {
			allowedOrgs = append(allowedOrgs, o.OrgName)
		}
	}

	// Validate the requested org against the live list.
	allowed := false
	for _, o := range allowedOrgs {
		if o == req.Org {
			allowed = true
			break
		}
	}
	if !allowed {
		http.Error(w, `{"error":"access denied"}`, http.StatusForbidden)
		return
	}

	// Find roles for the target org. SystemAdmin may not be a direct member,
	// but must retain the SystemAdmin role so permission checks continue to work.
	userOrgs, err := s.db.GetUserOrgs(r.Context(), userID)
	if err != nil {
		http.Error(w, `{"error":"internal error"}`, http.StatusInternalServerError)
		return
	}
	var roles []string
	for _, o := range userOrgs {
		if o.OrgName == req.Org {
			roles = o.Roles
			break
		}
	}
	if isSystemAdmin && len(roles) == 0 {
		roles = []string{"SystemAdmin"}
	}

	active, tokenVersion, err := s.db.GetUserAuthState(r.Context(), userID)
	if err != nil || !active {
		http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
		return
	}

	tokenString, err := s.buildJWT(claims.UserID, claims.Username, req.Org, roles, allowedOrgs, tokenVersion)
	if err != nil {
		http.Error(w, `{"error":"failed to generate token"}`, http.StatusInternalServerError)
		return
	}

	json.NewEncoder(w).Encode(LoginResponse{
		Token:     tokenString,
		TokenType: "Bearer",
		ExpiresIn: int((24 * time.Hour).Seconds()),
		User: User{
			ID:          claims.UserID,
			Username:    claims.Username,
			TenantID:    req.Org,
			Roles:       roles,
			AllowedOrgs: allowedOrgs,
		},
	})
}

// buildJWT creates and signs a JWT with the given claims.
func (s *Server) buildJWT(userID, username, tenantID string, roles, allowedOrgs []string, tokenVersion int) (string, error) {
	claims := JWTClaims{
		UserID:       userID,
		Username:     username,
		TenantID:     tenantID,
		Roles:        roles,
		AllowedOrgs:  allowedOrgs,
		TokenVersion: tokenVersion,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(24 * time.Hour)),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
			Issuer:    "xact",
		},
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString(s.jwtSecret)
}

// selectOrg picks the preferred org from a list: prefers preferredName, falls back to first.
// Returns (tenantID, roles).
func selectOrg(preferredName string, orgs []sqldb.UserOrg) (string, []string) {
	for _, org := range orgs {
		if org.OrgName == preferredName {
			return org.OrgName, org.Roles
		}
	}
	if len(orgs) > 0 {
		return orgs[0].OrgName, orgs[0].Roles
	}
	return preferredName, nil
}
