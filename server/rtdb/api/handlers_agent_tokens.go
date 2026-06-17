package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/xact-iot/xact/sqldb"
)

type agentTokenStore interface {
	ListAgentTokens(ctx context.Context, orgName string, userID int, includeAll bool) ([]sqldb.AgentToken, error)
	CreateAgentToken(ctx context.Context, orgName string, userID int, name string, roles []string, expiresAt *time.Time) (*sqldb.AgentToken, error)
	GetAgentToken(ctx context.Context, orgName string, id int, userID int, includeAll bool) (*sqldb.AgentToken, error)
	DeleteAgentToken(ctx context.Context, orgName string, id int, userID int, includeAll bool) error
}

func (s *Server) agentTokenStore() (agentTokenStore, bool) {
	store, ok := s.db.(agentTokenStore)
	return store, ok
}

func (s *Server) handleListAgentTokens(w http.ResponseWriter, r *http.Request) {
	store, ok := s.agentTokenStore()
	if !ok {
		http.Error(w, `{"error":"agent tokens are not supported by this database"}`, http.StatusNotImplemented)
		return
	}
	orgName := currentOrgFromContext(r)
	if orgName == "" {
		http.Error(w, `{"error":"organisation not found in token"}`, http.StatusUnauthorized)
		return
	}
	userID := currentUserIDFromContext(r)
	includeAll := s.checkUIPermission(r.Context(), "agentkeys", "manage")
	if !includeAll && !s.checkUIPermission(r.Context(), "agentkeys", "personal") && !s.checkUIPermission(r.Context(), "agentkeys", "access") {
		http.Error(w, `{"error":"insufficient permissions"}`, http.StatusForbidden)
		return
	}
	tokens, err := store.ListAgentTokens(r.Context(), orgName, userID, includeAll)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if tokens == nil {
		tokens = []sqldb.AgentToken{}
	}
	_ = json.NewEncoder(w).Encode(tokens)
}

func (s *Server) handleCreateAgentToken(w http.ResponseWriter, r *http.Request) {
	store, ok := s.agentTokenStore()
	if !ok {
		http.Error(w, `{"error":"agent tokens are not supported by this database"}`, http.StatusNotImplemented)
		return
	}
	orgName := currentOrgFromContext(r)
	if orgName == "" {
		http.Error(w, `{"error":"organisation not found in token"}`, http.StatusUnauthorized)
		return
	}
	var req struct {
		Name      string `json:"name"`
		UserID    int    `json:"userId"`
		ExpiresAt string `json:"expiresAt"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"invalid request body"}`, http.StatusBadRequest)
		return
	}
	req.Name = strings.TrimSpace(req.Name)
	if req.Name == "" {
		http.Error(w, `{"error":"name is required"}`, http.StatusBadRequest)
		return
	}
	callerID := currentUserIDFromContext(r)
	canManage := s.checkUIPermission(r.Context(), "agentkeys", "manage")
	if !canManage && !s.checkUIPermission(r.Context(), "agentkeys", "personal") {
		http.Error(w, `{"error":"insufficient permissions"}`, http.StatusForbidden)
		return
	}
	targetUserID := req.UserID
	if targetUserID <= 0 {
		targetUserID = callerID
	}
	if !canManage && targetUserID != callerID {
		http.Error(w, `{"error":"cannot create tokens for another user"}`, http.StatusForbidden)
		return
	}
	roles, err := s.agentTokenRolesForUser(r.Context(), orgName, targetUserID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	var expiresAt *time.Time
	if strings.TrimSpace(req.ExpiresAt) != "" {
		t, err := time.Parse(time.RFC3339, strings.TrimSpace(req.ExpiresAt))
		if err != nil {
			http.Error(w, `{"error":"expiresAt must be RFC3339"}`, http.StatusBadRequest)
			return
		}
		expiresAt = &t
	}
	token, err := store.CreateAgentToken(r.Context(), orgName, targetUserID, req.Name, roles, expiresAt)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(token)
}

