package api

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/xact-iot/xact/openapischema"
	"github.com/xact-iot/xact/sqldb"
)

func publicOrg(r *http.Request) string {
	if org := chi.URLParam(r, "org"); org != "" {
		return org
	}
	return "default"
}

func (h *DashboardHandlers) HandleListPublicDashboardsWithSchema() openapischema.Handler {
	return openapischema.WithSchema(h.HandleListPublicDashboards, nil, []sqldb.DashboardMeta{}, "public dashboards")
}

func (h *DashboardHandlers) HandleListPublicDashboards(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	org := publicOrg(r)
	organisation, err := h.DB.GetOrganisation(r.Context(), org)
	if err != nil {
		http.Error(w, "unable to load dashboards", http.StatusInternalServerError)
		return
	}
	if organisation == nil || !organisation.Active {
		http.NotFound(w, r)
		return
	}
	dashboards, err := h.DB.ListDashboards(r.Context(), org)
	if err != nil {
		http.Error(w, "unable to load dashboards", http.StatusInternalServerError)
		return
	}
	public := make([]sqldb.DashboardMeta, 0)
	for _, dashboard := range dashboards {
		if dashboard.IsPublic && !dashboard.IsCategory && dashboard.Permission == "" {
			public = append(public, dashboard)
		}
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(public)
}

func (h *DashboardHandlers) HandleGetPublicDashboardWithSchema() openapischema.Handler {
	return openapischema.WithSchema(h.HandleGetPublicDashboard, nil, sqldb.Dashboard{}, "public dashboards")
}

func (h *DashboardHandlers) HandleGetPublicDashboard(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	org := publicOrg(r)
	organisation, err := h.DB.GetOrganisation(r.Context(), org)
	if err != nil {
		http.Error(w, "unable to load dashboard", http.StatusInternalServerError)
		return
	}
	if organisation == nil || !organisation.Active {
		http.NotFound(w, r)
		return
	}
	id, err := dashboardIDParam(r)
	if err != nil || id < 1 {
		http.NotFound(w, r)
		return
	}
	dashboard, err := h.DB.GetDashboard(r.Context(), org, id)
	if err != nil {
		http.Error(w, "unable to load dashboard", http.StatusInternalServerError)
		return
	}
	public, err := sqldb.PublicDashboard(dashboard)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(public)
}
