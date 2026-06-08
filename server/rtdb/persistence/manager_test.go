package persistence

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
	"time"

	"github.com/xact-iot/xact/backups"
	"github.com/xact-iot/xact/events"
	"github.com/xact-iot/xact/rtdb/tree"
	"github.com/xact-iot/xact/sqldb"
)

// mockDB implements db.DB for testing
type mockDB struct {
	mu      sync.Mutex
	configs map[string]json.RawMessage
	saves   int
}

func newMockDB() *mockDB {
	return &mockDB{configs: make(map[string]json.RawMessage)}
}

func (m *mockDB) SaveConfig(_ context.Context, org string, name string, config json.RawMessage) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.configs[org+":"+name] = config
	m.saves++
	return nil
}

func (m *mockDB) LoadConfig(_ context.Context, org string, name string) (json.RawMessage, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	data, ok := m.configs[org+":"+name]
	if !ok {
		return nil, nil
	}
	return data, nil
}

func (m *mockDB) getSaveCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.saves
}

// Stubs for unused DB interface methods
func (m *mockDB) ListDashboards(_ context.Context, _ string) ([]sqldb.DashboardMeta, error) {
	return nil, nil
}
func (m *mockDB) GetDashboard(_ context.Context, _ string, _ int) (*sqldb.Dashboard, error) {
	return nil, nil
}
func (m *mockDB) CreateDashboard(_ context.Context, _ string, _ *sqldb.Dashboard) error { return nil }
func (m *mockDB) UpdateDashboard(_ context.Context, _ string, _ int, _ *sqldb.Dashboard) error {
	return nil
}
func (m *mockDB) DeleteDashboard(_ context.Context, _ string, _ int) error { return nil }
func (m *mockDB) ListPermissions(_ context.Context, _ string) ([]sqldb.RolePermissions, error) {
	return nil, nil
}
func (m *mockDB) GetPermissions(_ context.Context, _ string, _ string) (*sqldb.RolePermissions, error) {
	return nil, nil
}
func (m *mockDB) UpdatePermissions(_ context.Context, _ string, _ string, _ *sqldb.RolePermissions) error {
	return nil
}
func (m *mockDB) InsertEventEntries(_ context.Context, _ []events.EventEntry) error { return nil }
func (m *mockDB) QueryEvents(_ context.Context, _ sqldb.EventFilter) ([]events.EventEntry, error) {
	return nil, nil
}
func (m *mockDB) PurgeEventsBefore(_ context.Context, _ time.Time) error { return nil }
func (m *mockDB) Migrate(_ context.Context) error                        { return nil }
func (m *mockDB) Close()                                                 {}
func (m *mockDB) ListRoles(_ context.Context) ([]sqldb.Role, error)      { return nil, nil }
func (m *mockDB) ListUsers(_ context.Context) ([]sqldb.User, error)      { return nil, nil }
func (m *mockDB) GetUserByID(_ context.Context, _ int) (*sqldb.User, error) {
	return nil, nil
}
func (m *mockDB) GetUserByLogin(_ context.Context, _ string) (*sqldb.User, string, error) {
	return nil, "", nil
}
func (m *mockDB) CreateUser(_ context.Context, _ *sqldb.User, _ string) error  { return nil }
func (m *mockDB) UpdateUser(_ context.Context, _ *sqldb.User) error            { return nil }
func (m *mockDB) SetUserPassword(_ context.Context, _ int, _ string) error     { return nil }
func (m *mockDB) GetUserAuthState(_ context.Context, _ int) (bool, int, error) { return true, 1, nil }
func (m *mockDB) BumpUserTokenVersion(_ context.Context, _ int) error          { return nil }
func (m *mockDB) UpdateLastLogin(_ context.Context, _ int) error               { return nil }
func (m *mockDB) GetUserOrgs(_ context.Context, _ int) ([]sqldb.UserOrg, error) {
	return nil, nil
}
func (m *mockDB) AddUserToOrg(_ context.Context, _, _ int) error                 { return nil }
func (m *mockDB) AssignRoleToUser(_ context.Context, _, _ int, _ string) error   { return nil }
func (m *mockDB) RemoveRoleFromUser(_ context.Context, _, _ int, _ string) error { return nil }
func (m *mockDB) AssignUserToOrg(_ context.Context, _ int, _ string, _ []string) error {
	return nil
}
func (m *mockDB) SetUserOrgRoles(_ context.Context, _ int, _ string, _ []string) error {
	return nil
}
func (m *mockDB) ListOrganisations(_ context.Context) ([]sqldb.Organisation, error) {
	return nil, nil
}
func (m *mockDB) GetOrganisation(_ context.Context, _ string) (*sqldb.Organisation, error) {
	return nil, nil
}
func (m *mockDB) CreateOrganisation(_ context.Context, _ *sqldb.Organisation) error { return nil }
func (m *mockDB) UpdateOrganisation(_ context.Context, _ string, _ *sqldb.Organisation) error {
	return nil
}
func (m *mockDB) DeleteOrganisation(_ context.Context, _ string) error { return nil }
func (m *mockDB) InsertMetrics(_ context.Context, _ string, _ []sqldb.MetricEntry) error {
	return nil
}
func (m *mockDB) QueryMetricsByTagPaths(_ context.Context, _ string, _ []string, _, _ time.Time) ([]sqldb.MetricSeries, error) {
	return nil, nil
}
func (m *mockDB) QueryMetricsRange(_ context.Context, _, _ string, _ []string, _, _ time.Time) ([]sqldb.MetricSeries, error) {
	return nil, nil
}
func (m *mockDB) QueryMetricsSince(_ context.Context, _, _ string, _ []string, _ string, _ time.Time) ([]sqldb.MetricSeries, error) {
	return nil, nil
}
func (m *mockDB) ConfigureMetricsRetention(_ context.Context, _ time.Duration) error { return nil }
func (m *mockDB) ListAPIKeys(_ context.Context, _ string) ([]sqldb.APIKey, error)    { return nil, nil }
func (m *mockDB) CreateAPIKey(_ context.Context, _, _ string) (*sqldb.APIKey, error) { return nil, nil }
func (m *mockDB) DeleteAPIKey(_ context.Context, _ string, _ int) error              { return nil }
func (m *mockDB) GetAPIKeyOrg(_ context.Context, _ string) (string, error)           { return "", nil }
func (m *mockDB) ListPDFTemplates(_ context.Context, _ string) ([]sqldb.PDFTemplate, error) {
	return nil, nil
}
func (m *mockDB) GetPDFTemplate(_ context.Context, _, _ string) (*sqldb.PDFTemplate, error) {
	return nil, nil
}
func (m *mockDB) CreatePDFTemplate(_ context.Context, _ string, _ *sqldb.PDFTemplate) error {
	return nil
}
func (m *mockDB) UpdatePDFTemplate(_ context.Context, _, _ string, _ *sqldb.PDFTemplate) error {
	return nil
}
func (m *mockDB) DeletePDFTemplate(_ context.Context, _, _ string) error { return nil }
func (m *mockDB) ListNotificationProfiles(_ context.Context, _ string) ([]sqldb.NotificationProfile, error) {
	return nil, nil
}
func (m *mockDB) GetNotificationProfile(_ context.Context, _ string, _ int) (*sqldb.NotificationProfile, error) {
	return nil, nil
}
func (m *mockDB) GetNotificationProfileByName(_ context.Context, _, _ string) (*sqldb.NotificationProfile, error) {
	return nil, nil
}
func (m *mockDB) ResolveNotificationID(_ context.Context, _, _ string) (int, error) { return 0, nil }
func (m *mockDB) CreateNotificationProfile(_ context.Context, _ string, _ *sqldb.NotificationProfile) error {
	return nil
}
func (m *mockDB) UpdateNotificationProfile(_ context.Context, _ string, _ int, _ *sqldb.NotificationProfile) error {
	return nil
}
func (m *mockDB) DeleteNotificationProfile(_ context.Context, _ string, _ int) error { return nil }
func (m *mockDB) GetNotificationRecipients(_ context.Context, _ string, _ int) ([]sqldb.NotificationRecipient, error) {
	return nil, nil
}
func (m *mockDB) ListTagCalcs(_ context.Context, _ string) ([]sqldb.TagCalc, error) { return nil, nil }
func (m *mockDB) GetTagCalc(_ context.Context, _ string, _ int) (*sqldb.TagCalc, error) {
	return nil, nil
}
func (m *mockDB) CreateTagCalc(_ context.Context, _ string, _ *sqldb.TagCalc) error { return nil }
func (m *mockDB) UpdateTagCalc(_ context.Context, _ string, _ int, _ *sqldb.TagCalc) error {
	return nil
}
func (m *mockDB) DeleteTagCalc(_ context.Context, _ string, _ int) error { return nil }
func (m *mockDB) ListScheduledTasks(_ context.Context, _ string) ([]sqldb.ScheduledTask, error) {
	return nil, nil
}
func (m *mockDB) GetScheduledTask(_ context.Context, _, _ string) (*sqldb.ScheduledTask, error) {
	return nil, nil
}
func (m *mockDB) CreateScheduledTask(_ context.Context, _ string, _ *sqldb.ScheduledTask) error {
	return nil
}
func (m *mockDB) UpdateScheduledTask(_ context.Context, _, _ string, _ *sqldb.ScheduledTask) error {
	return nil
}
func (m *mockDB) DeleteScheduledTask(_ context.Context, _, _ string) error { return nil }
func (m *mockDB) UpdateScheduledTaskStatus(_ context.Context, _, _, _ string, _ time.Time) error {
	return nil
}
func (m *mockDB) AppendScheduleRunLog(_ context.Context, _ *sqldb.ScheduleRunLog) error {
	return nil
}
func (m *mockDB) UpdateScheduleRunLog(_ context.Context, _ int, _ time.Time, _, _, _ string) error {
	return nil
}
func (m *mockDB) ListScheduleRunLog(_ context.Context, _ string, _ int) ([]sqldb.ScheduleRunLog, error) {
	return nil, nil
}
func (m *mockDB) BackupAdapter() backups.Adapter { return nil }

