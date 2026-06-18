package tree

import (
	"testing"
)

func TestNewTree(t *testing.T) {
	tree := NewTree()
	if tree.Root == nil {
		t.Fatal("Root should not be nil")
	}
	if tree.Root.Name != "root" {
		t.Errorf("Expected root name to be 'root', got '%s'", tree.Root.Name)
	}
}

func TestResolveDotPath(t *testing.T) {
	tests := []struct {
		input    string
		expected []string
	}{
		{"/a/b/c", []string{"a", "b", "c"}},
		{"a/b/c", []string{"a", "b", "c"}},
		{"/", []string{}},
		{"", []string{}},
		{"/single", []string{"single"}},
	}

	for _, test := range tests {
		result := ResolvePath(test.input)
		if len(result) != len(test.expected) {
			t.Errorf("ResolvePath(%s): expected %v, got %v", test.input, test.expected, result)
			continue
		}
		for i := range result {
			if result[i] != test.expected[i] {
				t.Errorf("ResolvePath(%s)[%d]: expected %s, got %s", test.input, i, test.expected[i], result[i])
			}
		}
	}
}

func TestCreateNode(t *testing.T) {
	tree := NewTreeWithOperations(nil)

	// Create a simple node
	err := tree.CreateNode("device1", "")
	if err != nil {
		t.Fatalf("Failed to create node: %v", err)
	}

	// Verify it exists
	node, err := tree.FindNode("device1")
	if err != nil {
		t.Fatalf("Failed to find created node: %v", err)
	}
	if node.Name != "device1" {
		t.Errorf("Expected name 'device1', got '%s'", node.Name)
	}

	// Create if not exists - should succeed
	err = tree.CreateNode("device1", "")
	if err != nil {
		t.Fatalf("CreateNode should be idempotent: %v", err)
	}
}

func TestCreateNodeWithTemplate(t *testing.T) {
	tree := NewTreeWithOperations(nil)

	err := tree.CreateNode("device1", "sensor_template")
	if err != nil {
		t.Fatalf("Failed to create node with template: %v", err)
	}

	node, err := tree.FindNode("device1")
	if err != nil {
		t.Fatalf("Failed to find node: %v", err)
	}
	if node.GetTemplateName() != "sensor_template" {
		t.Errorf("Expected template name 'sensor_template', got '%s'", node.GetTemplateName())
	}
}

func TestCreateNestedNodes(t *testing.T) {
	tree := NewTreeWithOperations(nil)

	// Create nested structure
	err := tree.CreateNode("building.floor1.room1", "")
	if err != nil {
		t.Fatalf("Failed to create nested nodes: %v", err)
	}

	// Verify all intermediate nodes exist
	building, err := tree.FindNode("building")
	if err != nil {
		t.Fatalf("Failed to find building: %v", err)
	}
	if building.Name != "building" {
		t.Errorf("Expected 'building', got '%s'", building.Name)
	}

	room, err := tree.FindNode("building.floor1.room1")
	if err != nil {
		t.Fatalf("Failed to find room: %v", err)
	}
	if room.Name != "room1" {
		t.Errorf("Expected 'room1', got '%s'", room.Name)
	}
}

func TestCreateTag(t *testing.T) {
	tree := NewTreeWithOperations(nil)

	// First create parent node
	err := tree.CreateNode("device1", "")
	if err != nil {
		t.Fatalf("Failed to create parent node: %v", err)
	}
	tree.UnlockNode("device1")

	// Create a tag
	config := TagConfig{
		Name: "Temperature",
	}
	err = tree.CreateTag("device1.temperature", TypeFloat, config)
	if err != nil {
		t.Fatalf("Failed to create tag: %v", err)
	}

	// Verify it exists
	leaf, err := tree.FindLeaf("device1.temperature")
	if err != nil {
		t.Fatalf("Failed to find created tag: %v", err)
	}
	if leaf.GetName() != "temperature" {
		t.Errorf("Expected name 'temperature', got '%s'", leaf.GetName())
	}
	if leaf.GetConfig().Name != "Temperature" {
		t.Errorf("Expected Name 'Temperature', got '%s'", leaf.GetConfig().Name)
	}
	pipeline := leaf.GetPipeline()
	if len(pipeline) != 1 || pipeline[0].GetType() != "publish" {
		t.Fatalf("new tag pipeline = %#v, want default publish block", pipeline)
	}
}

