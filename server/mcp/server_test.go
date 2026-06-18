package mcp

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/xact-iot/xact/rtdb/tree"
	"github.com/xact-iot/xact/sqldb"
	"github.com/xact-iot/xact/sqldb/sqlite"
)

type noopTagValuePublisher struct{}

func (noopTagValuePublisher) TagValuePublish(string, []byte) error { return nil }

type publishedTreeChange struct {
	path    string
	deleted bool
}

type recordingTreePublisher struct {
	changes []publishedTreeChange
}

func (p *recordingTreePublisher) PublishChange(path string, node tree.TreeNode) error {
	p.changes = append(p.changes, publishedTreeChange{path: path, deleted: node == nil})
	return nil
}

func newCRUDTestServer(t *testing.T, writeTools bool) *Server {
	return newCRUDTestServerWithPublisher(t, writeTools, nil)
}

func newCRUDTestServerWithPublisher(t *testing.T, writeTools bool, publisher interface {
	PublishChange(path string, node tree.TreeNode) error
}) *Server {
	t.Helper()
	oldPublisher := tree.TagValuePublisher
	tree.TagValuePublisher = noopTagValuePublisher{}
	t.Cleanup(func() { tree.TagValuePublisher = oldPublisher })

	tr := tree.NewTreeWithOperations(nil)
	if err := tr.CreateOrganisationNode("default", ""); err != nil {
		t.Fatalf("create default org: %v", err)
	}
	return New(Config{WriteTools: writeTools}, Dependencies{
		Tree:          tr,
		TreePublisher: publisher,
		RequireAny: func(context.Context, string, ...string) bool {
			return true
		},
		CurrentOrg: func(context.Context) (string, bool) {
			return "default", true
		},
	})
}

func rawJSON(t *testing.T, v any) json.RawMessage {
	t.Helper()
	data, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal json: %v", err)
	}
	return data
}

func TestDefineReportCreatePreservesProvidedID(t *testing.T) {
	ctx := context.Background()
	db, err := sqlite.NewSQLiteDB(ctx, filepath.Join(t.TempDir(), "xact.db"))
	if err != nil {
		t.Fatalf("NewSQLiteDB: %v", err)
	}
	t.Cleanup(db.Close)
	if err := db.Migrate(ctx); err != nil {
		t.Fatalf("Migrate: %v", err)
	}

	srv := New(Config{WriteTools: true}, Dependencies{
		DB: db,
		RequireAny: func(context.Context, string, ...string) bool {
			return true
		},
		CurrentOrg: func(context.Context) (string, bool) {
			return "default", true
		},
	})
	templateID := "32dd8c7d-3aa6-42b4-bc4d-9df49c5d93fd"
	raw := rawJSON(t, map[string]any{
		"operation":    "create",
		"id":           templateID,
		"name":         "Agent report",
		"description":  "created by MCP",
		"templateJson": map[string]any{"config": map[string]any{}, "elements": []any{}},
		"variables":    []any{},
		"dryRun":       false,
	})

	result, err := srv.toolDefineReport(ctx, raw)
	if err != nil {
		t.Fatalf("toolDefineReport: %v", err)
	}
	out := result.(map[string]any)
	template := out["template"].(sqldb.PDFTemplate)
	if template.ID != templateID {
		t.Fatalf("returned template ID = %q, want %q", template.ID, templateID)
	}
	stored, err := db.GetPDFTemplate(ctx, "default", templateID)
	if err != nil {
		t.Fatalf("GetPDFTemplate: %v", err)
	}
	if stored == nil || stored.ID != templateID {
		t.Fatalf("stored template = %#v", stored)
	}
}

