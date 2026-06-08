package blocks

import (
	"encoding/json"
	"testing"

	"github.com/xact-iot/xact/rtdb/tree"
)

func ptrFloat(v float64) *float64 { return &v }

func TestScalingBlock(t *testing.T) {
	b := &ScalingBlock{Scale: 2.0, Offset: 10.0}

	result, err := b.Process(nil, 5.0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != 20.0 { // 5*2 + 10
		t.Errorf("expected 20.0, got %v", result)
	}
}

func TestScalingBlockClamp(t *testing.T) {
	b := &ScalingBlock{
		Scale:  1.0,
		Offset: 0.0,
		Min:    ptrFloat(0),
		Max:    ptrFloat(100),
	}

	// Value below min
	result, err := b.Process(nil, -5.0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != 0.0 {
		t.Errorf("expected 0.0 (clamped), got %v", result)
	}

	// Value above max
	result, err = b.Process(nil, 150.0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != 100.0 {
		t.Errorf("expected 100.0 (clamped), got %v", result)
	}
}

func TestScalingBlockIntInput(t *testing.T) {
	b := &ScalingBlock{Scale: 1.0, Offset: 5.0}

	result, err := b.Process(nil, 10) // int input
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != 15.0 {
		t.Errorf("expected 15.0, got %v", result)
	}
}

func TestLimitCheckBlock(t *testing.T) {
	leaf := tree.NewFloatLeaf("test", tree.TagConfig{Name: "Test", Type: tree.TypeFloat})

	b := &LimitCheckBlock{HiLimit: ptrFloat(100), LowLimit: ptrFloat(0)}

	// Normal value
	_, err := b.Process(leaf, 50.0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if leaf.GetState() != "" {
		t.Errorf("expected empty state (Normal), got %v", leaf.GetState())
	}

	// High alarm
	_, err = b.Process(leaf, 150.0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if leaf.GetState() != tree.StatusAlarm {
		t.Errorf("expected StatusAlarm, got %v", leaf.GetState())
	}

	// Low alarm
	_, err = b.Process(leaf, -10.0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if leaf.GetState() != tree.StatusAlarm {
		t.Errorf("expected StatusAlarm, got %v", leaf.GetState())
	}
}

func TestPublishBlock(t *testing.T) {
	b := &PublishBlock{}
	schema := b.Schema()
	if schema.Type != "publish" {
		t.Errorf("expected type 'publish', got %q", schema.Type)
	}
}

func TestPublishPersistAndHistoryBlocksSafePaths(t *testing.T) {
	tw := tree.NewTreeWithOperations(nil)
	if err := tw.CreateOrganisationNode("default", ""); err != nil {
		t.Fatalf("CreateOrganisationNode: %v", err)
	}
	if err := tw.CreateDeviceNode("default.device", ""); err != nil {
		t.Fatalf("CreateDeviceNode: %v", err)
	}
	if err := tw.CreateTag("default.device.temp", tree.TypeFloat, tree.TagConfig{}, tree.TagShared{Deadband: 1}); err != nil {
		t.Fatalf("CreateTag: %v", err)
	}
	leaf, err := tw.FindLeaf("default.device.temp")
	if err != nil {
		t.Fatalf("FindLeaf: %v", err)
	}
	if got := tagPath(leaf); got != "tagvalue.default.device.temp" {
		t.Fatalf("tagPath = %q", got)
	}
	if got := persistKey(leaf); got != "default.device.temp" {
		t.Fatalf("persistKey = %q", got)
	}

	publish := &PublishBlock{}
	publish.Init(leaf)
	if publish.leaf != leaf {
		t.Fatal("publish Init did not set leaf")
	}
	if out, err := publish.Process(leaf, 12.5); err != nil || out != 12.5 {
		t.Fatalf("publish Process = %v/%v", out, err)
	}
	publish.Close(leaf)
	if publish.leaf != nil {
		t.Fatal("publish Close did not clear leaf")
	}

	persist := &PersistBlock{}
	persist.Init(leaf)
	if out, err := persist.Process(leaf, 12.5); err != nil || out != 12.5 {
		t.Fatalf("persist Process first = %v/%v", out, err)
	}
	if out, err := persist.Process(leaf, 12.75); err != nil || out != 12.75 {
		t.Fatalf("persist Process deadband = %v/%v", out, err)
	}
	if !withinDeadband(float64(10), float64(10.5), 1) || withinDeadband(int64(10), int64(12), 1) || withinDeadband("a", "a", 1) {
		t.Fatal("withinDeadband mismatch")
	}
	if string(persist.GetParameters()) != "{}" || persist.SetParameters(json.RawMessage(`{"ignored":true}`)) != nil || persist.Schema().Type != "persist" {
		t.Fatal("persist parameter/schema mismatch")
	}

	history := &HistoryRecorderBlock{}
	history.Init(leaf)
	history.Close(leaf)
	if history.GetType() != "historyrecorder" || history.Schema().Type != "historyrecorder" {
		t.Fatal("history schema/type mismatch")
	}
	if out, err := history.Process(leaf, 99.0); err != nil || out != 99.0 {
		t.Fatalf("history numeric Process = %v/%v", out, err)
	}
	if out, err := history.Process(leaf, "skip"); err != nil || out != "skip" {
		t.Fatalf("history non-numeric Process = %v/%v", out, err)
	}
}

func TestPipelineIntegration(t *testing.T) {
	leaf := tree.NewFloatLeaf("test", tree.TagConfig{Name: "Test", Type: tree.TypeFloat})

	pipeline := []tree.ProcessBlock{
		&ScalingBlock{Scale: 2.0, Offset: 0.0, Max: ptrFloat(200)},
		&LimitCheckBlock{HiLimit: ptrFloat(100), LowLimit: ptrFloat(0)},
	}

	// Normal value: 25 * 2 = 50, within limits
	result, err := tree.RunPipeline(leaf, pipeline, 25.0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != 50.0 {
		t.Errorf("expected 50.0, got %v", result)
	}
	if leaf.GetState() != "" {
		t.Errorf("expected empty state (Normal), got %v", leaf.GetState())
	}

	// Alarm value: 75 * 2 = 150, above hi limit
	result, err = tree.RunPipeline(leaf, pipeline, 75.0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != 150.0 {
		t.Errorf("expected 150.0, got %v", result)
	}
	if leaf.GetState() != tree.StatusAlarm {
		t.Errorf("expected StatusAlarm, got %v", leaf.GetState())
	}

	// Clamped value: 150 * 2 = 300, clamped to 200
	result, err = tree.RunPipeline(leaf, pipeline, 150.0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != 200.0 {
		t.Errorf("expected 200.0 (clamped), got %v", result)
	}
}

func TestProcessBlockRegistryRoundTrip(t *testing.T) {
	// Create a pipeline
	original := []tree.ProcessBlock{
		&ScalingBlock{Scale: 2.5, Offset: 1.0, Min: ptrFloat(-10), Max: ptrFloat(100)},
		&LimitCheckBlock{HiLimit: ptrFloat(90), LowLimit: ptrFloat(5)},
		&PublishBlock{},
	}

	// Serialize
	envelopes, err := tree.MarshalPipeline(original)
	if err != nil {
		t.Fatalf("marshal error: %v", err)
	}

	// Serialize to JSON and back
	data, err := json.Marshal(envelopes)
	if err != nil {
		t.Fatalf("json marshal error: %v", err)
	}

	var decoded []tree.ProcessBlockEnvelope
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("json unmarshal error: %v", err)
	}

	// Deserialize
	restored, err := tree.UnmarshalPipeline(decoded)
	if err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}

	if len(restored) != 3 {
		t.Fatalf("expected 3 blocks, got %d", len(restored))
	}

	// Verify types
	if restored[0].GetType() != "scaling" {
		t.Errorf("expected scaling, got %s", restored[0].GetType())
	}
	if restored[1].GetType() != "limitcheck" {
		t.Errorf("expected limitcheck, got %s", restored[1].GetType())
	}
	if restored[2].GetType() != "publish" {
		t.Errorf("expected publish, got %s", restored[2].GetType())
	}

	// Verify scaling params survived round-trip
	scaling := restored[0].(*ScalingBlock)
	if scaling.Scale != 2.5 || scaling.Offset != 1.0 {
		t.Errorf("scaling params wrong: scale=%v offset=%v", scaling.Scale, scaling.Offset)
	}
	if scaling.Min == nil || *scaling.Min != -10 {
		t.Error("scaling Min not preserved")
	}
}
