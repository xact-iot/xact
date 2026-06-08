package ingest

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/xact-iot/xact/rtdb/tree"
)

// ── parseTS ──────────────────────────────────────────────────────────────────

func TestParseTS(t *testing.T) {
	tests := []struct {
		name   string
		input  any
		wantOk bool
		wantMs int64 // UnixMilli of result
	}{
		{"float64", float64(1700000000000), true, 1700000000000},
		{"int64", int64(1700000000000), true, 1700000000000},
		{"int", int(1700000000000), true, 1700000000000},
		{"string numeric", "1700000000000", true, 1700000000000},
		{"string non-numeric", "not-a-number", false, 0},
		{"bool", true, false, 0},
		{"nil", nil, false, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ts, ok := parseTS(tt.input)
			if ok != tt.wantOk {
				t.Errorf("parseTS(%v) ok = %v, want %v", tt.input, ok, tt.wantOk)
				return
			}
			if ok && ts != tt.wantMs {
				t.Errorf("parseTS(%v) = %d, want %d", tt.input, ts, tt.wantMs)
			}
		})
	}
}

// ── DetermineType ────────────────────────────────────────────────────────────

func TestDetermineType(t *testing.T) {
	tests := []struct {
		input any
		want  tree.ScalarType
	}{
		{true, tree.TypeBoolean},
		{false, tree.TypeBoolean},
		{"hello", tree.TypeString},
		{int(10), tree.TypeInteger},
		{int64(20), tree.TypeInteger},
		{uint(30), tree.TypeInteger},
		{float64(3.14), tree.TypeFloat},
		{float64(10.0), tree.TypeFloat},
		{float32(1.5), tree.TypeFloat},
		{nil, tree.TypeFloat}, // default
	}
	for _, tt := range tests {
		got := DetermineType(tt.input)
		if got != tt.want {
			t.Errorf("DetermineType(%v [%T]) = %v, want %v", tt.input, tt.input, got, tt.want)
		}
	}
}

// ── extractTagValue ──────────────────────────────────────────────────────────

func TestExtractTagValue_Scalar(t *testing.T) {
	ev := extractTagValue(42.0)
	if !ev.hasValue || ev.value != 42.0 {
		t.Errorf("scalar: %+v", ev)
	}
}

func TestExtractTagValue_ExpandedWithValue(t *testing.T) {
	raw := map[string]any{
		"value": 25.5,
		"units": "°C",
	}
	ev := extractTagValue(raw)
	if !ev.hasValue || ev.value != 25.5 {
		t.Errorf("value: %v", ev.value)
	}
	if ev.units != "°C" {
		t.Errorf("units: %q", ev.units)
	}
}

func TestExtractTagValue_WithHistory(t *testing.T) {
	raw := map[string]any{
		"value":   10.0,
		"history": true,
	}
	ev := extractTagValue(raw)
	if !ev.history {
		t.Errorf("history: %v", ev.history)
	}
}

func TestExtractTagValue_WithLimits(t *testing.T) {
	raw := map[string]any{
		"value": 50.0,
		"limits": map[string]any{
			"hi": 100.0,
			"lo": 0.0,
		},
	}
	ev := extractTagValue(raw)
	if ev.limits == nil {
		t.Fatal("limits should not be nil")
	}
	if ev.limits.Hi != 100.0 {
		t.Errorf("hi: %v", ev.limits.Hi)
	}
	if ev.limits.Lo != 0.0 {
		t.Errorf("lo: %v", ev.limits.Lo)
	}
}

func TestExtractTagValue_WithPersist(t *testing.T) {
	raw := map[string]any{
		"value":   1.0,
		"persist": true,
	}
	ev := extractTagValue(raw)
	if !ev.persist {
		t.Error("persist should be true")
	}
}