func TestRTDBTagCRUDTools(t *testing.T) {
	srv := newCRUDTestServer(t, true)
	ctx := context.Background()

	_, err := srv.toolCreateTag(ctx, rawJSON(t, map[string]any{
		"path": "Templates/AirQualityBackup/power/testing",
		"type": "float",
		"metadata": map[string]any{
			"description": "Test power tag",
			"units":       "kW",
		},
		"value": 12.5,
	}))
	if err != nil {
		t.Fatalf("create tag: %v", err)
	}
	leaf, err := srv.deps.Tree.FindLeaf("default.Templates.AirQualityBackup.power.testing")
	if err != nil {
		t.Fatalf("find created tag: %v", err)
	}
	if got := leaf.ValueType(); got != tree.TypeFloat {
		t.Fatalf("value type = %v, want %v", got, tree.TypeFloat)
	}
	if got := leaf.GetDescription(); got != "Test power tag" {
		t.Fatalf("description = %q", got)
	}
	if got := leaf.GetShared().Units; got != "kW" {
		t.Fatalf("units = %q", got)
	}
	if got, _ := leaf.GetFloat(); got != 12.5 {
		t.Fatalf("value = %v", got)
	}

	_, err = srv.toolUpdateTag(ctx, rawJSON(t, map[string]any{
		"path":        "Templates/AirQualityBackup/power/testing",
		"name":        "renamed",
		"description": "Renamed tag",
		"units":       "MW",
		"value":       17.25,
	}))
	if err != nil {
		t.Fatalf("update tag: %v", err)
	}
	if _, err := srv.deps.Tree.FindLeaf("default.Templates.AirQualityBackup.power.testing"); err == nil {
		t.Fatalf("old tag path still exists after rename")
	}
	leaf, err = srv.deps.Tree.FindLeaf("default.Templates.AirQualityBackup.power.renamed")
	if err != nil {
		t.Fatalf("find renamed tag: %v", err)
	}
	if got := leaf.GetDescription(); got != "Renamed tag" {
		t.Fatalf("renamed description = %q", got)
	}
	if got := leaf.GetShared().Units; got != "MW" {
		t.Fatalf("renamed units = %q", got)
	}
	if got, _ := leaf.GetFloat(); got != 17.25 {
		t.Fatalf("renamed value = %v", got)
	}

	_, err = srv.toolDeleteTag(ctx, rawJSON(t, map[string]any{
		"path": "Templates/AirQualityBackup/power/renamed",
	}))
	if err != nil {
		t.Fatalf("delete tag: %v", err)
	}
	if _, err := srv.deps.Tree.FindLeaf("default.Templates.AirQualityBackup.power.renamed"); err == nil {
		t.Fatalf("tag still exists after delete")
	}
}

func TestRTDBCreateTemplateTagPropagatesToDeviceInstances(t *testing.T) {
	srv := newCRUDTestServer(t, true)
	ctx := context.Background()

	if err := srv.deps.Tree.CreateNode("default.Templates.AirQualityStandard", ""); err != nil {
		t.Fatalf("create template: %v", err)
	}
	if err := srv.deps.Tree.CreateDeviceNode("default.AirQuality.AQ001", "Templates.AirQualityStandard"); err != nil {
		t.Fatalf("create AQ001: %v", err)
	}
	if err := srv.deps.Tree.CreateDeviceNode("default.AirQuality.AQ002", "Templates.AirQualityStandard"); err != nil {
		t.Fatalf("create AQ002: %v", err)
	}

	result, err := srv.toolCreateTag(ctx, rawJSON(t, map[string]any{
		"path": "Templates/AirQualityStandard/metrics/pm25",
		"type": "float",
		"metadata": map[string]any{
			"description": "Template PM2.5",
			"units":       "ug/m3",
		},
	}))
	if err != nil {
		t.Fatalf("create template tag: %v", err)
	}
	propagated, ok := result.(map[string]any)["propagatedTo"].([]string)
	if !ok {
		t.Fatalf("propagatedTo missing or wrong type: %#v", result)
	}
	if len(propagated) != 2 {
		t.Fatalf("propagated len = %d, want 2: %#v", len(propagated), propagated)
	}

	tmplLeaf, err := srv.deps.Tree.FindLeaf("default.Templates.AirQualityStandard.metrics.pm25")
	if err != nil {
		t.Fatalf("template leaf: %v", err)
	}
	for _, path := range []string{
		"default.AirQuality.AQ001.metrics.pm25",
		"default.AirQuality.AQ002.metrics.pm25",
	} {
		leaf, err := srv.deps.Tree.FindLeaf(path)
		if err != nil {
			t.Fatalf("instance leaf %s: %v", path, err)
		}
		if leaf.GetTemplate() != tmplLeaf {
			t.Fatalf("%s template pointer not linked", path)
		}
		if leaf.GetDescription() != "Template PM2.5" {
			t.Fatalf("%s description = %q", path, leaf.GetDescription())
		}
	}
}

