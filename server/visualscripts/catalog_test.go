package visualscripts

import (
	"encoding/json"
	"testing"
)

func TestCatalogUsesArraysForCollectionFields(t *testing.T) {
	for _, definition := range NewRegistry().Catalog() {
		if definition.Inputs == nil {
			t.Fatalf("%s inputs = nil, want an empty array", definition.Type)
		}
		if definition.Outputs == nil {
			t.Fatalf("%s outputs = nil, want an empty array", definition.Type)
		}
		if definition.Parameters == nil {
			t.Fatalf("%s parameters = nil, want an empty array", definition.Type)
		}
	}
}

func TestRegistrySupportsVersionsAndRejectsExactDuplicates(t *testing.T) {
	registry := NewRegistry()
	node := func(version int) PluginNode {
		return PluginNode{DefinitionValue: NodeDefinition{Type: "acme.echo", TypeVersion: version, Name: "Echo", Category: "Custom", Available: true}, CompileFunc: func(json.RawMessage, CompileServices) (PluginHandler, error) { return PluginHandler{}, nil }}
	}
	if err := registry.Register(node(1)); err != nil {
		t.Fatal(err)
	}
	if err := registry.Register(node(2)); err != nil {
		t.Fatal(err)
	}
	if definition, ok := registry.Definition("acme.echo"); !ok || definition.TypeVersion != 2 {
		t.Fatalf("latest definition = %#v, %v", definition, ok)
	}
	if _, ok := registry.DefinitionVersion("acme.echo", 1); !ok {
		t.Fatal("version 1 was not retained")
	}
	if err := registry.Register(node(2)); err == nil {
		t.Fatal("duplicate exact version was accepted")
	}
}

func TestValidationRequiresAnInstalledNodeVersion(t *testing.T) {
	registry := NewRegistry()
	if err := registry.Register(PluginNode{DefinitionValue: NodeDefinition{
		Type: "acme.echo", TypeVersion: 2, Name: "Echo", Category: "Custom", Available: true,
	}}); err != nil {
		t.Fatal(err)
	}
	result := ValidateGraph(registry, NormalizeGraph(GraphDocument{Nodes: []GraphNode{
		{ID: "manual", Type: "core.manual", TypeVersion: 1, Config: json.RawMessage(`{}`)},
		{ID: "echo", Type: "acme.echo", TypeVersion: 1, Config: json.RawMessage(`{}`)},
	}}))
	if result.Valid {
		t.Fatal("graph with an unavailable plugin version was valid")
	}
	found := false
	for _, diagnostic := range result.Diagnostics {
		if diagnostic.NodeID == "echo" && diagnostic.Code == "unsupported_node_version" {
			found = true
		}
	}
	if !found {
		t.Fatalf("diagnostics = %#v", result.Diagnostics)
	}
}

func TestContextNodeTypesUseVariableLabels(t *testing.T) {
	want := map[string]string{
		"core.get-context":       "Get Variable",
		"core.set-context":       "Set Variable",
		"core.delete-context":    "Delete Variable",
		"core.increment-context": "Increment Variable",
	}
	for _, definition := range NewRegistry().Catalog() {
		name, ok := want[definition.Type]
		if !ok {
			continue
		}
		if definition.Name != name || definition.Category != "Variables" {
			t.Errorf("%s label = %q in %q, want %q in Variables", definition.Type, definition.Name, definition.Category, name)
		}
		delete(want, definition.Type)
	}
	if len(want) != 0 {
		t.Fatalf("variable nodes missing from catalog: %#v", want)
	}
}

func TestTagChangedOffersTriggerOnStart(t *testing.T) {
	definition, ok := NewRegistry().Definition("core.tag-changed")
	if !ok {
		t.Fatal("Tag Changed definition is missing")
	}
	for _, parameter := range definition.Parameters {
		if parameter.Name == "triggerOnStart" {
			if parameter.Type != "boolean" || parameter.Default != false {
				t.Fatalf("Trigger on start parameter = %#v", parameter)
			}
			return
		}
	}
	t.Fatal("Tag Changed is missing Trigger on start")
}
