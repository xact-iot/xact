package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/xact-iot/xact/openapischema"
	"github.com/xact-iot/xact/visualscripts"
)

type VisualScriptHandlers struct {
	Store   visualscripts.Store
	Engine  *visualscripts.Engine
	GetOrg  func(*http.Request) string
	GetUser func(*http.Request) int
	Audit   func(*http.Request, string, map[string]any)
}

func NewVisualScriptHandlers(store visualscripts.Store, engine *visualscripts.Engine, getOrg func(*http.Request) string, getUser func(*http.Request) int) *VisualScriptHandlers {
	return &VisualScriptHandlers{Store: store, Engine: engine, GetOrg: getOrg, GetUser: getUser}
}

func (h *VisualScriptHandlers) audit(r *http.Request, action string, params map[string]any) {
	if h.Audit != nil {
		h.Audit(r, action, params)
	}
}

func (h *VisualScriptHandlers) HandleCatalogWithSchema() openapischema.Handler {
	return openapischema.WithSchema(h.HandleCatalog, nil, []visualscripts.NodeDefinition{}, "visual-scripts")
}
func (h *VisualScriptHandlers) HandleCatalog(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, h.Engine.Registry().Catalog())
}

func (h *VisualScriptHandlers) HandleListWithSchema() openapischema.Handler {
	return openapischema.WithSchema(h.HandleList, nil, []visualscripts.Script{}, "visual-scripts")
}
func (h *VisualScriptHandlers) HandleList(w http.ResponseWriter, r *http.Request) {
	items, err := h.Store.ListVisualScripts(r.Context(), h.GetOrg(r))
	if err != nil {
		writeVisualError(w, err)
		return
	}
	for i := range items {
		status, err := h.Engine.Status(r.Context(), h.GetOrg(r), items[i].ID)
		if err == nil {
			items[i].RuntimeState = status.RuntimeState
		}
	}
	writeJSON(w, http.StatusOK, items)
}

func (h *VisualScriptHandlers) HandleCreateWithSchema() openapischema.Handler {
	return openapischema.Handler{Handler: h.HandleCreate, RequestBody: openapischema.JSONRequestBody(visualscripts.CreateScriptRequest{}), Responses: openapischema.ResponseSchema(http.StatusCreated, visualscripts.Script{}), Tags: []string{"visual-scripts"}}
}
func (h *VisualScriptHandlers) HandleCreate(w http.ResponseWriter, r *http.Request) {
	var request visualscripts.CreateScriptRequest
	if !decodeJSON(w, r, &request) {
		return
	}
	request.Name = strings.TrimSpace(request.Name)
	if request.Name == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "name is required"})
		return
	}
	actor := h.GetUser(r)
	item := visualscripts.Script{Name: request.Name, Description: strings.TrimSpace(request.Description), CreatedBy: actor, UpdatedBy: actor}
	if err := h.Store.CreateVisualScript(r.Context(), h.GetOrg(r), &item); err != nil {
		writeVisualError(w, err)
		return
	}
	h.audit(r, "create", map[string]any{"scriptId": item.ID})
	writeJSON(w, http.StatusCreated, item)
}

func (h *VisualScriptHandlers) HandleGetWithSchema() openapischema.Handler {
	return openapischema.WithSchema(h.HandleGet, nil, visualscripts.Script{}, "visual-scripts")
}
func (h *VisualScriptHandlers) HandleGet(w http.ResponseWriter, r *http.Request) {
	item, err := h.Store.GetVisualScript(r.Context(), h.GetOrg(r), chi.URLParam(r, "id"))
	if err != nil {
		writeVisualError(w, err)
		return
	}
	if item == nil {
		writeVisualError(w, visualscripts.ErrNotFound)
		return
	}
	status, _ := h.Engine.Status(r.Context(), h.GetOrg(r), item.ID)
	item.RuntimeState = status.RuntimeState
	writeJSON(w, http.StatusOK, item)
}

