package blocks

import (
	"encoding/json"
	"sync"
	"testing"
	"time"

	"github.com/xact-iot/xact/rtdb/tree"
)

type noopTagValuePublisher struct{}

func (noopTagValuePublisher) TagValuePublish(_ string, _ []byte) error { return nil }

func init() {
	tree.TagValuePublisher = noopTagValuePublisher{}
}

// ── toFloat64 ────────────────────────────────────────────────────────────────

func TestToFloat64(t *testing.T) {
	tests := []struct {
		input   any
		want    float64
		wantErr bool
	}{
		{float64(3.14), 3.14, false},
		{float32(2.5), 2.5, false},
		{int(10), 10.0, false},
		{int64(20), 20.0, false},
		{int32(30), 30.0, false},
		{true, 1.0, false},
		{false, 0.0, false},
		{"string", 0, true},
		{nil, 0, true},
	}
	for _, tt := range tests {
		got, err := toFloat64(tt.input)
		if (err != nil) != tt.wantErr {
			t.Errorf("toFloat64(%v) error = %v, wantErr %v", tt.input, err, tt.wantErr)
			continue
		}
		if !tt.wantErr && got != tt.want {
			t.Errorf("toFloat64(%v) = %v, want %v", tt.input, got, tt.want)
		}
	}
}

// ── withinDeadband (persist) ─────────────────────────────────────────────────

func TestWithinDeadband(t *testing.T) {
	// Float64 within deadband
	if !withinDeadband(10.0, 10.4, 0.5) {
		t.Error("10.0 → 10.4 should be within 0.5 deadband")
	}
	// Float64 outside deadband
	if withinDeadband(10.0, 10.6, 0.5) {
		t.Error("10.0 → 10.6 should NOT be within 0.5 deadband")
	}
	// Int64 within deadband
	if !withinDeadband(int64(100), int64(102), 5.0) {
		t.Error("100 → 102 should be within 5.0 deadband")
	}
	// Int64 outside deadband
	if withinDeadband(int64(100), int64(110), 5.0) {
		t.Error("100 → 110 should NOT be within 5.0 deadband")
	}
	// Non-numeric always returns false
	if withinDeadband("hello", "world", 1.0) {
		t.Error("non-numeric should return false")
	}
	// Mixed types returns false
	if withinDeadband(10.0, int64(10), 0.5) {
		t.Error("mixed float64/int64 should return false")
	}
}

// ── PersistBlock ─────────────────────────────────────────────────────────────

func TestPersistBlockSchema(t *testing.T) {
	b := &PersistBlock{}
	schema := b.Schema()
	if schema.Type != "persist" {
		t.Errorf("schema type = %q", schema.Type)
	}
	if b.GetType() != "persist" {
		t.Errorf("GetType() = %q", b.GetType())
	}
}

func TestPersistBlockGetSetParameters(t *testing.T) {
	b := &PersistBlock{}
	params := b.GetParameters()
	if string(params) != "{}" {
		t.Errorf("GetParameters() = %s", params)
	}
	if err := b.SetParameters(json.RawMessage(`{}`)); err != nil {
		t.Errorf("SetParameters: %v", err)
	}
}

func TestPersistBlockClose(t *testing.T) {
	b := &PersistBlock{}
	b.Close(nil) // should not panic
}

func TestTagPathNoDevice(t *testing.T) {
	// Without a full org/device tree, tagPath still produces a path
	leaf := tree.NewFloatLeaf("temp", tree.TagConfig{Name: "temp", Type: tree.TypeFloat})
	path := tagPath(leaf)
	// No org or device → "tagvalue..temp" (empty org segment)
	if path == "" {
		t.Error("tagPath should not be empty")
	}
}

// ── LimitCheckBlock ─────────────────────────────────────────────────────────

func TestLimitCheckReturnToNormal(t *testing.T) {
	leaf := tree.NewFloatLeaf("t", tree.TagConfig{Name: "t", Type: tree.TypeFloat})
	b := &LimitCheckBlock{HiLimit: ptrFloat(100), LowLimit: ptrFloat(0)}

	// Go into alarm
	b.Process(leaf, 150.0)
	if leaf.GetState() != tree.StatusAlarm {
		t.Error("expected alarm")
	}

	// Return to normal
	b.Process(leaf, 50.0)
	if leaf.GetState() != "" {
		t.Errorf("expected normal, got %q", leaf.GetState())
	}
}

func TestLimitCheckInitializesFromExistingAlarm(t *testing.T) {
	leaf := tree.NewFloatLeaf("t", tree.TagConfig{Name: "t", Type: tree.TypeFloat})
	leaf.SetState(tree.StatusAlarm)

	b := &LimitCheckBlock{HiLimit: ptrFloat(100)}
	b.Init(leaf)

	if !b.wasInAlarm {
		t.Fatal("expected limitcheck to initialize from existing alarm state")
	}
	if _, err := b.Process(leaf, 150.0); err != nil {
		t.Fatal(err)
	}
	if !b.wasInAlarm || leaf.GetState() != tree.StatusAlarm {
		t.Fatalf("alarm state changed unexpectedly: wasInAlarm=%v state=%q", b.wasInAlarm, leaf.GetState())
	}
}

