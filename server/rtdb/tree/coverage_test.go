package tree

import (
	"encoding/json"
	"testing"
	"time"
)

// stubBlock is a minimal ProcessBlock for testing pipeline logic.
type stubBlock struct {
	blockType string
}

func (b *stubBlock) GetType() string                       { return b.blockType }
func (b *stubBlock) Init(_ Leaf)                           {}
func (b *stubBlock) Close(_ Leaf)                          {}
func (b *stubBlock) Process(_ Leaf, v any) (any, error)    { return v, nil }
func (b *stubBlock) GetParameters() json.RawMessage        { return json.RawMessage("{}") }
func (b *stubBlock) SetParameters(_ json.RawMessage) error { return nil }
func (b *stubBlock) Schema() BlockSchema                   { return BlockSchema{Type: b.blockType} }

type captureTagValuePublisher struct {
	count int
	data  [][]byte
}

func (p *captureTagValuePublisher) TagValuePublish(_ string, data []byte) error {
	p.count++
	p.data = append(p.data, data)
	return nil
}

// ── Node property tests ─────────────────────────────────────────────────────

func TestNodeProperties(t *testing.T) {
	n := NewNode("test")
	if n.GetName() != "test" {
		t.Errorf("GetName() = %q, want %q", n.GetName(), "test")
	}
	if !n.IsNode() {
		t.Error("IsNode() should be true")
	}
	if n.IsDeleted() {
		t.Error("new node should not be deleted")
	}

	// Description
	n.SetDescription("A test node")
	if n.GetDescription() != "A test node" {
		t.Errorf("GetDescription() = %q", n.GetDescription())
	}

	// TemplateName
	n.SetTemplateName("tmpl.VMS")
	if n.GetTemplateName() != "tmpl.VMS" {
		t.Errorf("GetTemplateName() = %q", n.GetTemplateName())
	}

	// NodeType
	n.SetNodeType(NodeTypeDevice)
	if n.GetNodeType() != NodeTypeDevice {
		t.Errorf("GetNodeType() = %q", n.GetNodeType())
	}

	// Locked
	n.SetLocked(true)
	if !n.IsLocked() {
		t.Error("IsLocked() should be true")
	}
	n.SetLocked(false)
	if n.IsLocked() {
		t.Error("IsLocked() should be false")
	}

	// IsArray
	n.SetIsArray(true)
	if !n.GetIsArray() {
		t.Error("GetIsArray() should be true")
	}
	n.SetIsArray(false)
	if n.GetIsArray() {
		t.Error("GetIsArray() should be false")
	}
}

func TestNodeChildOnDeleted(t *testing.T) {
	n := NewNode("parent")
	n.MarkDeleted()

	// GetChild on deleted node
	_, ok := n.GetChild("any")
	if ok {
		t.Error("GetChild on deleted node should return false")
	}

	// GetChildren on deleted node
	if children := n.GetChildren(); children != nil {
		t.Error("GetChildren on deleted node should return nil")
	}

	// RemoveChild on deleted node
	if err := n.RemoveChild("any"); err != ErrNodeDeleted {
		t.Errorf("RemoveChild on deleted node: expected ErrNodeDeleted, got %v", err)
	}
}

// ── Leaf property tests ─────────────────────────────────────────────────────

