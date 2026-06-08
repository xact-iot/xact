package blocks

import (
	"encoding/json"
	"log"
	"math"
	"strings"
	"sync"

	natspkg "github.com/xact-iot/xact/rtdb/nats"
	"github.com/xact-iot/xact/rtdb/tree"
)

func init() {
	tree.RegisterProcessBlock("persist", func() tree.ProcessBlock { return &PersistBlock{} })
}

// PersistBlock caches tag values to NATS KV (file-backed) and restores them on
// startup. Writes are suppressed when the change is within the tag's deadband.
type PersistBlock struct {
	lastValues sync.Map // map[string]any - per-leaf deadband state; not serialised
}

func (b *PersistBlock) GetType() string { return "persist" }

// persistKey builds a dot-separated key: org.devicePath.metricPath
func persistKey(leaf tree.Leaf) string {
	parts := []string{leaf.GetTreeOrg()}
	if dev := leaf.GetTreeDevice(); dev != "" {
		parts = append(parts, dev)
	}
	parts = append(parts, leaf.GetMetricPath())
	return strings.Join(parts, ".")
}

// Init restores the last-known value from the KV store. The restoration runs
// in a goroutine so it executes after InitPipelineBlocks finishes and all
// downstream blocks (e.g. publish) are fully initialised.
func (b *PersistBlock) Init(leaf tree.Leaf) {
	store := natspkg.GetPersistStore()
	if store == nil {
		log.Printf("[persist] store unavailable\n")
		return
	}
	key := persistKey(leaf)
	entry, err := store.Get(key)
	if err != nil || entry == nil {
		return
	}
	go func() {
		if err := leaf.SetAnyValue(entry.Value); err != nil {
			log.Printf("[persist] restore %s: %v", key, err)
		}
	}()
}

func (b *PersistBlock) Close(_ tree.Leaf) {}

// Process writes the value to the KV store (respecting deadband) then passes
// it through unchanged.
func (b *PersistBlock) Process(leaf tree.Leaf, value any) (any, error) {
	key := persistKey(leaf)

	deadband := leaf.GetShared().Deadband
	if deadband > 0 {
		if prev, ok := b.lastValues.Load(key); ok {
			if withinDeadband(prev, value, deadband) {
				return value, nil
			}
		}
	}
	b.lastValues.Store(key, value)

	lock := natspkg.NewPubLock(natspkg.SubjectName(tagPath(leaf) + ".persist"))
	rev, err := lock.TryLock()
	if err != nil {
		return value, nil
	}
	defer lock.Release(rev)

	store := natspkg.GetPersistStore()
	if store == nil {
		return value, nil // degrade gracefully
	}
	if err := store.Put(key, value); err != nil {
		log.Printf("[persist] put %s: %v", key, err)
	}
	return value, nil
}

// withinDeadband returns true when |new - prev| < deadband for numeric types.
func withinDeadband(prev, next any, deadband float64) bool {
	switch p := prev.(type) {
	case float64:
		if n, ok := next.(float64); ok {
			return math.Abs(n-p) < deadband
		}
	case int64:
		if n, ok := next.(int64); ok {
			return math.Abs(float64(n)-float64(p)) < deadband
		}
	}
	return false // non-numeric: always persist
}

func (b *PersistBlock) GetParameters() json.RawMessage        { return json.RawMessage("{}") }
func (b *PersistBlock) SetParameters(_ json.RawMessage) error { return nil }

func (b *PersistBlock) Schema() tree.BlockSchema {
	return tree.BlockSchema{
		Type:        "persist",
		Label:       "Persist",
		Description: "Caches the tag value to durable storage; restores last-known value on startup. Writes are suppressed within deadband.",
		Params:      map[string]tree.ParamSchema{},
	}
}
