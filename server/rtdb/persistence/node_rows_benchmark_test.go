package persistence

import (
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/xact-iot/xact/rtdb/tree"
)

// Measures checkpoint preparation, excluding database time and live ingest.
func BenchmarkCheckpointSerialization(b *testing.B) {
	ops := tree.NewTreeWithOperations(nil)
	for i := 0; i < 2000; i++ {
		path := fmt.Sprintf("default.group%d", i)
		for j := 0; j < 16; j++ {
			name := fmt.Sprintf("tag%d", j)
			if err := ops.CreateTag(path+"."+name, tree.TypeFloat, tree.TagConfig{Name: name}); err != nil {
				b.Fatal(err)
			}
		}
	}
	mgr := NewManager(nil, ops, "default", time.Hour)
	b.Run("whole_tree", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			config, err := SerializeTree(ops.Root)
			if err != nil {
				b.Fatal(err)
			}
			data, err := json.Marshal(config)
			if err != nil {
				b.Fatal(err)
			}
			b.ReportMetric(float64(len(config.Nodes)), "nodes/checkpoint")
			b.ReportMetric(float64(len(data)), "bytes/checkpoint")
		}
	})
	b.Run("one_dirty_node", func(b *testing.B) {
		b.ReportAllocs()
		paths := map[string]struct{}{"default.group100": {}}
		for i := 0; i < b.N; i++ {
			batch, err := mgr.nodeBatch(paths, nil, false)
			if err != nil {
				b.Fatal(err)
			}
			data, err := json.Marshal(batch.Upserts)
			if err != nil {
				b.Fatal(err)
			}
			b.ReportMetric(float64(len(batch.Upserts)), "nodes/checkpoint")
			b.ReportMetric(float64(len(data)), "bytes/checkpoint")
		}
	})
}
