package nats

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/nats-io/nats.go/jetstream"
	"github.com/xact-iot/xact/rtdb/tree"
)

// Compare the publication path against the previous per-leaf protocol using
// the same isolated embedded broker. This measures snapshot latency and
// allocations, not total production CPU or persistence/checkpoint work.
func BenchmarkDeviceTagPublications(b *testing.B) {
	for _, grouped := range []bool{false, true} {
		name := "individual"
		if grouped {
			name = "batched"
		}
		b.Run(name, func(b *testing.B) {
			cfg := testDefaultConfig()
			cfg.StoreDir = b.TempDir()
			s, err := newTestEmbeddedServer(cfg)
			if err != nil {
				b.Fatal(err)
			}
			b.Cleanup(s.Shutdown)
			js, err := jetstream.New(s.Conn())
			if err != nil {
				b.Fatal(err)
			}
			_, err = js.CreateStream(context.Background(), jetstream.StreamConfig{Name: "tagvalue", Subjects: []string{BroadcastStreamPrefix + "tagvalue.>", BroadcastStreamPrefix + "tagbatch.>"}, Storage: jetstream.MemoryStorage, MaxMsgsPerSubject: 1})
			if err != nil {
				b.Fatal(err)
			}
			pub := &BroadcastStream{js: js}
			paths := make([]string, 64)
			for i := range paths {
				paths[i] = fmt.Sprintf("tagvalue.acme.Bus.group%d.field%d", i/16, i%16)
			}
			b.ReportAllocs()
			b.ResetTimer()
			for iteration := 0; iteration < b.N; iteration++ {
				var finish func() error
				if grouped {
					finish = pub.BeginTagValueBatch("acme.Bus")
				}
				for _, path := range paths {
					value := tree.TagValue{Type: "value", Value: float64(iteration), Timestamp: 1750000000123}
					if grouped {
						if err := pub.PublishTagValue(path, value); err != nil {
							b.Fatal(err)
						}
					} else {
						data, err := json.Marshal(map[string]tree.TagValue{strings.TrimPrefix(path, "tagvalue.acme.Bus."): value})
						if err != nil {
							b.Fatal(err)
						}
						if _, err := pub.Publish(path, data, 0); err != nil {
							b.Fatal(err)
						}
					}
				}
				if finish != nil {
					if err := finish(); err != nil {
						b.Fatal(err)
					}
				}
			}
			b.StopTimer()
			publications := float64(64)
			if grouped {
				publications = 4
			}
			b.ReportMetric(publications, "publishes/snapshot")
		})
	}
}
