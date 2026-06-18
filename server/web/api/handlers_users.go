package api

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/xact-iot/xact/openapischema"
	"github.com/xact-iot/xact/sqldb"
	"github.com/xact-iot/xact/sqldb/psql"
)

// UserHandlers holds dependencies for user REST endpoints.
type UserHandlers struct {
	DB             sqldb.DB
	CanAccessOrg   func(r *http.Request, org string) bool
	AllowedOrgSet  func(r *http.Request) (map[string]bool, bool)
	IsSystemAdmin  func(r *http.Request) bool
	CurrentOrgName func(r *http.Request) string
}

// NewUserHandlers creates a new UserHandlers instance.
func NewUserHandlers(database sqldb.DB) *UserHandlers {
	return &UserHandlers{DB: database}
}

type createUserRequest struct {
	FirstName string   `json:"firstName"`
	LastName  string   `json:"lastName"`
	LoginName string   `json:"loginName"`
	Email     string   `json:"email"`
	Password  string   `json:"password"`
	OrgName   string   `json:"orgName"` // defaults to "default"
	Roles     []string `json:"roles"`
}

type updateUserRequest struct {
	FirstName           string           `json:"firstName"`
	LastName            string           `json:"lastName"`
	Email               string           `json:"email"`
	Active              *bool            `json:"active"`
	OrgName             string           `json:"orgName"` // if set, replaces the user's roles in this org
	Roles               []string         `json:"roles"`   // new role set; only applied when OrgName is non-empty
	NotificationOptions *json.RawMessage `json:"notificationOptions,omitempty"`
}

type resetPasswordResponse struct {
	Password string `json:"password"`
}

func (h *UserHandlers) HandleListUsersWithSchema() openapischema.Handler {
	return openapischema.WithSchema(h.HandleListUsers, nil, []sqldb.User{}, "users")
}