func TestLeafBasicProperties(t *testing.T) {
	leaf := NewFloatLeaf("temp", TagConfig{Name: "Temperature", Type: TypeFloat})
	if leaf.GetName() != "temp" {
		t.Errorf("GetName() = %q", leaf.GetName())
	}
	if leaf.IsNode() {
		t.Error("IsNode() should be false for leaf")
	}

	// Config
	cfg := leaf.GetConfig()
	if cfg.Name != "Temperature" {
		t.Errorf("config name = %q", cfg.Name)
	}

	// Shared
	shared := TagShared{Description: "sensor temp", Units: "°C", Deadband: 0.5}
	leaf.SetShared(shared)
	got := leaf.GetShared()
	if got.Description != "sensor temp" || got.Units != "°C" || got.Deadband != 0.5 {
		t.Errorf("SetShared/GetShared round-trip failed: %+v", got)
	}

	// Description from shared
	if leaf.GetDescription() != "sensor temp" {
		t.Errorf("GetDescription() = %q", leaf.GetDescription())
	}

	// State
	if leaf.GetState() != "" {
		t.Errorf("new leaf state should be empty, got %q", leaf.GetState())
	}
	leaf.SetState(StatusAlarm)
	if leaf.GetState() != StatusAlarm {
		t.Errorf("state after SetState(Alarm) = %q", leaf.GetState())
	}
	leaf.SetState(StatusStale) // add second state
	leaf.ClearState(StatusAlarm)
	if leaf.GetState() != StatusStale {
		t.Errorf("state after ClearState(Alarm) = %q, want %q", leaf.GetState(), StatusStale)
	}
	leaf.ClearState(StatusStale)
	if leaf.GetState() != "" {
		t.Errorf("state after clearing all = %q", leaf.GetState())
	}

	// UpdatedTime
	now := time.Now()
	leaf.SetUpdatedTime(now)
	if !leaf.GetUpdatedTime().Equal(now) {
		t.Error("SetUpdatedTime/GetUpdatedTime mismatch")
	}

	// Runtime
	rt := leaf.GetRuntime()
	_ = rt // just verify no panic

	// Parent (nil before AddChild)
	if leaf.GetParent() != nil {
		t.Error("parent should be nil before AddChild")
	}
}

func TestLeafTemplate(t *testing.T) {
	tmpl := NewFloatLeaf("tmpl_temp", TagConfig{Name: "TmplTemp", Type: TypeFloat})
	tmplShared := TagShared{Description: "from template", Units: "K", Deadband: 1.0}
	tmpl.SetShared(tmplShared)

	leaf := NewFloatLeaf("temp", TagConfig{Name: "Temperature", Type: TypeFloat})
	leaf.SetTemplate(tmpl)

	if leaf.GetTemplate() != tmpl {
		t.Error("GetTemplate() should return the template leaf")
	}

	// Description should come from template when local is empty
	if leaf.GetDescription() != "from template" {
		t.Errorf("GetDescription() = %q, want template description", leaf.GetDescription())
	}
}

// ── GetAnyValue / SetAnyValue ────────────────────────────────────────────────

func TestGetAnyValue(t *testing.T) {
	tests := []struct {
		name string
		leaf Leaf
		set  func(Leaf)
		want any
	}{
		{"float", NewFloatLeaf("f", TagConfig{Type: TypeFloat}), func(l Leaf) { l.SetFloat(3.14) }, 3.14},
		{"int", NewIntegerLeaf("i", TagConfig{Type: TypeInteger}), func(l Leaf) { l.SetInt(42) }, int64(42)},
		{"string", NewStringLeaf("s", TagConfig{Type: TypeString}), func(l Leaf) { l.SetString("hello") }, "hello"},
		{"bool", NewBoolLeaf("b", TagConfig{Type: TypeBoolean}), func(l Leaf) { l.SetBool(true) }, true},
		{"enum", NewEnumLeaf("e", TagConfig{Type: TypeEnum}, TagShared{EnumValues: map[int]string{0: "Off", 1: "On"}}), func(l Leaf) { l.SetEnum(1) }, 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.set(tt.leaf)
			got := tt.leaf.GetAnyValue()
			if got != tt.want {
				t.Errorf("GetAnyValue() = %v (%T), want %v (%T)", got, got, tt.want, tt.want)
			}
		})
	}
}

func TestSetAnyValue(t *testing.T) {
	tests := []struct {
		name    string
		leaf    Leaf
		input   any
		wantErr bool
	}{
		{"float64 to float", NewFloatLeaf("f", TagConfig{Type: TypeFloat}), 3.14, false},
		{"int to int", NewIntegerLeaf("i", TagConfig{Type: TypeInteger}), int64(42), false},
		{"float64 whole to int", NewIntegerLeaf("i", TagConfig{Type: TypeInteger}), float64(10), false},
		{"string to string", NewStringLeaf("s", TagConfig{Type: TypeString}), "hello", false},
		{"bool to bool", NewBoolLeaf("b", TagConfig{Type: TypeBoolean}), true, false},
		{"string to float", NewFloatLeaf("f", TagConfig{Type: TypeFloat}), "not a number", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.leaf.SetAnyValue(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("SetAnyValue(%v) error = %v, wantErr %v", tt.input, err, tt.wantErr)
			}
		})
	}
}

