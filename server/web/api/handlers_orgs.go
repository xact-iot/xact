package api

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/xact-iot/xact/openapischema"
	"github.com/xact-iot/xact/sqldb"
)

// OrgNodeSyncer is called after a successful organisation create or update so
// that the RTDB tree stays in sync. name is the immutable slug, displayName is
// the human-readable label, area is the bounding box (nil when not set).
type OrgNodeSyncer func(name, displayName string, area *sqldb.OrgArea)

// OrgNodeDeleter is called after a successful organisation delete so that the
// RTDB tree node and its subtree are removed.
type OrgNodeDeleter func(name string)

// UserIDExtractor extracts the authenticated user's numeric ID from a request context.
type UserIDExtractor func(ctx context.Context) (int, bool)

type userOrgAssignerWithoutTokenBump interface {
	AssignUserToOrgWithoutTokenBump(ctx context.Context, userID int, orgName string, roleNames []string) error
}

// OrgHandlers holds dependencies for organisation REST endpoints.
type OrgHandlers struct {
	DB            sqldb.DB
	NodeSync      OrgNodeSyncer   // optional; called after create/update
	NodeDelete    OrgNodeDeleter  // optional; called after delete
	GetUserID     UserIDExtractor // optional; used to auto-assign creator as SystemAdmin
	CanAccessOrg  func(r *http.Request, org string) bool
	AllowedOrgSet func(r *http.Request) (map[string]bool, bool)
	IsSystemAdmin func(r *http.Request) bool
}

// NewOrgHandlers creates a new OrgHandlers instance.
// syncer and deleter may be nil if no RTDB tree is available.
func NewOrgHandlers(database sqldb.DB, syncer OrgNodeSyncer, deleter OrgNodeDeleter) *OrgHandlers {
	return &OrgHandlers{DB: database, NodeSync: syncer, NodeDelete: deleter}
}

type createOrgRequest struct {
	Name        string         `json:"name"`
	DisplayName string         `json:"displayName"`
	Active      bool           `json:"active"`
	Logo        string         `json:"logo"`
	Favicon     string         `json:"favicon"`
	Area        *sqldb.OrgArea `json:"area"`
}

type updateOrgRequest struct {
	DisplayName string         `json:"displayName"`
	Active      bool           `json:"active"`
	Logo        string         `json:"logo"`
	Favicon     string         `json:"favicon"`
	Area        *sqldb.OrgArea `json:"area"`
}

func (h *OrgHandlers) HandleListOrganisationsWithSchema() openapischema.Handler {
	return openapischema.WithSchema(h.HandleListOrganisations, nil, []sqldb.Organisation{}, "organisations")
}

