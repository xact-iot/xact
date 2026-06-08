package ingest

import (
	"testing"

	"github.com/xact-iot/xact/rtdb/tree"
)

type noopTagValuePublisher struct{}

func (noopTagValuePublisher) TagValuePublish(_ string, _ []byte) error { return nil }

func init() {
	tree.TagValuePublisher = noopTagValuePublisher{}
}

// setupTree creates a minimal tree with an org + deviceType + device for testing.
func setupTree(t *testing.T) *tree.TreeWithOperations {
	t.Helper()
	treeOps := tree.NewTreeWithOperations(nil)
	if err := treeOps.CreateOrganisationNode("/TestOrg", ""); err != nil {
		t.Fatalf("create org: %v", err)
	}
	if err := treeOps.CreateNode("/TestOrg/VMS", ""); err != nil {
		t.Fatalf("create device type: %v", err)
	}
	if err := treeOps.CreateDeviceNode("/TestOrg/VMS/Dev1", ""); err != nil {
		t.Fatalf("create device: %v", err)
	}
	return treeOps
}

func TestParsePayload_ScalarArray(t *testing.T) {
	payload := []byte(`{
		"voltages": {
			"batteries": [12.6, 12.9, 11.8]
		}
	}`)
	data, err := ParsePayload(payload)
	if err != nil {
		t.Fatalf("ParsePayload: %v", err)
	}
	// "voltages" is an object → group
	grp, ok := data.Groups["voltages"]
	if !ok {
		t.Fatal("expected group 'voltages'")
	}
	arr, ok := grp["batteries"].([]any)
	if !ok {
		t.Fatalf("expected batteries to be []any, got %T", grp["batteries"])
	}
	if len(arr) != 3 {
		t.Fatalf("expected 3 elements, got %d", len(arr))
	}
}

func TestParsePayload_ObjectArray(t *testing.T) {
	payload := []byte(`{
		"controllers": {
			"pids": [{"P": 1.0, "I": 0.5}, {"P": 2.0, "I": 0.1}]
		}
	}`)
	data, err := ParsePayload(payload)
	if err != nil {
		t.Fatalf("ParsePayload: %v", err)
	}
	grp := data.Groups["controllers"]
	arr, ok := grp["pids"].([]any)
	if !ok {
		t.Fatalf("expected pids to be []any, got %T", grp["pids"])
	}
	if len(arr) != 2 {
		t.Fatalf("expected 2 elements, got %d", len(arr))
	}
	obj, ok := arr[0].(map[string]any)
	if !ok {
		t.Fatalf("expected first element to be map, got %T", arr[0])
	}
	if obj["P"] != 1.0 {
		t.Errorf("expected P=1.0, got %v", obj["P"])
	}
}

func TestParsePayload_TemplateName(t *testing.T) {
	payload := []byte(`{
		"templateName": "Templates.AirQualityStandard",
		"description": "Beach air quality sensor",
		"meta": {
			"online": true
		}
	}`)
	data, err := ParsePayload(payload)
	if err != nil {
		t.Fatalf("ParsePayload: %v", err)
	}
	if data.TemplateName != "Templates.AirQualityStandard" {
		t.Fatalf("TemplateName = %q", data.TemplateName)
	}
	if data.DeviceDescription != "Beach air quality sensor" {
		t.Fatalf("DeviceDescription = %q", data.DeviceDescription)
	}
	if _, ok := data.DirectTags["templateName"]; ok {
		t.Fatal("templateName should be consumed as ingest metadata, not stored as a tag")
	}
	if _, ok := data.DirectTags["description"]; ok {
		t.Fatal("description should be consumed as ingest metadata, not stored as a tag")
	}
	if _, ok := data.Groups["meta"]; !ok {
		t.Fatal("expected meta group")
	}
}