func TestEnumLeafAllowedValuesAndWrongTypes(t *testing.T) {
	enumLeaf := NewEnumLeaf("mode", TagConfig{Type: TypeEnum})
	enumLeaf.AddEnum(1, "Run")
	enumLeaf.AddEnum(2, "Stop")
	if err := enumLeaf.SetEnum(1); err != nil {
		t.Fatalf("SetEnum: %v", err)
	}
	if got, err := enumLeaf.GetEnum(); err != nil || got != 1 {
		t.Fatalf("GetEnum = %d/%v", got, err)
	}
	if display, err := enumLeaf.GetEnumDisplay(); err != nil || display != "Run" {
		t.Fatalf("GetEnumDisplay = %q/%v", display, err)
	}
	if err := enumLeaf.SetEnum(3); err == nil {
		t.Fatal("expected disallowed enum value error")
	}
	enumLeaf.SetEnumValues(map[int]string{3: "Auto"})
	if err := enumLeaf.SetAnyValue(float64(3)); err != nil {
		t.Fatalf("SetAnyValue enum float64: %v", err)
	}
	if allowed := enumLeaf.GetAllowedValues(); allowed[3] != "Auto" {
		t.Fatalf("allowed values = %#v", allowed)
	}
	if _, err := NewFloatLeaf("f", TagConfig{Type: TypeFloat}).GetEnum(); err != ErrWrongType {
		t.Fatalf("non-enum GetEnum error = %v", err)
	}
	nonEnum := NewStringLeaf("s", TagConfig{Type: TypeString})
	nonEnum.AddEnum(1, "ignored")
	nonEnum.SetEnumValues(map[int]string{1: "ignored"})
	if nonEnum.GetAllowedValues() != nil {
		t.Fatal("non-enum allowed values should be nil")
	}
}

func TestProcessBlockRegistrySchemasAndPipelineRoundTrip(t *testing.T) {
	blockType := "coverage-block"
	RegisterProcessBlock(blockType, func() ProcessBlock { return &stubBlock{blockType: blockType} })
	block, err := NewProcessBlockByType(blockType)
	if err != nil {
		t.Fatalf("NewProcessBlockByType: %v", err)
	}
	envelopes, err := MarshalPipeline([]ProcessBlock{block})
	if err != nil {
		t.Fatalf("MarshalPipeline: %v", err)
	}
	if len(envelopes) != 1 || envelopes[0].Type != blockType {
		t.Fatalf("envelopes = %#v", envelopes)
	}
	pipeline, err := UnmarshalPipeline(envelopes)
	if err != nil {
		t.Fatalf("UnmarshalPipeline: %v", err)
	}
	if len(pipeline) != 1 || pipeline[0].GetType() != blockType {
		t.Fatalf("pipeline = %#v", pipeline)
	}
	if _, err := UnmarshalPipeline([]ProcessBlockEnvelope{{Type: "missing"}}); err == nil {
		t.Fatal("expected unknown process block error")
	}
	found := false
	for _, schema := range GetRegisteredSchemas() {
		if schema.Type == blockType {
			found = true
		}
	}
	if !found {
		t.Fatalf("registered schemas missing %q", blockType)
	}
}

func TestRenameChildNotifiesSubtree(t *testing.T) {
	tw := NewTreeWithOperations(nil)
	if err := tw.CreateNode("default.device", ""); err != nil {
		t.Fatalf("CreateNode device: %v", err)
	}
	if err := tw.CreateNode("default.device.group", ""); err != nil {
		t.Fatalf("CreateNode group: %v", err)
	}
	if err := tw.CreateTag("default.device.group.temp", TypeFloat, TagConfig{}); err != nil {
		t.Fatalf("CreateTag: %v", err)
	}
	var changed []string
	tw.SetOnChange(func(path string, _ TreeNode) { changed = append(changed, path) })
	if err := tw.RenameChild("default.device", "group", "renamed"); err != nil {
		t.Fatalf("RenameChild: %v", err)
	}
	if _, err := tw.FindNode("default.device.group"); err == nil {
		t.Fatal("old group should not resolve")
	}
	if _, err := tw.FindLeaf("default.device.renamed.temp"); err != nil {
		t.Fatalf("new leaf path should resolve: %v", err)
	}
	if err := tw.RenameChild("default.device", "renamed", "renamed"); err != nil {
		t.Fatalf("same-name rename should be no-op: %v", err)
	}
	if err := tw.RenameChild("default.device", "", "x"); err == nil {
		t.Fatal("expected empty rename error")
	}
	if len(changed) == 0 {
		t.Fatal("expected rename notifications")
	}
}