func TestExtractTagValue_ObjectWithoutKnownFields(t *testing.T) {
	// Object without known provisioning fields → treated as plain value
	raw := map[string]any{
		"foo": "bar",
		"baz": 42,
	}
	ev := extractTagValue(raw)
	if !ev.hasValue {
		t.Error("should have value")
	}
	// The value should be the map itself
	if _, ok := ev.value.(map[string]any); !ok {
		t.Errorf("value should be the original map, got %T", ev.value)
	}
}

func TestExtractTagValueProvisioningMetadata(t *testing.T) {
	raw := map[string]any{
		"value":       2.0,
		"type":        "enum",
		"enumValues":  map[string]any{"0": "Off", "1": "On", "bad": "ignored"},
		"description": "run state",
		"deadband":    0.5,
		"publish":     true,
		"stalecheck":  30.0,
		"scaling": map[string]any{
			"scale": 2.0,
			"min":   -10.0,
			"max":   100.0,
		},
		"limits": map[string]any{
			"hi": 90.0,
			"hiEvent": map[string]any{
				"enabled":        true,
				"severity":       "warning",
				"message":        "too high",
				"notificationId": "Manager",
			},
		},
	}

	ev := extractTagValue(raw)
	if !ev.isEnum || ev.enumValues[1] != "On" || ev.description != "run state" || !ev.publish {
		t.Fatalf("unexpected extracted metadata: %+v", ev)
	}
	if ev.deadband == nil || *ev.deadband != 0.5 {
		t.Fatalf("deadband = %v", ev.deadband)
	}
	if ev.staleTimeout == nil || *ev.staleTimeout != 30 {
		t.Fatalf("stale timeout = %v", ev.staleTimeout)
	}
	if ev.scaling == nil || ev.scaling.Scale != 2 || ev.scaling.Min == nil || *ev.scaling.Min != -10 {
		t.Fatalf("scaling = %+v", ev.scaling)
	}
	if ev.limits == nil || ev.limits.HiEvent.NotificationID != "Manager" {
		t.Fatalf("limits = %+v", ev.limits)
	}
}

func TestParseEnumValues(t *testing.T) {
	fromAny := parseEnumValues(map[string]any{"1": "One", "bad": "ignored"})
	if len(fromAny) != 1 || fromAny[1] != "One" {
		t.Fatalf("map[string]any enum values = %#v", fromAny)
	}
	fromString := parseEnumValues(map[string]string{"2": "Two"})
	if len(fromString) != 1 || fromString[2] != "Two" {
		t.Fatalf("map[string]string enum values = %#v", fromString)
	}
	fromInt := parseEnumValues(map[int]string{3: "Three"})
	if len(fromInt) != 1 || fromInt[3] != "Three" {
		t.Fatalf("map[int]string enum values = %#v", fromInt)
	}
	if got := parseEnumValues([]string{"nope"}); len(got) != 0 {
		t.Fatalf("unsupported enum values = %#v", got)
	}
}

func TestParseLimitEventAndScalingConfig(t *testing.T) {
	event := parseLimitEvent(map[string]any{
		"enabled":        true,
		"severity":       "critical",
		"message":        "limit breached",
		"notificationId": float64(12),
	})
	if !event.Enabled || event.Severity != "critical" || event.Message != "limit breached" || event.NotificationID != float64(12) {
		t.Fatalf("event = %+v", event)
	}

	scaling := parseScalingConfig(map[string]any{"offset": 1.5, "max": 10})
	if scaling.Scale != 1 || scaling.Offset != 1.5 || scaling.Min != nil || scaling.Max == nil || *scaling.Max != 10 {
		t.Fatalf("scaling defaults = %+v", scaling)
	}
}

type testNotificationResolver struct {
	called bool
}

func (r *testNotificationResolver) ResolveNotificationID(_ context.Context, org, name string) (int, error) {
	r.called = org == "default" && name == "Manager"
	return 42, nil
}

