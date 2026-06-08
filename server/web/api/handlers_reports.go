package api

import (
	"context"
	"encoding/json"
	"maps"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/xact-iot/xact/reporting"
	"github.com/xact-iot/xact/sqldb"
)

// ReportHandlers holds dependencies for PDF report template REST endpoints.
type ReportHandlers struct {
	DB          sqldb.DB
	getTenantID func(ctx context.Context) (string, bool)
	// TagReader resolves RTDB tag paths to current values (may be nil).
	TagReader func(path string) (string, bool)
}

// NewReportHandlers creates a new ReportHandlers instance.
func NewReportHandlers(
	database sqldb.DB,
	getTenantID func(ctx context.Context) (string, bool),
	tagReader func(path string) (string, bool),
) *ReportHandlers {
	return &ReportHandlers{
		DB:          database,
		getTenantID: getTenantID,
		TagReader:   tagReader,
	}
}

// HandleListTemplates returns all PDF templates for the organisation.
func (h *ReportHandlers) HandleListTemplates(w http.ResponseWriter, r *http.Request) {
	org, ok := h.getTenantID(r.Context())
	if !ok {
		http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
		return
	}
	templates, err := h.DB.ListPDFTemplates(r.Context(), org)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	json.NewEncoder(w).Encode(templates)
}

// HandleGetTemplate returns a single PDF template by ID.
func (h *ReportHandlers) HandleGetTemplate(w http.ResponseWriter, r *http.Request) {
	org, ok := h.getTenantID(r.Context())
	if !ok {
		http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
		return
	}
	id := chi.URLParam(r, "id")
	t, err := h.DB.GetPDFTemplate(r.Context(), org, id)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if t == nil {
		http.Error(w, "template not found", http.StatusNotFound)
		return
	}
	json.NewEncoder(w).Encode(t)
}

// HandleCreateTemplate creates a new PDF template.
func (h *ReportHandlers) HandleCreateTemplate(w http.ResponseWriter, r *http.Request) {
	org, ok := h.getTenantID(r.Context())
	if !ok {
		http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
		return
	}
	var t sqldb.PDFTemplate
	if err := json.NewDecoder(r.Body).Decode(&t); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	if t.Name == "" {
		http.Error(w, "name is required", http.StatusBadRequest)
		return
	}
	if err := h.DB.CreatePDFTemplate(r.Context(), org, &t); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(t)
}

// HandleUpdateTemplate replaces an existing PDF template.
func (h *ReportHandlers) HandleUpdateTemplate(w http.ResponseWriter, r *http.Request) {
	org, ok := h.getTenantID(r.Context())
	if !ok {
		http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
		return
	}
	id := chi.URLParam(r, "id")
	var t sqldb.PDFTemplate
	if err := json.NewDecoder(r.Body).Decode(&t); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	if err := h.DB.UpdatePDFTemplate(r.Context(), org, id, &t); err != nil {
		if err.Error() == "pdf template not found" {
			http.Error(w, err.Error(), http.StatusNotFound)
			return
		}
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	t.ID = id
	json.NewEncoder(w).Encode(t)
}

// HandleDeleteTemplate removes a PDF template.
func (h *ReportHandlers) HandleDeleteTemplate(w http.ResponseWriter, r *http.Request) {
	org, ok := h.getTenantID(r.Context())
	if !ok {
		http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
		return
	}
	id := chi.URLParam(r, "id")
	if err := h.DB.DeletePDFTemplate(r.Context(), org, id); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// HandlePreviewTemplate generates a PDF from a template with sample/override variable values.
// Request body: { "variables": { "name": "value", ... } }
// Response: application/pdf bytes.
func (h *ReportHandlers) HandlePreviewTemplate(w http.ResponseWriter, r *http.Request) {
	org, ok := h.getTenantID(r.Context())
	if !ok {
		http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
		return
	}
	id := chi.URLParam(r, "id")
	t, err := h.DB.GetPDFTemplate(r.Context(), org, id)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if t == nil {
		http.Error(w, "template not found", http.StatusNotFound)
		return
	}

	// Optional override values from request body
	var body struct {
		Variables map[string]string `json:"variables"`
	}
	json.NewDecoder(r.Body).Decode(&body)

	pdfBytes, err := h.resolveAndGenerate(r.Context(), t, org, body.Variables)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/pdf")
	w.Header().Set("Content-Disposition", `inline; filename="preview.pdf"`)
	w.Write(pdfBytes)
}

// HandleGeneratePDF generates a PDF on demand and streams it as a download.
// Request body: { "templateId": "...", "variables": { "name": "value", ... } }
func (h *ReportHandlers) HandleGeneratePDF(w http.ResponseWriter, r *http.Request) {
	org, ok := h.getTenantID(r.Context())
	if !ok {
		http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
		return
	}
	var body struct {
		TemplateID string            `json:"templateId"`
		Variables  map[string]string `json:"variables"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	t, err := h.DB.GetPDFTemplate(r.Context(), org, body.TemplateID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if t == nil {
		http.Error(w, "template not found", http.StatusNotFound)
		return
	}

	pdfBytes, err := h.resolveAndGenerate(r.Context(), t, org, body.Variables)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/pdf")
	w.Header().Set("Content-Disposition", `attachment; filename="`+sanitizeFilename(t.Name)+`.pdf"`)
	w.Write(pdfBytes)
}

// resolveAndGenerate resolves all variable bindings, substitutes them into the
// template JSON, and renders the PDF. overrides map takes precedence over resolved values.
func (h *ReportHandlers) resolveAndGenerate(
	ctx context.Context,
	t *sqldb.PDFTemplate,
	org string,
	overrides map[string]string,
) ([]byte, error) {
	vars, err := reporting.ParseVariables(t)
	if err != nil {
		return nil, err
	}

	rc := reporting.ResolveContext{
		OrgName:        org,
		OrgDisplayName: h.orgDisplayName(ctx, org),
		ReportName:     t.Name,
		TagReader:      h.TagReader,
	}

	vals := reporting.ResolveVariables(ctx, vars, rc)

	// Apply any caller-supplied overrides
	maps.Copy(vals, overrides)

	resolved, err := reporting.SubstituteTemplate(t.TemplateJSON, vals)
	if err != nil {
		return nil, err
	}

	gc := reporting.GenerateContext{
		OrgName:         org,
		TagPathsQueryer: h.DB.QueryMetricsByTagPaths,
		EventsQueryer:   h.DB.QueryEvents,
	}
	return reporting.GeneratePDF(ctx, resolved, gc)
}

func (h *ReportHandlers) orgDisplayName(ctx context.Context, org string) string {
	if h.DB == nil {
		return org
	}
	o, err := h.DB.GetOrganisation(ctx, org)
	if err != nil || o == nil || o.DisplayName == "" {
		return org
	}
	return o.DisplayName
}

// sanitizeFilename strips characters unsafe in a Content-Disposition filename.
func sanitizeFilename(s string) string {
	safe := make([]byte, 0, len(s))
	for i := range s {
		c := s[i]
		if (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') ||
			(c >= '0' && c <= '9') || c == '-' || c == '_' || c == '.' {
			safe = append(safe, c)
		} else {
			safe = append(safe, '_')
		}
	}
	return string(safe)
}
