package api

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/xact-iot/xact/notifications"
	"github.com/xact-iot/xact/sqldb"
)

// NotificationHandlers holds dependencies for notification REST endpoints.
type NotificationHandlers struct {
	DB           sqldb.DB
	GetOrg       func(r *http.Request) string
	ReloadNotifiers func(ctx context.Context, org string) error
}

// NewNotificationHandlers creates a new NotificationHandlers instance.
func NewNotificationHandlers(database sqldb.DB, getOrg func(r *http.Request) string, reloadNotifiers func(ctx context.Context, org string) error) *NotificationHandlers {
	return &NotificationHandlers{DB: database, GetOrg: getOrg, ReloadNotifiers: reloadNotifiers}
}

// HandleListProfiles returns all notification profiles for the org.
func (h *NotificationHandlers) HandleListProfiles(w http.ResponseWriter, r *http.Request) {
	org := h.GetOrg(r)
	profiles, err := h.DB.ListNotificationProfiles(r.Context(), org)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if profiles == nil {
		profiles = []sqldb.NotificationProfile{}
	}
	json.NewEncoder(w).Encode(profiles)
}

// HandleGetProfile returns a single notification profile by ID.
func (h *NotificationHandlers) HandleGetProfile(w http.ResponseWriter, r *http.Request) {
	org := h.GetOrg(r)
	id, err := strconv.Atoi(chi.URLParam(r, "id"))
	if err != nil {
		http.Error(w, "invalid profile id", http.StatusBadRequest)
		return
	}

	profile, err := h.DB.GetNotificationProfile(r.Context(), org, id)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if profile == nil {
		http.Error(w, "profile not found", http.StatusNotFound)
		return
	}
	json.NewEncoder(w).Encode(profile)
}

// HandleCreateProfile creates a new notification profile.
func (h *NotificationHandlers) HandleCreateProfile(w http.ResponseWriter, r *http.Request) {
	org := h.GetOrg(r)
	var p sqldb.NotificationProfile
	if err := json.NewDecoder(r.Body).Decode(&p); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	if p.Name == "" {
		http.Error(w, "name is required", http.StatusBadRequest)
		return
	}

	if err := h.DB.CreateNotificationProfile(r.Context(), org, &p); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(p)
}

// HandleUpdateProfile updates an existing notification profile.
func (h *NotificationHandlers) HandleUpdateProfile(w http.ResponseWriter, r *http.Request) {
	org := h.GetOrg(r)
	id, err := strconv.Atoi(chi.URLParam(r, "id"))
	if err != nil {
		http.Error(w, "invalid profile id", http.StatusBadRequest)
		return
	}

	var p sqldb.NotificationProfile
	if err := json.NewDecoder(r.Body).Decode(&p); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	if err := h.DB.UpdateNotificationProfile(r.Context(), org, id, &p); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// Return the updated profile.
	updated, _ := h.DB.GetNotificationProfile(r.Context(), org, id)
	if updated != nil {
		json.NewEncoder(w).Encode(updated)
	} else {
		w.WriteHeader(http.StatusNoContent)
	}
}

// HandleDeleteProfile removes a notification profile.
func (h *NotificationHandlers) HandleDeleteProfile(w http.ResponseWriter, r *http.Request) {
	org := h.GetOrg(r)
	id, err := strconv.Atoi(chi.URLParam(r, "id"))
	if err != nil {
		http.Error(w, "invalid profile id", http.StatusBadRequest)
		return
	}

	if err := h.DB.DeleteNotificationProfile(r.Context(), org, id); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// HandleGetChannels returns the notification channel configuration.
func (h *NotificationHandlers) HandleGetChannels(w http.ResponseWriter, r *http.Request) {
	org := h.GetOrg(r)
	cfg, err := notifications.LoadChannelConfig(r.Context(), h.DB, org)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	json.NewEncoder(w).Encode(cfg)
}

// HandleSaveChannels saves the notification channel configuration.
func (h *NotificationHandlers) HandleSaveChannels(w http.ResponseWriter, r *http.Request) {
	org := h.GetOrg(r)
	var cfg notifications.ChannelConfig
	if err := json.NewDecoder(r.Body).Decode(&cfg); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	if err := notifications.SaveChannelConfig(r.Context(), h.DB, org, cfg); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	if h.ReloadNotifiers != nil {
		if err := h.ReloadNotifiers(r.Context(), org); err != nil {
			log.Printf("notification handlers: failed to reload notifiers: %v", err)
		}
	}

	json.NewEncoder(w).Encode(cfg)
}