func TestWriteDeviceData_ScalarArray(t *testing.T) {
	treeOps := setupTree(t)
	proc := NewProcessor(treeOps)

	payload := []byte(`{
		"VMS": {
			"batteries": [12.6, 12.9, 11.8]
		}
	}`)
	data, err := ParsePayload(payload)
	if err != nil {
		t.Fatalf("ParsePayload: %v", err)
	}
	if err := proc.WriteDeviceData("TestOrg", "", "VMS", "Dev1", data); err != nil {
		t.Fatalf("WriteDeviceData: %v", err)
	}

	// Array parent node should exist and be marked as array
	arrayNode, err := treeOps.FindNode("/TestOrg/VMS/Dev1/VMS/batteries")
	if err != nil {
		t.Fatalf("array parent node not found: %v", err)
	}
	if !arrayNode.GetIsArray() {
		t.Error("expected array parent to have isArray=true")
	}

	// Individual elements should be leaves
	for _, idx := range []string{"0", "1", "2"} {
		leafPath := "TestOrg/VMS/Dev1/VMS/batteries/" + idx
		leaf, err := treeOps.FindLeaf(leafPath)
		if err != nil {
			t.Errorf("leaf %s not found: %v", idx, err)
			continue
		}
		v := leaf.GetAnyValue()
		if v == nil {
			t.Errorf("leaf %s has nil value", idx)
		}
	}

	// Check specific values
	leaf0, _ := treeOps.FindLeaf("TestOrg/VMS/Dev1/VMS/batteries/0")
	if v, ok := leaf0.GetAnyValue().(float64); !ok || v != 12.6 {
		t.Errorf("expected leaf 0 = 12.6, got %v", leaf0.GetAnyValue())
	}
	leaf2, _ := treeOps.FindLeaf("TestOrg/VMS/Dev1/VMS/batteries/2")
	if v, ok := leaf2.GetAnyValue().(float64); !ok || v != 11.8 {
		t.Errorf("expected leaf 2 = 11.8, got %v", leaf2.GetAnyValue())
	}
}

func TestWriteDeviceData_ObjectArray(t *testing.T) {
	treeOps := setupTree(t)
	proc := NewProcessor(treeOps)

	payload := []byte(`{
		"ctrl": {
			"pids": [{"P": 1.5, "I": 0.3}, {"P": 2.0, "I": 0.1}]
		}
	}`)
	data, err := ParsePayload(payload)
	if err != nil {
		t.Fatalf("ParsePayload: %v", err)
	}
	if err := proc.WriteDeviceData("TestOrg", "", "VMS", "Dev1", data); err != nil {
		t.Fatalf("WriteDeviceData: %v", err)
	}

	// Array parent should be marked
	arrayNode, err := treeOps.FindNode("/TestOrg/VMS/Dev1/ctrl/pids")
	if err != nil {
		t.Fatalf("array parent not found: %v", err)
	}
	if !arrayNode.GetIsArray() {
		t.Error("expected isArray=true on pids node")
	}

	// Element sub-nodes should exist (0 and 1)
	for _, idx := range []string{"0", "1"} {
		_, err := treeOps.FindNode("/TestOrg/VMS/Dev1/ctrl/pids/" + idx)
		if err != nil {
			t.Errorf("element node %s not found: %v", idx, err)
		}
	}

	// Individual fields should be leaves
	leafP0, err := treeOps.FindLeaf("TestOrg/VMS/Dev1/ctrl/pids/0/P")
	if err != nil {
		t.Fatalf("leaf P[0] not found: %v", err)
	}
	if v, ok := leafP0.GetAnyValue().(float64); !ok || v != 1.5 {
		t.Errorf("expected P[0]=1.5, got %v", leafP0.GetAnyValue())
	}

	leafI1, err := treeOps.FindLeaf("TestOrg/VMS/Dev1/ctrl/pids/1/I")
	if err != nil {
		t.Fatalf("leaf I[1] not found: %v", err)
	}
	if v, ok := leafI1.GetAnyValue().(float64); !ok || v != 0.1 {
		t.Errorf("expected I[1]=0.1, got %v", leafI1.GetAnyValue())
	}
}

