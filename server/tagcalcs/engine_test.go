package tagcalcs

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"testing"
	"time"

	exprvm "github.com/expr-lang/expr/vm"
	"github.com/xact-iot/xact/backups"
	"github.com/xact-iot/xact/events"
	"github.com/xact-iot/xact/rtdb/tree"
	"github.com/xact-iot/xact/sqldb"
)

// ── mock DB ───────────────────────────────────────────────────────────────────

type mockDB struct {
	orgs    []sqldb.Organisation
	scripts map[int]*sqldb.TagCalc
	nextID  int
}

func newMockDB(orgNames ...string) *mockDB {
	m := &mockDB{scripts: make(map[int]*sqldb.TagCalc), nextID: 1}
	for _, n := range orgNames {
		m.orgs = append(m.orgs, sqldb.Organisation{Name: n})
	}
	return m
}

func (m *mockDB) addScript(s sqldb.TagCalc) *sqldb.TagCalc {
	s.ID = m.nextID
	m.nextID++
	m.scripts[s.ID] = &s
	return &s
}

func (m *mockDB) ListOrganisations(_ context.Context) ([]sqldb.Organisation, error) {
	return m.orgs, nil
}
func (m *mockDB) ListTagCalcs(_ context.Context, org string) ([]sqldb.TagCalc, error) {
	var out []sqldb.TagCalc
	for _, s := range m.scripts {
		if s.OrgName == org {
			out = append(out, *s)
		}
	}
	return out, nil
}
func (m *mockDB) GetTagCalc(_ context.Context, _ string, id int) (*sqldb.TagCalc, error) {
	s, ok := m.scripts[id]
	if !ok {
		return nil, nil
	}
	cp := *s
	return &cp, nil
}
func (m *mockDB) CreateTagCalc(_ context.Context, _ string, s *sqldb.TagCalc) error {
	s.ID = m.nextID
	m.nextID++
	cp := *s
	m.scripts[s.ID] = &cp
	return nil
}
func (m *mockDB) UpdateTagCalc(_ context.Context, _ string, id int, s *sqldb.TagCalc) error {
	m.scripts[id] = s
	return nil
}
func (m *mockDB) DeleteTagCalc(_ context.Context, _ string, id int) error {
	delete(m.scripts, id)
	return nil
}

// Stubs for the rest of the DB interface
func (m *mockDB) SaveConfig(_ context.Context, _, _ string, _ json.RawMessage) error { return nil }
func (m *mockDB) LoadConfig(_ context.Context, _, _ string) (json.RawMessage, error) {
	return nil, nil
}
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
func (m *mockDB) GetPermissions(_ context.Context, _, _ string) (*sqldb.RolePermissions, error) {
	return nil, nil
}
func (m *mockDB) UpdatePermissions(_ context.Context, _, _ string, _ *sqldb.RolePermissions) error {
	return nil
}
func (m *mockDB) InsertEventEntries(_ context.Context, _ []events.EventEntry) error { return nil }
func (m *mockDB) QueryEvents(_ context.Context, _ sqldb.EventFilter) ([]events.EventEntry, error) {
	return nil, nil
}
func (m *mockDB) PurgeEventsBefore(_ context.Context, _ time.Time) error    { return nil }
func (m *mockDB) Migrate(_ context.Context) error                           { return nil }
func (m *mockDB) Close()                                                    {}
func (m *mockDB) ListRoles(_ context.Context) ([]sqldb.Role, error)         { return nil, nil }
func (m *mockDB) ListUsers(_ context.Context) ([]sqldb.User, error)         { return nil, nil }
func (m *mockDB) GetUserByID(_ context.Context, _ int) (*sqldb.User, error) { return nil, nil }
func (m *mockDB) GetUserByLogin(_ context.Context, _ string) (*sqldb.User, string, error) {
	return nil, "", nil
}
func (m *mockDB) CreateUser(_ context.Context, _ *sqldb.User, _ string) error    { return nil }
func (m *mockDB) UpdateUser(_ context.Context, _ *sqldb.User) error              { return nil }
func (m *mockDB) SetUserPassword(_ context.Context, _ int, _ string) error       { return nil }
func (m *mockDB) GetUserAuthState(_ context.Context, _ int) (bool, int, error)   { return true, 1, nil }
func (m *mockDB) BumpUserTokenVersion(_ context.Context, _ int) error            { return nil }
func (m *mockDB) UpdateLastLogin(_ context.Context, _ int) error                 { return nil }
func (m *mockDB) GetUserOrgs(_ context.Context, _ int) ([]sqldb.UserOrg, error)  { return nil, nil }
func (m *mockDB) AddUserToOrg(_ context.Context, _, _ int) error                 { return nil }
func (m *mockDB) AssignRoleToUser(_ context.Context, _, _ int, _ string) error   { return nil }
func (m *mockDB) RemoveRoleFromUser(_ context.Context, _, _ int, _ string) error { return nil }
func (m *mockDB) AssignUserToOrg(_ context.Context, _ int, _ string, _ []string) error {
	return nil
}
func (m *mockDB) SetUserOrgRoles(_ context.Context, _ int, _ string, _ []string) error { return nil }
func (m *mockDB) GetOrganisation(_ context.Context, _ string) (*sqldb.Organisation, error) {
	return nil, nil
}
func (m *mockDB) CreateOrganisation(_ context.Context, _ *sqldb.Organisation) error { return nil }
func (m *mockDB) UpdateOrganisation(_ context.Context, _ string, _ *sqldb.Organisation) error {
	return nil
}
func (m *mockDB) DeleteOrganisation(_ context.Context, _ string) error            { return nil }
func (m *mockDB) ListAPIKeys(_ context.Context, _ string) ([]sqldb.APIKey, error) { return nil, nil }
func (m *mockDB) CreateAPIKey(_ context.Context, _, _ string) (*sqldb.APIKey, error) {
	return nil, nil
}
func (m *mockDB) DeleteAPIKey(_ context.Context, _ string, _ int) error    { return nil }
func (m *mockDB) GetAPIKeyOrg(_ context.Context, _ string) (string, error) { return "", nil }
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
func (m *mockDB) BackupAdapter() backups.Adapter { return nil }
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

