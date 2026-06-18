package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/xact-iot/xact/backups"
	"github.com/xact-iot/xact/events"
	"github.com/xact-iot/xact/rtdb/tree"
	"github.com/xact-iot/xact/sqldb"
	"github.com/xact-iot/xact/sqldb/psql"
	"golang.org/x/crypto/bcrypt"
)

// testDB is a minimal mock of sqldb.DB used by login tests.
type testDB struct {
	user          *sqldb.User
	hash          string
	userOrgs      []sqldb.UserOrg
	organisations []sqldb.Organisation
	authActive    bool
	authVersion   int
}

func newTestDB(loginName, password string) *testDB {
	h, _ := bcrypt.GenerateFromPassword([]byte(password), bcrypt.MinCost)
	return &testDB{
		user: &sqldb.User{
			ID: 1, LoginName: loginName, Active: true, TokenVersion: 1,
			Orgs: []sqldb.UserOrg{{OrgID: 1, OrgName: "default", Roles: []string{"Admin"}}},
		},
		hash: string(h),
	}
}

func newRoleTestDB(loginName, password, role string) *testDB {
	db := newTestDB(loginName, password)
	db.user.Orgs = []sqldb.UserOrg{{OrgID: 1, OrgName: "default", Roles: []string{role}}}
	return db
}

func newUnsetAdminTestDB() *testDB {
	return &testDB{
		user: &sqldb.User{
			ID: 1, LoginName: "admin", Active: true, TokenVersion: 1,
			Orgs: []sqldb.UserOrg{{OrgID: 1, OrgName: "default", Roles: []string{"SystemAdmin"}}},
		},
		hash: sqldb.UnsetBootstrapAdminHash,
	}
}

func (d *testDB) GetUserByLogin(_ context.Context, login string) (*sqldb.User, string, error) {
	if d.user != nil && (d.user.LoginName == login || d.user.Email == login) {
		return d.user, d.hash, nil
	}
	return nil, "", nil
}
func (d *testDB) UpdateLastLogin(_ context.Context, _ int) error { return nil }