func TestWriteDeviceData_TemplateNameAppliesToDeviceNode(t *testing.T) {
	treeOps := tree.NewTreeWithOperations(nil)
	if err := treeOps.CreateOrganisationNode("/TestOrg", ""); err != nil {
		t.Fatalf("create org: %v", err)
	}
	if err := treeOps.CreateNode("/TestOrg/Templates/AirQualityStandard", ""); err != nil {
		t.Fatalf("create standard template node: %v", err)
	}
	if err := treeOps.CreateNode("/TestOrg/Templates/AirQualityBackup", ""); err != nil {
		t.Fatalf("create backup template node: %v", err)
	}
	createTemplateMetric := func(templatePath, tagName, description string) {
		t.Helper()
		if err := treeOps.CreateTag(
			templatePath+"/metrics/"+tagName,
			tree.TypeFloat,
			tree.TagConfig{Name: tagName, Type: tree.TypeFloat},
			tree.TagShared{Description: description, Units: "C"},
		); err != nil {
			t.Fatalf("create %s template tag: %v", description, err)
		}
	}
	createTemplateMetric("/TestOrg/Templates/AirQualityStandard", "temperature", "Standard temperature")
	createTemplateMetric("/TestOrg/Templates/AirQualityStandard", "humidity", "Standard humidity")
	createTemplateMetric("/TestOrg/Templates/AirQualityBackup", "temperature", "Backup temperature")
	createTemplateMetric("/TestOrg/Templates/AirQualityBackup", "humidity", "Backup humidity")

	proc := NewProcessor(treeOps)
	for _, tc := range []struct {
		deviceName   string
		templateName string
		description  string
	}{
		{"AQ-S-0001", "Templates.AirQualityStandard", "Standard temperature"},
		{"AQ-B-0001", "Templates.AirQualityBackup", "Backup temperature"},
	} {
		data, err := ParsePayload([]byte(`{
			"templateName": "` + tc.templateName + `",
			"metrics": {
				"temperature": 22.5
			}
		}`))
		if err != nil {
			t.Fatalf("%s ParsePayload: %v", tc.deviceName, err)
		}
		if err := proc.WriteDeviceData("TestOrg", "", "AirQuality", tc.deviceName, data); err != nil {
			t.Fatalf("%s WriteDeviceData: %v", tc.deviceName, err)
		}
	}

	deviceTypeNode, err := treeOps.FindNode("/TestOrg/AirQuality")
	if err != nil {
		t.Fatalf("device type node: %v", err)
	}
	if deviceTypeNode.GetTemplateName() != "" {
		t.Fatalf("device type template = %q", deviceTypeNode.GetTemplateName())
	}

	for _, tc := range []struct {
		deviceName   string
		templateName string
		description  string
	}{
		{"AQ-S-0001", "Templates.AirQualityStandard", "Standard temperature"},
		{"AQ-B-0001", "Templates.AirQualityBackup", "Backup temperature"},
	} {
		deviceNode, err := treeOps.FindNode("/TestOrg/AirQuality/" + tc.deviceName)
		if err != nil {
			t.Fatalf("%s device node: %v", tc.deviceName, err)
		}
		if deviceNode.GetTemplateName() != tc.templateName {
			t.Fatalf("%s device template = %q, want %q", tc.deviceName, deviceNode.GetTemplateName(), tc.templateName)
		}

		leaf, err := treeOps.FindLeaf("/TestOrg/AirQuality/" + tc.deviceName + "/metrics/temperature")
		if err != nil {
			t.Fatalf("%s device temperature tag: %v", tc.deviceName, err)
		}
		if leaf.GetConfig().TemplateName != tc.templateName {
			t.Fatalf("%s leaf template = %q, want %q", tc.deviceName, leaf.GetConfig().TemplateName, tc.templateName)
		}
		if leaf.GetDescription() != tc.description {
			t.Fatalf("%s leaf description = %q, want %q", tc.deviceName, leaf.GetDescription(), tc.description)
		}
	}

	update, err := ParsePayload([]byte(`{
		"metrics": {
			"humidity": 58.4
		}
	}`))
	if err != nil {
		t.Fatalf("ParsePayload update: %v", err)
	}
	if err := proc.WriteDeviceData("TestOrg", "", "AirQuality", "AQ-B-0001", update); err != nil {
		t.Fatalf("WriteDeviceData update: %v", err)
	}
	leaf, err := treeOps.FindLeaf("/TestOrg/AirQuality/AQ-B-0001/metrics/humidity")
	if err != nil {
		t.Fatalf("backup humidity tag: %v", err)
	}
	if leaf.GetConfig().TemplateName != "Templates.AirQualityBackup" {
		t.Fatalf("humidity leaf template = %q, want Templates.AirQualityBackup", leaf.GetConfig().TemplateName)
	}
	if leaf.GetDescription() != "Backup humidity" {
		t.Fatalf("humidity leaf description = %q, want Backup humidity", leaf.GetDescription())
	}
}