func TestManagerDebounceSingleSave(t *testing.T) {
	mdb := newMockDB()
	treeOps := tree.NewTreeWithOperations(nil)
	treeOps.CreateNode("/test", "")
	treeOps.UnlockNode("/test")
	treeOps.CreateTag("/test/temp", tree.TypeFloat, tree.TagConfig{Name: "Temp"})

	mgr := NewManager(mdb, treeOps, "default", 50*time.Millisecond)

	// Multiple rapid dirty marks should result in a single save
	mgr.MarkDirty()
	mgr.MarkDirty()
	mgr.MarkDirty()

	// Wait for debounce to fire
	time.Sleep(200 * time.Millisecond)

	if count := mdb.getSaveCount(); count != 1 {
		t.Errorf("expected 1 save after debounce, got %d", count)
	}
}

func TestManagerForceSave(t *testing.T) {
	mdb := newMockDB()
	treeOps := tree.NewTreeWithOperations(nil)
	treeOps.CreateNode("/test", "")

	mgr := NewManager(mdb, treeOps, "default", 10*time.Second) // long debounce

	mgr.MarkDirty()
	err := mgr.Save(context.Background())
	if err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	if count := mdb.getSaveCount(); count != 1 {
		t.Errorf("expected 1 save, got %d", count)
	}
}