func (h *VisualScriptHandlers) HandleUpdateWithSchema() openapischema.Handler {
	return openapischema.WithSchema(h.HandleUpdate, visualscripts.UpdateScriptRequest{}, visualscripts.Script{}, "visual-scripts")
}
func (h *VisualScriptHandlers) HandleUpdate(w http.ResponseWriter, r *http.Request) {
	org, id := h.GetOrg(r), chi.URLParam(r, "id")
	current, err := h.Store.GetVisualScript(r.Context(), org, id)
	if err != nil || current == nil {
		if err == nil {
			err = visualscripts.ErrNotFound
		}
		writeVisualError(w, err)
		return
	}
	var request visualscripts.UpdateScriptRequest
	if !decodeJSON(w, r, &request) {
		return
	}
	if request.Name != nil {
		current.Name = strings.TrimSpace(*request.Name)
	}
	if request.Description != nil {
		current.Description = strings.TrimSpace(*request.Description)
	}
	if request.Simulation != nil {
		current.Simulation = *request.Simulation
	}
	if request.Activate != nil {
		current.Activate = *request.Activate
	}
	if current.Name == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "name is required"})
		return
	}
	if err = h.Store.UpdateVisualScript(r.Context(), org, id, current.Name, current.Description, h.GetUser(r)); err != nil {
		writeVisualError(w, err)
		return
	}
	if err = h.Store.SetVisualScriptOptions(r.Context(), org, id, current.Simulation, current.Activate, h.GetUser(r)); err != nil {
		writeVisualError(w, err)
		return
	}
	h.audit(r, "update", map[string]any{"scriptId": id})
	h.HandleGet(w, r)
}

func (h *VisualScriptHandlers) HandleDeleteWithSchema() openapischema.Handler {
	return openapischema.WithResponses(h.HandleDelete, map[int]any{http.StatusNoContent: nil}, "visual-scripts")
}
func (h *VisualScriptHandlers) HandleDelete(w http.ResponseWriter, r *http.Request) {
	org, id := h.GetOrg(r), chi.URLParam(r, "id")
	_, _ = h.Engine.Undeploy(r.Context(), org, id)
	if err := h.Store.DeleteVisualScript(r.Context(), org, id); err != nil {
		writeVisualError(w, err)
		return
	}
	h.audit(r, "delete", map[string]any{"scriptId": id})
	w.WriteHeader(http.StatusNoContent)
}

func (h *VisualScriptHandlers) HandleListRevisionsWithSchema() openapischema.Handler {
	return openapischema.WithSchema(h.HandleListRevisions, nil, []visualscripts.Revision{}, "visual-scripts")
}
func (h *VisualScriptHandlers) HandleListRevisions(w http.ResponseWriter, r *http.Request) {
	items, err := h.Store.ListVisualScriptRevisions(r.Context(), h.GetOrg(r), chi.URLParam(r, "id"))
	if err != nil {
		writeVisualError(w, err)
		return
	}
	for i := range items {
		items[i].Graph = visualscripts.GraphDocument{}
	}
	writeJSON(w, http.StatusOK, items)
}
func (h *VisualScriptHandlers) HandleGetRevisionWithSchema() openapischema.Handler {
	return openapischema.WithSchema(h.HandleGetRevision, nil, visualscripts.Revision{}, "visual-scripts")
}
func (h *VisualScriptHandlers) HandleGetRevision(w http.ResponseWriter, r *http.Request) {
	revision, ok := revisionParam(w, r)
	if !ok {
		return
	}
	item, err := h.Store.GetVisualScriptRevision(r.Context(), h.GetOrg(r), chi.URLParam(r, "id"), revision)
	if err != nil {
		writeVisualError(w, err)
		return
	}
	if item == nil {
		writeVisualError(w, visualscripts.ErrNotFound)
		return
	}
	writeJSON(w, http.StatusOK, item)
}

