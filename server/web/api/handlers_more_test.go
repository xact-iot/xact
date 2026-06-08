package api

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/xact-iot/xact/events"
	"github.com/xact-iot/xact/notifications"
	"github.com/xact-iot/xact/scheduler"
	"github.com/xact-iot/xact/sqldb"
	"github.com/xact-iot/xact/sqldb/sqlite"
	"github.com/xact-iot/xact/tagcalcs"
)

func TestMetricHandlersRangeSinceAndValidation(t *testing.T) {
	ctx := context.Background()
	db := newWebAPITestDB(t, "web-metrics")
	start := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	if err := db.InsertMetrics(ctx, "default", []sqldb.MetricEntry{
		{DeviceName: "pump", MetricName: "temp", Timestamp: start, Value: 1},
		{DeviceName: "pump", MetricName: "temp", Timestamp: start.Add(time.Second), Value: 2},
		{DeviceName: "pump", MetricName: "flow", Timestamp: start.Add(2 * time.Second), Value: 3},
	}); err != nil {
		t.Fatalf("InsertMetrics: %v", err)
	}
	h := NewMetricHandlers(db, func(context.Context) (string, bool) { return "default", true })
	r := chi.NewRouter()
	r.Get("/metrics/*", func(w http.ResponseWriter, req *http.Request) {
		if chi.URLParam(req, "*") == "pump/since" {
			h.HandleQuerySince(w, req)
			return
		}
		h.HandleQueryRange(w, req)
	})

	var rangeResp struct {
		Device string             `json:"device"`
		Series []metricSeriesJSON `json:"series"`
	}
	doJSON(t, r, http.MethodGet, "/metrics/pump?start="+start.Format(time.RFC3339)+"&metrics=temp,flow&max_points=1", nil, http.StatusOK, &rangeResp)
	if rangeResp.Device != "pump" || len(rangeResp.Series) != 2 || len(rangeResp.Series[0].Data) != 1 {
		t.Fatalf("range response = %#v", rangeResp)
	}

	var sinceResp struct {
		Device string             `json:"device"`
		Series []metricSeriesJSON `json:"series"`
	}
	doJSON(t, r, http.MethodGet, "/metrics/pump/since?after="+start.Format(time.RFC3339)+"&start_metric=temp&metrics=temp,flow", nil, http.StatusOK, &sinceResp)
	if sinceResp.Device != "pump" || len(sinceResp.Series) == 0 {
		t.Fatalf("since response = %#v", sinceResp)
	}
	doJSON(t, r, http.MethodGet, "/metrics/pump?metrics=temp", nil, http.StatusBadRequest, nil)
}

func TestNotificationHandlersProfilesAndChannels(t *testing.T) {
	ctx := context.Background()
	db := newWebAPITestDB(t, "web-notifications")
	reloads := 0
	h := NewNotificationHandlers(db, func(*http.Request) string { return "default" }, func(context.Context, string) error {
		reloads++
		return nil
	})
	r := chi.NewRouter()
	r.Get("/profiles", h.HandleListProfiles)
	r.Post("/profiles", h.HandleCreateProfile)
	r.Get("/profiles/{id}", h.HandleGetProfile)
	r.Put("/profiles/{id}", h.HandleUpdateProfile)
	r.Delete("/profiles/{id}", h.HandleDeleteProfile)
	r.Get("/channels", h.HandleGetChannels)
	r.Put("/channels", h.HandleSaveChannels)

	var created sqldb.NotificationProfile
	doJSON(t, r, http.MethodPost, "/profiles", map[string]any{"name": "Ops", "roles": []string{"Operator"}, "ackRequired": true}, http.StatusCreated, &created)
	var listed []sqldb.NotificationProfile
	doJSON(t, r, http.MethodGet, "/profiles", nil, http.StatusOK, &listed)
	if len(listed) == 0 {
		t.Fatal("expected notification profiles")
	}
	var updated sqldb.NotificationProfile
	doJSON(t, r, http.MethodPut, "/profiles/"+strconv.Itoa(created.ID), map[string]any{"name": "Ops", "description": "Updated"}, http.StatusOK, &updated)
	if updated.Description != "Updated" {
		t.Fatalf("updated profile = %#v", updated)
	}
	doJSON(t, r, http.MethodGet, "/profiles/bad", nil, http.StatusBadRequest, nil)

	var cfg notifications.ChannelConfig
	doJSON(t, r, http.MethodPut, "/channels", map[string]any{
		"email":    map[string]any{"host": "smtp.example.test", "port": 2525, "from": "from@example.test"},
		"telegram": map[string]any{"botToken": "token"},
	}, http.StatusOK, &cfg)
	if cfg.Email.Host != "smtp.example.test" || cfg.Telegram.BotToken != "token" || reloads != 1 {
		t.Fatalf("channel cfg=%#v reloads=%d", cfg, reloads)
	}
	var gotCfg notifications.ChannelConfig
	doJSON(t, r, http.MethodGet, "/channels", nil, http.StatusOK, &gotCfg)
	if gotCfg.Email.Host != cfg.Email.Host {
		t.Fatalf("got channel cfg = %#v", gotCfg)
	}
	doJSON(t, r, http.MethodDelete, "/profiles/"+strconv.Itoa(created.ID), nil, http.StatusNoContent, nil)
	_ = ctx
}