func (s *Server) handleListAgentTokenUsers(w http.ResponseWriter, r *http.Request) {
	orgName := currentOrgFromContext(r)
	if orgName == "" {
		http.Error(w, `{"error":"organisation not found in token"}`, http.StatusUnauthorized)
		return
	}
	users, err := s.db.ListUsers(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	type tokenUser struct {
		ID          int      `json:"id"`
		LoginName   string   `json:"loginName"`
		DisplayName string   `json:"displayName"`
		Roles       []string `json:"roles"`
	}
	out := []tokenUser{}
	for _, user := range users {
		if !user.Active {
			continue
		}
		for _, org := range user.Orgs {
			if org.OrgName != orgName {
				continue
			}
			roles := []string{}
			for _, role := range org.Roles {
				role = strings.TrimSpace(role)
				if role == "" {
					continue
				}
				roles = append(roles, role)
			}
			if len(roles) == 0 {
				continue
			}
			displayName := strings.TrimSpace(user.FirstName + " " + user.LastName)
			if displayName == "" {
				displayName = user.LoginName
			}
			out = append(out, tokenUser{
				ID:          user.ID,
				LoginName:   user.LoginName,
				DisplayName: displayName,
				Roles:       roles,
			})
			break
		}
	}
	_ = json.NewEncoder(w).Encode(out)
}

func (s *Server) handleGetAgentToken(w http.ResponseWriter, r *http.Request) {
	store, ok := s.agentTokenStore()
	if !ok {
		http.Error(w, `{"error":"agent tokens are not supported by this database"}`, http.StatusNotImplemented)
		return
	}
	orgName := currentOrgFromContext(r)
	if orgName == "" {
		http.Error(w, `{"error":"organisation not found in token"}`, http.StatusUnauthorized)
		return
	}
	id, err := strconv.Atoi(chi.URLParam(r, "id"))
	if err != nil || id <= 0 {
		http.Error(w, `{"error":"invalid token id"}`, http.StatusBadRequest)
		return
	}
	userID := currentUserIDFromContext(r)
	includeAll := s.checkUIPermission(r.Context(), "agentkeys", "manage")
	if !includeAll && !s.checkUIPermission(r.Context(), "agentkeys", "access") {
		http.Error(w, `{"error":"insufficient permissions"}`, http.StatusForbidden)
		return
	}
	token, err := store.GetAgentToken(r.Context(), orgName, id, userID, includeAll)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if token == nil {
		http.Error(w, `{"error":"agent token not found"}`, http.StatusNotFound)
		return
	}
	_ = json.NewEncoder(w).Encode(token)
}

func (s *Server) handleDeleteAgentToken(w http.ResponseWriter, r *http.Request) {
	store, ok := s.agentTokenStore()
	if !ok {
		http.Error(w, `{"error":"agent tokens are not supported by this database"}`, http.StatusNotImplemented)
		return
	}
	orgName := currentOrgFromContext(r)
	if orgName == "" {
		http.Error(w, `{"error":"organisation not found in token"}`, http.StatusUnauthorized)
		return
	}
	id, err := strconv.Atoi(chi.URLParam(r, "id"))
	if err != nil || id <= 0 {
		http.Error(w, `{"error":"invalid token id"}`, http.StatusBadRequest)
		return
	}
	userID := currentUserIDFromContext(r)
	includeAll := s.checkUIPermission(r.Context(), "agentkeys", "manage")
	if !includeAll && !s.checkUIPermission(r.Context(), "agentkeys", "personal") {
		http.Error(w, `{"error":"insufficient permissions"}`, http.StatusForbidden)
		return
	}
	if err := store.DeleteAgentToken(r.Context(), orgName, id, userID, includeAll); err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func currentOrgFromContext(r *http.Request) string {
	claims, ok := GetClaimsFromContext(r.Context())
	if !ok {
		return ""
	}
	return strings.TrimSpace(claims.TenantID)
}

func currentUserIDFromContext(r *http.Request) int {
	claims, ok := GetClaimsFromContext(r.Context())
	if !ok {
		return 0
	}
	id, _ := strconv.Atoi(strings.TrimSpace(claims.UserID))
	return id
}

func (s *Server) agentTokenRolesForUser(ctx context.Context, orgName string, userID int) ([]string, error) {
	if userID <= 0 {
		return nil, fmt.Errorf("user id not found in token")
	}
	user, err := s.db.GetUserByID(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("loading user: %w", err)
	}
	if user == nil || !user.Active {
		return nil, fmt.Errorf("user not found or inactive")
	}
	for _, org := range user.Orgs {
		if org.OrgName != orgName {
			continue
		}
		roles := make([]string, 0, len(org.Roles))
		for _, role := range org.Roles {
			role = strings.TrimSpace(role)
			if role == "" {
				continue
			}
			roles = append(roles, role)
		}
		if len(roles) == 0 {
			return nil, fmt.Errorf("user has no token-eligible roles in this organisation")
		}
		return roles, nil
	}
	return nil, fmt.Errorf("user is not a member of this organisation")
}