func TestResolveLimitEvent(t *testing.T) {
	proc := NewProcessor(setupTree(t))
	if got := proc.resolveLimitEvent(nil, "default"); got != nil {
		t.Fatalf("nil event = %#v", got)
	}

	event := proc.resolveLimitEvent(&limitEventConfig{Enabled: true, Severity: "warning", Message: "none", NotificationID: "none"}, "default")
	if event == nil || event.NotificationID != 0 || !event.Enabled {
		t.Fatalf("none event = %#v", event)
	}

	resolver := &testNotificationResolver{}
	proc.SetNotificationResolver(resolver)
	event = proc.resolveLimitEvent(&limitEventConfig{NotificationID: "Manager"}, "default")
	if event == nil || event.NotificationID != 42 || !resolver.called {
		t.Fatalf("resolved event = %#v resolver=%+v", event, resolver)
	}

	if got := proc.resolveLimitEvent(&limitEventConfig{NotificationID: float64(7)}, "default"); got.NotificationID != 7 {
		t.Fatalf("float notification id = %#v", got)
	}
	if got := proc.resolveLimitEvent(&limitEventConfig{NotificationID: 8}, "default"); got.NotificationID != 8 {
		t.Fatalf("int notification id = %#v", got)
	}
}

// ── toFloat64Any ─────────────────────────────────────────────────────────────

func TestToFloat64Any(t *testing.T) {
	tests := []struct {
		input  any
		want   float64
		wantOk bool
	}{
		{float64(3.14), 3.14, true},
		{int(10), 10.0, true},
		{int64(20), 20.0, true},
		{"string", 0, false},
		{nil, 0, false},
		{true, 0, false},
	}
	for _, tt := range tests {
		got, ok := toFloat64Any(tt.input)
		if ok != tt.wantOk {
			t.Errorf("toFloat64Any(%v) ok = %v, want %v", tt.input, ok, tt.wantOk)
			continue
		}
		if ok && got != tt.want {
			t.Errorf("toFloat64Any(%v) = %v, want %v", tt.input, got, tt.want)
		}
	}
}

// ── blockRank / insertOrdered / findBlockIndex ───────────────────────────────

type stubBlock struct{ blockType string }

func (b *stubBlock) GetType() string                         { return b.blockType }
func (b *stubBlock) Init(_ tree.Leaf)                        {}
func (b *stubBlock) Close(_ tree.Leaf)                       {}
func (b *stubBlock) Process(_ tree.Leaf, v any) (any, error) { return v, nil }
func (b *stubBlock) GetParameters() json.RawMessage          { return json.RawMessage("{}") }
func (b *stubBlock) SetParameters(_ json.RawMessage) error   { return nil }
func (b *stubBlock) Schema() tree.BlockSchema                { return tree.BlockSchema{Type: b.blockType} }

func TestBlockRank(t *testing.T) {
	if r := blockRank("scaling"); r != 0 {
		t.Errorf("scaling rank = %d, want 0", r)
	}
	if r := blockRank("publish"); r != 5 {
		t.Errorf("publish rank = %d, want 5", r)
	}
	// Unknown block gets sentinel value
	if r := blockRank("unknown"); r != len(canonicalBlockOrder) {
		t.Errorf("unknown rank = %d, want %d", r, len(canonicalBlockOrder))
	}
}

func TestFindBlockIndex(t *testing.T) {
	pipeline := []tree.ProcessBlock{
		&stubBlock{"scaling"},
		&stubBlock{"limitcheck"},
		&stubBlock{"publish"},
	}
	if idx := findBlockIndex(pipeline, "limitcheck"); idx != 1 {
		t.Errorf("findBlockIndex(limitcheck) = %d, want 1", idx)
	}
	if idx := findBlockIndex(pipeline, "missing"); idx != -1 {
		t.Errorf("findBlockIndex(missing) = %d, want -1", idx)
	}
}

