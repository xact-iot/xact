package persistence

import (
	"testing"

	"github.com/xact-iot/xact/rtdb/tree"
)

func TestSerializeDeserializeRoundTrip(t *testing.T) {
	// Build a tree
	treeOps := tree.NewTreeWithOperations(nil)

	treeOps.CreateNode("/building", "")
	treeOps.UnlockNode("/building")
	treeOps.CreateNode("/building/floor1", "")
	treeOps.UnlockNode("/building/floor1")
	treeOps.CreateNode("/building/floor2", "")
	treeOps.UnlockNode("/building/floor2")
	treeOps.CreateNode("/devices", "")
	treeOps.UnlockNode("/devices")
	treeOps.CreateNode("/devices/device1", "sensor_template")
	treeOps.UnlockNode("/devices/device1")

	treeOps.CreateTag("/building/floor1/temperature", tree.TypeFloat, tree.TagConfig{
		Name: "Temperature",
	}, tree.TagShared{
		Description: "Floor 1 temperature sensor",
		Units:       "°C",
		Deadband:    0.25,
	})
	treeOps.CreateTag("/building/floor1/humidity", tree.TypeFloat, tree.TagConfig{
		Name: "Humidity",
	})
	treeOps.CreateTag("/building/floor2/temperature", tree.TypeFloat, tree.TagConfig{
		Name: "Temperature",
	})
	treeOps.CreateTag("/devices/device1/status", tree.TypeString, tree.TagConfig{
		Name: "Status",
	})
	treeOps.CreateTag("/devices/device1/mode", tree.TypeEnum, tree.TagConfig{
		Name: "Mode",
	}, tree.TagShared{
		EnumValues: map[int]string{0: "Off", 1: "On", 2: "Auto"},
	})

	// Set a description on a node
	node, _ := treeOps.FindNode("/building/floor1")
	node.SetDescription("First floor")

	// Serialize
	config, err := SerializeTree(treeOps.Root)
	if err != nil {
		t.Fatalf("SerializeTree failed: %v", err)
	}

	if len(config.Nodes) == 0 {
		t.Fatal("expected nodes in config")
	}

	// Deserialize into a fresh tree
	treeOps2 := tree.NewTreeWithOperations(nil)
	err = DeserializeTree(config, treeOps2)
	if err != nil {
		t.Fatalf("DeserializeTree failed: %v", err)
	}

	// Verify structure
	n, err := treeOps2.FindNode("/building/floor1")
	if err != nil {
		t.Fatalf("expected /building/floor1: %v", err)
	}
	if n.GetDescription() != "First floor" {
		t.Errorf("expected description 'First floor', got '%s'", n.GetDescription())
	}

	// Verify device1 has template name
	d1, err := treeOps2.FindNode("/devices/device1")
	if err != nil {
		t.Fatalf("expected /devices/device1: %v", err)
	}
	if d1.GetTemplateName() != "sensor_template" {
		t.Errorf("expected template name 'sensor_template', got '%s'", d1.GetTemplateName())
	}

	// Verify leaves
	leaf, err := treeOps2.FindLeaf("/building/floor1/temperature")
	if err != nil {
		t.Fatalf("expected /building/floor1/temperature: %v", err)
	}
	if leaf.ValueType() != tree.TypeFloat {
		t.Errorf("expected float type, got %v", leaf.ValueType())
	}
	if leaf.GetDescription() != "Floor 1 temperature sensor" {
		t.Errorf("expected description, got '%s'", leaf.GetDescription())
	}
	if leaf.GetShared().Units != "°C" {
		t.Errorf("expected units °C, got %q", leaf.GetShared().Units)
	}
	if leaf.GetShared().Deadband != 0.25 {
		t.Errorf("expected deadband 0.25, got %v", leaf.GetShared().Deadband)
	}

	// Verify enum leaf
	enumLeaf, err := treeOps2.FindLeaf("/devices/device1/mode")
	if err != nil {
		t.Fatalf("expected /devices/device1/mode: %v", err)
	}
	if enumLeaf.ValueType() != tree.TypeEnum {
		t.Errorf("expected enum type, got %v", enumLeaf.ValueType())
	}

	// Verify string leaf
	statusLeaf, err := treeOps2.FindLeaf("/devices/device1/status")
	if err != nil {
		t.Fatalf("expected /devices/device1/status: %v", err)
	}
	if statusLeaf.ValueType() != tree.TypeString {
		t.Errorf("expected string type, got %v", statusLeaf.ValueType())
	}
}

func TestSerializeEmptyTree(t *testing.T) {
	treeOps := tree.NewTreeWithOperations(nil)

	config, err := SerializeTree(treeOps.Root)
	if err != nil {
		t.Fatalf("SerializeTree failed: %v", err)
	}

	if len(config.Nodes) != 0 {
		t.Errorf("expected 0 nodes for empty tree, got %d", len(config.Nodes))
	}
}

func TestDeserializeTemplateLinkedLeafWithZoneDevicePath(t *testing.T) {
	config := &TreeConfig{Nodes: []NodeConfig{
		{Path: ".default", Type: string(tree.NodeTypeOrganisation)},
		{Path: ".default.Templates"},
		{Path: ".default.Templates.AirQualityStandard"},
		{Path: ".default.Templates.AirQualityStandard.env", Children: []LeafConfig{
			{Name: "pressure", Type: "float", Description: "Template pressure"},
		}},
		{Path: ".default.LA_LongBeach"},
		{Path: ".default.LA_LongBeach.AirQuality"},
		{Path: ".default.LA_LongBeach.AirQuality.AQ-S-0002", Type: string(tree.NodeTypeDevice), TemplateName: "Templates.AirQualityStandard"},
		{Path: ".default.LA_LongBeach.AirQuality.AQ-S-0002.env", Children: []LeafConfig{
			{Name: "pressure", Type: "float", TemplateName: "Templates.AirQualityStandard"},
		}},
	}}

	treeOps := tree.NewTreeWithOperations(nil)
	if err := DeserializeTree(config, treeOps); err != nil {
		t.Fatalf("DeserializeTree: %v", err)
	}

	tmplLeaf, err := treeOps.FindLeaf("default.Templates.AirQualityStandard.env.pressure")
	if err != nil {
		t.Fatalf("template leaf: %v", err)
	}
	deviceLeaf, err := treeOps.FindLeaf("default.LA_LongBeach.AirQuality.AQ-S-0002.env.pressure")
	if err != nil {
		t.Fatalf("device leaf: %v", err)
	}
	if deviceLeaf.GetTemplate() != tmplLeaf {
		t.Fatalf("device leaf template pointer was not restored")
	}
	if deviceLeaf.GetDescription() != "Template pressure" {
		t.Fatalf("device leaf description = %q, want inherited template description", deviceLeaf.GetDescription())
	}
}
