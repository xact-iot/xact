package template

import (
	"testing"

	"github.com/xact-iot/xact/rtdb/tree"
)

func TestStorage(t *testing.T) {
	storage := NewStorage()

	// Create a template
	template := tree.NewNode("sensor_template")
	template.SetDescription("Generic Sensor Template")
	template.SetTemplateName("base")

	// Add some children
	tempTag := tree.NewFloatLeaf("temperature", tree.TagConfig{
		Name: "Temperature",
		Type: tree.TypeFloat,
	})
	template.AddChild(tempTag)

	humidityTag := tree.NewFloatLeaf("humidity", tree.TagConfig{
		Name: "Humidity",
		Type: tree.TypeFloat,
	})
	template.AddChild(humidityTag)

	// Store it
	err := storage.StoreTemplate("sensors/generic", template)
	if err != nil {
		t.Fatalf("Failed to store template: %v", err)
	}

	// Retrieve it
	retrieved, err := storage.GetTemplate("sensors/generic")
	if err != nil {
		t.Fatalf("Failed to get template: %v", err)
	}

	if retrieved.Name != "generic" {
		t.Errorf("Expected name 'generic', got '%s'", retrieved.Name)
	}

	if retrieved.GetDescription() != "Generic Sensor Template" {
		t.Errorf("Expected description 'Generic Sensor Template', got '%s'", retrieved.GetDescription())
	}

	// Check children
	if _, ok := retrieved.GetChild("temperature"); !ok {
		t.Error("temperature child not found")
	}
	if _, ok := retrieved.GetChild("humidity"); !ok {
		t.Error("humidity child not found")
	}
}

func TestStorageListTemplates(t *testing.T) {
	storage := NewStorage()

	// Create templates
	template1 := tree.NewNode("temp1")
	template2 := tree.NewNode("temp2")
	template3 := tree.NewNode("temp3")

	storage.StoreTemplate("a/b/c", template1)
	storage.StoreTemplate("a/b/d", template2)
	storage.StoreTemplate("x/y", template3)

	// List all
	paths := storage.ListTemplates()
	if len(paths) != 6 { // /a, /a/b, /a/b/c, /a/b/d, /x, /x/y
		t.Errorf("Expected 6 paths, got %d: %v", len(paths), paths)
	}

	// Check specific paths exist
	hasABC := false
	hasXY := false
	for _, p := range paths {
		if p == "/a/b/c" {
			hasABC = true
		}
		if p == "/x/y" {
			hasXY = true
		}
	}
	if !hasABC {
		t.Error("Path /a/b/c not found")
	}
	if !hasXY {
		t.Error("Path /x/y not found")
	}
}

func TestInstantiate(t *testing.T) {
	// Create a template
	template := tree.NewNode("sensor_template")
	template.SetDescription("Generic Sensor")
	template.SetTemplateName("sensor_base")

	// Add children
	tempTag := tree.NewFloatLeaf("temperature", tree.TagConfig{
		Name: "Temperature",
		Type: tree.TypeFloat,
	})
	template.AddChild(tempTag)

	statusTag := tree.NewStringLeaf("status", tree.TagConfig{
		Name: "Device Status",
		Type: tree.TypeString,
	})
	template.AddChild(statusTag)

	// Instantiate
	instance := Instantiate(template, "device1")

	// Verify structure
	if instance.Name != "device1" {
		t.Errorf("Expected name 'device1', got '%s'", instance.Name)
	}

	if instance.GetTemplateName() != "sensor_base" {
		t.Errorf("Expected template name 'sensor_base', got '%s'", instance.GetTemplateName())
	}

	// Verify children exist
	tempLeafNode, ok := instance.GetChild("temperature")
	if !ok {
		t.Fatal("temperature not found in instance")
	}

	tempLeaf, ok := tempLeafNode.(tree.Leaf)
	if !ok {
		t.Fatal("temperature is not a leaf")
	}

	// Value should be unset (zero value)
	val, err := tempLeaf.GetFloat()
	if err != nil || val != 0 {
		t.Errorf("Expected 0 value for instantiated tag, got %v (err: %v)", val, err)
	}

	// Verify config was copied
	if tempLeaf.GetConfig().Name != "Temperature" {
		t.Errorf("Expected config name 'Temperature', got '%s'", tempLeaf.GetConfig().Name)
	}
}