func TestInsertOrdered(t *testing.T) {
	pipeline := []tree.ProcessBlock{
		&stubBlock{"scaling"},
		&stubBlock{"publish"},
	}
	// Insert limitcheck - should go between scaling and publish
	pipeline = insertOrdered(pipeline, &stubBlock{"limitcheck"})
	if len(pipeline) != 3 {
		t.Fatalf("len = %d, want 3", len(pipeline))
	}
	if pipeline[0].GetType() != "scaling" {
		t.Errorf("[0] = %q", pipeline[0].GetType())
	}
	if pipeline[1].GetType() != "limitcheck" {
		t.Errorf("[1] = %q", pipeline[1].GetType())
	}
	if pipeline[2].GetType() != "publish" {
		t.Errorf("[2] = %q", pipeline[2].GetType())
	}
}

func TestInsertOrderedAppend(t *testing.T) {
	pipeline := []tree.ProcessBlock{
		&stubBlock{"scaling"},
	}
	// Publish has highest rank, should be appended
	pipeline = insertOrdered(pipeline, &stubBlock{"publish"})
	if len(pipeline) != 2 {
		t.Fatalf("len = %d", len(pipeline))
	}
	if pipeline[1].GetType() != "publish" {
		t.Errorf("[1] = %q", pipeline[1].GetType())
	}
}

// ── ParsePayload with timestamp ──────────────────────────────────────────────

func TestParsePayloadWithTimestamp(t *testing.T) {
	payload := []byte(`{
		"ts": 1700000000000,
		"temp": 25.5
	}`)
	data, err := ParsePayload(payload)
	if err != nil {
		t.Fatal(err)
	}
	if data.TSUnixMilli != 1700000000000 {
		t.Errorf("timestamp = %d", data.TSUnixMilli)
	}
	if _, ok := data.DirectTags["temp"]; !ok {
		t.Error("expected 'temp' direct tag")
	}
}

func TestParsePayloadGroupTimestamp(t *testing.T) {
	payload := []byte(`{
		"grp": {"ts": 1700000000000, "temp": 25.5}
	}`)
	data, err := ParsePayload(payload)
	if err != nil {
		t.Fatal(err)
	}
	if data.TSUnixMilli != 1700000000000 {
		t.Errorf("timestamp = %d", data.TSUnixMilli)
	}
	// ts should be removed from the group
	grp := data.Groups["grp"]
	if _, ok := grp["ts"]; ok {
		t.Error("ts should be removed from group")
	}
}

func TestParsePayloadInvalidJSON(t *testing.T) {
	_, err := ParsePayload([]byte(`{invalid`))
	if err == nil {
		t.Error("expected error for invalid JSON")
	}
}

func TestParsePayloadNoTimestamp(t *testing.T) {
	payload := []byte(`{"temp": 25.5}`)
	data, err := ParsePayload(payload)
	if err != nil {
		t.Fatal(err)
	}
	// Should use current time (non-zero)
	if data.TSUnixMilli == 0 {
		t.Error("timestamp should not be zero")
	}
}

// ── WriteDeviceData with empty names ─────────────────────────────────────────

func TestWriteDeviceDataEmptyNames(t *testing.T) {
	treeOps := setupTree(t)
	proc := NewProcessor(treeOps)

	// Empty tag names should be skipped
	data := TagData{
		DirectTags: map[string]any{"": 42.0, "valid": 1.0},
		Groups:     map[string]map[string]any{"": {"x": 1}, "grp": {"": 2, "y": 3}},
	}
	if err := proc.WriteDeviceData("TestOrg", "", "VMS", "Dev1", data); err != nil {
		t.Fatal(err)
	}
	// Only "valid" and "y" should have been created
	if _, err := treeOps.FindLeaf("TestOrg/VMS/Dev1/valid"); err != nil {
		t.Error("'valid' tag should exist")
	}
	if _, err := treeOps.FindLeaf("TestOrg/VMS/Dev1/grp/y"); err != nil {
		t.Error("'y' tag should exist")
	}
}