func (h *VisualScriptHandlers) HandleCreateRevisionWithSchema() openapischema.Handler {
	return openapischema.Handler{Handler: h.HandleCreateRevision, RequestBody: openapischema.JSONRequestBody(visualscripts.SaveRevisionRequest{}), Responses: openapischema.ResponseSchema(http.StatusCreated, visualscripts.Revision{}), Tags: []string{"visual-scripts"}}
}
func (h *VisualScriptHandlers) HandleCreateRevision(w http.ResponseWriter, r *http.Request) {
	var request visualscripts.SaveRevisionRequest
	if !decodeJSON(w, r, &request) {
		return
	}
	validation := h.Engine.Validate(request.Graph)
	status := "valid"
	if !validation.Valid {
		status = "invalid"
	}
	item := visualscripts.Revision{SchemaVersion: validation.Graph.SchemaVersion, Graph: validation.Graph, GraphHash: validation.GraphHash, ValidationStatus: status, Diagnostics: validation.Diagnostics, CreatedBy: h.GetUser(r)}
	if err := h.Store.CreateVisualScriptRevision(r.Context(), h.GetOrg(r), chi.URLParam(r, "id"), request.BaseRevision, &item); err != nil {
		writeVisualError(w, err)
		return
	}
	h.audit(r, "save-revision", map[string]any{"scriptId": item.ScriptID, "revision": item.Revision, "validationStatus": status})
	writeJSON(w, http.StatusCreated, item)
}

func (h *VisualScriptHandlers) HandleValidateWithSchema() openapischema.Handler {
	return openapischema.WithSchema(h.HandleValidate, visualscripts.ValidateRequest{}, visualscripts.ValidationResult{}, "visual-scripts")
}
func (h *VisualScriptHandlers) HandleValidate(w http.ResponseWriter, r *http.Request) {
	var request visualscripts.ValidateRequest
	if !decodeJSON(w, r, &request) {
		return
	}
	result := h.Engine.Validate(request.Graph)
	h.audit(r, "validate", map[string]any{"scriptId": chi.URLParam(r, "id"), "valid": result.Valid})
	writeJSON(w, http.StatusOK, result)
}

func (h *VisualScriptHandlers) HandleDeployWithSchema() openapischema.Handler {
	return openapischema.WithSchema(h.HandleDeploy, visualscripts.DeployRequest{}, visualscripts.RuntimeStatus{}, "visual-scripts")
}
func (h *VisualScriptHandlers) HandleDeploy(w http.ResponseWriter, r *http.Request) {
	var request visualscripts.DeployRequest
	if !decodeJSON(w, r, &request) {
		return
	}
	status, err := h.Engine.Deploy(r.Context(), h.GetOrg(r), chi.URLParam(r, "id"), request.Revision)
	if err != nil {
		writeVisualError(w, err)
		return
	}
	h.audit(r, "deploy", map[string]any{"scriptId": status.ScriptID, "revision": request.Revision})
	writeJSON(w, http.StatusOK, status)
}
func (h *VisualScriptHandlers) HandleUndeployWithSchema() openapischema.Handler {
	return openapischema.WithSchema(h.HandleUndeploy, nil, visualscripts.RuntimeStatus{}, "visual-scripts")
}
func (h *VisualScriptHandlers) HandleUndeploy(w http.ResponseWriter, r *http.Request) {
	status, err := h.Engine.Undeploy(r.Context(), h.GetOrg(r), chi.URLParam(r, "id"))
	if err != nil {
		writeVisualError(w, err)
		return
	}
	h.audit(r, "undeploy", map[string]any{"scriptId": status.ScriptID})
	writeJSON(w, http.StatusOK, status)
}

func (h *VisualScriptHandlers) Lifecycle(action string) openapischema.Handler {
	return openapischema.WithSchema(func(w http.ResponseWriter, r *http.Request) {
		if action == "start" || action == "resume" {
			status, err := h.Engine.StartCurrent(r.Context(), h.GetOrg(r), chi.URLParam(r, "id"))
			if err != nil {
				writeVisualError(w, err)
				return
			}
			h.audit(r, action, map[string]any{"scriptId": status.ScriptID})
			writeJSON(w, http.StatusOK, status)
			return
		}
		state := action
		status, err := h.Engine.SetDesiredState(r.Context(), h.GetOrg(r), chi.URLParam(r, "id"), state)
		if err != nil {
			writeVisualError(w, err)
			return
		}
		h.audit(r, action, map[string]any{"scriptId": status.ScriptID})
		writeJSON(w, http.StatusOK, status)
	}, nil, visualscripts.RuntimeStatus{}, "visual-scripts")
}