// Stubs for the rest of the interface
func (d *testDB) ListDashboards(_ context.Context, _ string) ([]sqldb.DashboardMeta, error) {
	return nil, nil
}
func (d *testDB) GetDashboard(_ context.Context, _ string, _ int) (*sqldb.Dashboard, error) {
	return nil, nil
}
func (d *testDB) CreateDashboard(_ context.Context, _ string, _ *sqldb.Dashboard) error { return nil }
func (d *testDB) UpdateDashboard(_ context.Context, _ string, _ int, _ *sqldb.Dashboard) error {
	return nil
}
func (d *testDB) DeleteDashboard(_ context.Context, _ string, _ int) error { return nil }
func (d *testDB) ListPermissions(_ context.Context, _ string) ([]sqldb.RolePermissions, error) {
	return []sqldb.RolePermissions{{
		Role: "Admin",
		UI: json.RawMessage(`{
			"nodes":{"read":true,"write":true},
			"tags":{"read":true,"write":true},
			"organisations":{"view":true,"change":true},
			"users":{"view":true,"manage":true},
			"logs":{"read":true},
			"profile":{"change":true}
		}`),
	}}, nil
}
func (d *testDB) GetPermissions(_ context.Context, _, _ string) (*sqldb.RolePermissions, error) {
	return nil, nil
}
func (d *testDB) UpdatePermissions(_ context.Context, _, _ string, _ *sqldb.RolePermissions) error {
	return nil
}
func (d *testDB) SaveConfig(_ context.Context, _, _ string, _ json.RawMessage) error { return nil }
func (d *testDB) LoadConfig(_ context.Context, _, _ string) (json.RawMessage, error) { return nil, nil }
func (d *testDB) InsertEventEntries(_ context.Context, _ []events.EventEntry) error  { return nil }
func (d *testDB) QueryEvents(_ context.Context, _ sqldb.EventFilter) ([]events.EventEntry, error) {
	return nil, nil
}
func (d *testDB) PurgeEventsBefore(_ context.Context, _ time.Time) error      { return nil }
func (d *testDB) Migrate(_ context.Context) error                             { return nil }
func (d *testDB) Close()                                                      {}
func (d *testDB) ListRoles(_ context.Context) ([]sqldb.Role, error)           { return nil, nil }
func (d *testDB) ListUsers(_ context.Context) ([]sqldb.User, error)           { return nil, nil }
func (d *testDB) GetUserByID(_ context.Context, _ int) (*sqldb.User, error)   { return nil, nil }
func (d *testDB) CreateUser(_ context.Context, _ *sqldb.User, _ string) error { return nil }
func (d *testDB) UpdateUser(_ context.Context, _ *sqldb.User) error           { return nil }
func (d *testDB) SetUserPassword(_ context.Context, id int, passwordHash string) error {
	if d.user != nil && d.user.ID == id {
		d.hash = passwordHash
		d.user.TokenVersion++
	}
	return nil
}
func (d *testDB) GetUserAuthState(_ context.Context, _ int) (bool, int, error) {
	if d.authVersion != 0 {
		return d.authActive, d.authVersion, nil
	}
	return true, 1, nil
}
func (d *testDB) BumpUserTokenVersion(_ context.Context, _ int) error { return nil }
func (d *testDB) GetUserOrgs(_ context.Context, _ int) ([]sqldb.UserOrg, error) {
	if d.userOrgs != nil {
		return d.userOrgs, nil
	}
	return nil, nil
}
func (d *testDB) AddUserToOrg(_ context.Context, _, _ int) error                       { return nil }
func (d *testDB) AssignRoleToUser(_ context.Context, _, _ int, _ string) error         { return nil }
func (d *testDB) RemoveRoleFromUser(_ context.Context, _, _ int, _ string) error       { return nil }
func (d *testDB) AssignUserToOrg(_ context.Context, _ int, _ string, _ []string) error { return nil }
func (d *testDB) SetUserOrgRoles(_ context.Context, _ int, _ string, _ []string) error { return nil }
func (d *testDB) ListOrganisations(_ context.Context) ([]sqldb.Organisation, error) {
	if d.organisations != nil {
		return d.organisations, nil
	}
	return []sqldb.Organisation{{ID: 1, Name: "default", DisplayName: "Default"}}, nil
}
func (d *testDB) GetOrganisation(_ context.Context, name string) (*sqldb.Organisation, error) {
	for _, org := range d.organisations {
		if org.Name == name {
			copy := org
			return &copy, nil
		}
	}
	return nil, nil
}
func (d *testDB) CreateOrganisation(_ context.Context, _ *sqldb.Organisation) error { return nil }
func (d *testDB) UpdateOrganisation(_ context.Context, _ string, _ *sqldb.Organisation) error {
	return nil
}
func (d *testDB) DeleteOrganisation(_ context.Context, _ string) error                   { return nil }
func (d *testDB) InsertMetrics(_ context.Context, _ string, _ []sqldb.MetricEntry) error { return nil }
func (d *testDB) QueryMetricsByTagPaths(_ context.Context, _ string, _ []string, _, _ time.Time) ([]sqldb.MetricSeries, error) {
	return nil, nil
}
func (d *testDB) QueryMetricsRange(_ context.Context, _, _ string, _ []string, _, _ time.Time) ([]sqldb.MetricSeries, error) {
	return nil, nil
}
func (d *testDB) QueryMetricsSince(_ context.Context, _, _ string, _ []string, _ string, _ time.Time) ([]sqldb.MetricSeries, error) {
	return nil, nil
}
func (d *testDB) ConfigureMetricsRetention(_ context.Context, _ time.Duration) error { return nil }
func (d *testDB) ListAPIKeys(_ context.Context, _ string) ([]sqldb.APIKey, error)    { return nil, nil }
func (d *testDB) CreateAPIKey(_ context.Context, _, _ string) (*sqldb.APIKey, error) { return nil, nil }
func (d *testDB) DeleteAPIKey(_ context.Context, _ string, _ int) error              { return nil }
func (d *testDB) GetAPIKeyOrg(_ context.Context, _ string) (string, error)           { return "", nil }
func (d *testDB) ListPDFTemplates(_ context.Context, _ string) ([]sqldb.PDFTemplate, error) {
	return nil, nil
}
func (d *testDB) GetPDFTemplate(_ context.Context, _, _ string) (*sqldb.PDFTemplate, error) {
	return nil, nil
}
func (d *testDB) CreatePDFTemplate(_ context.Context, _ string, _ *sqldb.PDFTemplate) error {
	return nil
}
func (d *testDB) UpdatePDFTemplate(_ context.Context, _, _ string, _ *sqldb.PDFTemplate) error {
	return nil
}
func (d *testDB) DeletePDFTemplate(_ context.Context, _, _ string) error { return nil }
func (d *testDB) ListNotificationProfiles(_ context.Context, _ string) ([]sqldb.NotificationProfile, error) {
	return nil, nil
}
func (d *testDB) GetNotificationProfile(_ context.Context, _ string, _ int) (*sqldb.NotificationProfile, error) {
	return nil, nil
}
func (d *testDB) GetNotificationProfileByName(_ context.Context, _, _ string) (*sqldb.NotificationProfile, error) {
	return nil, nil
}
func (d *testDB) ResolveNotificationID(_ context.Context, _, _ string) (int, error) { return 0, nil }
func (d *testDB) CreateNotificationProfile(_ context.Context, _ string, _ *sqldb.NotificationProfile) error {
	return nil
}
func (d *testDB) UpdateNotificationProfile(_ context.Context, _ string, _ int, _ *sqldb.NotificationProfile) error {
	return nil
}
func (d *testDB) DeleteNotificationProfile(_ context.Context, _ string, _ int) error { return nil }
func (d *testDB) GetNotificationRecipients(_ context.Context, _ string, _ int) ([]sqldb.NotificationRecipient, error) {
	return nil, nil
}
func (d *testDB) ListTagCalcs(_ context.Context, _ string) ([]sqldb.TagCalc, error) { return nil, nil }
func (d *testDB) GetTagCalc(_ context.Context, _ string, _ int) (*sqldb.TagCalc, error) {
	return nil, nil
}
func (d *testDB) CreateTagCalc(_ context.Context, _ string, _ *sqldb.TagCalc) error { return nil }
func (d *testDB) UpdateTagCalc(_ context.Context, _ string, _ int, _ *sqldb.TagCalc) error {
	return nil
}
func (d *testDB) DeleteTagCalc(_ context.Context, _ string, _ int) error { return nil }
func (d *testDB) ListScheduledTasks(_ context.Context, _ string) ([]sqldb.ScheduledTask, error) {
	return nil, nil
}
func (d *testDB) GetScheduledTask(_ context.Context, _, _ string) (*sqldb.ScheduledTask, error) {
	return nil, nil
}
func (d *testDB) CreateScheduledTask(_ context.Context, _ string, _ *sqldb.ScheduledTask) error {
	return nil
}
func (d *testDB) UpdateScheduledTask(_ context.Context, _, _ string, _ *sqldb.ScheduledTask) error {
	return nil
}
func (d *testDB) DeleteScheduledTask(_ context.Context, _, _ string) error { return nil }
func (d *testDB) UpdateScheduledTaskStatus(_ context.Context, _, _, _ string, _ time.Time) error {
	return nil
}
func (d *testDB) AppendScheduleRunLog(_ context.Context, _ *sqldb.ScheduleRunLog) error {
	return nil
}
func (d *testDB) UpdateScheduleRunLog(_ context.Context, _ int, _ time.Time, _, _, _ string) error {
	return nil
}
func (d *testDB) ListScheduleRunLog(_ context.Context, _ string, _ int) ([]sqldb.ScheduleRunLog, error) {
	return nil, nil
}
func (d *testDB) BackupAdapter() backups.Adapter { return nil }