func TestDeleteNodeNotifiesSubtree(t *testing.T) {
	tw := NewTreeWithOperations(nil)
	if err := tw.CreateNode("default.device.group.room", ""); err != nil {
		t.Fatalf("CreateNode: %v", err)
	}
	if err := tw.CreateTag("default.device.group.room.temp", TypeFloat, TagConfig{}); err != nil {
		t.Fatalf("CreateTag: %v", err)
	}

	deleted := map[string]bool{}
	tw.SetOnChange(func(path string, node TreeNode) {
		if node == nil {
			deleted[path] = true
		}
	})
	if err := tw.DeleteNode("default.device.group"); err != nil {
		t.Fatalf("DeleteNode: %v", err)
	}
	for _, path := range []string{
		"default.device.group",
		"default.device.group.room",
		"default.device.group.room.temp",
	} {
		if !deleted[path] {
			t.Fatalf("missing delete notification for %s; got %#v", path, deleted)
		}
	}
}

func TestPublishSuppressesValuesInsideDeadbandAcrossTimestampChanges(t *testing.T) {
	oldPublisher := TagValuePublisher
	defer func() { TagValuePublisher = oldPublisher }()

	pub := &captureTagValuePublisher{}
	TagValuePublisher = pub

	leaf := NewFloatLeaf("level", TagConfig{Type: TypeFloat}, TagShared{Deadband: 0.5})
	leaf.SetUpdatedTime(time.UnixMilli(1000))
	if err := leaf.Publish(10.0); err != nil {
		t.Fatal(err)
	}
	leaf.SetUpdatedTime(time.UnixMilli(2000))
	if err := leaf.Publish(10.2); err != nil {
		t.Fatal(err)
	}
	leaf.SetUpdatedTime(time.UnixMilli(3000))
	if err := leaf.Publish(10.6); err != nil {
		t.Fatal(err)
	}

	if pub.count != 2 {
		t.Fatalf("publish count = %d, want 2", pub.count)
	}
}

func TestPublishIncludesStatusChange(t *testing.T) {
	oldPublisher := TagValuePublisher
	defer func() { TagValuePublisher = oldPublisher }()

	pub := &captureTagValuePublisher{}
	TagValuePublisher = pub

	leaf := NewFloatLeaf("level", TagConfig{Type: TypeFloat}, TagShared{Deadband: 0.5})
	if err := leaf.Publish(10.0); err != nil {
		t.Fatal(err)
	}
	leaf.SetState(StatusAlarm)
	if err := leaf.Publish(10.0); err != nil {
		t.Fatal(err)
	}

	if pub.count != 2 {
		t.Fatalf("publish count = %d, want 2", pub.count)
	}
}

// ── GetTreeOrg / GetTreeDevice / GetMetricPath ───────────────────────────────

func setupOrgDeviceTree(t *testing.T) *TreeWithOperations {
	t.Helper()
	treeOps := NewTreeWithOperations(nil)
	if err := treeOps.CreateOrganisationNode("/MyOrg", ""); err != nil {
		t.Fatal(err)
	}
	if err := treeOps.CreateNode("/MyOrg/SiteA", ""); err != nil {
		t.Fatal(err)
	}
	if err := treeOps.CreateDeviceNode("/MyOrg/SiteA/Dev1", ""); err != nil {
		t.Fatal(err)
	}
	treeOps.UnlockNode("/MyOrg/SiteA/Dev1")
	if err := treeOps.CreateNode("/MyOrg/SiteA/Dev1/group1", ""); err != nil {
		t.Fatal(err)
	}
	treeOps.UnlockNode("/MyOrg/SiteA/Dev1/group1")
	if err := treeOps.CreateTag("/MyOrg/SiteA/Dev1/group1/temp", TypeFloat, TagConfig{Name: "temp", Type: TypeFloat}); err != nil {
		t.Fatal(err)
	}
	return treeOps
}