func TestCreateTagWithAutoParent(t *testing.T) {
	tree := NewTreeWithOperations(nil)

	// Create tag without creating parent first
	config := TagConfig{Name: "Auto Parent Test"}
	err := tree.CreateTag("auto.parent.tag", TypeInteger, config)
	if err != nil {
		t.Fatalf("Failed to create tag with auto parent: %v", err)
	}

	// Verify parent was created
	_, err = tree.FindNode("auto.parent")
	if err != nil {
		t.Fatalf("Auto parent should have been created: %v", err)
	}

	// Verify tag was created
	_, err = tree.FindLeaf("auto.parent.tag")
	if err != nil {
		t.Fatalf("Tag should exist: %v", err)
	}
}

func TestDeleteTag(t *testing.T) {
	tree := NewTreeWithOperations(nil)

	// Create tag
	tree.CreateNode("device1", "")
	tree.UnlockNode("device1")
	tree.CreateTag("device1.temperature", TypeFloat, TagConfig{Name: "Temp"})

	// Delete it
	err := tree.DeleteTag("device1.temperature")
	if err != nil {
		t.Fatalf("Failed to delete tag: %v", err)
	}

	// Verify it's gone
	_, err = tree.FindLeaf("device1.temperature")
	if err == nil {
		t.Fatal("Tag should have been deleted")
	}
}

func TestDeleteNodeCascade(t *testing.T) {
	tree := NewTreeWithOperations(nil)

	// Create nested structure
	tree.CreateNode("building.floor1", "")
	tree.UnlockNode("building.floor1")
	tree.CreateTag("building.floor1.temp", TypeFloat, TagConfig{Name: "Temp"})
	tree.CreateNode("building.floor1.room1", "")
	tree.CreateTag("building.floor1.room1.humidity", TypeFloat, TagConfig{Name: "Humidity"})

	// Delete floor (should cascade)
	err := tree.DeleteNode("building.floor1")
	if err != nil {
		t.Fatalf("Failed to delete node: %v", err)
	}

	// Verify floor is gone
	_, err = tree.FindNode("building.floor1")
	if err == nil {
		t.Fatal("Floor should have been deleted")
	}

	// Verify children are gone
	_, err = tree.FindLeaf("building.floor1.temp")
	if err == nil {
		t.Fatal("Temp tag should have been deleted")
	}

	_, err = tree.FindNode("building.floor1.room1")
	if err == nil {
		t.Fatal("Room1 should have been deleted")
	}
}

func TestDeleteNodeMarksChildrenAsDeleted(t *testing.T) {
	tree := NewTreeWithOperations(nil)

	// Create nested structure: building.floor1/room1/sensor
	tree.CreateNode("building.floor1", "")
	tree.UnlockNode("building.floor1")
	tree.CreateTag("building.floor1.temp", TypeFloat, TagConfig{Name: "Temp"})
	tree.CreateNode("building.floor1.room1", "")
	tree.CreateTag("building.floor1.room1.humidity", TypeFloat, TagConfig{Name: "Humidity"})
	tree.CreateNode("building.floor1.room1.sensor", "")
	tree.CreateTag("building.floor1.room1.sensor.value", TypeInteger, TagConfig{Name: "Value"})

	// Get references to nodes before deletion
	floor1, _ := tree.FindNode("building.floor1")
	room1, _ := tree.FindNode("building.floor1.room1")
	sensor, _ := tree.FindNode("building.floor1.room1.sensor")

	// Delete floor1
	err := tree.DeleteNode("building.floor1")
	if err != nil {
		t.Fatalf("Failed to delete node: %v", err)
	}

	// Verify floor1 is marked as deleted
	if !floor1.IsDeleted() {
		t.Error("floor1 should be marked as deleted")
	}

	// Verify room1 is marked as deleted
	if !room1.IsDeleted() {
		t.Error("room1 should be marked as deleted")
	}

	// Verify sensor is marked as deleted
	if !sensor.IsDeleted() {
		t.Error("sensor should be marked as deleted")
	}

	// Verify operations on deleted nodes return ErrNodeDeleted
	err = floor1.AddChild(NewNode("test"))
	if err != ErrNodeDeleted {
		t.Errorf("Expected ErrNodeDeleted, got %v", err)
	}
}