func (h *VisualScriptHandlers) HandleBackupWithSchema() openapischema.Handler {
	return openapischema.WithSchema(h.HandleBackup, nil, visualscripts.Script{}, "visual-scripts")
}
func (h *VisualScriptHandlers) HandleBackup(w http.ResponseWriter, r *http.Request) {
	org, id := h.GetOrg(r), chi.URLParam(r, "id")
	script, err := h.Store.GetVisualScript(r.Context(), org, id)
	if err != nil || script == nil {
		if err == nil {
			err = visualscripts.ErrNotFound
		}
		writeVisualError(w, err)
		return
	}
	if script.LatestRevision < 1 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "save the script before creating a backup"})
		return
	}
	revision := script.LatestRevision
	if err = h.Store.SetVisualScriptBackupRevision(r.Context(), org, id, &revision, h.GetUser(r)); err != nil {
		writeVisualError(w, err)
		return
	}
	h.audit(r, "backup", map[string]any{"scriptId": id})
	h.HandleGet(w, r)
}

func (h *VisualScriptHandlers) HandleRestoreWithSchema() openapischema.Handler {
	return openapischema.WithSchema(h.HandleRestore, nil, visualscripts.Revision{}, "visual-scripts")
}
func (h *VisualScriptHandlers) HandleRestore(w http.ResponseWriter, r *http.Request) {
	org, id := h.GetOrg(r), chi.URLParam(r, "id")
	script, err := h.Store.GetVisualScript(r.Context(), org, id)
	if err != nil || script == nil {
		if err == nil {
			err = visualscripts.ErrNotFound
		}
		writeVisualError(w, err)
		return
	}
	if script.BackupRevision == nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "no backup exists"})
		return
	}
	backup, err := h.Store.GetVisualScriptRevision(r.Context(), org, id, *script.BackupRevision)
	if err != nil || backup == nil {
		if err == nil {
			err = visualscripts.ErrNotFound
		}
		writeVisualError(w, err)
		return
	}
	validation := h.Engine.Validate(backup.Graph)
	status := "valid"
	if !validation.Valid {
		status = "invalid"
	}
	item := visualscripts.Revision{SchemaVersion: validation.Graph.SchemaVersion, Graph: validation.Graph, GraphHash: validation.GraphHash, ValidationStatus: status, Diagnostics: validation.Diagnostics, CreatedBy: h.GetUser(r)}
	if err = h.Store.CreateVisualScriptRevision(r.Context(), org, id, script.LatestRevision, &item); err != nil {
		writeVisualError(w, err)
		return
	}
	_, _ = h.Engine.SetDesiredState(r.Context(), org, id, "stopped")
	h.audit(r, "restore", map[string]any{"scriptId": id})
	writeJSON(w, http.StatusOK, item)
}
func (h *VisualScriptHandlers) HandleStatusWithSchema() openapischema.Handler {
	return openapischema.WithSchema(h.HandleStatus, nil, visualscripts.RuntimeStatus{}, "visual-scripts")
}
func (h *VisualScriptHandlers) HandleStatus(w http.ResponseWriter, r *http.Request) {
	status, err := h.Engine.Status(r.Context(), h.GetOrg(r), chi.URLParam(r, "id"))
	if err != nil {
		writeVisualError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, status)
}