func TestRTDBDeleteTemplateTagAndNodePropagatesToDeviceInstances(t *testing.T) {
	srv := newCRUDTestServer(t, true)
	ctx := context.Background()

	if err := srv.deps.Tree.CreateNode("default.Templates.AirQualityStandard", ""); err != nil {
		t.Fatalf("create template: %v", err)
	}
	for _, path := range []string{
		"default.AirQuality.AQ001",
		"default.AirQuality.AQ002",
	} {
		if err := srv.deps.Tree.CreateDeviceNode(path, "Templates.AirQualityStandard"); err != nil {
			t.Fatalf("create device %s: %v", path, err)
		}
	}

	result, err := srv.toolCreateTag(ctx, rawJSON(t, map[string]any{
		"path": "Templates/AirQualityStandard/metrics/pm25",
		"type": "float",
	}))
	if err != nil {
		t.Fatalf("create template tag: %v", err)
	}
	if propagated := result.(map[string]any)["propagatedTo"].([]string); len(propagated) != 2 {
		t.Fatalf("create propagated len = %d, want 2: %#v", len(propagated), propagated)
	}

	result, err = srv.toolDeleteTag(ctx, rawJSON(t, map[string]any{
		"path": "Templates/AirQualityStandard/metrics/pm25",
	}))
	if err != nil {
		t.Fatalf("delete template tag: %v", err)
	}
	if propagated := result.(map[string]any)["propagatedTo"].([]string); len(propagated) != 2 {
		t.Fatalf("delete tag propagated len = %d, want 2: %#v", len(propagated), propagated)
	}
	for _, path := range []string{
		"default.Templates.AirQualityStandard.metrics.pm25",
		"default.AirQuality.AQ001.metrics.pm25",
		"default.AirQuality.AQ002.metrics.pm25",
	} {
		if _, err := srv.deps.Tree.FindLeaf(path); err == nil {
			t.Fatalf("%s still exists after template tag delete", path)
		}
	}

	for _, name := range []string{"temp", "humidity"} {
		result, err = srv.toolCreateTag(ctx, rawJSON(t, map[string]any{
			"path": "Templates/AirQualityStandard/metrics/" + name,
			"type": "float",
		}))
		if err != nil {
			t.Fatalf("create template tag %s: %v", name, err)
		}
		if propagated := result.(map[string]any)["propagatedTo"].([]string); len(propagated) != 2 {
			t.Fatalf("create %s propagated len = %d, want 2: %#v", name, len(propagated), propagated)
		}
	}

	result, err = srv.toolDeleteNode(ctx, rawJSON(t, map[string]any{
		"path": "Templates/AirQualityStandard/metrics",
	}))
	if err != nil {
		t.Fatalf("delete template node: %v", err)
	}
	if propagated := result.(map[string]any)["propagatedTo"].([]string); len(propagated) != 2 {
		t.Fatalf("delete node propagated len = %d, want 2: %#v", len(propagated), propagated)
	}
	for _, path := range []string{
		"default.Templates.AirQualityStandard.metrics",
		"default.AirQuality.AQ001.metrics",
		"default.AirQuality.AQ002.metrics",
	} {
		if _, err := srv.deps.Tree.FindNode(path); err == nil {
			t.Fatalf("%s still exists after template node delete", path)
		}
	}
}