func TestInstantiateWithSubTemplate(t *testing.T) {
	// Main template with common fields
	mainTemplate := tree.NewNode("device_base")
	mainTemplate.SetDescription("Base Device")

	tempTag := tree.NewFloatLeaf("temperature", tree.TagConfig{
		Name: "Temperature",
		Type: tree.TypeFloat,
	})
	mainTemplate.AddChild(tempTag)

	// Sub-template with manufacturer-specific fields
	subTemplate := tree.NewNode("acme_specific")
	subTemplate.SetDescription("Acme Sensors")

	firmwareTag := tree.NewStringLeaf("firmware", tree.TagConfig{
		Name: "Firmware Version",
		Type: tree.TypeString,
	})
	subTemplate.AddChild(firmwareTag)

	modelTag := tree.NewStringLeaf("model", tree.TagConfig{
		Name: "Model Number",
		Type: tree.TypeString,
	})
	subTemplate.AddChild(modelTag)

	// Instantiate with both
	instance := InstantiateWithSubTemplate(mainTemplate, subTemplate, "acme_device1")

	// Verify main template children exist
	if _, ok := instance.GetChild("temperature"); !ok {
		t.Error("temperature from main template not found")
	}

	// Verify sub-template children exist
	if _, ok := instance.GetChild("firmware"); !ok {
		t.Error("firmware from sub-template not found")
	}
	if _, ok := instance.GetChild("model"); !ok {
		t.Error("model from sub-template not found")
	}

	// Should have 3 children total
	if len(instance.Children) != 3 {
		t.Errorf("Expected 3 children, got %d", len(instance.Children))
	}
}

func TestTemplateInheritanceFallthrough(t *testing.T) {
	// Create a template with description on a leaf
	tmpl := tree.NewNode("sensor_template")
	tmpl.SetTemplateName("sensors/generic")

	tempLeaf := tree.NewFloatLeaf("temperature", tree.TagConfig{
		Name: "Temperature",
		Type: tree.TypeFloat,
	}, tree.TagShared{
		Description: "Template description",
	})
	tmpl.AddChild(tempLeaf)

	// Instantiate - instance leaf should inherit description from template
	instance := Instantiate(tmpl, "device1")
	instChild, ok := instance.GetChild("temperature")
	if !ok {
		t.Fatal("temperature not found in instance")
	}

	instLeaf := instChild.(tree.Leaf)

	// Description should fall through to template
	if instLeaf.GetDescription() != "Template description" {
		t.Errorf("expected inherited description 'Template description', got '%s'", instLeaf.GetDescription())
	}

	// Template reference should be set
	if instLeaf.GetTemplate() == nil {
		t.Error("expected template reference to be set")
	}
	if instLeaf.GetConfig().TemplateName != "sensors/generic" {
		t.Errorf("expected template name 'sensors/generic', got '%s'", instLeaf.GetConfig().TemplateName)
	}
}

func TestTemplateLocalOverride(t *testing.T) {
	// Create a template
	tmpl := tree.NewNode("sensor_template")
	tmpl.SetTemplateName("sensors/generic")

	tempLeaf := tree.NewFloatLeaf("temperature", tree.TagConfig{
		Name: "Temperature",
		Type: tree.TypeFloat,
	}, tree.TagShared{
		Description: "Template description",
	})
	tmpl.AddChild(tempLeaf)

	// Instantiate
	instance := Instantiate(tmpl, "device1")
	instChild, _ := instance.GetChild("temperature")
	instLeaf := instChild.(tree.Leaf)

	// Set a local override
	shared := instLeaf.GetShared()
	shared.Description = "Local override"
	instLeaf.SetShared(shared)

	// Local value should take precedence
	if instLeaf.GetDescription() != "Local override" {
		t.Errorf("expected local override 'Local override', got '%s'", instLeaf.GetDescription())
	}
}

