package persistence

import (
	"testing"

	"github.com/xact-iot/xact/rtdb/tree"
)

func TestParseScalarType(t *testing.T) {
	tests := []struct {
		input string
		want  tree.ScalarType
	}{
		{"integer", tree.TypeInteger},
		{"float", tree.TypeFloat},
		{"string", tree.TypeString},
		{"boolean", tree.TypeBoolean},
		{"enum", tree.TypeEnum},
		{"unknown", tree.TypeString}, // default
		{"", tree.TypeString},
	}
	for _, tt := range tests {
		got := parseScalarType(tt.input)
		if got != tt.want {
			t.Errorf("parseScalarType(%q) = %v, want %v", tt.input, got, tt.want)
		}
	}
}

func TestSerializeDeserializeDeviceNode(t *testing.T) {
	treeOps := tree.NewTreeWithOperations(nil)
	treeOps.CreateNode("/org", "")
	treeOps.UnlockNode("/org")
	treeOps.CreateDeviceNode("/org/dev1", "tmpl.VMS")
	treeOps.UnlockNode("/org/dev1")
	treeOps.CreateTag("/org/dev1/temp", tree.TypeFloat, tree.TagConfig{Name: "temp"})

	config, err := SerializeTree(treeOps.Root)
	if err != nil {
		t.Fatal(err)
	}

	// Verify device node serialized with correct type
	found := false
	for _, nc := range config.Nodes {
		if nc.Path == ".org.dev1" {
			found = true
			if nc.Type != string(tree.NodeTypeDevice) {
				t.Errorf("type = %q, want Device", nc.Type)
			}
			if nc.TemplateName != "tmpl.VMS" {
				t.Errorf("template = %q", nc.TemplateName)
			}
		}
	}
	if !found {
		t.Error("device node not found in serialized config")
	}

	// Deserialize
	treeOps2 := tree.NewTreeWithOperations(nil)
	if err := DeserializeTree(config, treeOps2); err != nil {
		t.Fatal(err)
	}
	node, err := treeOps2.FindNode("/org/dev1")
	if err != nil {
		t.Fatal(err)
	}
	if node.GetNodeType() != tree.NodeTypeDevice {
		t.Errorf("deserialized type = %q", node.GetNodeType())
	}
}

func TestSerializeDeserializeOrgNode(t *testing.T) {
	treeOps := tree.NewTreeWithOperations(nil)
	treeOps.CreateOrganisationNode("/MyOrg", "")

	config, err := SerializeTree(treeOps.Root)
	if err != nil {
		t.Fatal(err)
	}

	treeOps2 := tree.NewTreeWithOperations(nil)
	if err := DeserializeTree(config, treeOps2); err != nil {
		t.Fatal(err)
	}
	node, err := treeOps2.FindNode("/MyOrg")
	if err != nil {
		t.Fatal(err)
	}
	if node.GetNodeType() != tree.NodeTypeOrganisation {
		t.Errorf("type = %q, want Organisation", node.GetNodeType())
	}
}

func TestSerializeDeserializeIsArray(t *testing.T) {
	treeOps := tree.NewTreeWithOperations(nil)
	treeOps.CreateNode("/parent", "")
	treeOps.UnlockNode("/parent")
	treeOps.CreateNode("/parent/batteries", "")

	node, _ := treeOps.FindNode("/parent/batteries")
	node.SetIsArray(true)

	config, err := SerializeTree(treeOps.Root)
	if err != nil {
		t.Fatal(err)
	}

	// Verify isArray in serialized form
	found := false
	for _, nc := range config.Nodes {
		if nc.Path == ".parent.batteries" {
			found = true
			if !nc.IsArray {
				t.Error("isArray should be true in serialized config")
			}
		}
	}
	if !found {
		t.Error("batteries node not found")
	}

	// Deserialize
	treeOps2 := tree.NewTreeWithOperations(nil)
	if err := DeserializeTree(config, treeOps2); err != nil {
		t.Fatal(err)
	}
	node2, err := treeOps2.FindNode("/parent/batteries")
	if err != nil {
		t.Fatal(err)
	}
	if !node2.GetIsArray() {
		t.Error("isArray should be true after deserialization")
	}
}

func TestSerializeDeserializeLockedNode(t *testing.T) {
	treeOps := tree.NewTreeWithOperations(nil)
	treeOps.CreateNode("/locked_parent", "")
	treeOps.LockNode("/locked_parent")

	config, err := SerializeTree(treeOps.Root)
	if err != nil {
		t.Fatal(err)
	}

	treeOps2 := tree.NewTreeWithOperations(nil)
	if err := DeserializeTree(config, treeOps2); err != nil {
		t.Fatal(err)
	}
	node, err := treeOps2.FindNode("/locked_parent")
	if err != nil {
		t.Fatal(err)
	}
	if !node.IsLocked() {
		t.Error("node should be locked after deserialization")
	}
}

func TestSerializeDeserializeAllLeafTypes(t *testing.T) {
	treeOps := tree.NewTreeWithOperations(nil)
	treeOps.CreateNode("/d", "")
	treeOps.UnlockNode("/d")

	treeOps.CreateTag("/d/i", tree.TypeInteger, tree.TagConfig{Name: "i"})
	treeOps.CreateTag("/d/f", tree.TypeFloat, tree.TagConfig{Name: "f"})
	treeOps.CreateTag("/d/s", tree.TypeString, tree.TagConfig{Name: "s"})
	treeOps.CreateTag("/d/b", tree.TypeBoolean, tree.TagConfig{Name: "b"})

	config, err := SerializeTree(treeOps.Root)
	if err != nil {
		t.Fatal(err)
	}

	treeOps2 := tree.NewTreeWithOperations(nil)
	if err := DeserializeTree(config, treeOps2); err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		path string
		want tree.ScalarType
	}{
		{"d/i", tree.TypeInteger},
		{"d/f", tree.TypeFloat},
		{"d/s", tree.TypeString},
		{"d/b", tree.TypeBoolean},
	}
	for _, tt := range tests {
		leaf, err := treeOps2.FindLeaf(tt.path)
		if err != nil {
			t.Errorf("leaf %s not found: %v", tt.path, err)
			continue
		}
		if leaf.ValueType() != tt.want {
			t.Errorf("leaf %s type = %v, want %v", tt.path, leaf.ValueType(), tt.want)
		}
	}
}