func TestJWTAuth(t *testing.T) {
	secret := []byte("test-secret")

	tests := []struct {
		name       string
		authHeader string
		wantStatus int
	}{
		{
			name:       "valid token",
			authHeader: "Bearer " + generateTestToken(secret, "user1"),
			wantStatus: http.StatusOK,
		},
		{
			name:       "missing auth header",
			authHeader: "",
			wantStatus: http.StatusUnauthorized,
		},
		{
			name:       "invalid format",
			authHeader: "InvalidFormat",
			wantStatus: http.StatusUnauthorized,
		},
		{
			name:       "invalid token",
			authHeader: "Bearer invalid.token.here",
			wantStatus: http.StatusUnauthorized,
		},
	}

	handler := JWTAuth(secret)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest("GET", "/test", nil)
			if tt.authHeader != "" {
				req.Header.Set("Authorization", tt.authHeader)
			}
			rr := httptest.NewRecorder()
			handler.ServeHTTP(rr, req)

			if rr.Code != tt.wantStatus {
				t.Errorf("expected status %d, got %d", tt.wantStatus, rr.Code)
			}
		})
	}
}

func TestCreateNode(t *testing.T) {
	treeOps := tree.NewTreeWithOperations(nil)
	server := NewServer(ServerConfig{}, treeOps, nil, nil, "test-secret", nil, "")

	// Generate valid JWT token (TenantID = "tenant1")
	token := generateTestToken([]byte("test-secret"), "user1")

	reqBody := CreateNodeRequest{
		Path:        "building/floor1",
		Description: "Floor 1",
	}
	body, _ := json.Marshal(reqBody)

	req := httptest.NewRequest("POST", "/api/v1/nodes/", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+token)
	rr := httptest.NewRecorder()

	server.Router().ServeHTTP(rr, req)

	if rr.Code != http.StatusCreated {
		t.Errorf("expected status %d, got %d", http.StatusCreated, rr.Code)
	}

	var response CreateNodeResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &response); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}

	if response.Path != "tenant1/building/floor1" {
		t.Errorf("expected path tenant1/building/floor1, got %s", response.Path)
	}
}

func TestCreateNodeScopesRelativeBodyPathToTenant(t *testing.T) {
	treeOps := tree.NewTreeWithOperations(nil)
	server := NewServer(ServerConfig{}, treeOps, nil, nil, "test-secret", nil, "")
	token := generateTestToken([]byte("test-secret"), "1")

	body, _ := json.Marshal(CreateNodeRequest{Path: "building/floor2"})
	req := httptest.NewRequest("POST", "/api/v1/nodes/", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+token)
	rr := httptest.NewRecorder()

	server.Router().ServeHTTP(rr, req)

	if rr.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d; body: %s", rr.Code, http.StatusCreated, rr.Body.String())
	}
	if _, err := treeOps.FindNode("tenant1/building/floor2"); err != nil {
		t.Fatalf("relative path was not scoped to tenant1: %v", err)
	}
}

