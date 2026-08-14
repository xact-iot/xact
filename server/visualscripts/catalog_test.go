package visualscripts

import "testing"

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