// ── tree helpers ──────────────────────────────────────────────────────────────

const testOrg = "testorg"

// newTestEngine returns a fresh Engine backed by an in-memory tree.
func newTestEngine() *Engine {
	treeOps := tree.NewTreeWithOperations(nil)
	return New(newMockDB(testOrg), treeOps)
}

// setFloat creates (if needed) and sets a float tag at the org-relative dot path.
func setFloat(e *Engine, dotPath string, value float64) {
	slashPath := "/" + testOrg + "/" + dotPathToSlash(dotPath)
	_ = e.writeOutput(slashPath, value)
}

// setBool creates and sets a boolean tag.
func setBool(e *Engine, dotPath string, value bool) {
	slashPath := "/" + testOrg + "/" + dotPathToSlash(dotPath)
	parts := splitLast(slashPath)
	_ = e.treeOps.CreateTag(slashPath, tree.TypeBoolean, tree.TagConfig{Name: parts})
	_ = e.treeOps.SetLeafValue(slashPath, value)
}

// setString creates and sets a string tag.
func setString(e *Engine, dotPath string, value string) {
	slashPath := "/" + testOrg + "/" + dotPathToSlash(dotPath)
	parts := splitLast(slashPath)
	_ = e.treeOps.CreateTag(slashPath, tree.TypeString, tree.TagConfig{Name: parts})
	_ = e.treeOps.SetLeafValue(slashPath, value)
}

// setInt creates and sets an integer tag.
func setInt(e *Engine, dotPath string, value int) {
	slashPath := "/" + testOrg + "/" + dotPathToSlash(dotPath)
	parts := splitLast(slashPath)
	_ = e.treeOps.CreateTag(slashPath, tree.TypeInteger, tree.TagConfig{Name: parts})
	_ = e.treeOps.SetLeafValue(slashPath, value)
}

func setDeviceFloat(e *Engine, deviceType, deviceName, description, metricPath string, value float64) {
	orgPath := "/" + testOrg
	_ = e.treeOps.CreateOrganisationNode(orgPath, "")
	devicePath := orgPath + "/" + deviceType + "/" + deviceName
	_ = e.treeOps.CreateDeviceNode(devicePath, "")
	if node, err := e.treeOps.FindNode(devicePath); err == nil {
		node.SetDescription(description)
	}
	tagPath := devicePath + "/" + dotPathToSlash(metricPath)
	parts := splitLast(tagPath)
	_ = e.treeOps.CreateTag(tagPath, tree.TypeFloat, tree.TagConfig{Name: parts})
	_ = e.treeOps.SetLeafValue(tagPath, value)
}

func dotPathToSlash(dotPath string) string {
	out := make([]byte, len(dotPath))
	for i := range dotPath {
		if dotPath[i] == '.' {
			out[i] = '/'
		} else {
			out[i] = dotPath[i]
		}
	}
	return string(out)
}

func splitLast(slashPath string) string {
	for i := len(slashPath) - 1; i >= 0; i-- {
		if slashPath[i] == '/' {
			return slashPath[i+1:]
		}
	}
	return slashPath
}

// eval is a convenience wrapper around EvaluateNow.
func eval(t *testing.T, e *Engine, expr string) float64 {
	t.Helper()
	v, err := e.EvaluateNow(testOrg, expr)
	if err != nil {
		t.Fatalf("EvaluateNow(%q) unexpected error: %v", expr, err)
	}
	return v
}

func evalAny(t *testing.T, e *Engine, expr string) any {
	t.Helper()
	v, err := e.EvaluateAny(testOrg, expr)
	if err != nil {
		t.Fatalf("EvaluateAny(%q) unexpected error: %v", expr, err)
	}
	return v
}

// approx checks floating-point equality within a small tolerance.
func approx(a, b float64) bool {
	return math.Abs(a-b) < 1e-9
}

// ── EvaluateNow - arithmetic ──────────────────────────────────────────────────

func TestEvaluateNow_LiteralArithmetic(t *testing.T) {
	e := newTestEngine()
	tests := []struct {
		expr string
		want float64
	}{
		{"1 + 2", 3},
		{"10 - 3", 7},
		{"4 * 5", 20},
		{"10 / 4", 2.5},
		{"10 % 3", 1},
		{"2 + 3 * 4", 14},   // precedence: multiply first
		{"(2 + 3) * 4", 20}, // parentheses override
	}
	for _, tc := range tests {
		t.Run(tc.expr, func(t *testing.T) {
			got := eval(t, e, tc.expr)
			if got != tc.want {
				t.Errorf("got %v, want %v", got, tc.want)
			}
		})
	}
}

// ── EvaluateNow - tag references ──────────────────────────────────────────────