func TestGetNode(t *testing.T) {
	treeOps := tree.NewTreeWithOperations(nil)
	// Create test nodes under the org scope (tenant1)
	treeOps.CreateNode("tenant1/building/floor1", "")

	server := NewServer(ServerConfig{}, treeOps, nil, nil, "test-secret", nil, "")
	token := generateTestToken([]byte("test-secret"), "user1")

	req := httptest.NewRequest("GET", "/api/v1/nodes/tenant1/building/floor1", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rr := httptest.NewRecorder()

	server.Router().ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("expected status %d, got %d", http.StatusOK, rr.Code)
	}

	var response NodeResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &response); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}

	if response.Name != "floor1" {
		t.Errorf("expected name floor1, got %s", response.Name)
	}
}

func TestDeleteNode(t *testing.T) {
	treeOps := tree.NewTreeWithOperations(nil)
	treeOps.CreateNode("tenant1/building/floor1", "")
	treeOps.UnlockNode("tenant1/building")

	server := NewServer(ServerConfig{}, treeOps, nil, nil, "test-secret", nil, "")
	token := generateTestToken([]byte("test-secret"), "user1")

	req := httptest.NewRequest("DELETE", "/api/v1/nodes/tenant1/building/floor1", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rr := httptest.NewRecorder()

	server.Router().ServeHTTP(rr, req)

	if rr.Code != http.StatusNoContent {
		t.Errorf("expected status %d, got %d", http.StatusNoContent, rr.Code)
	}

	// Verify node is deleted
	_, err := treeOps.FindNode("tenant1/building/floor1")
	if err == nil {
		t.Error("expected node to be deleted")
	}
}

func TestCreateTag(t *testing.T) {
	treeOps := tree.NewTreeWithOperations(nil)
	server := NewServer(ServerConfig{}, treeOps, nil, nil, "test-secret", nil, "")
	token := generateTestToken([]byte("test-secret"), "user1")

	reqBody := CreateTagRequest{
		Path: "tenant1/building/floor1/temperature",
		Type: tree.TypeFloat,
		Config: tree.TagConfig{
			Name: "Temperature",
		},
		Value: 22.5,
	}
	body, _ := json.Marshal(reqBody)

	req := httptest.NewRequest("POST", "/api/v1/tags/", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+token)
	rr := httptest.NewRecorder()

	server.Router().ServeHTTP(rr, req)

	if rr.Code != http.StatusCreated {
		t.Errorf("expected status %d, got %d", http.StatusCreated, rr.Code)
	}

	var response TagResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &response); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}

	if response.Name != "temperature" {
		t.Errorf("expected name temperature, got %s", response.Name)
	}

	if response.Value != 22.5 {
		t.Errorf("expected value 22.5, got %v", response.Value)
	}
	if len(response.Shared.Pipeline) != 1 || response.Shared.Pipeline[0].Type != "publish" {
		t.Fatalf("expected default publish pipeline, got %#v", response.Shared.Pipeline)
	}
}

func TestCreateTemplateTagPropagatesToDeviceInstances(t *testing.T) {
	treeOps := tree.NewTreeWithOperations(nil)
	server := NewServer(ServerConfig{}, treeOps, nil, nil, "test-secret", nil, "")
	token := generateTestToken([]byte("test-secret"), "user1")

	if err := treeOps.CreateDeviceNode("tenant1.AirQuality.AQ001", "Templates.AirQualityStandard"); err != nil {
		t.Fatalf("create AQ001: %v", err)
	}
	if err := treeOps.CreateDeviceNode("tenant1.AirQuality.AQ002", "Templates.AirQualityStandard"); err != nil {
		t.Fatalf("create AQ002: %v", err)
	}

	body, _ := json.Marshal(CreateTagRequest{
		Path: "tenant1/Templates/AirQualityStandard/metrics/pm25",
		Type: tree.TypeFloat,
		Shared: tree.TagShared{
			Description: "Template PM2.5",
			Units:       "ug/m3",
		},
	})
	req := httptest.NewRequest("POST", "/api/v1/tags/", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+token)
	rr := httptest.NewRecorder()
	server.Router().ServeHTTP(rr, req)

	if rr.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d; body: %s", rr.Code, http.StatusCreated, rr.Body.String())
	}
	tmplLeaf, err := treeOps.FindLeaf("tenant1.Templates.AirQualityStandard.metrics.pm25")
	if err != nil {
		t.Fatalf("template leaf: %v", err)
	}
	for _, path := range []string{
		"tenant1.AirQuality.AQ001.metrics.pm25",
		"tenant1.AirQuality.AQ002.metrics.pm25",
	} {
		leaf, err := treeOps.FindLeaf(path)
		if err != nil {
			t.Fatalf("instance leaf %s: %v", path, err)
		}
		if leaf.GetTemplate() != tmplLeaf {
			t.Fatalf("%s template pointer not linked", path)
		}
		if leaf.GetConfig().TemplateName != "Templates.AirQualityStandard" {
			t.Fatalf("%s templateName = %q", path, leaf.GetConfig().TemplateName)
		}
		if leaf.GetDescription() != "Template PM2.5" {
			t.Fatalf("%s description = %q", path, leaf.GetDescription())
		}
	}
}