func TestChangingTemplateReflectsInInstances(t *testing.T) {
	// Create a template leaf
	tmplLeaf := tree.NewFloatLeaf("temperature", tree.TagConfig{
		Name: "Temperature",
		Type: tree.TypeFloat,
	}, tree.TagShared{
		Description: "Original description",
	})

	// Create an instance leaf that references the template
	cfg := tree.TagConfig{
		Name:         "Temperature",
		Type:         tree.TypeFloat,
		TemplateName: "test_template",
	}
	instLeaf := tree.NewFloatLeaf("temperature", cfg)
	instLeaf.SetTemplate(tmplLeaf)

	// Verify it inherits the description
	if instLeaf.GetDescription() != "Original description" {
		t.Errorf("expected 'Original description', got '%s'", instLeaf.GetDescription())
	}

	// Change the template's description
	tmplShared := tmplLeaf.GetShared()
	tmplShared.Description = "Updated description"
	tmplLeaf.SetShared(tmplShared)

	// Instance should reflect the change
	if instLeaf.GetDescription() != "Updated description" {
		t.Errorf("expected 'Updated description', got '%s'", instLeaf.GetDescription())
	}
}

func TestResolveTemplateRefs(t *testing.T) {
	storage := NewStorage()

	// Create and store a template
	tmpl := tree.NewNode("sensor")
	tempLeaf := tree.NewFloatLeaf("temperature", tree.TagConfig{
		Name: "Temperature",
		Type: tree.TypeFloat,
	}, tree.TagShared{
		Description: "Sensor temperature",
	})
	tmpl.AddChild(tempLeaf)
	storage.StoreTemplate("sensors/generic", tmpl)

	// Create a tree with a node that has leaves with template name but no resolved ref
	root := tree.NewNode("root")
	device := tree.NewNode("device1")
	device.SetTemplateName("sensors/generic")

	cfg := tree.TagConfig{
		Name:         "Temperature",
		Type:         tree.TypeFloat,
		TemplateName: "sensors/generic",
	}
	instLeaf := tree.NewFloatLeaf("temperature", cfg)
	device.AddChild(instLeaf)
	root.AddChild(device)

	// Before resolution, no template ref
	if instLeaf.GetTemplate() != nil {
		t.Error("expected no template before resolution")
	}
	if instLeaf.GetDescription() != "" {
		t.Error("expected empty description before resolution")
	}

	// Resolve
	err := storage.ResolveTemplateRefs(root)
	if err != nil {
		t.Fatalf("ResolveTemplateRefs failed: %v", err)
	}

	// After resolution, should have template ref and inherit description
	if instLeaf.GetTemplate() == nil {
		t.Error("expected template ref after resolution")
	}
	if instLeaf.GetDescription() != "Sensor temperature" {
		t.Errorf("expected inherited description 'Sensor temperature', got '%s'", instLeaf.GetDescription())
	}
}

func TestDeleteTemplate(t *testing.T) {
	storage := NewStorage()

	// Create and store template
	template := tree.NewNode("to_delete")
	storage.StoreTemplate("test/template", template)

	// Verify it exists
	_, err := storage.GetTemplate("test/template")
	if err != nil {
		t.Fatalf("Template should exist: %v", err)
	}

	// Delete it
	err = storage.DeleteTemplate("test/template")
	if err != nil {
		t.Fatalf("Failed to delete template: %v", err)
	}

	// Verify it's gone
	_, err = storage.GetTemplate("test/template")
	if err == nil {
		t.Error("Template should have been deleted")
	}
}