func TestReportHandlersCRUDAndValidation(t *testing.T) {
	db := newWebAPITestDB(t, "web-reports")
	h := NewReportHandlers(db, func(context.Context) (string, bool) { return "default", true }, nil)
	r := chi.NewRouter()
	r.Get("/templates", h.HandleListTemplates)
	r.Post("/templates", h.HandleCreateTemplate)
	r.Get("/templates/{id}", h.HandleGetTemplate)
	r.Put("/templates/{id}", h.HandleUpdateTemplate)
	r.Delete("/templates/{id}", h.HandleDeleteTemplate)

	var created sqldb.PDFTemplate
	doJSON(t, r, http.MethodPost, "/templates", map[string]any{"name": "Daily", "templateJson": map[string]any{"pages": []any{}}}, http.StatusCreated, &created)
	if created.ID == "" {
		t.Fatal("created template missing id")
	}
	var got sqldb.PDFTemplate
	doJSON(t, r, http.MethodGet, "/templates/"+created.ID, nil, http.StatusOK, &got)
	if got.Name != "Daily" {
		t.Fatalf("got template = %#v", got)
	}
	doJSON(t, r, http.MethodPut, "/templates/"+created.ID, map[string]any{"name": "Daily Updated", "templateJson": map[string]any{}}, http.StatusOK, nil)
	var listed []sqldb.PDFTemplate
	doJSON(t, r, http.MethodGet, "/templates", nil, http.StatusOK, &listed)
	if len(listed) != 1 {
		t.Fatalf("listed templates = %#v", listed)
	}
	doJSON(t, r, http.MethodGet, "/templates/missing", nil, http.StatusNotFound, nil)
	doJSON(t, r, http.MethodDelete, "/templates/"+created.ID, nil, http.StatusNoContent, nil)
	if sanitizeFilename("Daily Report: Pump/1") != "Daily_Report__Pump_1" {
		t.Fatalf("sanitizeFilename mismatch")
	}
}