func TestEvaluateNow_FloatTag(t *testing.T) {
	e := newTestEngine()
	setFloat(e, "VMS.Sign001.brightness", 75.5)
	got := eval(t, e, "VMS.Sign001.brightness")
	if got != 75.5 {
		t.Errorf("got %v, want 75.5", got)
	}
}

func TestEvaluateNow_IntegerTag(t *testing.T) {
	e := newTestEngine()
	setInt(e, "VMS.Sign001.errorCount", 42)
	got := eval(t, e, "VMS.Sign001.errorCount")
	if got != 42 {
		t.Errorf("got %v, want 42", got)
	}
}

func TestEvaluateNow_BooleanTag_True(t *testing.T) {
	e := newTestEngine()
	setBool(e, "VMS.Sign001.online", true)
	got := eval(t, e, "VMS.Sign001.online")
	if got != 1 {
		t.Errorf("bool true: got %v, want 1", got)
	}
}

func TestEvaluateNow_BooleanTag_False(t *testing.T) {
	e := newTestEngine()
	setBool(e, "VMS.Sign001.online", false)
	got := eval(t, e, "VMS.Sign001.online")
	if got != 0 {
		t.Errorf("bool false: got %v, want 0", got)
	}
}

func TestEvaluateNow_MissingTagReturnsZero(t *testing.T) {
	e := newTestEngine()
	got := eval(t, e, "VMS.DoesNotExist.temperature")
	if got != 0 {
		t.Errorf("missing tag: got %v, want 0", got)
	}
}

func TestEvaluateNow_TagArithmetic(t *testing.T) {
	e := newTestEngine()
	setFloat(e, "Feeder.phaseA.current", 10.0)
	setFloat(e, "Feeder.phaseB.current", 12.0)
	setFloat(e, "Feeder.phaseC.current", 8.0)
	got := eval(t, e, "Feeder.phaseA.current + Feeder.phaseB.current + Feeder.phaseC.current")
	if got != 30.0 {
		t.Errorf("got %v, want 30.0", got)
	}
}

// ── EvaluateNow - aggregate functions ────────────────────────────────────────

func TestEvaluateNow_Sum(t *testing.T) {
	e := newTestEngine()
	setFloat(e, "VMS.Sign001.brightness", 100)
	setFloat(e, "VMS.Sign002.brightness", 80)
	setFloat(e, "VMS.Sign003.brightness", 60)
	got := eval(t, e, "sum(VMS.*.brightness)")
	if got != 240 {
		t.Errorf("sum: got %v, want 240", got)
	}
}

func TestEvaluateNow_Avg(t *testing.T) {
	e := newTestEngine()
	setFloat(e, "VMS.Sign001.brightness", 100)
	setFloat(e, "VMS.Sign002.brightness", 80)
	setFloat(e, "VMS.Sign003.brightness", 60)
	got := eval(t, e, "avg(VMS.*.brightness)")
	if !approx(got, 80.0) {
		t.Errorf("avg: got %v, want 80.0", got)
	}
}

func TestEvaluateNow_Min(t *testing.T) {
	e := newTestEngine()
	setFloat(e, "Tanks.Tank001.level", 55.0)
	setFloat(e, "Tanks.Tank002.level", 20.0)
	setFloat(e, "Tanks.Tank003.level", 80.0)
	got := eval(t, e, "min(Tanks.*.level)")
	if got != 20.0 {
		t.Errorf("min: got %v, want 20.0", got)
	}
}

func TestEvaluateNow_Max(t *testing.T) {
	e := newTestEngine()
	setFloat(e, "Pumps.P001.motorTemp", 45.0)
	setFloat(e, "Pumps.P002.motorTemp", 92.0)
	setFloat(e, "Pumps.P003.motorTemp", 67.0)
	got := eval(t, e, "max(Pumps.*.motorTemp)")
	if got != 92.0 {
		t.Errorf("max: got %v, want 92.0", got)
	}
}

func TestEvaluateNow_Count(t *testing.T) {
	e := newTestEngine()
	setFloat(e, "VMS.Sign001.brightness", 100)
	setFloat(e, "VMS.Sign002.brightness", 80)
	setFloat(e, "VMS.Sign003.brightness", 60)
	got := eval(t, e, "count(VMS.*.brightness)")
	if got != 3 {
		t.Errorf("count: got %v, want 3", got)
	}
}

func TestEvaluateAny_ListHighest(t *testing.T) {
	e := newTestEngine()
	setDeviceFloat(e, "AirQuality", "SensorA", "Port sensor", "air.aqi", 55)
	setDeviceFloat(e, "AirQuality", "SensorB", "Downtown sensor", "air.aqi", 91)
	setDeviceFloat(e, "AirQuality", "SensorC", "Beach sensor", "air.aqi", 73)

	got, ok := evalAny(t, e, "listHighest(AirQuality.*.air.aqi, 2)").([]ListEntry)
	if !ok {
		t.Fatalf("listHighest returned %T, want []ListEntry", got)
	}
	if len(got) != 2 {
		t.Fatalf("len = %d, want 2", len(got))
	}
	if got[0].DeviceName != "SensorB" || got[0].DeviceDescriptor != "Downtown sensor" || got[0].TagName != "air.aqi" || got[0].TagValue != 91 {
		t.Fatalf("first entry = %#v", got[0])
	}
	if got[1].DeviceName != "SensorC" || got[1].TagValue != 73 {
		t.Fatalf("second entry = %#v", got[1])
	}
}

