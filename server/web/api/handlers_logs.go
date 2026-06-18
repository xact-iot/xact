package api

import (
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"github.com/xact-iot/xact/events"
	"github.com/xact-iot/xact/openapischema"
	"github.com/xact-iot/xact/sqldb"
)

// LogHandlers holds dependencies for event query endpoints.
type LogHandlers struct {
	DB            sqldb.DB
	MaxQueryLimit int
	Publisher     *events.Publisher
	GetTenantID   func(r *http.Request) string
	GetUserID     func(r *http.Request) int
	CanAccessOrg  func(r *http.Request, org string) bool
	IsSystemAdmin func(r *http.Request) bool
}

// NewLogHandlers creates a new LogHandlers instance.
func NewLogHandlers(database sqldb.DB, maxQueryLimit int) *LogHandlers {
	if maxQueryLimit <= 0 {
		maxQueryLimit = 1000
	}
	return &LogHandlers{DB: database, MaxQueryLimit: maxQueryLimit}
}

type createLogRequest struct {
	Timestamp      *time.Time     `json:"timestamp"`
	OrgName        string         `json:"orgName"`
	Severity       string         `json:"severity"`
	NotificationID int            `json:"notificationId"`
	Device         string         `json:"device"`
	Message        string         `json:"message"`
	Params         map[string]any `json:"params"`
}

type okResponse struct {
	OK bool `json:"ok"`
}

func (h *LogHandlers) HandleCreateLogWithSchema() openapischema.Handler {
	return openapischema.Handler{
		Handler:     h.HandleCreateLog,
		RequestBody: openapischema.JSONRequestBody(createLogRequest{}),
		Responses:   openapischema.ResponseSchema(http.StatusAccepted, okResponse{}),
		Tags:        []string{"logs"},
	}
}

// HandleCreateLog handles POST /api/v1/logs. It publishes through the events
// pipeline so the entry is echoed to the console and persisted by the
// notification handler.
func (h *LogHandlers) HandleCreateLog(w http.ResponseWriter, r *http.Request) {
	if h.Publisher == nil {
		http.Error(w, "event publisher unavailable", http.StatusServiceUnavailable)
		return
	}
	var req createLogRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	if req.Message == "" {
		http.Error(w, "message is required", http.StatusBadRequest)
		return
	}
	if req.Severity == "" {
		req.Severity = string(events.Info)
	}
	switch events.Severity(req.Severity) {
	case events.Info, events.Warn, events.Error, events.Critical, events.Debug:
	default:
		http.Error(w, "invalid severity", http.StatusBadRequest)
		return
	}
	if req.OrgName == "" && h.GetTenantID != nil {
		req.OrgName = h.GetTenantID(r)
	}
	if req.OrgName == "" {
		req.OrgName = "default"
	}
	if !h.canAccessOrg(r, req.OrgName) {
		http.Error(w, "target organisation is outside your scope", http.StatusForbidden)
		return
	}
	entry := events.EventEntry{
		Timestamp:      time.Now(),
		OrgName:        req.OrgName,
		Severity:       req.Severity,
		NotificationID: req.NotificationID,
		Device:         req.Device,
		Message:        req.Message,
		Params:         req.Params,
	}
	if req.Timestamp != nil {
		entry.Timestamp = *req.Timestamp
	}
	if h.GetUserID != nil {
		if id := h.GetUserID(r); id != 0 {
			entry.UserID = &id
		}
	}
	if err := h.Publisher.PublishEntry(entry); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusAccepted)
	_ = json.NewEncoder(w).Encode(okResponse{OK: true})
}

func (h *LogHandlers) HandleQueryLogsWithSchema() openapischema.Handler {
	return openapischema.WithSchema(h.HandleQueryLogs, nil, []events.EventEntry{}, "logs")
}

// HandleQueryLogs handles GET /api/v1/logs with optional query parameters.
func (h *LogHandlers) HandleQueryLogs(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()

	filter := sqldb.EventFilter{
		OrgName:  q.Get("org_name"),
		Severity: q.Get("severity"),
		Device:   q.Get("device"),
		Search:   q.Get("search"),
	}
	if filter.OrgName == "" && !h.isSystemAdmin(r) {
		if h.GetTenantID != nil {
			filter.OrgName = h.GetTenantID(r)
		}
		if filter.OrgName == "" {
			filter.OrgName = "default"
		}
	}
	if filter.OrgName != "" && !h.canAccessOrg(r, filter.OrgName) {
		http.Error(w, "target organisation is outside your scope", http.StatusForbidden)
		return
	}

	if v := q.Get("user_id"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 {
			http.Error(w, "invalid user_id", http.StatusBadRequest)
			return
		}
		filter.UserID = n
	}

	if v := q.Get("notification_id"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 0 {
			http.Error(w, "invalid notification_id", http.StatusBadRequest)
			return
		}
		filter.NotificationID = n
	}

	if v := q.Get("after_id"); v != "" {
		n, err := strconv.ParseInt(v, 10, 64)
		if err != nil || n < 0 {
			http.Error(w, "invalid after_id", http.StatusBadRequest)
			return
		}
		filter.AfterID = n
	}

	if v := q.Get("startTime"); v != "" {
		t, err := time.Parse(time.RFC3339, v)
		if err != nil {
			http.Error(w, "invalid startTime: use RFC3339 format", http.StatusBadRequest)
			return
		}
		filter.StartTime = &t
	}

	if v := q.Get("endTime"); v != "" {
		t, err := time.Parse(time.RFC3339, v)
		if err != nil {
			http.Error(w, "invalid endTime: use RFC3339 format", http.StatusBadRequest)
			return
		}
		filter.EndTime = &t
	}

	if v := q.Get("limit"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 {
			http.Error(w, "invalid limit", http.StatusBadRequest)
			return
		}
		filter.Limit = n
	}

	// Cap at max
	if filter.Limit <= 0 || filter.Limit > h.MaxQueryLimit {
		filter.Limit = h.MaxQueryLimit
	}

	entries, err := h.DB.QueryEvents(r.Context(), filter)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if entries == nil {
		entries = []events.EventEntry{}
	}

	// Resolve user IDs to human-friendly names.
	h.resolveUserNames(r, entries)

	json.NewEncoder(w).Encode(entries)
}

func (h *LogHandlers) canAccessOrg(r *http.Request, org string) bool {
	if h.CanAccessOrg == nil {
		return true
	}
	return h.CanAccessOrg(r, org)
}

func (h *LogHandlers) isSystemAdmin(r *http.Request) bool {
	return h.IsSystemAdmin != nil && h.IsSystemAdmin(r)
}

// resolveUserNames populates UserName fields on each entry by looking
// up users from the database.
func (h *LogHandlers) resolveUserNames(r *http.Request, entries []events.EventEntry) {
	ctx := r.Context()

	userIDs := map[int]bool{}
	for _, e := range entries {
		if e.UserID != nil {
			userIDs[*e.UserID] = true
		}
	}
	if len(userIDs) == 0 {
		return
	}

	userNames := map[int]string{}
	users, err := h.DB.ListUsers(ctx)
	if err == nil {
		for _, u := range users {
			name := u.LoginName
			if u.FirstName != "" || u.LastName != "" {
				name = u.FirstName + " " + u.LastName
			}
			userNames[u.ID] = name
		}
	}

	for i := range entries {
		if entries[i].UserID != nil {
			entries[i].UserName = userNames[*entries[i].UserID]
		}
	}
}
