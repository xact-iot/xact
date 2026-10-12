package tree

import (
	"testing"
	"time"
)

type restoreCountingBlock struct {
	stubBlock
	calls int
}

func (b *restoreCountingBlock) Process(_ Leaf, v any) (any, error) {
	b.calls++
	return v.(float64) * 2, nil
}

func TestRestoreValueDoesNotReplayPipeline(t *testing.T) {
	block := &restoreCountingBlock{}
	leaf := NewLeaf(TypeFloat, "latitude", TagConfig{}, TagShared{Pipeline: []ProcessBlock{block}})
	savedAt := time.UnixMilli(123456789)
	if err := leaf.RestoreValue(49.28, savedAt); err != nil {
		t.Fatal(err)
	}
	if leaf.GetAnyValue() != 49.28 || !leaf.GetUpdatedTime().Equal(savedAt) || block.calls != 0 {
		t.Fatalf("restored = %v, timestamp = %v, pipeline calls = %d", leaf.GetAnyValue(), leaf.GetUpdatedTime(), block.calls)
	}
	if err := leaf.SetAnyValue(2.0); err != nil {
		t.Fatal(err)
	}
	if block.calls != 1 || leaf.GetAnyValue() != 4.0 {
		t.Fatal("normal writes must still execute the pipeline")
	}
}