func TestDeleteTemplateTagPropagatesToDeviceInstances(t *testing.T) {
	treeOps := tree.NewTreeWithOperations(nil)
	server := NewServer(ServerConfig{}, treeOps, nil, nil, "test-secret", nil, "")
	token := generateTestToken([]byte("test-secret"), "user1")

	for _, path := range []string{
		"tenant1.AirQuality.AQ001",
		"tenant1.AirQuality.AQ002",
	} {
		if err := treeOps.CreateDeviceNode(path, "Templates.AirQualityStandard"); err != nil {
			t.Fatalf("create device %s: %v", path, err)
		}
	}

	body, _ := json.Marshal(CreateTagRequest{
		Path: "tenant1/Templates/AirQualityStandard/metrics/pm25",
		Type: tree.TypeFloat,
	})
	createReq := httptest.NewRequest("POST", "/api/v1/tags/", bytes.NewReader(body))
	createReq.Header.Set("Authorization", "Bearer "+token)
	createRR := httptest.NewRecorder()
	server.Router().ServeHTTP(createRR, createReq)
	if createRR.Code != http.StatusCreated {
		t.Fatalf("create status = %d, want %d; body: %s", createRR.Code, http.StatusCreated, createRR.Body.String())
	}

	req := httptest.NewRequest("DELETE", "/api/v1/tags/tenant1/Templates/AirQualityStandard/metrics/pm25", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rr := httptest.NewRecorder()
	server.Router().ServeHTTP(rr, req)
	if rr.Code != http.StatusNoContent {
		t.Fatalf("delete status = %d, want %d; body: %s", rr.Code, http.StatusNoContent, rr.Body.String())
	}
	for _, path := range []string{
		"tenant1.Templates.AirQualityStandard.metrics.pm25",
		"tenant1.AirQuality.AQ001.metrics.pm25",
		"tenant1.AirQuality.AQ002.metrics.pm25",
	} {
		if _, err := treeOps.FindLeaf(path); err == nil {
			t.Fatalf("%s still exists after template tag delete", path)
		}
	}
}

func TestDeleteTemplateNodePropagatesToDeviceInstances(t *testing.T) {
	treeOps := tree.NewTreeWithOperations(nil)
	server := NewServer(ServerConfig{}, treeOps, nil, nil, "test-secret", nil, "")
	token := generateTestToken([]byte("test-secret"), "user1")

	for _, path := range []string{
		"tenant1.AirQuality.AQ001",
		"tenant1.AirQuality.AQ002",
	} {
		if err := treeOps.CreateDeviceNode(path, "Templates.AirQualityStandard"); err != nil {
			t.Fatalf("create device %s: %v", path, err)
		}
	}
	for _, path := range []string{
		"tenant1.Templates.AirQualityStandard.metrics.pm25",
		"tenant1.Templates.AirQualityStandard.metrics.temp",
	} {
		if err := treeOps.CreateTag(path, tree.TypeFloat, tree.TagConfig{}, tree.TagShared{}); err != nil {
			t.Fatalf("create template tag %s: %v", path, err)
		}
		if propagated := treeOps.PropagateTemplateTag(path); len(propagated) != 2 {
			t.Fatalf("propagated %s len = %d, want 2: %#v", path, len(propagated), propagated)
		}
	}

	req := httptest.NewRequest("DELETE", "/api/v1/nodes/tenant1/Templates/AirQualityStandard/metrics", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rr := httptest.NewRecorder()
	server.Router().ServeHTTP(rr, req)
	if rr.Code != http.StatusNoContent {
		t.Fatalf("delete status = %d, want %d; body: %s", rr.Code, http.StatusNoContent, rr.Body.String())
	}
	for _, path := range []string{
		"tenant1.Templates.AirQualityStandard.metrics",
		"tenant1.AirQuality.AQ001.metrics",
		"tenant1.AirQuality.AQ002.metrics",
	} {
		if _, err := treeOps.FindNode(path); err == nil {
			t.Fatalf("%s still exists after template node delete", path)
		}
	}
}

func TestCreateTagScopesRelativeBodyPathToTenant(t *testing.T) {
	treeOps := tree.NewTreeWithOperations(nil)
	server := NewServer(ServerConfig{}, treeOps, nil, nil, "test-secret", nil, "")
	token := generateTestToken([]byte("test-secret"), "1")

	body, _ := json.Marshal(CreateTagRequest{
		Path: "building/humidity",
		Type: tree.TypeFloat,
	})
	req := httptest.NewRequest("POST", "/api/v1/tags/", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+token)
	rr := httptest.NewRecorder()

	server.Router().ServeHTTP(rr, req)

	if rr.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d; body: %s", rr.Code, http.StatusCreated, rr.Body.String())
	}
	if _, err := treeOps.FindLeaf("tenant1/building/humidity"); err != nil {
		t.Fatalf("relative path was not scoped to tenant1: %v", err)
	}
}

func TestGetTag(t *testing.T) {
	treeOps := tree.NewTreeWithOperations(nil)
	treeOps.CreateNode("tenant1/building", "")
	treeOps.UnlockNode("tenant1/building")
	treeOps.CreateTag("tenant1/building/temperature", tree.TypeFloat, tree.TagConfig{Name: "Temp"})

	// Set value
	leaf, _ := treeOps.FindLeaf("tenant1/building/temperature")
	leaf.SetFloat(25.5)
	leaf.SetUpdatedTime(time.UnixMilli(1700000000000))

	server := NewServer(ServerConfig{}, treeOps, nil, nil, "test-secret", nil, "")
	token := generateTestToken([]byte("test-secret"), "user1")

	req := httptest.NewRequest("GET", "/api/v1/tags/tenant1/building/temperature", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rr := httptest.NewRecorder()

	server.Router().ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("expected status %d, got %d", http.StatusOK, rr.Code)
	}

	var response TagResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &response); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}

	if response.Value != 25.5 {
		t.Errorf("expected value 25.5, got %v", response.Value)
	}
	if response.Status != "" {
		t.Errorf("expected normal empty status, got %q", response.Status)
	}
	if response.Timestamp != 1700000000000 {
		t.Errorf("expected timestamp 1700000000000, got %d", response.Timestamp)
	}
}

