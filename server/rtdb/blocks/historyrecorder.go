package blocks

import (
	"encoding/json"
	"errors"
	"log"
	"math"
	"sync"
	"sync/atomic"
	"time"

	xnats "github.com/xact-iot/xact/rtdb/nats"
	"github.com/xact-iot/xact/rtdb/tree"
	"github.com/xact-iot/xact/sqldb"
)

// HistoryRecorderBlock records numeric tag value changes to the time-series database.
// It is shared across all leaves that inherit a template pipeline, so Init() is a no-op.
// Identity (org, device, metric) is resolved at Process() time from the leaf's parent chain.
type HistoryRecorderBlock struct {
	writer     *sqldb.MetricWriter
	lastValues sync.Map // map[string]*float64 - per-leaf deadband state; not serialised
}

// RegisterHistoryRecorder registers the historyrecorder block type with the given DB.
// Call this from startup after the database is initialised.
func RegisterHistoryRecorder(db sqldb.DB) *sqldb.MetricWriter {
	writer := sqldb.NewMetricWriter(db, sqldb.MetricWriterConfigFromEnv())
	tree.RegisterProcessBlock("historyrecorder", func() tree.ProcessBlock {
		return &HistoryRecorderBlock{writer: writer}
	})
	return writer
}

func (b *HistoryRecorderBlock) GetType() string { return "historyrecorder" }

// Init is a no-op - the block is shared across template instances.
func (b *HistoryRecorderBlock) Init(_ tree.Leaf)  {}
func (b *HistoryRecorderBlock) Close(_ tree.Leaf) {}

// Process records the value to the TSDB, applying deadband filtering if configured.
func (b *HistoryRecorderBlock) Process(leaf tree.Leaf, value any) (any, error) {
	org := leaf.GetTreeOrg()
	device := leaf.GetTreeDevice()
	if org == "" || device == "" {
		return value, nil // leaf is not under a DeviceNode - skip
	}
	metric := leaf.GetMetricPath()

	v, err := toFloat64(value)
	if err != nil {
		return value, nil // non-numeric - pass through without recording
	}

	// Apply deadband from tag shared config (per-leaf state stored in sync.Map to avoid data races)
	leafKey := org + "/" + device + "/" + metric
	if deadband := leaf.GetShared().Deadband; deadband > 0 {
		if prev, ok := b.lastValues.Load(leafKey); ok {
			if math.Abs(v-*prev.(*float64)) < deadband {
				return value, nil
			}
		}
	}
	b.lastValues.Store(leafKey, &v)

	lock := xnats.NewPubLock(xnats.SubjectName(tagPath(leaf) + ".historyrecorder"))
	rev, err := lock.TryLock()
	if err != nil {
		return value, nil
	}
	defer lock.Release(rev)

	if b.writer == nil {
		return value, nil
	}
	if err := b.writer.TryWrite(org, sqldb.MetricEntry{
		DeviceName: device,
		MetricName: metric,
		Timestamp:  leaf.GetUpdatedTime(),
		Value:      float32(v),
	}); err != nil && !errors.Is(err, sqldb.ErrMetricWriterQueueFull) {
		log.Printf("historyrecorder: queue metric %s/%s/%s: %v", org, device, metric, err)
	} else if errors.Is(err, sqldb.ErrMetricWriterQueueFull) {
		logMetricWriterDrops(b.writer)
	}

	return value, nil
}

var lastMetricDropLogUnixNano int64

func logMetricWriterDrops(writer *sqldb.MetricWriter) {
	now := time.Now()
	last := atomic.LoadInt64(&lastMetricDropLogUnixNano)
	if now.UnixNano()-last < int64(10*time.Second) {
		return
	}
	if !atomic.CompareAndSwapInt64(&lastMetricDropLogUnixNano, last, now.UnixNano()) {
		return
	}
	log.Printf("historyrecorder: metric writer queue full; dropped=%d queue=%d/%d", writer.Dropped(), writer.QueueLen(), writer.QueueCap())
}

func (b *HistoryRecorderBlock) GetParameters() json.RawMessage {
	data, _ := json.Marshal(b)
	return data
}

func (b *HistoryRecorderBlock) SetParameters(data json.RawMessage) error {
	return json.Unmarshal(data, b)
}

func (b *HistoryRecorderBlock) Schema() tree.BlockSchema {
	return tree.BlockSchema{
		Type:        "historyrecorder",
		Label:       "History Recorder",
		Description: "Records numeric value changes to the time-series database",
		Params:      map[string]tree.ParamSchema{},
	}
}