func TestTagCalcHandlersCRUDAndTestExpression(t *testing.T) {
	ctx := context.Background()
	db := newWebAPITestDB(t, "web-tagcalcs")
	engine := tagcalcs.New(db, nil)
	h := NewTagCalcHandlers(db, engine, func(*http.Request) string { return "default" })
	r := chi.NewRouter()
	r.Get("/tagcalcs", h.HandleList)
	r.Post("/tagcalcs", h.HandleCreate)
	r.Get("/tagcalcs/{id}", h.HandleGet)
	r.Put("/tagcalcs/{id}", h.HandleUpdate)
	r.Delete("/tagcalcs/{id}", h.HandleDelete)
	r.Post("/tagcalcs/test", h.HandleTest)

	var created sqldb.TagCalc
	doJSON(t, r, http.MethodPost, "/tagcalcs", map[string]any{"name": "Two", "outputTag": "CUSTOM.Two", "expression": "1+1"}, http.StatusCreated, &created)
	if created.IntervalSeconds != 60 {
		t.Fatalf("default interval = %d", created.IntervalSeconds)
	}
	var result map[string]any
	doJSON(t, r, http.MethodPost, "/tagcalcs/test", map[string]any{"expression": "2+2"}, http.StatusOK, &result)
	if result["result"] == nil {
		t.Fatalf("test result = %#v", result)
	}
	var listed []sqldb.TagCalc
	doJSON(t, r, http.MethodGet, "/tagcalcs", nil, http.StatusOK, &listed)
	if len(listed) != 1 {
		t.Fatalf("listed tag calcs = %#v", listed)
	}
	doJSON(t, r, http.MethodPut, "/tagcalcs/"+strconv.Itoa(created.ID), map[string]any{"name": "Three", "outputTag": "CUSTOM.Three", "expression": "3+3"}, http.StatusOK, nil)
	doJSON(t, r, http.MethodGet, "/tagcalcs/bad", nil, http.StatusBadRequest, nil)
	doJSON(t, r, http.MethodDelete, "/tagcalcs/"+strconv.Itoa(created.ID), nil, http.StatusNoContent, nil)
	if got, err := db.ListTagCalcs(ctx, "default"); err != nil || len(got) != 0 {
		t.Fatalf("remaining tag calcs = %#v err=%v", got, err)
	}
}

func TestUserHandlersCRUDVisibilityAndRoles(t *testing.T) {
	ctx := context.Background()
	db := newWebAPITestDB(t, "web-users")
	if err := db.CreateOrganisation(ctx, &sqldb.Organisation{Name: "plant", DisplayName: "Plant", Active: true}); err != nil {
		t.Fatalf("CreateOrganisation: %v", err)
	}
	h := NewUserHandlers(db)
	h.CurrentOrgName = func(*http.Request) string { return "plant" }
	h.CanAccessOrg = func(_ *http.Request, org string) bool { return org == "plant" }
	h.IsSystemAdmin = func(*http.Request) bool { return false }
	h.AllowedOrgSet = func(*http.Request) (map[string]bool, bool) { return map[string]bool{"plant": true}, false }

	r := chi.NewRouter()
	r.Get("/users", h.HandleListUsers)
	r.Post("/users", h.HandleCreateUser)
	r.Get("/users/{id}", h.HandleGetUser)
	r.Put("/users/{id}", h.HandleUpdateUser)
	r.Post("/users/{id}/reset-password", h.HandleResetPassword)
	r.Get("/roles", h.HandleListRoles)

	doJSON(t, r, http.MethodPost, "/users", map[string]any{"loginName": "root", "email": "root@example.test", "password": "pw", "roles": []string{"SystemAdmin"}}, http.StatusForbidden, nil)
	var user sqldb.User
	doJSON(t, r, http.MethodPost, "/users", map[string]any{"firstName": "Op", "loginName": "op", "email": "op@example.test", "password": "pw", "roles": []string{"Operator"}}, http.StatusCreated, &user)
	if len(user.Orgs) != 1 || user.Orgs[0].OrgName != "plant" {
		t.Fatalf("created user = %#v", user)
	}
	var listed []sqldb.User
	doJSON(t, r, http.MethodGet, "/users", nil, http.StatusOK, &listed)
	if len(listed) != 1 {
		t.Fatalf("listed users = %#v", listed)
	}
	active := false
	var updated sqldb.User
	doJSON(t, r, http.MethodPut, "/users/"+strconv.Itoa(user.ID), map[string]any{"lastName": "One", "active": active, "roles": []string{"Manager"}}, http.StatusOK, &updated)
	if updated.Active || updated.LastName != "One" || updated.Orgs[0].Roles[0] != "Manager" {
		t.Fatalf("updated user = %#v", updated)
	}
	var reset resetPasswordResponse
	doJSON(t, r, http.MethodPost, "/users/"+strconv.Itoa(user.ID)+"/reset-password", nil, http.StatusOK, &reset)
	if reset.Password == "" {
		t.Fatal("reset password response missing password")
	}
	var roles []sqldb.Role
	doJSON(t, r, http.MethodGet, "/roles", nil, http.StatusOK, &roles)
	if len(roles) == 0 {
		t.Fatal("expected roles")
	}

	hidden := &sqldb.User{LoginName: "hidden", Email: "hidden@example.test", Active: true}
	hash, _ := sqlite.HashPassword("pw")
	if err := db.CreateUser(ctx, hidden, hash); err != nil {
		t.Fatalf("Create hidden user: %v", err)
	}
	if err := db.AssignUserToOrg(ctx, hidden.ID, "default", []string{"User"}); err != nil {
		t.Fatalf("Assign hidden user: %v", err)
	}
	doJSON(t, r, http.MethodGet, "/users/"+strconv.Itoa(hidden.ID), nil, http.StatusNotFound, nil)
	doJSON(t, r, http.MethodGet, "/users/bad", nil, http.StatusBadRequest, nil)
}