func TestHealthEndpoint(t *testing.T) {
	treeOps := tree.NewTreeWithOperations(nil)
	server := NewServer(ServerConfig{AppVersion: "test-version"}, treeOps, nil, nil, "test-secret", nil, "")

	req := httptest.NewRequest("GET", "/health", nil)
	rr := httptest.NewRecorder()

	server.Router().ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("expected status %d, got %d", http.StatusOK, rr.Code)
	}

	var response map[string]interface{}
	if err := json.Unmarshal(rr.Body.Bytes(), &response); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}

	if response["status"] != "healthy" {
		t.Errorf("expected status healthy, got %v", response["status"])
	}
	if response["appVersion"] != "test-version" {
		t.Errorf("expected appVersion test-version, got %v", response["appVersion"])
	}
	if response["goVersion"] != runtime.Version() {
		t.Errorf("expected goVersion %s, got %v", runtime.Version(), response["goVersion"])
	}
}

func TestLoginEndpoint(t *testing.T) {
	treeOps := tree.NewTreeWithOperations(nil)

	t.Run("no database returns 503", func(t *testing.T) {
		srv := NewServer(ServerConfig{}, treeOps, nil, nil, "test-secret", nil, "")
		req := httptest.NewRequest("POST", "/login",
			bytes.NewReader([]byte(`{"username":"u","password":"p"}`)))
		req.Header.Set("Content-Type", "application/json")
		rr := httptest.NewRecorder()
		srv.Router().ServeHTTP(rr, req)
		if rr.Code != http.StatusServiceUnavailable {
			t.Errorf("expected 503, got %d", rr.Code)
		}
	})

	t.Run("missing username returns 400", func(t *testing.T) {
		srv := NewServer(ServerConfig{}, treeOps, nil, nil, "test-secret", newTestDB("testuser", "pass"), "")
		req := httptest.NewRequest("POST", "/login",
			bytes.NewReader([]byte(`{"password":"pass"}`)))
		rr := httptest.NewRecorder()
		srv.Router().ServeHTTP(rr, req)
		if rr.Code != http.StatusBadRequest {
			t.Errorf("expected 400, got %d", rr.Code)
		}
	})

	t.Run("wrong password returns 401", func(t *testing.T) {
		srv := NewServer(ServerConfig{}, treeOps, nil, nil, "test-secret", newTestDB("testuser", "correct"), "")
		req := httptest.NewRequest("POST", "/login",
			bytes.NewReader([]byte(`{"username":"testuser","password":"wrong"}`)))
		rr := httptest.NewRecorder()
		srv.Router().ServeHTTP(rr, req)
		if rr.Code != http.StatusUnauthorized {
			t.Errorf("expected 401, got %d", rr.Code)
		}
	})

	t.Run("unset admin password returns setup required", func(t *testing.T) {
		srv := NewServer(ServerConfig{}, treeOps, nil, nil, "test-secret", newUnsetAdminTestDB(), "")
		req := httptest.NewRequest("POST", "/login",
			bytes.NewReader([]byte(`{"username":"admin","password":"anything"}`)))
		rr := httptest.NewRecorder()
		srv.Router().ServeHTTP(rr, req)
		if rr.Code != http.StatusForbidden {
			t.Errorf("expected 403, got %d", rr.Code)
		}
	})

	t.Run("valid credentials return JWT", func(t *testing.T) {
		srv := NewServer(ServerConfig{}, treeOps, nil, nil, "test-secret", newTestDB("testuser", "testpass"), "")
		req := httptest.NewRequest("POST", "/login",
			bytes.NewReader([]byte(`{"username":"testuser","password":"testpass"}`)))
		req.Header.Set("Content-Type", "application/json")
		rr := httptest.NewRecorder()
		srv.Router().ServeHTTP(rr, req)

		if rr.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d; body: %s", rr.Code, rr.Body.String())
		}

		var resp LoginResponse
		if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
			t.Fatalf("failed to unmarshal: %v", err)
		}
		if resp.Token == "" {
			t.Error("expected token to be present")
		}
		if resp.TokenType != "Bearer" {
			t.Errorf("expected Bearer, got %s", resp.TokenType)
		}
		if resp.User.Username != "testuser" {
			t.Errorf("expected username testuser, got %s", resp.User.Username)
		}
	})

	t.Run("user role can list dashboards after login", func(t *testing.T) {
		srv := NewServer(ServerConfig{}, treeOps, nil, nil, "test-secret", newRoleTestDB("viewer", "testpass", "User"), "")
		req := httptest.NewRequest("POST", "/login",
			bytes.NewReader([]byte(`{"username":"viewer","password":"testpass"}`)))
		req.Header.Set("Content-Type", "application/json")
		rr := httptest.NewRecorder()
		srv.Router().ServeHTTP(rr, req)
		if rr.Code != http.StatusOK {
			t.Fatalf("login status = %d; body: %s", rr.Code, rr.Body.String())
		}
		var resp LoginResponse
		if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
			t.Fatalf("decode login: %v", err)
		}

		req = httptest.NewRequest("GET", "/api/v1/dashboards", nil)
		req.Header.Set("Authorization", "Bearer "+resp.Token)
		rr = httptest.NewRecorder()
		srv.Router().ServeHTTP(rr, req)
		if rr.Code != http.StatusOK {
			t.Fatalf("dashboard list status = %d; body: %s", rr.Code, rr.Body.String())
		}
	})
}