// HandleListUsers returns all users with their org/role memberships.
func (h *UserHandlers) HandleListUsers(w http.ResponseWriter, r *http.Request) {
	users, err := h.DB.ListUsers(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	users = h.filterVisibleUsers(r, users)
	if users == nil {
		users = []sqldb.User{}
	}
	json.NewEncoder(w).Encode(users)
}

func (h *UserHandlers) HandleCreateUserWithSchema() openapischema.Handler {
	return openapischema.Handler{
		Handler:     h.HandleCreateUser,
		RequestBody: openapischema.JSONRequestBody(createUserRequest{}),
		Responses:   openapischema.ResponseSchema(http.StatusCreated, sqldb.User{}),
		Tags:        []string{"users"},
	}
}

// HandleCreateUser creates a new user, adds them to an org, and assigns roles.
func (h *UserHandlers) HandleCreateUser(w http.ResponseWriter, r *http.Request) {
	var req createUserRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	if req.LoginName == "" || req.Email == "" || req.Password == "" {
		http.Error(w, "loginName, email and password are required", http.StatusBadRequest)
		return
	}
	if req.OrgName == "" {
		req.OrgName = h.currentOrg(r)
	}
	if !h.canAccessOrg(r, req.OrgName) {
		http.Error(w, "target organisation is outside your scope", http.StatusForbidden)
		return
	}
	if !h.canAssignRoles(r, req.Roles) {
		http.Error(w, "cannot assign requested roles", http.StatusForbidden)
		return
	}

	hash, err := psql.HashPassword(req.Password)
	if err != nil {
		http.Error(w, "failed to hash password", http.StatusInternalServerError)
		return
	}

	user := &sqldb.User{
		FirstName: req.FirstName,
		LastName:  req.LastName,
		LoginName: req.LoginName,
		Email:     req.Email,
		Active:    true,
	}

	if err := h.DB.CreateUser(r.Context(), user, hash); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	if err := h.DB.AssignUserToOrg(r.Context(), user.ID, req.OrgName, req.Roles); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// Refresh to include org memberships in the response
	user.Orgs, _ = h.DB.GetUserOrgs(r.Context(), user.ID)

	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(user)
}

func (h *UserHandlers) HandleGetUserWithSchema() openapischema.Handler {
	return openapischema.WithSchema(h.HandleGetUser, nil, sqldb.User{}, "users")
}

// HandleGetUser returns a single user by ID.
func (h *UserHandlers) HandleGetUser(w http.ResponseWriter, r *http.Request) {
	id, err := parseUserID(r)
	if err != nil {
		http.Error(w, "invalid user id", http.StatusBadRequest)
		return
	}

	user, err := h.DB.GetUserByID(r.Context(), id)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if user == nil {
		http.Error(w, "user not found", http.StatusNotFound)
		return
	}
	if !h.canViewUser(r, user) {
		http.Error(w, "user not found", http.StatusNotFound)
		return
	}
	user.Orgs = h.filterVisibleOrgs(r, user.Orgs)
	json.NewEncoder(w).Encode(user)
}

func (h *UserHandlers) HandleUpdateUserWithSchema() openapischema.Handler {
	return openapischema.WithSchema(h.HandleUpdateUser, updateUserRequest{}, sqldb.User{}, "users")
}

// HandleUpdateUser updates mutable user fields.
func (h *UserHandlers) HandleUpdateUser(w http.ResponseWriter, r *http.Request) {
	id, err := parseUserID(r)
	if err != nil {
		http.Error(w, "invalid user id", http.StatusBadRequest)
		return
	}

	var req updateUserRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	user, err := h.DB.GetUserByID(r.Context(), id)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if user == nil {
		http.Error(w, "user not found", http.StatusNotFound)
		return
	}
	if !h.canViewUser(r, user) {
		http.Error(w, "user not found", http.StatusNotFound)
		return
	}
	if !h.canManageUser(r, user) {
		http.Error(w, "cannot manage this user", http.StatusForbidden)
		return
	}
	if req.OrgName == "" && req.Roles != nil {
		req.OrgName = h.currentOrg(r)
	}
	if req.OrgName != "" && req.Roles != nil {
		if !h.canAccessOrg(r, req.OrgName) {
			http.Error(w, "target organisation is outside your scope", http.StatusForbidden)
			return
		}
		if !h.canAssignRoles(r, req.Roles) {
			http.Error(w, "cannot assign requested roles", http.StatusForbidden)
			return
		}
	}

	oldActive := user.Active
	if req.FirstName != "" {
		user.FirstName = req.FirstName
	}
	if req.LastName != "" {
		user.LastName = req.LastName
	}
	if req.Email != "" {
		user.Email = req.Email
	}
	if req.Active != nil {
		user.Active = *req.Active
	}
	if req.NotificationOptions != nil {
		user.NotificationOptions = *req.NotificationOptions
	}

	if err := h.DB.UpdateUser(r.Context(), user); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if req.Active != nil && oldActive != user.Active {
		_ = h.DB.BumpUserTokenVersion(r.Context(), id)
	}

	// Replace the user's roles in the given org when requested.
	if req.OrgName != "" && req.Roles != nil {
		if err := h.DB.SetUserOrgRoles(r.Context(), id, req.OrgName, req.Roles); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		// Refresh org memberships so the response reflects the new roles.
		user.Orgs, _ = h.DB.GetUserOrgs(r.Context(), id)
	}
	user.Orgs = h.filterVisibleOrgs(r, user.Orgs)

	json.NewEncoder(w).Encode(user)
}

func (h *UserHandlers) HandleResetPasswordWithSchema() openapischema.Handler {
	return openapischema.WithSchema(h.HandleResetPassword, nil, resetPasswordResponse{}, "users")
}

// HandleResetPassword generates a new random password, stores it, and returns the plaintext.
func (h *UserHandlers) HandleResetPassword(w http.ResponseWriter, r *http.Request) {
	id, err := parseUserID(r)
	if err != nil {
		http.Error(w, "invalid user id", http.StatusBadRequest)
		return
	}
	user, err := h.DB.GetUserByID(r.Context(), id)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if user == nil || !h.canViewUser(r, user) {
		http.Error(w, "user not found", http.StatusNotFound)
		return
	}
	if !h.canManageUser(r, user) {
		http.Error(w, "cannot manage this user", http.StatusForbidden)
		return
	}

	newPass, err := psql.GenerateRandomPassword()
	if err != nil {
		http.Error(w, "failed to generate password", http.StatusInternalServerError)
		return
	}

	hash, err := psql.HashPassword(newPass)
	if err != nil {
		http.Error(w, "failed to hash password", http.StatusInternalServerError)
		return
	}

	if err := h.DB.SetUserPassword(r.Context(), id, hash); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	json.NewEncoder(w).Encode(resetPasswordResponse{Password: newPass})
}

func (h *UserHandlers) HandleListRolesWithSchema() openapischema.Handler {
	return openapischema.WithSchema(h.HandleListRoles, nil, []sqldb.Role{}, "users")
}

// HandleListRoles returns all defined roles.
func (h *UserHandlers) HandleListRoles(w http.ResponseWriter, r *http.Request) {
	roles, err := h.DB.ListRoles(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if roles == nil {
		roles = []sqldb.Role{}
	}
	json.NewEncoder(w).Encode(roles)
}

// parseUserID extracts and parses the {id} URL parameter.
func parseUserID(r *http.Request) (int, error) {
	return strconv.Atoi(chi.URLParam(r, "id"))
}

func (h *UserHandlers) canAccessOrg(r *http.Request, org string) bool {
	if h.CanAccessOrg == nil {
		return true
	}
	return h.CanAccessOrg(r, org)
}

func (h *UserHandlers) isSystemAdmin(r *http.Request) bool {
	return h.IsSystemAdmin != nil && h.IsSystemAdmin(r)
}

func (h *UserHandlers) currentOrg(r *http.Request) string {
	if h.CurrentOrgName != nil {
		if org := h.CurrentOrgName(r); org != "" {
			return org
		}
	}
	return "default"
}

func (h *UserHandlers) canAssignRoles(r *http.Request, roles []string) bool {
	if h.isSystemAdmin(r) {
		return true
	}
	for _, role := range roles {
		if role == "SystemAdmin" {
			return false
		}
	}
	return true
}

func (h *UserHandlers) filterVisibleUsers(r *http.Request, users []sqldb.User) []sqldb.User {
	if h.AllowedOrgSet == nil {
		return users
	}
	allowed, systemAdmin := h.AllowedOrgSet(r)
	if systemAdmin {
		return users
	}
	filtered := make([]sqldb.User, 0, len(users))
	for _, user := range users {
		user.Orgs = filterUserOrgs(user.Orgs, allowed)
		if len(user.Orgs) > 0 {
			filtered = append(filtered, user)
		}
	}
	return filtered
}

func (h *UserHandlers) filterVisibleOrgs(r *http.Request, orgs []sqldb.UserOrg) []sqldb.UserOrg {
	if h.AllowedOrgSet == nil {
		return orgs
	}
	allowed, systemAdmin := h.AllowedOrgSet(r)
	if systemAdmin {
		return orgs
	}
	return filterUserOrgs(orgs, allowed)
}

func (h *UserHandlers) canViewUser(r *http.Request, user *sqldb.User) bool {
	if h.isSystemAdmin(r) || h.AllowedOrgSet == nil {
		return true
	}
	allowed, systemAdmin := h.AllowedOrgSet(r)
	if systemAdmin {
		return true
	}
	for _, org := range user.Orgs {
		if allowed[org.OrgName] {
			return true
		}
	}
	return false
}

func (h *UserHandlers) canManageUser(r *http.Request, user *sqldb.User) bool {
	if h.isSystemAdmin(r) {
		return true
	}
	return !userHasRole(user, "SystemAdmin")
}

func userHasRole(user *sqldb.User, role string) bool {
	if user == nil {
		return false
	}
	for _, org := range user.Orgs {
		for _, assigned := range org.Roles {
			if assigned == role {
				return true
			}
		}
	}
	return false
}

func filterUserOrgs(orgs []sqldb.UserOrg, allowed map[string]bool) []sqldb.UserOrg {
	filtered := make([]sqldb.UserOrg, 0, len(orgs))
	for _, org := range orgs {
		if allowed[org.OrgName] {
			filtered = append(filtered, org)
		}
	}
	return filtered
}
