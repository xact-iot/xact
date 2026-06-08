// Package api provides REST API handlers for non-RTDB resources.
package api

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/xact-iot/xact/sqldb"
)

// DashboardHandlers holds dependencies for dashboard REST endpoints.
type DashboardHandlers struct {
	DB          sqldb.DB
	getTenantID func(ctx context.Context) (string, bool)
}

// NewDashboardHandlers creates a new DashboardHandlers instance.
// getTenantID extracts the current user's tenant/org from the request context.
func NewDashboardHandlers(database sqldb.DB, getTenantID func(ctx context.Context) (string, bool)) *DashboardHandlers {
	return &DashboardHandlers{DB: database, getTenantID: getTenantID}
}

// dashboardRequest is the JSON body for create/update dashboard requests.
type dashboardRequest struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Icon        string          `json:"icon"`
	Variation   string          `json:"variation"`
	DeviceType  string          `json:"deviceType"`
	Permission  string          `json:"permission"`
	IsCategory  bool            `json:"isCategory"`
	ParentID    *int            `json:"parentId,omitempty"`
	SortOrder   int             `json:"sortOrder"`
	Widgets     json.RawMessage `json:"widgets,omitempty"`
}

func dashboardIDParam(r *http.Request) (int, error) {
	return strconv.Atoi(chi.URLParam(r, "id"))
}

func isEmptyWidgetsPayload(widgets json.RawMessage) bool {
	var values []json.RawMessage
	if err := json.Unmarshal(widgets, &values); err != nil {
		return false
	}
	return len(values) == 0
}

func isMetadataUpdate(raw map[string]json.RawMessage) bool {
	if len(raw) == 0 {
		return false
	}
	for key := range raw {
		if key != "widgets" {
			return true
		}
	}
	return false
}

// HandleListDashboards returns dashboard metadata (without widgets) for the organisation.
func (h *DashboardHandlers) HandleListDashboards(w http.ResponseWriter, r *http.Request) {
	org, ok := h.getTenantID(r.Context())
	if !ok {
		http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
		return
	}
	dashboards, err := h.DB.ListDashboards(r.Context(), org)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if dashboards == nil {
		dashboards = []sqldb.DashboardMeta{}
	}
	json.NewEncoder(w).Encode(dashboards)
}

// HandleGetDashboard returns a single dashboard with full data including widgets.
func (h *DashboardHandlers) HandleGetDashboard(w http.ResponseWriter, r *http.Request) {
	org, ok := h.getTenantID(r.Context())
	if !ok {
		http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
		return
	}
	id, err := dashboardIDParam(r)
	if err != nil {
		http.Error(w, "invalid dashboard id", http.StatusBadRequest)
		return
	}
	dashboard, err := h.DB.GetDashboard(r.Context(), org, id)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if dashboard == nil {
		http.Error(w, "dashboard not found", http.StatusNotFound)
		return
	}
	json.NewEncoder(w).Encode(dashboard)
}

// HandleCreateDashboard creates a new dashboard.
func (h *DashboardHandlers) HandleCreateDashboard(w http.ResponseWriter, r *http.Request) {
	org, ok := h.getTenantID(r.Context())
	if !ok {
		http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
		return
	}
	var req dashboardRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	if req.Name == "" {
		http.Error(w, "name is required", http.StatusBadRequest)
		return
	}

	dashboard := &sqldb.Dashboard{
		Name:        req.Name,
		Description: req.Description,
		Icon:        req.Icon,
		Variation:   req.Variation,
		DeviceType:  req.DeviceType,
		Permission:  req.Permission,
		IsCategory:  req.IsCategory,
		ParentID:    req.ParentID,
		SortOrder:   req.SortOrder,
		Widgets:     req.Widgets,
	}

	if err := h.DB.CreateDashboard(r.Context(), org, dashboard); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(dashboard)
}

// HandleUpdateDashboard updates an existing dashboard by id.
// It merges the request into the existing dashboard so that omitted fields
// are preserved rather than being blanked out.
func (h *DashboardHandlers) HandleUpdateDashboard(w http.ResponseWriter, r *http.Request) {
	org, ok := h.getTenantID(r.Context())
	if !ok {
		http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
		return
	}
	id, err := dashboardIDParam(r)
	if err != nil {
		http.Error(w, "invalid dashboard id", http.StatusBadRequest)
		return
	}

	// Fetch existing dashboard to use as base for merge.
	dashboard, err := h.DB.GetDashboard(r.Context(), org, id)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if dashboard == nil {
		http.Error(w, "dashboard not found", http.StatusNotFound)
		return
	}

	// Decode into a map so we know which fields were actually sent.
	var raw map[string]json.RawMessage
	if err := json.NewDecoder(r.Body).Decode(&raw); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	if v, ok := raw["name"]; ok {
		json.Unmarshal(v, &dashboard.Name)
	}
	if v, ok := raw["description"]; ok {
		json.Unmarshal(v, &dashboard.Description)
	}
	if v, ok := raw["icon"]; ok {
		json.Unmarshal(v, &dashboard.Icon)
	}
	if v, ok := raw["variation"]; ok {
		json.Unmarshal(v, &dashboard.Variation)
	}
	if v, ok := raw["deviceType"]; ok {
		json.Unmarshal(v, &dashboard.DeviceType)
	}
	if v, ok := raw["permission"]; ok {
		json.Unmarshal(v, &dashboard.Permission)
	}
	if v, ok := raw["isCategory"]; ok {
		json.Unmarshal(v, &dashboard.IsCategory)
	}
	if v, ok := raw["sortOrder"]; ok {
		json.Unmarshal(v, &dashboard.SortOrder)
	}
	if v, ok := raw["widgets"]; ok {
		// Dashboard-manager metadata updates should never be able to erase an
		// existing layout by accidentally sending widgets:[]. A true dashboard-editor
		// save sends only the widgets field, so explicit clears still work there.
		if !(isMetadataUpdate(raw) && isEmptyWidgetsPayload(v) && !isEmptyWidgetsPayload(dashboard.Widgets)) {
			dashboard.Widgets = v
		}
	}
	if v, ok := raw["parentId"]; ok {
		if string(v) == "null" {
			dashboard.ParentID = nil
		} else {
			var parentID int
			json.Unmarshal(v, &parentID)
			dashboard.ParentID = &parentID
		}
	}

	if err := h.DB.UpdateDashboard(r.Context(), org, id, dashboard); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	json.NewEncoder(w).Encode(dashboard)
}

// HandleDeleteDashboard deletes a dashboard by id.
func (h *DashboardHandlers) HandleDeleteDashboard(w http.ResponseWriter, r *http.Request) {
	org, ok := h.getTenantID(r.Context())
	if !ok {
		http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
		return
	}
	id, err := dashboardIDParam(r)
	if err != nil {
		http.Error(w, "invalid dashboard id", http.StatusBadRequest)
		return
	}

	if err := h.DB.DeleteDashboard(r.Context(), org, id); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}
