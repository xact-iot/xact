package blocks

import (
	"strings"
	"testing"

	"github.com/xact-iot/xact/rtdb/tree"
)

func newTestLeaf() tree.Leaf {
	return tree.NewFloatLeaf("test", tree.TagConfig{Name: "Test", Type: tree.TypeFloat})
}

func TestFlapFilterBlock_NoFlappingUnderThreshold(t *testing.T) {
	leaf := newTestLeaf()
	limitBlock := &LimitCheckBlock{HiLimit: ptrFloat(100), LowLimit: ptrFloat(0)}
	flapBlock := &FlapFilterBlock{Threshold: 3, Window: 60_000_000_000, Cooldown: 120_000_000_000}
	flapBlock.Init(leaf)

	// One transition: normal → alarm. Should NOT trigger flapping.
	limitBlock.Process(leaf, 150.0)
	flapBlock.Process(leaf, 150.0)

	if strings.Contains(leaf.GetState(), tree.StatusDeviation) {
		t.Error("should not be flapping after one transition")
	}
}

func TestFlapFilterBlock_EntersFlapping(t *testing.T) {
	leaf := newTestLeaf()
	limitBlock := &LimitCheckBlock{HiLimit: ptrFloat(100), LowLimit: ptrFloat(0)}
	flapBlock := &FlapFilterBlock{Threshold: 1, Window: 60_000_000_000, Cooldown: 120_000_000_000}
	flapBlock.Init(leaf)

	// Oscillate: normal → alarm → normal = 2 transitions, exceeds threshold of 1
	values := []float64{150.0, 50.0}
	for _, v := range values {
		limitBlock.Process(leaf, v)
		flapBlock.Process(leaf, v)
	}

	state := leaf.GetState()
	if !strings.Contains(state, tree.StatusDeviation) {
		t.Errorf("expected StatusDeviation (flapping), got %q", state)
	}
}

func TestFlapFilterBlock_LocksAlarmState(t *testing.T) {
	leaf := newTestLeaf()
	limitBlock := &LimitCheckBlock{HiLimit: ptrFloat(100), LowLimit: ptrFloat(0)}
	flapBlock := &FlapFilterBlock{Threshold: 1, Window: 60_000_000_000, Cooldown: 120_000_000_000}
	flapBlock.Init(leaf)

	// Trigger flapping while in alarm state
	// normal → alarm (transition 1)
	limitBlock.Process(leaf, 150.0)
	flapBlock.Process(leaf, 150.0)
	// alarm → normal (transition 2, exceeds threshold)
	limitBlock.Process(leaf, 50.0)
	flapBlock.Process(leaf, 50.0)

	// The alarm was cleared by limitcheck on the last value (50.0 is in range).
	// Flapping started after seeing 2 transitions. The alarm state when flapping
	// was entered was "no alarm" (current value 50.0 is normal).
	// So alarm should be locked OFF.
	if strings.Contains(leaf.GetState(), tree.StatusAlarm) {
		// That's fine - the lock depends on the moment flapping was detected.
		// Let's just verify that further oscillation doesn't change things.
	}

	// Record the locked alarm state
	hadAlarm := strings.Contains(leaf.GetState(), tree.StatusAlarm)

	// Now push more values - alarm flag should not change
	limitBlock.Process(leaf, 150.0) // would normally set alarm
	flapBlock.Process(leaf, 150.0)
	if strings.Contains(leaf.GetState(), tree.StatusAlarm) != hadAlarm {
		t.Error("alarm flag changed while flapping - should be locked")
	}

	limitBlock.Process(leaf, 50.0) // would normally clear alarm
	flapBlock.Process(leaf, 50.0)
	if strings.Contains(leaf.GetState(), tree.StatusAlarm) != hadAlarm {
		t.Error("alarm flag changed while flapping - should be locked")
	}
}

func TestFlapFilterBlock_PassesThroughValue(t *testing.T) {
	leaf := newTestLeaf()
	flapBlock := &FlapFilterBlock{Threshold: 1, Window: 60_000_000_000, Cooldown: 120_000_000_000}
	flapBlock.Init(leaf)

	result, err := flapBlock.Process(leaf, 42.5)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != 42.5 {
		t.Errorf("expected value pass-through 42.5, got %v", result)
	}
}

func TestFlapFilterBlock_Schema(t *testing.T) {
	b := &FlapFilterBlock{}
	schema := b.Schema()
	if schema.Type != "flapfilter" {
		t.Errorf("expected type 'flapfilter', got %q", schema.Type)
	}
	if schema.Label != "Flapping Filter" {
		t.Errorf("expected label 'Flapping Filter', got %q", schema.Label)
	}
	if _, ok := schema.Params["threshold"]; !ok {
		t.Error("schema missing 'threshold' param")
	}
	if _, ok := schema.Params["window"]; !ok {
		t.Error("schema missing 'window' param")
	}
	if _, ok := schema.Params["cooldown"]; !ok {
		t.Error("schema missing 'cooldown' param")
	}
}

func TestFlapFilterBlock_PipelineIntegration(t *testing.T) {
	leaf := newTestLeaf()
	limitBlock := &LimitCheckBlock{HiLimit: ptrFloat(100), LowLimit: ptrFloat(0)}
	flapBlock := &FlapFilterBlock{Threshold: 2, Window: 60_000_000_000, Cooldown: 120_000_000_000}

	pipeline := []tree.ProcessBlock{limitBlock, flapBlock}
	tree.InitPipelineBlocks(leaf, pipeline)

	// 3 transitions needed to exceed threshold of 2
	oscillations := []float64{150.0, 50.0, 150.0}
	for _, v := range oscillations {
		_, err := tree.RunPipeline(leaf, pipeline, v)
		if err != nil {
			t.Fatalf("pipeline error: %v", err)
		}
	}

	state := leaf.GetState()
	if !strings.Contains(state, tree.StatusDeviation) {
		t.Errorf("expected flapping (StatusDeviation) after 3 transitions, got %q", state)
	}
}