func TestEvaluateAny_ListLowest(t *testing.T) {
	e := newTestEngine()
	setDeviceFloat(e, "AirQuality", "SensorA", "Port sensor", "air.aqi", 55)
	setDeviceFloat(e, "AirQuality", "SensorB", "Downtown sensor", "air.aqi", 91)
	setDeviceFloat(e, "AirQuality", "SensorC", "Beach sensor", "air.aqi", 73)

	got, ok := evalAny(t, e, "listLowest(AirQuality.*.air.aqi, 2)").([]ListEntry)
	if !ok {
		t.Fatalf("listLowest returned %T, want []ListEntry", got)
	}
	if len(got) != 2 {
		t.Fatalf("len = %d, want 2", len(got))
	}
	if got[0].DeviceName != "SensorA" || got[0].TagValue != 55 {
		t.Fatalf("first entry = %#v", got[0])
	}
	if got[1].DeviceName != "SensorC" || got[1].TagValue != 73 {
		t.Fatalf("second entry = %#v", got[1])
	}
}

func TestEvaluateNow_CountWhere_Number(t *testing.T) {
	e := newTestEngine()
	setFloat(e, "Pumps.P001.stage", 1)
	setFloat(e, "Pumps.P002.stage", 2)
	setFloat(e, "Pumps.P003.stage", 2)
	setFloat(e, "Pumps.P004.stage", 1)
	got := eval(t, e, "countWhere(Pumps.*.stage, 2)")
	if got != 2 {
		t.Errorf("countWhere numeric: got %v, want 2", got)
	}
}

func TestEvaluateNow_CountWhere_True(t *testing.T) {
	e := newTestEngine()
	setBool(e, "VMS.Sign001.online", true)
	setBool(e, "VMS.Sign002.online", false)
	setBool(e, "VMS.Sign003.online", true)
	setBool(e, "VMS.Sign004.online", true)
	got := eval(t, e, "countWhere(VMS.*.online, true)")
	if got != 3 {
		t.Errorf("countWhere true: got %v, want 3", got)
	}
}

func TestEvaluateNow_CountWhere_False(t *testing.T) {
	e := newTestEngine()
	setBool(e, "VMS.Sign001.alarm", false)
	setBool(e, "VMS.Sign002.alarm", true)
	setBool(e, "VMS.Sign003.alarm", false)
	got := eval(t, e, "countWhere(VMS.*.alarm, false)")
	if got != 2 {
		t.Errorf("countWhere false: got %v, want 2", got)
	}
}

func TestEvaluateNow_CountWhere_String(t *testing.T) {
	e := newTestEngine()
	setString(e, "Singapore.Incidents.I001.kpi.type", "Accident")
	setString(e, "Singapore.Incidents.I002.kpi.type", "Near Miss")
	setString(e, "Singapore.Incidents.I003.kpi.type", "Accident")
	got := eval(t, e, `countWhere(Singapore.Incidents.*.kpi.type, 'Accident')`)
	if got != 2 {
		t.Errorf("countWhere string: got %v, want 2", got)
	}
}

func TestEvaluateNow_EmptyAggregateReturnsZero(t *testing.T) {
	e := newTestEngine()
	// No tags matching the pattern
	for _, fn := range []string{"avg", "sum", "min", "max", "count"} {
		got := eval(t, e, fn+"(NoSuch.*.tag)")
		if got != 0 {
			t.Errorf("%s with no matches: got %v, want 0", fn, got)
		}
	}
}

// ── EvaluateNow - wildcards ───────────────────────────────────────────────────

func TestEvaluateNow_StarWildcard_MatchesMultipleSegments(t *testing.T) {
	e := newTestEngine()
	setFloat(e, "VMS.Sign001.brightness", 10)
	setFloat(e, "VMS.Sign002.brightness", 20)
	setFloat(e, "VMS.Sign100.brightness", 30)
	got := eval(t, e, "count(VMS.*.brightness)")
	if got != 3 {
		t.Errorf("* wildcard: got %v, want 3", got)
	}
}

func TestEvaluateNow_StarWildcard_DoesNotMatchSubnodes(t *testing.T) {
	e := newTestEngine()
	setFloat(e, "VMS.Sign001.brightness", 10)
	setFloat(e, "VMS.Sign001.status.brightness", 20) // deeper - should NOT match VMS.*.brightness
	got := eval(t, e, "count(VMS.*.brightness)")
	if got != 1 {
		t.Errorf("* should not cross dot boundary: got %v, want 1", got)
	}
}

func TestEvaluateNow_QuestionMarkWildcard(t *testing.T) {
	e := newTestEngine()
	setFloat(e, "VMS.Sign001.brightness", 10)
	setFloat(e, "VMS.Sign002.brightness", 20)
	setFloat(e, "VMS.Sign010.brightness", 30) // 4-digit number - should NOT match Sign00?
	got := eval(t, e, "count(VMS.Sign00?.brightness)")
	if got != 2 {
		t.Errorf("? wildcard: got %v, want 2", got)
	}
}

// ── EvaluateNow - math functions ──────────────────────────────────────────────

func TestEvaluateNow_Abs(t *testing.T) {
	e := newTestEngine()
	setFloat(e, "Sensor.deviation", -15.5)
	got := eval(t, e, "abs(Sensor.deviation)")
	if got != 15.5 {
		t.Errorf("abs: got %v, want 15.5", got)
	}
}

func TestEvaluateNow_Round(t *testing.T) {
	e := newTestEngine()
	setFloat(e, "Meter.reading", 3.14159)
	got := eval(t, e, "round(Meter.reading, 2)")
	if got != 3.14 {
		t.Errorf("round: got %v, want 3.14", got)
	}
}