func TestWriteDeviceData_ExpandedArrayWithMetadata(t *testing.T) {
	treeOps := setupTree(t)
	proc := NewProcessor(treeOps)

	payload := []byte(`{
		"VMS": {
			"batteries": {"value": [12.6, 12.9], "units": "V"}
		}
	}`)
	data, err := ParsePayload(payload)
	if err != nil {
		t.Fatalf("ParsePayload: %v", err)
	}
	if err := proc.WriteDeviceData("TestOrg", "", "VMS", "Dev1", data); err != nil {
		t.Fatalf("WriteDeviceData: %v", err)
	}

	// Array parent should exist
	arrayNode, err := treeOps.FindNode("/TestOrg/VMS/Dev1/VMS/batteries")
	if err != nil {
		t.Fatalf("array parent not found: %v", err)
	}
	if !arrayNode.GetIsArray() {
		t.Error("expected isArray=true")
	}

	// Elements should have values
	leaf0, err := treeOps.FindLeaf("TestOrg/VMS/Dev1/VMS/batteries/0")
	if err != nil {
		t.Fatalf("leaf 0 not found: %v", err)
	}
	if v, ok := leaf0.GetAnyValue().(float64); !ok || v != 12.6 {
		t.Errorf("expected 12.6, got %v", leaf0.GetAnyValue())
	}

	// Units should have been inherited from metadata
	if leaf0.GetShared().Units != "V" {
		t.Errorf("expected units 'V', got %q", leaf0.GetShared().Units)
	}
}

func TestWriteDeviceData_ExpandedTagDescription(t *testing.T) {
	treeOps := setupTree(t)
	proc := NewProcessor(treeOps)

	payload := []byte(`{
		"metrics": {
			"supplyTemp": {
				"value": 72.4,
				"description": "Supply air temperature",
				"units": "C"
			}
		}
	}`)
	data, err := ParsePayload(payload)
	if err != nil {
		t.Fatalf("ParsePayload: %v", err)
	}
	if err := proc.WriteDeviceData("TestOrg", "", "VMS", "Dev1", data); err != nil {
		t.Fatalf("WriteDeviceData: %v", err)
	}

	leaf, err := treeOps.FindLeaf("TestOrg/VMS/Dev1/metrics/supplyTemp")
	if err != nil {
		t.Fatalf("leaf not found: %v", err)
	}
	if got := leaf.GetShared().Description; got != "Supply air temperature" {
		t.Fatalf("description = %q, want Supply air temperature", got)
	}

	updatePayload := []byte(`{
		"metrics": {
			"supplyTemp": {
				"description": "Updated description"
			}
		}
	}`)
	updateData, err := ParsePayload(updatePayload)
	if err != nil {
		t.Fatalf("ParsePayload update: %v", err)
	}
	if err := proc.WriteDeviceData("TestOrg", "", "VMS", "Dev1", updateData); err != nil {
		t.Fatalf("WriteDeviceData update: %v", err)
	}
	if got := leaf.GetShared().Description; got != "Updated description" {
		t.Fatalf("updated description = %q, want Updated description", got)
	}
	if got := leaf.GetAnyValue(); got != 72.4 {
		t.Fatalf("value after metadata-only update = %v, want 72.4", got)
	}
}

