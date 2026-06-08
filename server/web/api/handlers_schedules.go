package api

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/robfig/cron/v3"
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
	if t.Enabled {
		if err := h.Engine.Reload(r.Context(), org, t.ID); err != nil {
			// Non-fatal - task is saved, just log and continue.
			_ = err
		}
	}
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(t)
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
	if err := h.Engine.Reload(r.Context(), org, id); err != nil {
		_ = err
	}
	json.NewEncoder(w).Encode(t)
}

func (h *ScheduleHandlers) unsafeTaskDisabled(taskType string) bool {
	return scheduler.IsUnsafeTaskType(taskType) && (h.Engine == nil || !h.Engine.AllowUnsafeTasks())
}

func (h *ScheduleHandlers) HandleDelete(w http.ResponseWriter, r *http.Request) {
	org := h.GetOrg(r)
	id := chi.URLParam(r, "id")
	if err := h.DB.DeleteScheduledTask(r.Context(), org, id); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	h.Engine.Remove(id)
	w.WriteHeader(http.StatusNoContent)
}

func (h *ScheduleHandlers) HandleRunNow(w http.ResponseWriter, r *http.Request) {
	org := h.GetOrg(r)
	id := chi.URLParam(r, "id")
	outputPath, err := h.Engine.RunNow(r.Context(), org, id)
	if err != nil {
		json.NewEncoder(w).Encode(map[string]any{"error": err.Error()})
		return
	}
	json.NewEncoder(w).Encode(map[string]any{"outputPath": outputPath})
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