func TestRTDBNodeCRUDTools(t *testing.T) {
	srv := newCRUDTestServer(t, true)
	ctx := context.Background()

	_, err := srv.toolCreateNode(ctx, rawJSON(t, map[string]any{
		"path":        "WaterWorks/PUMP_STATION/TestStation",
		"nodeType":    "Device",
		"description": "Pump station",
		"isArray":     true,
	}))
	if err != nil {
		t.Fatalf("create node: %v", err)
	}
	node, err := srv.deps.Tree.FindNode("default.WaterWorks.PUMP_STATION.TestStation")
	if err != nil {
		t.Fatalf("find created node: %v", err)
	}
	if got := node.GetNodeType(); got != tree.NodeTypeDevice {
		t.Fatalf("node type = %q", got)
	}
	if got := node.GetDescription(); got != "Pump station" {
		t.Fatalf("description = %q", got)
	}
	if !node.GetIsArray() {
		t.Fatalf("node should be marked as array")
	}

	_, err = srv.toolUpdateNode(ctx, rawJSON(t, map[string]any{
		"path":        "WaterWorks/PUMP_STATION/TestStation",
		"name":        "StationA",
		"description": "Station A",
		"isArray":     false,
		"locked":      true,
	}))
	if err != nil {
		t.Fatalf("update node: %v", err)
	}
	if _, err := srv.deps.Tree.FindNode("default.WaterWorks.PUMP_STATION.TestStation"); err == nil {
		t.Fatalf("old node path still exists after rename")
	}
	node, err = srv.deps.Tree.FindNode("default.WaterWorks.PUMP_STATION.StationA")
	if err != nil {
		t.Fatalf("find renamed node: %v", err)
	}
	if got := node.GetDescription(); got != "Station A" {
		t.Fatalf("updated description = %q", got)
	}
	if node.GetIsArray() {
		t.Fatalf("node should no longer be marked as array")
	}
	if !node.IsLocked() {
		t.Fatalf("node should be locked")
	}

	_, err = srv.toolDeleteNode(ctx, rawJSON(t, map[string]any{
		"path": "WaterWorks/PUMP_STATION/StationA",
	}))
	if err != nil {
		t.Fatalf("delete node: %v", err)
	}
	if _, err := srv.deps.Tree.FindNode("default.WaterWorks.PUMP_STATION.StationA"); err == nil {
		t.Fatalf("node still exists after delete")
	}
}

func TestRTDBCRUDToolsPublishTreeChanges(t *testing.T) {
	publisher := &recordingTreePublisher{}
	srv := newCRUDTestServerWithPublisher(t, true, publisher)
	ctx := context.Background()

	_, err := srv.toolCreateTag(ctx, rawJSON(t, map[string]any{
		"path": "Device/status",
		"type": "string",
	}))
	if err != nil {
		t.Fatalf("create tag: %v", err)
	}
	if len(publisher.changes) != 1 {
		t.Fatalf("publish count after create = %d, want 1", len(publisher.changes))
	}
	if got := publisher.changes[0]; got.path != "default.Device.status" || got.deleted {
		t.Fatalf("create publish = %#v", got)
	}

	_, err = srv.toolUpdateTag(ctx, rawJSON(t, map[string]any{
		"path": "Device/status",
		"name": "renamed",
	}))
	if err != nil {
		t.Fatalf("rename tag: %v", err)
	}
	if len(publisher.changes) != 3 {
		t.Fatalf("publish count after rename = %d, want 3", len(publisher.changes))
	}
	if got := publisher.changes[1]; got.path != "default.Device.status" || !got.deleted {
		t.Fatalf("rename deletion publish = %#v", got)
	}
	if got := publisher.changes[2]; got.path != "default.Device.renamed" || got.deleted {
		t.Fatalf("rename creation publish = %#v", got)
	}

	_, err = srv.toolDeleteTag(ctx, rawJSON(t, map[string]any{
		"path": "Device/renamed",
	}))
	if err != nil {
		t.Fatalf("delete tag: %v", err)
	}
	if len(publisher.changes) != 4 {
		t.Fatalf("publish count after delete = %d, want 4", len(publisher.changes))
	}
	if got := publisher.changes[3]; got.path != "default.Device.renamed" || !got.deleted {
		t.Fatalf("delete publish = %#v", got)
	}
}

func TestRTDBWriteToolsDisabled(t *testing.T) {
	srv := newCRUDTestServer(t, false)
	_, err := srv.toolCreateTag(context.Background(), rawJSON(t, map[string]any{
		"path": "Device/value",
		"type": "float",
	}))
	if err == nil || !strings.Contains(err.Error(), "MCP write tools are disabled") {
		t.Fatalf("create tag error = %v", err)
	}
}