func TestEvaluateNow_Floor(t *testing.T) {
	e := newTestEngine()
	got := eval(t, e, "floor(7.9)")
	if got != 7 {
		t.Errorf("floor: got %v, want 7", got)
	}
}

func TestEvaluateNow_Ceil(t *testing.T) {
	e := newTestEngine()
	got := eval(t, e, "ceil(7.1)")
	if got != 8 {
		t.Errorf("ceil: got %v, want 8", got)
	}
}

func TestEvaluateNow_Sqrt(t *testing.T) {
	e := newTestEngine()
	got := eval(t, e, "sqrt(144)")
	if got != 12 {
		t.Errorf("sqrt: got %v, want 12", got)
	}
}

func TestEvaluateNow_Pow(t *testing.T) {
	e := newTestEngine()
	got := eval(t, e, "pow(2, 10)")
	if got != 1024 {
		t.Errorf("pow: got %v, want 1024", got)
	}
}

func TestEvaluateNow_Log(t *testing.T) {
	e := newTestEngine()
	got := eval(t, e, "log(1)")
	if got != 0 {
		t.Errorf("log(1): got %v, want 0", got)
	}
}

func TestEvaluateNow_Log10(t *testing.T) {
	e := newTestEngine()
	got := eval(t, e, "log10(1000)")
	if !approx(got, 3.0) {
		t.Errorf("log10(1000): got %v, want 3.0", got)
	}
}

func TestEvaluateNow_Trig(t *testing.T) {
	e := newTestEngine()
	got := eval(t, e, "sin(0)")
	if got != 0 {
		t.Errorf("sin(0): got %v, want 0", got)
	}
	got = eval(t, e, "cos(0)")
	if got != 1 {
		t.Errorf("cos(0): got %v, want 1", got)
	}
}

// ── EvaluateNow - conditionals ────────────────────────────────────────────────

func TestEvaluateNow_If_TrueBranch(t *testing.T) {
	e := newTestEngine()
	setFloat(e, "Tank.level", 15.0)
	got := eval(t, e, "if(Tank.level < 20, 1, 0)")
	if got != 1 {
		t.Errorf("if true branch: got %v, want 1", got)
	}
}

func TestEvaluateNow_If_FalseBranch(t *testing.T) {
	e := newTestEngine()
	setFloat(e, "Tank.level", 50.0)
	got := eval(t, e, "if(Tank.level < 20, 1, 0)")
	if got != 0 {
		t.Errorf("if false branch: got %v, want 0", got)
	}
}

func TestEvaluateNow_NestedIf(t *testing.T) {
	e := newTestEngine()
	tests := []struct {
		level float64
		want  float64
	}{
		{10, 0}, // low
		{50, 1}, // normal
		{90, 2}, // high
	}
	setFloat(e, "Tank.level", 0) // will be overwritten
	for _, tc := range tests {
		_ = e.treeOps.SetLeafValue("/testorg/Tank/level", tc.level)
		got := eval(t, e, "if(Tank.level < 20, 0, if(Tank.level < 70, 1, 2))")
		if got != tc.want {
			t.Errorf("level=%.0f: got %v, want %v", tc.level, got, tc.want)
		}
	}
}

func TestEvaluateNow_Comparison_Equal(t *testing.T) {
	e := newTestEngine()
	setFloat(e, "Pump.status", 1)
	got := eval(t, e, "if(Pump.status == 1, 100, 0)")
	if got != 100 {
		t.Errorf("==: got %v, want 100", got)
	}
}

func TestEvaluateNow_Comparison_NotEqual(t *testing.T) {
	e := newTestEngine()
	setFloat(e, "Pump.status", 0)
	got := eval(t, e, "if(Pump.status != 1, 1, 0)")
	if got != 1 {
		t.Errorf("!=: got %v, want 1", got)
	}
}

func TestEvaluateNow_Comparison_GreaterThanOrEqual(t *testing.T) {
	e := newTestEngine()
	setFloat(e, "Pump.runHours", 1000)
	got := eval(t, e, "if(Pump.runHours >= 1000, 1, 0)")
	if got != 1 {
		t.Errorf(">=: got %v, want 1", got)
	}
}

func TestEvaluateNow_Comparison_LessThanOrEqual(t *testing.T) {
	e := newTestEngine()
	setFloat(e, "Tank.pressure", 5.0)
	got := eval(t, e, "if(Tank.pressure <= 5.5, 1, 0)")
	if got != 1 {
		t.Errorf("<=: got %v, want 1", got)
	}
}

func TestEvaluateNow_LogicalAnd(t *testing.T) {
	e := newTestEngine()
	setFloat(e, "Tank.level", 15.0)
	setBool(e, "Pump.running", false)
	got := eval(t, e, "if(Tank.level < 20 && Pump.running == 0, 1, 0)")
	if got != 1 {
		t.Errorf("&&: got %v, want 1", got)
	}
}

func TestEvaluateNow_LogicalOr(t *testing.T) {
	e := newTestEngine()
	setFloat(e, "Tank.level", 5.0)
	setBool(e, "Tank.overflow", false)
	got := eval(t, e, "if(Tank.level < 10 || Tank.overflow == 1, 1, 0)")
	if got != 1 {
		t.Errorf("||: got %v, want 1", got)
	}
}

// ── EvaluateNow - error handling ──────────────────────────────────────────────