func TestBootstrapAdminPasswordSetup(t *testing.T) {
	treeOps := tree.NewTreeWithOperations(nil)

	t.Run("status reports setup required", func(t *testing.T) {
		srv := NewServer(ServerConfig{}, treeOps, nil, nil, "test-secret", newUnsetAdminTestDB(), "")
		req := httptest.NewRequest("GET", "/api/v1/bootstrap/admin", nil)
		rr := httptest.NewRecorder()
		srv.Router().ServeHTTP(rr, req)
		if rr.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d", rr.Code)
		}
		var resp BootstrapAdminStatusResponse
		if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
			t.Fatalf("failed to unmarshal: %v", err)
		}
		if !resp.SetupRequired || resp.PasswordSet {
			t.Fatalf("unexpected status: %+v", resp)
		}
	})

	t.Run("sets password and returns JWT", func(t *testing.T) {
		db := newUnsetAdminTestDB()
		srv := NewServer(ServerConfig{}, treeOps, nil, nil, "test-secret", db, "")
		req := httptest.NewRequest("POST", "/api/v1/bootstrap/admin/password",
			bytes.NewReader([]byte(`{"password":"new-admin-password"}`)))
		rr := httptest.NewRecorder()
		srv.Router().ServeHTTP(rr, req)
		if rr.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", rr.Code, rr.Body.String())
		}
		var resp LoginResponse
		if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
			t.Fatalf("failed to unmarshal: %v", err)
		}
		if resp.Token == "" || resp.User.Username != "admin" {
			t.Fatalf("unexpected login response: %+v", resp)
		}
		if !psql.CheckPassword(db.hash, "new-admin-password") {
			t.Fatalf("admin password hash was not updated")
		}
	})

	t.Run("rejects setup after password is set", func(t *testing.T) {
		srv := NewServer(ServerConfig{}, treeOps, nil, nil, "test-secret", newTestDB("admin", "existing-password"), "")
		req := httptest.NewRequest("POST", "/api/v1/bootstrap/admin/password",
			bytes.NewReader([]byte(`{"password":"new-admin-password"}`)))
		rr := httptest.NewRecorder()
		srv.Router().ServeHTTP(rr, req)
		if rr.Code != http.StatusConflict {
			t.Fatalf("expected 409, got %d", rr.Code)
		}
	})
}

func TestProxyPath(t *testing.T) {
	treeOps := tree.NewTreeWithOperations(nil)
	config := ServerConfig{
		ProxyPath: "/xact",
	}
	server := NewServer(config, treeOps, nil, nil, "test-secret", nil, "")

	// Health should work with proxy path
	req := httptest.NewRequest("GET", "/xact/health", nil)
	rr := httptest.NewRecorder()

	server.Router().ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("expected status %d, got %d", http.StatusOK, rr.Code)
	}
}