func (h *VisualScriptHandlers) HandleRunWithSchema() openapischema.Handler {
	return openapischema.WithSchema(h.HandleRun, visualscripts.RunRequest{}, visualscripts.Run{}, "visual-scripts")
}
func (h *VisualScriptHandlers) HandleRun(w http.ResponseWriter, r *http.Request) {
	var request visualscripts.RunRequest
	if !decodeJSON(w, r, &request) {
		return
	}
	run, err := h.Engine.RunManual(r.Context(), h.GetOrg(r), chi.URLParam(r, "id"), request)
	if err != nil {
		writeVisualError(w, err)
		return
	}
	h.audit(r, "manual-run", map[string]any{"scriptId": run.ScriptID, "revision": run.ActiveRevision, "runId": run.RunID, "status": run.Status})
	writeJSON(w, http.StatusAccepted, run)
}
func (h *VisualScriptHandlers) HandleRunsWithSchema() openapischema.Handler {
	return openapischema.WithSchema(h.HandleRuns, nil, []visualscripts.Run{}, "visual-scripts")
}
func (h *VisualScriptHandlers) HandleRuns(w http.ResponseWriter, r *http.Request) {
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	items, err := h.Store.ListVisualScriptRuns(r.Context(), h.GetOrg(r), chi.URLParam(r, "id"), limit)
	if err != nil {
		writeVisualError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, items)
}
func (h *VisualScriptHandlers) HandleClearRunsWithSchema() openapischema.Handler {
	return openapischema.WithResponses(h.HandleClearRuns, map[int]any{http.StatusNoContent: nil}, "visual-scripts")
}
func (h *VisualScriptHandlers) HandleClearRuns(w http.ResponseWriter, r *http.Request) {
	org, id := h.GetOrg(r), chi.URLParam(r, "id")
	if err := h.Store.ClearVisualScriptRuns(r.Context(), org, id); err != nil {
		writeVisualError(w, err)
		return
	}
	h.audit(r, "clear-run-trace", map[string]any{"scriptId": id})
	w.WriteHeader(http.StatusNoContent)
}
func (h *VisualScriptHandlers) HandleRunDetailWithSchema() openapischema.Handler {
	return openapischema.WithSchema(h.HandleRunDetail, nil, visualscripts.Run{}, "visual-scripts")
}
func (h *VisualScriptHandlers) HandleRunDetail(w http.ResponseWriter, r *http.Request) {
	item, err := h.Store.GetVisualScriptRun(r.Context(), h.GetOrg(r), chi.URLParam(r, "id"), chi.URLParam(r, "runId"))
	if err != nil {
		writeVisualError(w, err)
		return
	}
	if item == nil {
		writeVisualError(w, visualscripts.ErrNotFound)
		return
	}
	writeJSON(w, http.StatusOK, item)
}
func (h *VisualScriptHandlers) HandleTraceWithSchema() openapischema.Handler {
	return openapischema.WithSchema(h.HandleTrace, nil, []visualscripts.TraceEvent{}, "visual-scripts")
}
func (h *VisualScriptHandlers) HandleTrace(w http.ResponseWriter, r *http.Request) {
	item, err := h.Store.GetVisualScriptRun(r.Context(), h.GetOrg(r), chi.URLParam(r, "id"), chi.URLParam(r, "runId"))
	if err != nil {
		writeVisualError(w, err)
		return
	}
	if item == nil {
		writeVisualError(w, visualscripts.ErrNotFound)
		return
	}
	writeJSON(w, http.StatusOK, item.Trace)
}

func decodeJSON(w http.ResponseWriter, r *http.Request, target any) bool {
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body: " + err.Error()})
		return false
	}
	return true
}
func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
func writeVisualError(w http.ResponseWriter, err error) {
	status := http.StatusInternalServerError
	if errors.Is(err, visualscripts.ErrNotFound) {
		status = http.StatusNotFound
	} else if errors.Is(err, visualscripts.ErrConflict) {
		status = http.StatusConflict
	} else if errors.Is(err, visualscripts.ErrNotRunning) {
		status = http.StatusConflict
	} else if errors.Is(err, visualscripts.ErrQueueFull) {
		status = http.StatusTooManyRequests
	} else if strings.Contains(err.Error(), "invalid") || strings.Contains(err.Error(), "deploy") || strings.Contains(err.Error(), "active revision") || strings.Contains(err.Error(), "Manual trigger") {
		status = http.StatusBadRequest
	}
	writeJSON(w, status, map[string]string{"error": err.Error()})
}
func revisionParam(w http.ResponseWriter, r *http.Request) (int, bool) {
	value, err := strconv.Atoi(chi.URLParam(r, "revision"))
	if err != nil || value < 1 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid revision"})
		return 0, false
	}
	return value, true
}