func TestMetricSeriesJSONDownsampling(t *testing.T) {
	now := time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC)
	series := toJSONSeries([]sqldb.MetricSeries{{Name: "x", Data: []sqldb.MetricPoint{
		{Timestamp: now, Value: 1}, {Timestamp: now.Add(time.Second), Value: 2}, {Timestamp: now.Add(2 * time.Second), Value: 3},
	}}}, 2)
	raw, _ := json.Marshal(series)
	if len(series) != 1 || len(series[0].Data) != 2 || len(raw) == 0 {
		t.Fatalf("series = %#v raw=%s", series, raw)
	}
}

func TestLogHandlersQueryValidationAndUserNames(t *testing.T) {
	ctx := context.Background()
	db := newWebAPITestDB(t, "web-logs")
	hash, _ := sqlite.HashPassword("pw")
	user := &sqldb.User{FirstName: "Log", LastName: "User", LoginName: "logger", Email: "logger@example.test", Active: true}
	if err := db.CreateUser(ctx, user, hash); err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	uid := user.ID
	now := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	if err := db.InsertEventEntries(ctx, []events.EventEntry{{Timestamp: now, OrgName: "default", UserID: &uid, Severity: string(events.Warn), Device: "pump", Message: "hot"}}); err != nil {
		t.Fatalf("InsertEventEntries: %v", err)
	}
	h := NewLogHandlers(db, 5)
	h.GetTenantID = func(*http.Request) string { return "default" }
	h.CanAccessOrg = func(_ *http.Request, org string) bool { return org == "default" }
	h.IsSystemAdmin = func(*http.Request) bool { return false }
	r := chi.NewRouter()
	r.Get("/logs", h.HandleQueryLogs)
	r.Post("/logs", h.HandleCreateLog)

	var entries []events.EventEntry
	doJSON(t, r, http.MethodGet, "/logs?severity=WARN&device=pump&limit=99", nil, http.StatusOK, &entries)
	if len(entries) != 1 || entries[0].UserName != "Log User" {
		t.Fatalf("entries = %#v", entries)
	}
	doJSON(t, r, http.MethodGet, "/logs?user_id=bad", nil, http.StatusBadRequest, nil)
	doJSON(t, r, http.MethodGet, "/logs?org_name=other", nil, http.StatusForbidden, nil)
	doJSON(t, r, http.MethodPost, "/logs", map[string]any{"message": "x"}, http.StatusServiceUnavailable, nil)
}