// HandleListOrganisations returns all organisations.
func (h *OrgHandlers) HandleListOrganisations(w http.ResponseWriter, r *http.Request) {
	orgs, err := h.DB.ListOrganisations(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if orgs == nil {
		orgs = []sqldb.Organisation{}
	}
	orgs = h.filterVisibleOrgs(r, orgs)
	json.NewEncoder(w).Encode(orgs)
}

func (h *OrgHandlers) HandleGetOrganisationWithSchema() openapischema.Handler {
	return openapischema.WithSchema(h.HandleGetOrganisation, nil, sqldb.Organisation{}, "organisations")
}

// HandleGetOrganisation returns a single organisation by name.
func (h *OrgHandlers) HandleGetOrganisation(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	if !h.canAccessOrg(r, name) {
		http.Error(w, "organisation not found", http.StatusNotFound)
		return
	}
	org, err := h.DB.GetOrganisation(r.Context(), name)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if org == nil {
		http.Error(w, "organisation not found", http.StatusNotFound)
		return
	}
	json.NewEncoder(w).Encode(org)
}

func (h *OrgHandlers) HandleCreateOrganisationWithSchema() openapischema.Handler {
	return openapischema.Handler{
		Handler:     h.HandleCreateOrganisation,
		RequestBody: openapischema.JSONRequestBody(createOrgRequest{}),
		Responses:   openapischema.ResponseSchema(http.StatusCreated, sqldb.Organisation{}),
		Tags:        []string{"organisations"},
	}
}

// HandleCreateOrganisation creates a new organisation.
func (h *OrgHandlers) HandleCreateOrganisation(w http.ResponseWriter, r *http.Request) {
	if !h.isSystemAdmin(r) {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	var req createOrgRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	if req.Name == "" {
		http.Error(w, "name is required", http.StatusBadRequest)
		return
	}

	org := &sqldb.Organisation{
		Name:        req.Name,
		DisplayName: req.DisplayName,
		Active:      req.Active,
		Logo:        req.Logo,
		Favicon:     req.Favicon,
		Area:        req.Area,
	}
	if err := h.DB.CreateOrganisation(r.Context(), org); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// Auto-assign the creating user as SystemAdmin in the new org so they
	// retain full access without a separate role assignment step.
	if h.GetUserID != nil {
		if userID, ok := h.GetUserID(r.Context()); ok {
			if assigner, ok := h.DB.(userOrgAssignerWithoutTokenBump); ok {
				_ = assigner.AssignUserToOrgWithoutTokenBump(r.Context(), userID, org.Name, []string{"SystemAdmin"})
			} else {
				_ = h.DB.AssignUserToOrg(r.Context(), userID, org.Name, []string{"SystemAdmin"})
			}
		}
	}

	if h.NodeSync != nil {
		h.NodeSync(org.Name, org.DisplayName, org.Area)
	}
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(org)
}

func (h *OrgHandlers) HandleUpdateOrganisationWithSchema() openapischema.Handler {
	return openapischema.WithSchema(h.HandleUpdateOrganisation, updateOrgRequest{}, sqldb.Organisation{}, "organisations")
}

// HandleUpdateOrganisation updates an existing organisation.
// The name (URL parameter) is immutable; only displayName, active, and area are updated.
func (h *OrgHandlers) HandleUpdateOrganisation(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	if !h.canAccessOrg(r, name) {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}

	var req updateOrgRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	org := &sqldb.Organisation{
		Name:        name,
		DisplayName: req.DisplayName,
		Active:      req.Active,
		Logo:        req.Logo,
		Favicon:     req.Favicon,
		Area:        req.Area,
	}
	if err := h.DB.UpdateOrganisation(r.Context(), name, org); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if h.NodeSync != nil {
		h.NodeSync(org.Name, org.DisplayName, org.Area)
	}
	json.NewEncoder(w).Encode(org)
}

func (h *OrgHandlers) HandleDeleteOrganisationWithSchema() openapischema.Handler {
	return openapischema.WithResponses(h.HandleDeleteOrganisation, map[int]any{http.StatusNoContent: nil}, "organisations")
}

// HandleDeleteOrganisation removes an organisation and its RTDB subtree.
func (h *OrgHandlers) HandleDeleteOrganisation(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	if name == "default" {
		http.Error(w, "cannot delete the default organisation", http.StatusForbidden)
		return
	}
	if !h.isSystemAdmin(r) {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	if err := h.DB.DeleteOrganisation(r.Context(), name); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if h.NodeDelete != nil {
		h.NodeDelete(name)
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *OrgHandlers) canAccessOrg(r *http.Request, org string) bool {
	if h.CanAccessOrg == nil {
		return true
	}
	return h.CanAccessOrg(r, org)
}

func (h *OrgHandlers) isSystemAdmin(r *http.Request) bool {
	return h.IsSystemAdmin != nil && h.IsSystemAdmin(r)
}

func (h *OrgHandlers) filterVisibleOrgs(r *http.Request, orgs []sqldb.Organisation) []sqldb.Organisation {
	if h.AllowedOrgSet == nil {
		return orgs
	}
	allowed, systemAdmin := h.AllowedOrgSet(r)
	if systemAdmin {
		return orgs
	}
	filtered := make([]sqldb.Organisation, 0, len(orgs))
	for _, org := range orgs {
		if allowed[org.Name] {
			filtered = append(filtered, org)
		}
	}
	return filtered
}
