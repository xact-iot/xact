package api

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/xact-iot/xact/sqldb"
	"github.com/xact-iot/xact/sqldb/psql"
)

// MeHandlers handles /api/v1/me endpoints for the authenticated user's own profile.
type MeHandlers struct {
	DB        sqldb.DB
	getUserID func(ctx context.Context) (int, bool)
}

// NewMeHandlers creates a new MeHandlers.
func NewMeHandlers(database sqldb.DB, getUserID func(ctx context.Context) (int, bool)) *MeHandlers {
	return &MeHandlers{DB: database, getUserID: getUserID}
}

type updateProfileRequest struct {
	FirstName           string           `json:"firstName"`
	LastName            string           `json:"lastName"`
	Email               string           `json:"email"`
	NotificationOptions *json.RawMessage `json:"notificationOptions,omitempty"`
}

type changePasswordRequest struct {
	OldPassword string `json:"oldPassword"`
	NewPassword string `json:"newPassword"`
}

// HandleGetMe returns the current user's profile.
func (h *MeHandlers) HandleGetMe(w http.ResponseWriter, r *http.Request) {
	id, ok := h.getUserID(r.Context())
	if !ok {
		http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
		return
	}
	user, err := h.DB.GetUserByID(r.Context(), id)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if user == nil {
		http.Error(w, `{"error":"user not found"}`, http.StatusNotFound)
		return
	}
	json.NewEncoder(w).Encode(user)
}

// HandleUpdateMe updates mutable profile fields of the current user.
func (h *MeHandlers) HandleUpdateMe(w http.ResponseWriter, r *http.Request) {
	id, ok := h.getUserID(r.Context())
	if !ok {
		http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
		return
	}

	var req updateProfileRequest
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
		http.Error(w, `{"error":"user not found"}`, http.StatusNotFound)
		return
	}

	if req.FirstName != "" {
		user.FirstName = req.FirstName
	}
	if req.LastName != "" {
		user.LastName = req.LastName
	}
	if req.Email != "" {
		user.Email = req.Email
	}
	if req.NotificationOptions != nil {
		user.NotificationOptions = *req.NotificationOptions
	}

	if err := h.DB.UpdateUser(r.Context(), user); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	json.NewEncoder(w).Encode(user)
}

// HandleChangePassword verifies the current password and sets a new one.
func (h *MeHandlers) HandleChangePassword(w http.ResponseWriter, r *http.Request) {
	id, ok := h.getUserID(r.Context())
	if !ok {
		http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
		return
	}

	var req changePasswordRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.OldPassword == "" || req.NewPassword == "" {
		http.Error(w, `{"error":"oldPassword and newPassword are required"}`, http.StatusBadRequest)
		return
	}

	// Fetch user to get login name for hash lookup
	user, err := h.DB.GetUserByID(r.Context(), id)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if user == nil {
		http.Error(w, `{"error":"user not found"}`, http.StatusNotFound)
		return
	}

	// GetUserByLogin returns the current password hash
	_, hash, err := h.DB.GetUserByLogin(r.Context(), user.LoginName)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	if !psql.CheckPassword(hash, req.OldPassword) {
		w.WriteHeader(http.StatusUnauthorized)
		json.NewEncoder(w).Encode(map[string]string{"error": "current password is incorrect"})
		return
	}

	newHash, err := psql.HashPassword(req.NewPassword)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	if err := h.DB.SetUserPassword(r.Context(), id, newHash); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}