func TestWriteDeviceData_DeviceDescription(t *testing.T) {
	treeOps := setupTree(t)
	proc := NewProcessor(treeOps)

	payload := []byte(`{
		"description": {
			"value": "Beach air quality sensor",
			"description": "Pseudo tag metadata is ignored"
		},
		"meta": {
			"online": true
		}
	}`)
	data, err := ParsePayload(payload)
	if err != nil {
		t.Fatalf("ParsePayload: %v", err)
	}
	if err := proc.WriteDeviceData("TestOrg", "", "VMS", "Dev1", data); err != nil {
		t.Fatalf("WriteDeviceData: %v", err)
	}

	deviceNode, err := treeOps.FindNode("/TestOrg/VMS/Dev1")
	if err != nil {
		t.Fatalf("device node not found: %v", err)
	}
	if got := deviceNode.GetDescription(); got != "Beach air quality sensor" {
		t.Fatalf("device description = %q, want Beach air quality sensor", got)
	}
	if _, err := treeOps.FindLeaf("/TestOrg/VMS/Dev1/description"); err == nil {
		t.Fatal("top-level description should not be written as a tag")
	}
}

func TestWriteDeviceData_MetaDescriptionIsOrdinaryTag(t *testing.T) {
	treeOps := setupTree(t)
	proc := NewProcessor(treeOps)

	payload := []byte(`{
		"meta": {
			"description": {
				"value": "Roadside air quality sensor",
				"description": "Ignored tag metadata"
			},
			"online": true
		}
	}`)
	data, err := ParsePayload(payload)
	if err != nil {
		t.Fatalf("ParsePayload: %v", err)
	}
	if err := proc.WriteDeviceData("TestOrg", "", "VMS", "Dev1", data); err != nil {
		t.Fatalf("WriteDeviceData: %v", err)
	}

	deviceNode, err := treeOps.FindNode("/TestOrg/VMS/Dev1")
	if err != nil {
		t.Fatalf("device node not found: %v", err)
	}
	if got := deviceNode.GetDescription(); got != "" {
		t.Fatalf("meta.description must not update device description, got %q", got)
	}
	leaf, err := treeOps.FindLeaf("/TestOrg/VMS/Dev1/meta/description")
	if err != nil {
		t.Fatalf("meta.description should be written as an ordinary tag: %v", err)
	}
	if got := leaf.GetAnyValue(); got != "Roadside air quality sensor" {
		t.Fatalf("meta.description value = %v, want Roadside air quality sensor", got)
	}
	if _, err := treeOps.FindLeaf("/TestOrg/VMS/Dev1/meta/online"); err != nil {
		t.Fatalf("meta.online should still be written as a tag: %v", err)
	}
}