func TestLimitCheckDeadbandSuppressesAlarmTransitions(t *testing.T) {
	leaf := tree.NewFloatLeaf("t", tree.TagConfig{Name: "t", Type: tree.TypeFloat}, tree.TagShared{Deadband: 1})
	b := &LimitCheckBlock{HiLimit: ptrFloat(100)}

	if _, err := b.Process(leaf, 99.8); err != nil {
		t.Fatal(err)
	}
	if _, err := b.Process(leaf, 100.2); err != nil {
		t.Fatal(err)
	}
	if leaf.GetState() == tree.StatusAlarm || b.wasInAlarm {
		t.Fatalf("within-deadband threshold crossing should not alarm: state=%q wasInAlarm=%v", leaf.GetState(), b.wasInAlarm)
	}

	if _, err := b.Process(leaf, 101.0); err != nil {
		t.Fatal(err)
	}
	if leaf.GetState() != tree.StatusAlarm || !b.wasInAlarm {
		t.Fatalf("outside-deadband threshold crossing should alarm: state=%q wasInAlarm=%v", leaf.GetState(), b.wasInAlarm)
	}
}

func TestLimitCheckNonNumeric(t *testing.T) {
	b := &LimitCheckBlock{HiLimit: ptrFloat(100)}
	result, err := b.Process(nil, "not-a-number")
	if err != nil {
		t.Fatal(err)
	}
	if result != "not-a-number" {
		t.Error("non-numeric should pass through")
	}
}

func TestLimitCheckHiOnlyLowOnly(t *testing.T) {
	leaf := tree.NewFloatLeaf("t", tree.TagConfig{Name: "t", Type: tree.TypeFloat})

	// Hi only
	bHi := &LimitCheckBlock{HiLimit: ptrFloat(100)}
	bHi.Process(leaf, -999.0)
	if leaf.GetState() == tree.StatusAlarm {
		t.Error("hi-only: negative value should not alarm")
	}
	bHi.Process(leaf, 200.0)
	if leaf.GetState() != tree.StatusAlarm {
		t.Error("hi-only: value above limit should alarm")
	}

	// Lo only
	leaf2 := tree.NewFloatLeaf("t2", tree.TagConfig{Name: "t2", Type: tree.TypeFloat})
	bLo := &LimitCheckBlock{LowLimit: ptrFloat(0)}
	bLo.Process(leaf2, 999.0)
	if leaf2.GetState() == tree.StatusAlarm {
		t.Error("lo-only: positive value should not alarm")
	}
	bLo.Process(leaf2, -10.0)
	if leaf2.GetState() != tree.StatusAlarm {
		t.Error("lo-only: value below limit should alarm")
	}
}

func TestLimitCheckConcurrentProcess(t *testing.T) {
	leaf := tree.NewFloatLeaf("t", tree.TagConfig{Name: "t", Type: tree.TypeFloat})
	b := &LimitCheckBlock{HiLimit: ptrFloat(100), LowLimit: ptrFloat(0)}

	var wg sync.WaitGroup
	values := []float64{50, 150, -10, 75, 200, 25}
	for i := 0; i < 64; i++ {
		wg.Add(1)
		go func(offset int) {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				if _, err := b.Process(leaf, values[(offset+j)%len(values)]); err != nil {
					t.Errorf("Process: %v", err)
					return
				}
			}
		}(i)
	}
	wg.Wait()
}

func TestLimitCheckSchema(t *testing.T) {
	b := &LimitCheckBlock{}
	schema := b.Schema()
	if schema.Type != "limitcheck" {
		t.Errorf("schema type = %q", schema.Type)
	}
}

func TestLimitCheckClose(t *testing.T) {
	b := &LimitCheckBlock{}
	b.Close(nil) // should not panic
}

// ── ScalingBlock ────────────────────────────────────────────────────────────

func TestScalingBlockSchema(t *testing.T) {
	b := &ScalingBlock{}
	schema := b.Schema()
	if schema.Type != "scaling" {
		t.Errorf("schema type = %q", schema.Type)
	}
}

func TestScalingBlockNonNumeric(t *testing.T) {
	b := &ScalingBlock{Scale: 2.0, Offset: 0.0}
	result, err := b.Process(nil, "text")
	if err != nil {
		t.Fatal(err)
	}
	if result != "text" {
		t.Error("non-numeric should pass through")
	}
}