func TestEvaluateNow_SyntaxError(t *testing.T) {
	e := newTestEngine()
	_, err := e.EvaluateNow(testOrg, "avg(VMS.*.brightness")
	if err == nil {
		t.Error("expected error for unclosed parenthesis, got nil")
	}
}

func TestEvaluateNow_UnknownFunction(t *testing.T) {
	e := newTestEngine()
	_, err := e.EvaluateNow(testOrg, "nonexistentFn(VMS.*.brightness)")
	if err == nil {
		t.Error("expected error for unknown function, got nil")
	}
}

func TestEvaluateNow_DivideByZeroReturnsZero(t *testing.T) {
	// The expr library treats division by zero as returning 0 (or +Inf), not a panic.
	// We verify no panic and the function returns without error.
	e := newTestEngine()
	setFloat(e, "Pump.input", 0)
	setFloat(e, "Pump.output", 100)
	// Should not panic
	v, err := e.EvaluateNow(testOrg, "Pump.output / Pump.input")
	if err != nil {
		t.Logf("divide by zero returned error (acceptable): %v", err)
	} else {
		// Result may be +Inf or 0 depending on library - just verify no panic
		t.Logf("divide by zero returned: %v", v)
	}
}

func TestEvaluateNow_DivideByZeroProtected(t *testing.T) {
	// The recommended idiom: max(1, denominator)
	e := newTestEngine()
	setFloat(e, "Pump.input", 0)
	setFloat(e, "Pump.output", 100)
	got := eval(t, e, "Pump.output / max(1, Pump.input)")
	if got != 100 {
		t.Errorf("protected divide by zero: got %v, want 100", got)
	}
}

// ── EvaluateNow - complex / real-world expressions ───────────────────────────

func TestEvaluateNow_OnlinePercentage(t *testing.T) {
	e := newTestEngine()
	// 3 of 4 devices online
	for i, v := range []bool{true, true, true, false} {
		setBool(e, fmt.Sprintf("VMS.Sign%03d.online", i+1), v)
	}
	got := eval(t, e, "round(countWhere(VMS.*.online, true) / max(1, count(VMS.*.online)) * 100, 1)")
	if got != 75.0 {
		t.Errorf("online %%: got %v, want 75.0", got)
	}
}

func TestEvaluateNow_HealthScore(t *testing.T) {
	e := newTestEngine()
	setFloat(e, "SYSTEM.LoopTime.Current", 3.0) // fast → 100 points
	setFloat(e, "SYSTEM.Database.Errors", 0)    // no errors → 100 points
	got := eval(t, e, "if(SYSTEM.LoopTime.Current < 5, 100, if(SYSTEM.LoopTime.Current < 10, 50, 0)) + if(SYSTEM.Database.Errors == 0, 100, 0)")
	if got != 200 {
		t.Errorf("health score: got %v, want 200", got)
	}
}

func TestEvaluateNow_BooleanArithmetic(t *testing.T) {
	// Sum of boolean tags = count of trues
	e := newTestEngine()
	setBool(e, "VMS.Sign001.online", true)
	setBool(e, "VMS.Sign002.online", false)
	setBool(e, "VMS.Sign003.online", true)
	got := eval(t, e, "sum(VMS.*.online)")
	if got != 2 {
		t.Errorf("bool sum: got %v, want 2", got)
	}
}

// ── writeOutput ───────────────────────────────────────────────────────────────

func TestWriteOutput_CreatesTagOnFirstCall(t *testing.T) {
	e := newTestEngine()
	path := "/" + testOrg + "/VMS/KPI/total"
	if err := e.writeOutput(path, 42.0); err != nil {
		t.Fatalf("writeOutput: %v", err)
	}
	leaf, err := e.treeOps.FindLeaf(path)
	if err != nil {
		t.Fatalf("tag not found after writeOutput: %v", err)
	}
	v, _ := leaf.GetFloat()
	if v != 42.0 {
		t.Errorf("tag value: got %v, want 42.0", v)
	}
}

func TestWriteOutput_UpdatesValueOnSubsequentCalls(t *testing.T) {
	e := newTestEngine()
	path := "/" + testOrg + "/VMS/KPI/total"
	_ = e.writeOutput(path, 10.0)
	_ = e.writeOutput(path, 99.0)
	leaf, _ := e.treeOps.FindLeaf(path)
	v, _ := leaf.GetFloat()
	if v != 99.0 {
		t.Errorf("updated value: got %v, want 99.0", v)
	}
}

func TestWriteOutput_TagIsReadableViaEvaluateNow(t *testing.T) {
	e := newTestEngine()
	_ = e.writeOutput("/"+testOrg+"/VMS/KPI/online_count", 7.0)
	got := eval(t, e, "VMS.KPI.online_count")
	if got != 7.0 {
		t.Errorf("output tag readable: got %v, want 7.0", got)
	}
}