func TestGetTreeOrg(t *testing.T) {
	treeOps := setupOrgDeviceTree(t)
	leaf, err := treeOps.FindLeaf("MyOrg/SiteA/Dev1/group1/temp")
	if err != nil {
		t.Fatal(err)
	}
	if org := leaf.GetTreeOrg(); org != "MyOrg" {
		t.Errorf("GetTreeOrg() = %q, want %q", org, "MyOrg")
	}
}

func TestGetTreeDevice(t *testing.T) {
	treeOps := setupOrgDeviceTree(t)
	leaf, err := treeOps.FindLeaf("MyOrg/SiteA/Dev1/group1/temp")
	if err != nil {
		t.Fatal(err)
	}
	dev := leaf.GetTreeDevice()
	if dev != "SiteA.Dev1" {
		t.Errorf("GetTreeDevice() = %q, want %q", dev, "SiteA.Dev1")
	}
}

func TestGetMetricPath(t *testing.T) {
	treeOps := setupOrgDeviceTree(t)
	leaf, err := treeOps.FindLeaf("MyOrg/SiteA/Dev1/group1/temp")
	if err != nil {
		t.Fatal(err)
	}
	mp := leaf.GetMetricPath()
	if mp != "group1.temp" {
		t.Errorf("GetMetricPath() = %q, want %q", mp, "group1.temp")
	}
}

func TestGetTreeOrgNoOrg(t *testing.T) {
	// Leaf not under an org node
	treeOps := NewTreeWithOperations(nil)
	treeOps.CreateNode("/standalone", "")
	treeOps.UnlockNode("/standalone")
	treeOps.CreateTag("/standalone/tag", TypeFloat, TagConfig{Name: "tag", Type: TypeFloat})
	leaf, _ := treeOps.FindLeaf("standalone/tag")
	if org := leaf.GetTreeOrg(); org != "" {
		t.Errorf("GetTreeOrg() = %q, want empty", org)
	}
	if dev := leaf.GetTreeDevice(); dev != "" {
		t.Errorf("GetTreeDevice() = %q, want empty", dev)
	}
}

// ── CreateOrganisationNode / EnsureDeviceNode / LockNode ─────────────────────

func TestCreateOrganisationNode(t *testing.T) {
	treeOps := NewTreeWithOperations(nil)
	if err := treeOps.CreateOrganisationNode("/TestOrg", ""); err != nil {
		t.Fatal(err)
	}
	node, err := treeOps.FindNode("/TestOrg")
	if err != nil {
		t.Fatal(err)
	}
	if node.GetNodeType() != NodeTypeOrganisation {
		t.Errorf("NodeType = %q, want Organisation", node.GetNodeType())
	}
}

func TestEnsureDeviceNode(t *testing.T) {
	treeOps := NewTreeWithOperations(nil)
	treeOps.CreateOrganisationNode("/Org", "")
	treeOps.CreateNode("/Org/DevType", "")

	err := treeOps.EnsureDeviceNode("Org", "Org/DevType", "Org/DevType/Dev1")
	if err != nil {
		t.Fatal(err)
	}
	// Calling again should be idempotent
	err = treeOps.EnsureDeviceNode("Org", "Org/DevType", "Org/DevType/Dev1")
	if err != nil {
		t.Fatalf("second call: %v", err)
	}

	node, err := treeOps.FindNode("Org/DevType/Dev1")
	if err != nil {
		t.Fatal(err)
	}
	if node.GetNodeType() != NodeTypeDevice {
		t.Errorf("NodeType = %q, want Device", node.GetNodeType())
	}
}

func TestEnsureDeviceNodeMissingOrg(t *testing.T) {
	treeOps := NewTreeWithOperations(nil)
	err := treeOps.EnsureDeviceNode("NoOrg", "NoOrg/DT", "NoOrg/DT/D1")
	if err == nil {
		t.Fatal("expected error for missing org")
	}
}