func TestScalingBlockBoolInput(t *testing.T) {
	b := &ScalingBlock{Scale: 10.0, Offset: 0.0}
	result, _ := b.Process(nil, true)
	if result != 10.0 {
		t.Errorf("true * 10 = %v, want 10", result)
	}
	result, _ = b.Process(nil, false)
	if result != 0.0 {
		t.Errorf("false * 10 = %v, want 0", result)
	}
}

func TestScalingBlockInt64(t *testing.T) {
	b := &ScalingBlock{Scale: 1.0, Offset: 5.0}
	result, _ := b.Process(nil, int64(10))
	if result != 15.0 {
		t.Errorf("int64(10) * 1 + 5 = %v, want 15", result)
	}
}

func TestScalingBlockInt32(t *testing.T) {
	b := &ScalingBlock{Scale: 1.0, Offset: 0.0}
	result, _ := b.Process(nil, int32(7))
	if result != 7.0 {
		t.Errorf("int32(7) = %v, want 7", result)
	}
}

func TestScalingBlockFloat32(t *testing.T) {
	b := &ScalingBlock{Scale: 2.0, Offset: 0.0}
	result, _ := b.Process(nil, float32(3.5))
	if result != 7.0 {
		t.Errorf("float32(3.5) * 2 = %v, want 7", result)
	}
}

// ── StaleCheckBlock ─────────────────────────────────────────────────────────

func TestStaleCheckSchema(t *testing.T) {
	b := &StaleCheckBlock{}
	schema := b.Schema()
	if schema.Type != "stalecheck" {
		t.Errorf("schema type = %q", schema.Type)
	}
	if b.GetType() != "stalecheck" {
		t.Errorf("GetType() = %q", b.GetType())
	}
}

func TestStaleCheckProcess(t *testing.T) {
	leaf := tree.NewFloatLeaf("temp", tree.TagConfig{Name: "temp", Type: tree.TypeFloat})
	timeout := 50 * time.Millisecond
	b := &StaleCheckBlock{Timeout: &timeout}
	b.Init(leaf)
	defer b.Close(leaf)

	// Process a value - should clear stale
	leaf.SetState(tree.StatusStale)
	b.Process(leaf, 25.0)
	if leaf.GetState() == tree.StatusStale {
		t.Error("process should clear stale state")
	}

	// Wait for timeout to fire (timing wheel has 1s resolution, so wait longer)
	time.Sleep(1200 * time.Millisecond)
	if leaf.GetState() != tree.StatusStale {
		t.Error("expected stale state after timeout")
	}
}

func TestStaleCheckGetSetParameters(t *testing.T) {
	timeout := 30 * time.Second
	b := &StaleCheckBlock{Timeout: &timeout}
	params := b.GetParameters()

	b2 := &StaleCheckBlock{}
	if err := b2.SetParameters(params); err != nil {
		t.Fatal(err)
	}
	if b2.Timeout == nil || *b2.Timeout != 30*time.Second {
		t.Errorf("round-trip failed: %v", b2.Timeout)
	}
}

func TestStaleCheckCloseNilTimer(t *testing.T) {
	b := &StaleCheckBlock{}
	b.Close(nil) // should not panic
}

// ── HistoryRecorderBlock ────────────────────────────────────────────────────

func TestHistoryRecorderSchema(t *testing.T) {
	b := &HistoryRecorderBlock{}
	schema := b.Schema()
	if schema.Type != "historyrecorder" {
		t.Errorf("schema type = %q", schema.Type)
	}
	if b.GetType() != "historyrecorder" {
		t.Errorf("GetType() = %q", b.GetType())
	}
}

func TestHistoryRecorderProcessNoDb(t *testing.T) {
	// Without a DB, Process should skip without panic
	b := &HistoryRecorderBlock{}
	leaf := tree.NewFloatLeaf("t", tree.TagConfig{Name: "t", Type: tree.TypeFloat})

	// Process with no org/device - returns value unchanged
	result, err := b.Process(leaf, 42.0)
	if err != nil {
		t.Fatal(err)
	}
	if result != 42.0 {
		t.Errorf("result = %v", result)
	}
}

func TestHistoryRecorderProcessNonNumeric(t *testing.T) {
	// Non-numeric values pass through without recording (no DB needed)
	b := &HistoryRecorderBlock{}
	leaf := tree.NewStringLeaf("name", tree.TagConfig{Name: "name", Type: tree.TypeString})
	result, err := b.Process(leaf, "hello")
	if err != nil {
		t.Fatal(err)
	}
	if result != "hello" {
		t.Error("non-numeric should pass through")
	}
}

func TestHistoryRecorderGetSetParameters(t *testing.T) {
	b := &HistoryRecorderBlock{}
	params := b.GetParameters()
	b2 := &HistoryRecorderBlock{}
	if err := b2.SetParameters(params); err != nil {
		t.Fatal(err)
	}
}