func TestWriteListOutput_CreatesObjectArray(t *testing.T) {
	e := newTestEngine()
	path := "/" + testOrg + "/Computed/topAQI"
	entries := []ListEntry{
		{DeviceName: "SensorB", DeviceDescriptor: "Downtown sensor", TagName: "air.aqi", TagValue: 91},
		{DeviceName: "SensorC", DeviceDescriptor: "Beach sensor", TagName: "air.aqi", TagValue: 73},
	}

	if err := e.writeListOutput(path, entries); err != nil {
		t.Fatalf("writeListOutput: %v", err)
	}
	node, err := e.treeOps.FindNode(path)
	if err != nil {
		t.Fatalf("array node not found: %v", err)
	}
	if !node.GetIsArray() {
		t.Fatal("output node is not marked as array")
	}

	nameLeaf, err := e.treeOps.FindLeaf(path + "/0/deviceName")
	if err != nil {
		t.Fatalf("deviceName leaf: %v", err)
	}
	name, _ := nameLeaf.GetString()
	if name != "SensorB" {
		t.Fatalf("deviceName = %q, want SensorB", name)
	}
	valueLeaf, err := e.treeOps.FindLeaf(path + "/0/tagValue")
	if err != nil {
		t.Fatalf("tagValue leaf: %v", err)
	}
	value, _ := valueLeaf.GetFloat()
	if value != 91 {
		t.Fatalf("tagValue = %v, want 91", value)
	}
}

func TestWriteListOutput_PrunesExtraElements(t *testing.T) {
	e := newTestEngine()
	path := "/" + testOrg + "/Computed/topAQI"
	first := []ListEntry{
		{DeviceName: "SensorA", TagName: "air.aqi", TagValue: 55},
		{DeviceName: "SensorB", TagName: "air.aqi", TagValue: 91},
		{DeviceName: "SensorC", TagName: "air.aqi", TagValue: 73},
	}
	if err := e.writeListOutput(path, first); err != nil {
		t.Fatalf("initial writeListOutput: %v", err)
	}
	second := []ListEntry{{DeviceName: "SensorB", TagName: "air.aqi", TagValue: 91}}
	if err := e.writeListOutput(path, second); err != nil {
		t.Fatalf("second writeListOutput: %v", err)
	}
	if _, err := e.treeOps.FindNode(path + "/1"); err == nil {
		t.Fatal("stale array element 1 still exists")
	}
	if _, err := e.treeOps.FindNode(path + "/2"); err == nil {
		t.Fatal("stale array element 2 still exists")
	}
	if _, err := e.treeOps.FindNode(path + "/0"); err != nil {
		t.Fatalf("kept array element 0 missing: %v", err)
	}
}

// ── Engine.Load and scheduling ────────────────────────────────────────────────

func TestEngine_Load_SchedulesEnabledScripts(t *testing.T) {
	db := newMockDB(testOrg)
	treeOps := tree.NewTreeWithOperations(nil)
	e := New(db, treeOps)

	setFloat(e, "A.val", 5.0)

	db.addScript(sqldb.TagCalc{
		OrgName:         testOrg,
		Name:            "double",
		OutputTag:       "Computed.double",
		Expression:      "A.val * 2",
		IntervalSeconds: 3600, // long interval - won't fire during test
		Enabled:         true,
	})
	db.addScript(sqldb.TagCalc{
		OrgName:         testOrg,
		Name:            "disabled",
		OutputTag:       "Computed.disabled",
		Expression:      "A.val",
		IntervalSeconds: 3600,
		Enabled:         false,
	})

	if err := e.Load(context.Background()); err != nil {
		t.Fatalf("Load: %v", err)
	}

	e.mu.Lock()
	scriptCount := len(e.scripts)
	timerCount := len(e.timers)
	e.mu.Unlock()

	if scriptCount != 2 {
		t.Errorf("scripts loaded: got %d, want 2", scriptCount)
	}
	// Only the enabled script should have a timer
	if timerCount != 1 {
		t.Errorf("timers started: got %d, want 1 (enabled only)", timerCount)
	}
}

func TestEngine_Reload_ReplacesScript(t *testing.T) {
	db := newMockDB(testOrg)
	treeOps := tree.NewTreeWithOperations(nil)
	e := New(db, treeOps)

	setFloat(e, "A.val", 10.0)

	s := db.addScript(sqldb.TagCalc{
		OrgName:         testOrg,
		Name:            "test",
		OutputTag:       "Computed.out",
		Expression:      "A.val",
		IntervalSeconds: 3600,
		Enabled:         true,
	})

	if err := e.Load(context.Background()); err != nil {
		t.Fatalf("Load: %v", err)
	}

	// Update the script expression in the mock DB
	s.Expression = "A.val * 3"
	db.scripts[s.ID] = s

	e.Reload(context.Background(), testOrg, s.ID)

	e.mu.Lock()
	cs, ok := e.scripts[s.ID]
	e.mu.Unlock()

	if !ok {
		t.Fatal("script not found after Reload")
	}
	if cs.script.Expression != "A.val * 3" {
		t.Errorf("reloaded expression: got %q, want %q", cs.script.Expression, "A.val * 3")
	}
}

func TestEngine_Unschedule_RemovesScript(t *testing.T) {
	db := newMockDB(testOrg)
	treeOps := tree.NewTreeWithOperations(nil)
	e := New(db, treeOps)

	s := db.addScript(sqldb.TagCalc{
		OrgName:         testOrg,
		Name:            "to-delete",
		OutputTag:       "Computed.x",
		Expression:      "1",
		IntervalSeconds: 3600,
		Enabled:         true,
	})

	if err := e.Load(context.Background()); err != nil {
		t.Fatalf("Load: %v", err)
	}

	e.Unschedule(s.ID)

	e.mu.Lock()
	_, scriptExists := e.scripts[s.ID]
	_, timerExists := e.timers[s.ID]
	e.mu.Unlock()

	if scriptExists {
		t.Error("script still present after Unschedule")
	}
	if timerExists {
		t.Error("timer still present after Unschedule")
	}
}