func TestSecurityHeadersConfiguredCORSAndBodyLimit(t *testing.T) {
	treeOps := tree.NewTreeWithOperations(nil)
	server := NewServer(ServerConfig{
		AllowedOrigins:      []string{"https://ui.example.com"},
		MaxRequestBodyBytes: 8,
	}, treeOps, nil, nil, "test-secret", newTestDB("testuser", "testpass"), "")

	req := httptest.NewRequest("GET", "/health", nil)
	req.Header.Set("Origin", "https://ui.example.com")
	rr := httptest.NewRecorder()
	server.Router().ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("health status = %d, want %d", rr.Code, http.StatusOK)
	}
	if got := rr.Header().Get("X-Content-Type-Options"); got != "nosniff" {
		t.Fatalf("X-Content-Type-Options = %q, want nosniff", got)
	}
	if got := rr.Header().Get("X-Frame-Options"); got != "DENY" {
		t.Fatalf("X-Frame-Options = %q, want DENY", got)
	}
	if got := rr.Header().Get("Referrer-Policy"); got != "strict-origin-when-cross-origin" {
		t.Fatalf("Referrer-Policy = %q, want strict-origin-when-cross-origin", got)
	}
	if got := rr.Header().Get("Access-Control-Allow-Origin"); got != "https://ui.example.com" {
		t.Fatalf("Access-Control-Allow-Origin = %q, want configured origin", got)
	}

	req = httptest.NewRequest("POST", "/login", strings.NewReader(`{"username":"testuser","password":"testpass"}`))
	rr = httptest.NewRecorder()
	server.Router().ServeHTTP(rr, req)
	if rr.Code == http.StatusOK {
		t.Fatalf("oversized login body unexpectedly succeeded")
	}
}

func TestNATSInternalConfigDisabledByDefault(t *testing.T) {
	treeOps := tree.NewTreeWithOperations(nil)
	server := NewServer(ServerConfig{}, treeOps, nil, nil, "test-secret", nil, "")
	server.SetNATSInternalConfig(NATSInternalConfig{
		URL:      "nats://localhost:4222",
		Username: "internal",
		Password: "secret",
	})

	req := httptest.NewRequest("GET", "/api/v1/system/nats-internal-config", nil)
	req.Header.Set("Authorization", "Bearer "+generateTestToken([]byte("test-secret"), "1"))
	rr := httptest.NewRecorder()

	server.Router().ServeHTTP(rr, req)

	if rr.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", rr.Code, http.StatusNotFound)
	}
}

func TestNATSInternalConfigRequiresSystemAdminWhenExposed(t *testing.T) {
	treeOps := tree.NewTreeWithOperations(nil)
	server := NewServer(ServerConfig{ExposeNATSInternalConfig: true}, treeOps, nil, nil, "test-secret", nil, "")
	server.SetNATSInternalConfig(NATSInternalConfig{
		URL:      "nats://localhost:4222",
		Username: "internal",
		Password: "secret",
	})

	rr := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/v1/system/nats-internal-config", nil)
	req.Header.Set("Authorization", "Bearer "+generateTestTokenWithRoles([]byte("test-secret"), "1", []string{"Admin"}))
	server.Router().ServeHTTP(rr, req)
	if rr.Code != http.StatusForbidden {
		t.Fatalf("non-admin status = %d, want %d", rr.Code, http.StatusForbidden)
	}

	rr = httptest.NewRecorder()
	req = httptest.NewRequest("GET", "/api/v1/system/nats-internal-config", nil)
	req.Header.Set("Authorization", "Bearer "+generateTestToken([]byte("test-secret"), "1"))
	server.Router().ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("system-admin status = %d, want %d", rr.Code, http.StatusOK)
	}
}

func TestJWTAuthRejectsStaleTokenVersion(t *testing.T) {
	db := newTestDB("testuser", "testpass")
	handler := JWTAuth([]byte("test-secret"), db)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	claims := JWTClaims{
		UserID:       "1",
		Username:     "testuser",
		TenantID:     "tenant1",
		Roles:        []string{"Admin"},
		AllowedOrgs:  []string{"tenant1"},
		TokenVersion: 2,
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	tokenString, _ := token.SignedString([]byte("test-secret"))

	req := httptest.NewRequest("GET", "/test", nil)
	req.Header.Set("Authorization", "Bearer "+tokenString)
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", rr.Code, http.StatusUnauthorized)
	}
}

// generateTestToken creates a test JWT token with SystemAdmin role so it
// bypasses permission DB checks (matching production SystemAdmin bypass behaviour).
func generateTestToken(secret []byte, userID string) string {
	return generateTestTokenWithRoles(secret, userID, []string{"SystemAdmin"})
}

func generateTestTokenWithRoles(secret []byte, userID string, roles []string) string {
	claims := JWTClaims{
		UserID:       userID,
		Username:     "testuser",
		TenantID:     "tenant1",
		Roles:        roles,
		AllowedOrgs:  []string{"tenant1"},
		TokenVersion: 1,
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	tokenString, _ := token.SignedString(secret)
	return tokenString
}