func TestWriteDeviceData_DirectTagArray(t *testing.T) {
	treeOps := setupTree(t)
	proc := NewProcessor(treeOps)

	payload := []byte(`{
		"temps": [22.5, 23.1, 21.9]
	}`)
	data, err := ParsePayload(payload)
	if err != nil {
		t.Fatalf("ParsePayload: %v", err)
	}
	if err := proc.WriteDeviceData("TestOrg", "", "VMS", "Dev1", data); err != nil {
		t.Fatalf("WriteDeviceData: %v", err)
	}

	// Direct tag arrays create array parent at device level
	arrayNode, err := treeOps.FindNode("/TestOrg/VMS/Dev1/temps")
	if err != nil {
		t.Fatalf("array parent not found: %v", err)
	}
	if !arrayNode.GetIsArray() {
		t.Error("expected isArray=true")
	}

	leaf1, err := treeOps.FindLeaf("TestOrg/VMS/Dev1/temps/1")
	if err != nil {
		t.Fatalf("leaf 1 not found: %v", err)
	}
	if v, ok := leaf1.GetAnyValue().(float64); !ok || v != 23.1 {
		t.Errorf("expected 23.1, got %v", leaf1.GetAnyValue())
	}
}

func TestWriteDeviceData_ArrayGrowsOnly(t *testing.T) {
	treeOps := setupTree(t)
	proc := NewProcessor(treeOps)

	// First write with 3 elements
	data1, _ := ParsePayload([]byte(`{"grp": {"arr": [1.0, 2.0, 3.0]}}`))
	if err := proc.WriteDeviceData("TestOrg", "", "VMS", "Dev1", data1); err != nil {
		t.Fatalf("first write: %v", err)
	}

	// Second write with 2 elements - element 2 should still exist
	data2, _ := ParsePayload([]byte(`{"grp": {"arr": [10.0, 20.0]}}`))
	if err := proc.WriteDeviceData("TestOrg", "", "VMS", "Dev1", data2); err != nil {
		t.Fatalf("second write: %v", err)
	}

	// Elements 0 and 1 should have new values
	leaf0, _ := treeOps.FindLeaf("TestOrg/VMS/Dev1/grp/arr/0")
	if v, ok := leaf0.GetAnyValue().(float64); !ok || v != 10 {
		t.Errorf("expected leaf 0 = 10 after update, got %v (%T)", leaf0.GetAnyValue(), leaf0.GetAnyValue())
	}

	// Element 2 should still exist with old value
	leaf2, err := treeOps.FindLeaf("TestOrg/VMS/Dev1/grp/arr/2")
	if err != nil {
		t.Fatalf("leaf 2 should still exist after smaller array: %v", err)
	}
	if v, ok := leaf2.GetAnyValue().(float64); !ok || v != 3 {
		t.Errorf("expected leaf 2 = 3 (unchanged), got %v (%T)", leaf2.GetAnyValue(), leaf2.GetAnyValue())
	}
}

func TestGetMetricPath_ArrayElement(t *testing.T) {
	treeOps := tree.NewTreeWithOperations(nil)
	if err := treeOps.CreateOrganisationNode("/TestOrg", ""); err != nil {
		t.Fatal(err)
	}
	if err := treeOps.CreateNode("/TestOrg/VMS", ""); err != nil {
		t.Fatal(err)
	}
	if err := treeOps.CreateDeviceNode("/TestOrg/VMS/Dev1", ""); err != nil {
		t.Fatal(err)
	}
	if err := treeOps.CreateNode("/TestOrg/VMS/Dev1/grp", ""); err != nil {
		t.Fatal(err)
	}
	if err := treeOps.CreateNode("/TestOrg/VMS/Dev1/grp/batteries", ""); err != nil {
		t.Fatal(err)
	}
	if err := treeOps.CreateTag("/TestOrg/VMS/Dev1/grp/batteries/0", tree.TypeFloat, tree.TagConfig{Name: "0", Type: tree.TypeFloat}); err != nil {
		t.Fatal(err)
	}

	leaf, err := treeOps.FindLeaf("TestOrg/VMS/Dev1/grp/batteries/0")
	if err != nil {
		t.Fatalf("find leaf: %v", err)
	}

	path := leaf.GetMetricPath()
	expected := "grp.batteries.0"
	if path != expected {
		t.Errorf("GetMetricPath() = %q, want %q", path, expected)
	}
}