func TestEngine_Evaluate_WritesResultToTree(t *testing.T) {
	db := newMockDB(testOrg)
	treeOps := tree.NewTreeWithOperations(nil)
	e := New(db, treeOps)

	setFloat(e, "A.val", 7.0)

	s := db.addScript(sqldb.TagCalc{
		OrgName:         testOrg,
		Name:            "triple",
		OutputTag:       "Computed.triple",
		Expression:      "A.val * 3",
		IntervalSeconds: 3600,
		Enabled:         true,
	})

	if err := e.Load(context.Background()); err != nil {
		t.Fatalf("Load: %v", err)
	}

	// Trigger evaluation directly (bypassing the timer)
	e.mu.Lock()
	cs := e.scripts[s.ID]
	e.mu.Unlock()
	e.evaluate(cs)

	leaf, err := treeOps.FindLeaf("/" + testOrg + "/Computed/triple")
	if err != nil {
		t.Fatalf("output tag not found: %v", err)
	}
	v, _ := leaf.GetFloat()
	if v != 21.0 {
		t.Errorf("evaluated result: got %v, want 21.0", v)
	}
}

// ── benchmarks ────────────────────────────────────────────────────────────────

func BenchmarkEvaluate_SingleTag(b *testing.B) {
	e := newTestEngine()
	setFloat(e, "Sensor.temperature", 22.5)
	prog, _ := compileExpression("Sensor.temperature")
	env := &runtimeEnv{treeOps: e.treeOps, org: testOrg}
	v := exprvm.VM{}
	b.ResetTimer()
	for range b.N {
		out, _ := v.Run(prog, env)
		_, _ = normaliseResult(out)
	}
}

func BenchmarkEvaluate_Arithmetic_FourTags(b *testing.B) {
	e := newTestEngine()
	setFloat(e, "Feeder.phaseA.current", 10.0)
	setFloat(e, "Feeder.phaseB.current", 12.0)
	setFloat(e, "Feeder.phaseC.current", 8.0)
	setFloat(e, "Feeder.neutral.current", 2.0)
	prog, _ := compileExpression("Feeder.phaseA.current + Feeder.phaseB.current + Feeder.phaseC.current + Feeder.neutral.current")
	env := &runtimeEnv{treeOps: e.treeOps, org: testOrg}
	v := exprvm.VM{}
	b.ResetTimer()
	for range b.N {
		out, _ := v.Run(prog, env)
		_, _ = normaliseResult(out)
	}
}

func BenchmarkEvaluate_Avg_10Devices(b *testing.B) {
	e := newTestEngine()
	for i := range 10 {
		setFloat(e, fmt.Sprintf("VMS.Sign%03d.brightness", i+1), float64(50+i))
	}
	prog, _ := compileExpression("avg(VMS.*.brightness)")
	env := &runtimeEnv{treeOps: e.treeOps, org: testOrg}
	v := exprvm.VM{}
	b.ResetTimer()
	for range b.N {
		out, _ := v.Run(prog, env)
		_, _ = normaliseResult(out)
	}
}

func BenchmarkEvaluate_Avg_100Devices(b *testing.B) {
	e := newTestEngine()
	for i := range 100 {
		setFloat(e, fmt.Sprintf("VMS.Sign%03d.brightness", i+1), float64(50+i%50))
	}
	prog, _ := compileExpression("avg(VMS.*.brightness)")
	env := &runtimeEnv{treeOps: e.treeOps, org: testOrg}
	v := exprvm.VM{}
	b.ResetTimer()
	for range b.N {
		out, _ := v.Run(prog, env)
		_, _ = normaliseResult(out)
	}
}

func BenchmarkEvaluate_CountWhere_100Devices(b *testing.B) {
	e := newTestEngine()
	for i := range 100 {
		setBool(e, fmt.Sprintf("VMS.Sign%03d.online", i+1), i%3 != 0)
	}
	prog, _ := compileExpression("countWhere(VMS.*.online, true)")
	env := &runtimeEnv{treeOps: e.treeOps, org: testOrg}
	v := exprvm.VM{}
	b.ResetTimer()
	for range b.N {
		out, _ := v.Run(prog, env)
		_, _ = normaliseResult(out)
	}
}

func BenchmarkEvaluate_ComplexExpression(b *testing.B) {
	e := newTestEngine()
	for i := range 50 {
		setBool(e, fmt.Sprintf("VMS.Sign%03d.online", i+1), i%4 != 0)
		setBool(e, fmt.Sprintf("VMS.Sign%03d.alarm", i+1), i%10 == 0)
	}
	expr := `round(
		(countWhere(VMS.*.online, true) * 50 + countWhere(VMS.*.alarm, false) * 50)
		/ max(1, count(VMS.*.online)),
	1)`
	prog, _ := compileExpression(expr)
	env := &runtimeEnv{treeOps: e.treeOps, org: testOrg}
	v := exprvm.VM{}
	b.ResetTimer()
	for range b.N {
		out, _ := v.Run(prog, env)
		_, _ = normaliseResult(out)
	}
}

func BenchmarkEvaluateNow_EndToEnd(b *testing.B) {
	// Full EvaluateNow path including compile - representative of an API test call.
	e := newTestEngine()
	for i := range 20 {
		setFloat(e, fmt.Sprintf("Pumps.P%03d.motorTemp", i+1), float64(40+i))
	}
	b.ResetTimer()
	for range b.N {
		_, _ = e.EvaluateNow(testOrg, "max(Pumps.*.motorTemp)")
	}
}
