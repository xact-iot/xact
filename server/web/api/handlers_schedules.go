package api

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/robfig/cron/v3"
	"github.com/xact-iot/xact/openapischema"
	"github.com/xact-iot/xact/scheduler"
	"github.com/xact-iot/xact/sqldb"
)

// ScheduleHandlers holds dependencies for scheduled task REST endpoints.
type ScheduleHandlers struct {
	DB     sqldb.DB
	Engine *scheduler.Engine
	GetOrg func(r *http.Request) string
}

// NewScheduleHandlers creates a new ScheduleHandlers.
func NewScheduleHandlers(db sqldb.DB, engine *scheduler.Engine, getOrg func(r *http.Request) string) *ScheduleHandlers {
	return &ScheduleHandlers{DB: db, Engine: engine, GetOrg: getOrg}
}

type runNowResponse struct {
	Status string `json:"status"`
}

func (h *ScheduleHandlers) HandleListWithSchema() openapischema.Handler {
	return openapischema.WithSchema(h.HandleList, nil, []sqldb.ScheduledTask{}, "schedules")
}

func (h *ScheduleHandlers) HandleList(w http.ResponseWriter, r *http.Request) {
	org := h.GetOrg(r)
	tasks, err := h.DB.ListScheduledTasks(r.Context(), org)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if tasks == nil {
		tasks = []sqldb.ScheduledTask{}
	}
	json.NewEncoder(w).Encode(tasks)
}

func (h *ScheduleHandlers) HandleGetWithSchema() openapischema.Handler {
	return openapischema.WithSchema(h.HandleGet, nil, sqldb.ScheduledTask{}, "schedules")
}

func (h *ScheduleHandlers) HandleGet(w http.ResponseWriter, r *http.Request) {
	org := h.GetOrg(r)
	id := chi.URLParam(r, "id")
	t, err := h.DB.GetScheduledTask(r.Context(), org, id)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if t == nil {
		http.Error(w, `{"error":"not found"}`, http.StatusNotFound)
		return
	}
	json.NewEncoder(w).Encode(t)
}

func (h *ScheduleHandlers) HandleCreateWithSchema() openapischema.Handler {
	return openapischema.Handler{
		Handler:     h.HandleCreate,
		RequestBody: openapischema.JSONRequestBody(sqldb.ScheduledTask{}),
		Responses:   openapischema.ResponseSchema(http.StatusCreated, sqldb.ScheduledTask{}),
		Tags:        []string{"schedules"},
	}
}

func (h *ScheduleHandlers) HandleCreate(w http.ResponseWriter, r *http.Request) {
	org := h.GetOrg(r)
	var t sqldb.ScheduledTask
	if err := json.NewDecoder(r.Body).Decode(&t); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	if t.Name == "" || t.TaskType == "" || t.Schedule == "" {
		http.Error(w, "name, taskType, and schedule are required", http.StatusBadRequest)
		return
	}
	if _, err := cron.ParseStandard(t.Schedule); err != nil {
		http.Error(w, "invalid cron schedule: "+err.Error(), http.StatusBadRequest)
		return
	}
	if h.unsafeTaskDisabled(t.TaskType) {
		http.Error(w, "task type is disabled by server configuration", http.StatusForbidden)
		return
	}
	if err := h.DB.CreateScheduledTask(r.Context(), org, &t); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if t.Enabled && h.Engine != nil {
		if err := h.Engine.Reload(r.Context(), org, t.ID); err != nil {
			// Non-fatal - task is saved, just log and continue.
			_ = err
		}
	}
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(t)
}

func (h *ScheduleHandlers) HandleUpdateWithSchema() openapischema.Handler {
	return openapischema.WithSchema(h.HandleUpdate, sqldb.ScheduledTask{}, sqldb.ScheduledTask{}, "schedules")
}

func (h *ScheduleHandlers) HandleUpdate(w http.ResponseWriter, r *http.Request) {
	org := h.GetOrg(r)
	id := chi.URLParam(r, "id")
	var t sqldb.ScheduledTask
	if err := json.NewDecoder(r.Body).Decode(&t); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	if t.Name == "" || t.TaskType == "" || t.Schedule == "" {
		http.Error(w, "name, taskType, and schedule are required", http.StatusBadRequest)
		return
	}
	if _, err := cron.ParseStandard(t.Schedule); err != nil {
		http.Error(w, "invalid cron schedule: "+err.Error(), http.StatusBadRequest)
		return
	}
	if h.unsafeTaskDisabled(t.TaskType) {
		http.Error(w, "task type is disabled by server configuration", http.StatusForbidden)
		return
	}
	if err := h.DB.UpdateScheduledTask(r.Context(), org, id, &t); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if h.Engine != nil {
		if err := h.Engine.Reload(r.Context(), org, id); err != nil {
			_ = err
		}
	}
	json.NewEncoder(w).Encode(t)
}

func (h *ScheduleHandlers) unsafeTaskDisabled(taskType string) bool {
	return scheduler.IsUnsafeTaskType(taskType) && (h.Engine == nil || !h.Engine.AllowUnsafeTasks())
}

func (h *ScheduleHandlers) HandleDeleteWithSchema() openapischema.Handler {
	return openapischema.WithResponses(h.HandleDelete, map[int]any{http.StatusNoContent: nil}, "schedules")
}

func (h *ScheduleHandlers) HandleDelete(w http.ResponseWriter, r *http.Request) {
	org := h.GetOrg(r)
	id := chi.URLParam(r, "id")
	if err := h.DB.DeleteScheduledTask(r.Context(), org, id); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if h.Engine != nil {
		h.Engine.Remove(id)
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *ScheduleHandlers) HandleRunNowWithSchema() openapischema.Handler {
	return openapischema.Handler{
		Handler:   h.HandleRunNow,
		Responses: openapischema.ResponseSchema(http.StatusAccepted, runNowResponse{}),
		Tags:      []string{"schedules"},
	}
}

func (h *ScheduleHandlers) HandleRunNow(w http.ResponseWriter, r *http.Request) {
	if h.Engine == nil {
		http.Error(w, "scheduler engine unavailable", http.StatusServiceUnavailable)
		return
	}
	org := h.GetOrg(r)
	id := chi.URLParam(r, "id")
	if err := h.Engine.StartNow(r.Context(), org, id); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusAccepted)
	json.NewEncoder(w).Encode(runNowResponse{Status: "started"})
}

func (h *ScheduleHandlers) HandleHistoryWithSchema() openapischema.Handler {
	return openapischema.WithSchema(h.HandleHistory, nil, []sqldb.ScheduleRunLog{}, "schedules")
}

func (h *ScheduleHandlers) HandleHistory(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	entries, err := h.DB.ListScheduleRunLog(r.Context(), id, 50)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if entries == nil {
		entries = []sqldb.ScheduleRunLog{}
	}
	json.NewEncoder(w).Encode(entries)
}
