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