func TestManagerRestoreRoundTrip(t *testing.T) {
	mdb := newMockDB()

	// Build and save tree
	treeOps1 := tree.NewTreeWithOperations(nil)
	treeOps1.CreateNode("/building", "")
	treeOps1.UnlockNode("/building")
	treeOps1.CreateNode("/building/floor1", "")
	treeOps1.UnlockNode("/building/floor1")
	treeOps1.CreateTag("/building/floor1/temperature", tree.TypeFloat, tree.TagConfig{
		Name: "Temperature",
	}, tree.TagShared{
		Description: "Floor temp",
	})

	node, _ := treeOps1.FindNode("/building/floor1")
	node.SetDescription("First floor")

	mgr1 := NewManager(mdb, treeOps1, "default", time.Millisecond)
	mgr1.MarkDirty()
	err := mgr1.Save(context.Background())
	if err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	// Restore into a fresh tree
	treeOps2 := tree.NewTreeWithOperations(nil)
	mgr2 := NewManager(mdb, treeOps2, "default", time.Millisecond)

	restored, err := mgr2.Restore(context.Background())
	if err != nil {
		t.Fatalf("Restore failed: %v", err)
	}
	if !restored {
		t.Fatal("expected config to be restored")
	}

	// Verify structure
	n, err := treeOps2.FindNode("/building/floor1")
	if err != nil {
		t.Fatalf("expected /building/floor1: %v", err)
	}
	if n.GetDescription() != "First floor" {
		t.Errorf("expected description 'First floor', got '%s'", n.GetDescription())
	}

	leaf, err := treeOps2.FindLeaf("/building/floor1/temperature")
	if err != nil {
		t.Fatalf("expected leaf: %v", err)
	}
	if leaf.ValueType() != tree.TypeFloat {
		t.Errorf("expected float, got %v", leaf.ValueType())
	}
}

func TestManagerRestoreNoConfig(t *testing.T) {
	mdb := newMockDB()
	treeOps := tree.NewTreeWithOperations(nil)
	mgr := NewManager(mdb, treeOps, "default", time.Millisecond)

	restored, err := mgr.Restore(context.Background())
	if err != nil {
		t.Fatalf("Restore failed: %v", err)
	}
	if restored {
		t.Error("expected no config to restore")
	}
}

func TestManagerStopFinalSave(t *testing.T) {
	mdb := newMockDB()
	treeOps := tree.NewTreeWithOperations(nil)
	treeOps.CreateNode("/test", "")

	mgr := NewManager(mdb, treeOps, "default", 10*time.Second) // long debounce

	mgr.MarkDirty()

	// Stop should trigger final save
	err := mgr.Stop(context.Background())
	if err != nil {
		t.Fatalf("Stop failed: %v", err)
	}

	if count := mdb.getSaveCount(); count != 1 {
		t.Errorf("expected 1 save on stop, got %d", count)
	}
}