func TestDeleteRoot(t *testing.T) {
	tree := NewTreeWithOperations(nil)

	err := tree.DeleteNode(".")
	if err == nil {
		t.Fatal("Should not be able to delete root")
	}
}

func TestCannotDeleteNodeAsTag(t *testing.T) {
	tree := NewTreeWithOperations(nil)

	tree.CreateNode("device1", "")

	err := tree.DeleteTag("device1")
	if err == nil {
		t.Fatal("Should not be able to delete node as tag")
	}
}

func TestCannotDeleteTagAsNode(t *testing.T) {
	tree := NewTreeWithOperations(nil)

	tree.CreateNode("device1", "")
	tree.UnlockNode("device1")
	tree.CreateTag("device1.temp", TypeFloat, TagConfig{Name: "Temp"})

	err := tree.DeleteNode("device1.temp")
	if err == nil {
		t.Fatal("Should not be able to delete tag as node")
	}
}

func TestTypeSpecificLeaves(t *testing.T) {
	tree := NewTreeWithOperations(nil)
	tree.CreateNode("device1", "")
	tree.UnlockNode("device1")

	// Create different types of leaves
	tree.CreateTag("device1.temp", TypeFloat, TagConfig{Name: "Temperature"})
	tree.CreateTag("device1.count", TypeInteger, TagConfig{Name: "Count"})
	tree.CreateTag("device1.name", TypeString, TagConfig{Name: "Name"})
	tree.CreateTag("device1.active", TypeBoolean, TagConfig{Name: "Active"})

	// Test float leaf
	floatLeaf, _ := tree.FindLeaf("device1.temp")
	if _, ok := floatLeaf.(*LeafNode); !ok {
		t.Errorf("Expected *LeafNode, got %T", floatLeaf)
	}
	if floatLeaf.ValueType() != TypeFloat {
		t.Errorf("Expected TypeFloat, got %v", floatLeaf.ValueType())
	}
	floatLeaf.SetFloat(25.5)
	val, err := floatLeaf.GetFloat()
	if err != nil || val != 25.5 {
		t.Errorf("Expected 25.5, got %v (err: %v)", val, err)
	}

	// Test integer leaf
	intLeaf, _ := tree.FindLeaf("device1.count")
	if _, ok := intLeaf.(*LeafNode); !ok {
		t.Errorf("Expected *LeafNode, got %T", intLeaf)
	}
	if intLeaf.ValueType() != TypeInteger {
		t.Errorf("Expected TypeInteger, got %v", intLeaf.ValueType())
	}
	intLeaf.SetInt(42)
	intVal, err := intLeaf.GetInt()
	if err != nil || intVal != 42 {
		t.Errorf("Expected 42, got %v (err: %v)", intVal, err)
	}

	// Test string leaf
	stringLeaf, _ := tree.FindLeaf("device1.name")
	if _, ok := stringLeaf.(*LeafNode); !ok {
		t.Errorf("Expected *LeafNode, got %T", stringLeaf)
	}
	if stringLeaf.ValueType() != TypeString {
		t.Errorf("Expected TypeString, got %v", stringLeaf.ValueType())
	}
	stringLeaf.SetString("device-A")
	strVal, err := stringLeaf.GetString()
	if err != nil || strVal != "device-A" {
		t.Errorf("Expected 'device-A', got %v (err: %v)", strVal, err)
	}

	// Test bool leaf
	boolLeaf, _ := tree.FindLeaf("device1.active")
	if _, ok := boolLeaf.(*LeafNode); !ok {
		t.Errorf("Expected *LeafNode, got %T", boolLeaf)
	}
	if boolLeaf.ValueType() != TypeBoolean {
		t.Errorf("Expected TypeBoolean, got %v", boolLeaf.ValueType())
	}
	boolLeaf.SetBool(true)
	boolVal, err := boolLeaf.GetBool()
	if err != nil || boolVal != true {
		t.Errorf("Expected true, got %v (err: %v)", boolVal, err)
	}
}