func TestEnsureDeviceNodeAutoCreatesDevType(t *testing.T) {
	treeOps := NewTreeWithOperations(nil)
	treeOps.CreateOrganisationNode("/Org", "")
	err := treeOps.EnsureDeviceNode("Org", "Org/Missing", "Org/Missing/D1")
	if err != nil {
		t.Fatalf("EnsureDeviceNode should auto-create device type: %v", err)
	}
	if _, err := treeOps.FindNode("Org/Missing"); err != nil {
		t.Fatalf("device type should have been created: %v", err)
	}
	node, err := treeOps.FindNode("Org/Missing/D1")
	if err != nil {
		t.Fatal(err)
	}
	if node.GetNodeType() != NodeTypeDevice {
		t.Errorf("NodeType = %q, want Device", node.GetNodeType())
	}
}

func TestPropagateTemplateTagCreatesLinkedDeviceTags(t *testing.T) {
	treeOps := NewTreeWithOperations(nil)
	if err := treeOps.CreateOrganisationNode("default", ""); err != nil {
		t.Fatalf("CreateOrganisationNode: %v", err)
	}
	if err := treeOps.CreateNode("default.Templates.AirQualityStandard", ""); err != nil {
		t.Fatalf("CreateNode template: %v", err)
	}
	if err := treeOps.CreateDeviceNode("default.AirQuality.AQ001", "Templates.AirQualityStandard"); err != nil {
		t.Fatalf("CreateDeviceNode AQ001: %v", err)
	}
	if err := treeOps.CreateDeviceNode("default.AirQuality.AQ002", "Templates.AirQualityStandard"); err != nil {
		t.Fatalf("CreateDeviceNode AQ002: %v", err)
	}
	if err := treeOps.CreateDeviceNode("default.AirQuality.Other", "Templates.Other"); err != nil {
		t.Fatalf("CreateDeviceNode Other: %v", err)
	}

	shared := TagShared{Description: "Template PM2.5", Units: "ug/m3"}
	if err := treeOps.CreateTag("default.Templates.AirQualityStandard.metrics.pm25", TypeFloat, TagConfig{Name: "PM2.5"}, shared); err != nil {
		t.Fatalf("CreateTag template: %v", err)
	}
	propagated := treeOps.PropagateTemplateTag("default.Templates.AirQualityStandard.metrics.pm25")
	if len(propagated) != 2 {
		t.Fatalf("propagated len = %d, want 2: %#v", len(propagated), propagated)
	}

	tmplLeaf, err := treeOps.FindLeaf("default.Templates.AirQualityStandard.metrics.pm25")
	if err != nil {
		t.Fatalf("FindLeaf template: %v", err)
	}
	for _, path := range []string{
		"default.AirQuality.AQ001.metrics.pm25",
		"default.AirQuality.AQ002.metrics.pm25",
	} {
		leaf, err := treeOps.FindLeaf(path)
		if err != nil {
			t.Fatalf("FindLeaf %s: %v", path, err)
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
	if _, err := treeOps.FindLeaf("default.AirQuality.Other.metrics.pm25"); err == nil {
		t.Fatalf("unrelated template instance received propagated tag")
	}
}

func TestPropagateTemplateDeleteRemovesLinkedDeviceTagsAndNodes(t *testing.T) {
	treeOps := NewTreeWithOperations(nil)
	if err := treeOps.CreateOrganisationNode("default", ""); err != nil {
		t.Fatalf("CreateOrganisationNode: %v", err)
	}
	if err := treeOps.CreateNode("default.Templates.AirQualityStandard", ""); err != nil {
		t.Fatalf("CreateNode template: %v", err)
	}
	for _, path := range []string{
		"default.AirQuality.AQ001",
		"default.AirQuality.AQ002",
	} {
		if err := treeOps.CreateDeviceNode(path, "Templates.AirQualityStandard"); err != nil {
			t.Fatalf("CreateDeviceNode %s: %v", path, err)
		}
	}
	if err := treeOps.CreateDeviceNode("default.AirQuality.Other", "Templates.Other"); err != nil {
		t.Fatalf("CreateDeviceNode Other: %v", err)
	}

	if err := treeOps.CreateTag("default.Templates.AirQualityStandard.metrics.pm25", TypeFloat, TagConfig{Name: "PM2.5"}, TagShared{}); err != nil {
		t.Fatalf("CreateTag pm25: %v", err)
	}
	if err := treeOps.CreateTag("default.Templates.AirQualityStandard.metrics.temp", TypeFloat, TagConfig{Name: "Temp"}, TagShared{}); err != nil {
		t.Fatalf("CreateTag temp: %v", err)
	}
	if propagated := treeOps.PropagateTemplateTag("default.Templates.AirQualityStandard.metrics.pm25"); len(propagated) != 2 {
		t.Fatalf("pm25 propagated len = %d, want 2: %#v", len(propagated), propagated)
	}
	if propagated := treeOps.PropagateTemplateTag("default.Templates.AirQualityStandard.metrics.temp"); len(propagated) != 2 {
		t.Fatalf("temp propagated len = %d, want 2: %#v", len(propagated), propagated)
	}

	deletedTags := treeOps.PropagateTemplateTagDelete("default.Templates.AirQualityStandard.metrics.pm25")
	if len(deletedTags) != 2 {
		t.Fatalf("deletedTags len = %d, want 2: %#v", len(deletedTags), deletedTags)
	}
	if _, err := treeOps.FindLeaf("default.Templates.AirQualityStandard.metrics.pm25"); err != nil {
		t.Fatalf("template pm25 should still exist before direct delete: %v", err)
	}
	for _, path := range []string{
		"default.AirQuality.AQ001.metrics.pm25",
		"default.AirQuality.AQ002.metrics.pm25",
	} {
		if _, err := treeOps.FindLeaf(path); err == nil {
			t.Fatalf("%s still exists after propagated tag delete", path)
		}
	}
	if _, err := treeOps.FindLeaf("default.AirQuality.AQ001.metrics.temp"); err != nil {
		t.Fatalf("sibling template-linked tag should remain until node delete: %v", err)
	}

	deletedNodes := treeOps.PropagateTemplateNodeDelete("default.Templates.AirQualityStandard.metrics")
	if len(deletedNodes) != 2 {
		t.Fatalf("deletedNodes len = %d, want 2: %#v", len(deletedNodes), deletedNodes)
	}
	for _, path := range []string{
		"default.AirQuality.AQ001.metrics",
		"default.AirQuality.AQ002.metrics",
	} {
		if _, err := treeOps.FindNode(path); err == nil {
			t.Fatalf("%s still exists after propagated node delete", path)
		}
	}
	if _, err := treeOps.FindNode("default.AirQuality.Other.metrics"); err == nil {
		t.Fatalf("unrelated template instance node was deleted")
	}
}

func TestLockNode(t *testing.T) {
	treeOps := NewTreeWithOperations(nil)
	treeOps.CreateNode("/n1", "")
	treeOps.UnlockNode("/n1")

	treeOps.LockNode("/n1")
	node, _ := treeOps.FindNode("/n1")
	if !node.IsLocked() {
		t.Error("node should be locked after LockNode")
	}

	treeOps.UnlockNode("/n1")
	if node.IsLocked() {
		t.Error("node should be unlocked after UnlockNode")
	}
}

// ── FindNodeOrLeaf ───────────────────────────────────────────────────────────

func TestFindNodeOrLeaf(t *testing.T) {
	treeOps := NewTreeWithOperations(nil)
	treeOps.CreateNode("/a/b", "")
	treeOps.UnlockNode("/a/b")
	treeOps.CreateTag("/a/b/leaf", TypeFloat, TagConfig{Name: "leaf", Type: TypeFloat})

	// Find node
	tn, err := treeOps.FindNodeOrLeaf("a/b")
	if err != nil {
		t.Fatal(err)
	}
	if !tn.IsNode() {
		t.Error("expected a node")
	}

	// Find leaf
	tn, err = treeOps.FindNodeOrLeaf("a/b/leaf")
	if err != nil {
		t.Fatal(err)
	}
	if tn.IsNode() {
		t.Error("expected a leaf")
	}

	// Not found
	_, err = treeOps.FindNodeOrLeaf("a/b/missing")
	if err == nil {
		t.Error("expected error for missing path")
	}
}

// ── WalkLeaves ───────────────────────────────────────────────────────────────

func TestWalkLeaves(t *testing.T) {
	treeOps := NewTreeWithOperations(nil)
	treeOps.CreateNode("/org/dt", "")
	treeOps.UnlockNode("/org/dt")
	treeOps.CreateTag("/org/dt/a", TypeFloat, TagConfig{Name: "a"})
	treeOps.CreateTag("/org/dt/b", TypeInteger, TagConfig{Name: "b"})
	treeOps.CreateNode("/org/dt/sub", "")
	treeOps.UnlockNode("/org/dt/sub")
	treeOps.CreateTag("/org/dt/sub/c", TypeString, TagConfig{Name: "c"})

	var leaves []string
	treeOps.WalkLeaves(func(path string, leaf Leaf) {
		leaves = append(leaves, path)
	})
	if len(leaves) != 3 {
		t.Errorf("expected 3 leaves, got %d: %v", len(leaves), leaves)
	}
}

// ── ScalarType.String ────────────────────────────────────────────────────────

func TestScalarTypeString(t *testing.T) {
	tests := []struct {
		st   ScalarType
		want string
	}{
		{TypeInteger, "integer"},
		{TypeFloat, "float"},
		{TypeString, "string"},
		{TypeBoolean, "boolean"},
		{TypeEnum, "enum"},
		{ScalarType(99), "unknown"},
	}
	for _, tt := range tests {
		if got := tt.st.String(); got != tt.want {
			t.Errorf("ScalarType(%d).String() = %q, want %q", tt.st, got, tt.want)
		}
	}
}

// ── SetLeafValue ────────────────────────────────────────────────────────────

func TestSetLeafValue(t *testing.T) {
	treeOps := NewTreeWithOperations(nil)
	treeOps.CreateNode("/d", "")
	treeOps.UnlockNode("/d")
	treeOps.CreateTag("/d/t", TypeFloat, TagConfig{Name: "t", Type: TypeFloat})

	err := treeOps.SetLeafValue("d/t", 42.5)
	if err != nil {
		t.Fatal(err)
	}
	leaf, _ := treeOps.FindLeaf("d/t")
	v, _ := leaf.GetFloat()
	if v != 42.5 {
		t.Errorf("value = %v, want 42.5", v)
	}
}

// ── PipelineEnvelopes on empty pipeline ─────────────────────────────────────

func TestPipelineEnvelopesEmpty(t *testing.T) {
	s := TagShared{}
	envs, err := s.PipelineEnvelopes()
	if err != nil || envs != nil {
		t.Errorf("expected nil, nil; got %v, %v", envs, err)
	}
}

// ── GetPipeline with template fallback ──────────────────────────────────────

func TestGetPipelineFromTemplate(t *testing.T) {
	tmpl := NewFloatLeaf("tmpl", TagConfig{Type: TypeFloat})
	block := &stubBlock{blockType: "publish"}
	tmplShared := TagShared{Pipeline: []ProcessBlock{block}}
	tmpl.SetShared(tmplShared)

	leaf := NewFloatLeaf("leaf", TagConfig{Type: TypeFloat})
	leaf.SetTemplate(tmpl)

	pipeline := leaf.GetPipeline()
	if len(pipeline) != 1 {
		t.Fatalf("expected 1 block from template, got %d", len(pipeline))
	}
	if pipeline[0].GetType() != "publish" {
		t.Errorf("block type = %q", pipeline[0].GetType())
	}
}

// ── Notify callbacks ────────────────────────────────────────────────────────

func TestSetOnChange(t *testing.T) {
	var notified string
	treeOps := NewTreeWithOperations(nil)
	treeOps.SetOnChange(func(path string, node TreeNode) {
		notified = path
	})
	treeOps.CreateNode("/notify_test", "")
	if notified != "/notify_test" {
		t.Errorf("onChange not called, notified=%q", notified)
	}
}

func TestSetOnStructureChange(t *testing.T) {
	var notified string
	treeOps := NewTreeWithOperations(nil)
	treeOps.SetOnStructureChange(func(path string, node TreeNode) {
		notified = path
	})
	treeOps.CreateNode("/struct_test", "")
	if notified != "/struct_test" {
		t.Errorf("onStructureChange not called, notified=%q", notified)
	}
}

func TestNotifyChange(t *testing.T) {
	var notified bool
	treeOps := NewTreeWithOperations(func(path string, node TreeNode) {
		notified = true
	})
	treeOps.CreateNode("/nc", "")
	node, _ := treeOps.FindNode("/nc")
	notified = false
	treeOps.NotifyChange("/nc", node)
	if !notified {
		t.Error("NotifyChange should trigger onChange callback")
	}
}