func TestLogHandlersCreateValidation(t *testing.T) {
	db := newWebAPITestDB(t, "web-logs-create")
	h := NewLogHandlers(db, 100)
	h.GetTenantID = func(*http.Request) string { return "default" }
	h.GetUserID = func(*http.Request) int { return 7 }
	h.CanAccessOrg = func(_ *http.Request, org string) bool { return org == "default" }

	r := chi.NewRouter()
	r.Post("/logs", h.HandleCreateLog)

	doJSON(t, r, http.MethodPost, "/logs", map[string]any{"message": "pump started"}, http.StatusServiceUnavailable, nil)

	h.Publisher = &events.Publisher{}
	doJSON(t, r, http.MethodPost, "/logs", nil, http.StatusBadRequest, nil)
	doJSON(t, r, http.MethodPost, "/logs", map[string]any{"severity": "info"}, http.StatusBadRequest, nil)
	doJSON(t, r, http.MethodPost, "/logs", map[string]any{"message": "pump started", "severity": "bogus"}, http.StatusBadRequest, nil)
	doJSON(t, r, http.MethodPost, "/logs", map[string]any{"message": "pump started", "orgName": "other"}, http.StatusForbidden, nil)
}

func TestScheduleHandlersCRUDRunNowAndHistory(t *testing.T) {
	ctx := context.Background()
	db := newWebAPITestDB(t, "web-schedules")
	t.Setenv("SCHEDULER_OUTPUT_DIR", t.TempDir())
	engine := scheduler.NewWithOptions(db, nil, scheduler.EngineOptions{AllowUnsafeTasks: true})
	h := NewScheduleHandlers(db, engine, func(*http.Request) string { return "default" })
	r := chi.NewRouter()
	r.Get("/schedules", h.HandleList)
	r.Post("/schedules", h.HandleCreate)
	r.Get("/schedules/{id}", h.HandleGet)
	r.Put("/schedules/{id}", h.HandleUpdate)
	r.Delete("/schedules/{id}", h.HandleDelete)
	r.Post("/schedules/{id}/run", h.HandleRunNow)
	r.Get("/schedules/{id}/history", h.HandleHistory)

	doJSON(t, r, http.MethodPost, "/schedules", map[string]any{"name": "Bad", "taskType": "shell", "schedule": "bad"}, http.StatusBadRequest, nil)
	var created sqldb.ScheduledTask
	doJSON(t, r, http.MethodPost, "/schedules", map[string]any{
		"name": "Noop", "taskType": "shell", "schedule": "0 0 * * *", "taskConfig": map[string]any{"command": "true"}, "enabled": false,
	}, http.StatusCreated, &created)
	if created.ID == "" {
		t.Fatal("created schedule missing id")
	}
	var got sqldb.ScheduledTask
	doJSON(t, r, http.MethodGet, "/schedules/"+created.ID, nil, http.StatusOK, &got)
	if got.Name != "Noop" {
		t.Fatalf("got schedule = %#v", got)
	}
	doJSON(t, r, http.MethodPut, "/schedules/"+created.ID, map[string]any{
		"name": "Noop Updated", "taskType": "shell", "schedule": "0 0 * * *", "taskConfig": map[string]any{"command": "true"}, "enabled": false,
	}, http.StatusOK, nil)
	var runResp map[string]any
	doJSON(t, r, http.MethodPost, "/schedules/"+created.ID+"/run", nil, http.StatusOK, &runResp)
	if _, ok := runResp["outputPath"]; !ok {
		t.Fatalf("run response = %#v", runResp)
	}
	var history []sqldb.ScheduleRunLog
	doJSON(t, r, http.MethodGet, "/schedules/"+created.ID+"/history", nil, http.StatusOK, &history)
	if len(history) != 1 || history[0].Status != "ok" {
		t.Fatalf("history = %#v", history)
	}
	var listed []sqldb.ScheduledTask
	doJSON(t, r, http.MethodGet, "/schedules", nil, http.StatusOK, &listed)
	if len(listed) != 1 {
		t.Fatalf("listed schedules = %#v", listed)
	}
	doJSON(t, r, http.MethodGet, "/schedules/missing", nil, http.StatusNotFound, nil)
	doJSON(t, r, http.MethodDelete, "/schedules/"+created.ID, nil, http.StatusNoContent, nil)
	if tasks, err := db.ListScheduledTasks(ctx, "default"); err != nil || len(tasks) != 0 {
		t.Fatalf("remaining tasks = %#v err=%v", tasks, err)
	}
}