func TestWrongTypeAccessors(t *testing.T) {
	tree := NewTreeWithOperations(nil)
	tree.CreateNode("device1", "")
	tree.UnlockNode("device1")
	tree.CreateTag("device1.temp", TypeFloat, TagConfig{Name: "Temperature"})

	leaf, _ := tree.FindLeaf("device1.temp")

	// Try to access float leaf as integer
	_, err := leaf.GetInt()
	if err != ErrWrongType {
		t.Errorf("Expected ErrWrongType, got %v", err)
	}

	// Try to set float leaf as integer
	err = leaf.SetInt(42)
	if err != ErrWrongType {
		t.Errorf("Expected ErrWrongType, got %v", err)
	}
}

func TestEnumLeaf(t *testing.T) {
	tree := NewTreeWithOperations(nil)
	tree.CreateNode("device1", "")
	tree.UnlockNode("device1")

	config := TagConfig{
		Name: "Status",
	}
	shared := TagShared{
		EnumValues: map[int]string{0: "Off", 1: "On", 2: "Error"},
	}
	tree.CreateTag("device1.status", TypeEnum, config, shared)

	leaf, _ := tree.FindLeaf("device1.status")
	enumLeaf, ok := leaf.(*LeafNode)
	if !ok {
		t.Fatalf("Expected *LeafNode, got %T", leaf)
	}
	if enumLeaf.ValueType() != TypeEnum {
		t.Fatalf("Expected TypeEnum, got %v", enumLeaf.ValueType())
	}

	// Set valid enum value
	err := enumLeaf.SetEnum(1)
	if err != nil {
		t.Errorf("Expected no error, got %v", err)
	}

	// Try to set invalid enum value
	err = enumLeaf.SetEnum(99)
	if err == nil {
		t.Error("Expected error for invalid enum value")
	}

	// Get display string
	enumLeaf.SetEnum(2)
	display, err := enumLeaf.GetEnumDisplay()
	if err != nil || display != "Error" {
		t.Errorf("Expected 'Error', got %v (err: %v)", display, err)
	}
}

func TestCreateDeviceNode(t *testing.T) {
	tree := NewTreeWithOperations(nil)
	tree.CreateNode("org", "")
	tree.UnlockNode("org") // unlock top level node to allow child creation

	err := tree.CreateDeviceNode("org.dev1", "")
	if err != nil {
		t.Fatalf("Failed to create device node: %v", err)
	}

	node, err := tree.FindNode("org.dev1")
	if err != nil {
		t.Fatalf("Failed to find device node: %v", err)
	}
	if node.GetNodeType() != NodeTypeDevice {
		t.Errorf("Expected NodeTypeDevice, got %s", node.GetNodeType())
	}

	if _, err := tree.FindNode("org.dev1.meta"); err != nil {
		t.Errorf("Expected 'meta' subnode: %v", err)
	}
	if _, err := tree.FindNode("org.dev1.kpi"); err != nil {
		t.Errorf("Expected 'kpi' subnode: %v", err)
	}
}

func TestNodeLocking(t *testing.T) {
	t.Skip("node locking feature deferred")
	tree := NewTreeWithOperations(nil)
	tree.CreateNode("org1", "")

	// Default tree creates /org1 as Locked because it's a top-level node
	err := tree.CreateNode("org1.dev1", "")
	if err == nil {
		t.Fatal("Expected error creating node under locked parent")
	}

	tree.UnlockNode("org1")
	err = tree.CreateNode("org1.dev1", "")
	if err != nil {
		t.Fatalf("Expected success creating node after unlock: %v", err)
	}

	tree.LockNode("org1")
	err = tree.DeleteNode("org1.dev1")
	if err == nil {
		t.Fatal("Expected error deleting node under locked parent")
	}
}
