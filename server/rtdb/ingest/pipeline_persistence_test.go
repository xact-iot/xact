package ingest

import (
	"testing"

	_ "github.com/xact-iot/xact/rtdb/blocks"
	"github.com/xact-iot/xact/rtdb/tree"
)

func TestExistingTagPipelineChangeNotifiesPersistence(t *testing.T) {
	ops := setupTree(t)
	p := NewProcessor(ops)
	const path = "/TestOrg/VMS/Dev1/meta/lat"
	value := expandedTagValue{value: float64(49.28), hasValue: true}
	if err := p.writeTag(path, "meta", "lat", value, "", "TestOrg", 0); err != nil {
		t.Fatal(err)
	}
	changes := 0
	ops.SetOnStructureChange(func(changedPath string, node tree.TreeNode) {
		if changedPath != path {
			t.Errorf("unexpected path: %s", changedPath)
		}
		changes++
	})
	value.persist = true
	if err := p.writeTag(path, "meta", "lat", value, "", "TestOrg", 0); err != nil {
		t.Fatal(err)
	}
	if changes != 1 {
		t.Fatalf("pipeline change notified persistence %d times", changes)
	}
	// Repeated value writes with the same pipeline should not trigger saves.
	if err := p.writeTag(path, "meta", "lat", value, "", "TestOrg", 0); err != nil {
		t.Fatal(err)
	}
	if changes != 1 {
		t.Fatalf("unchanged pipeline triggered persistence: %d", changes)
	}
}

func TestRepeatedHistoryMetricsDoNotNotifyPersistence(t *testing.T) {
	ops := setupTree(t)
	p := NewProcessor(ops)
	const path = "/TestOrg/VMS/Dev1/metrics/cpu"
	value := expandedTagValue{value: float64(1), hasValue: true, history: true}
	if err := p.writeTag(path, "metrics", "cpu", value, "", "TestOrg", 0); err != nil {
		t.Fatal(err)
	}
	changes := 0
	ops.SetOnStructureChange(func(string, tree.TreeNode) { changes++ })
	for i := 0; i < 10; i++ {
		value.value = float64(i)
		if err := p.writeTag(path, "metrics", "cpu", value, "", "TestOrg", int64(i)); err != nil {
			t.Fatal(err)
		}
	}
	if changes != 0 {
		t.Fatalf("unchanged history pipeline triggered %d configuration saves", changes)
	}
}
