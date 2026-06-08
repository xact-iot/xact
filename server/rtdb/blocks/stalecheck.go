package blocks

import (
	"encoding/json"
	"sync"
	"sync/atomic"
	"time"

	"github.com/xact-iot/xact/rtdb/tree"
)

var staleTimingWheel *tree.TimingWheel

func init() {
	tree.RegisterProcessBlock("stalecheck", func() tree.ProcessBlock { return &StaleCheckBlock{} })
	staleTimingWheel = tree.NewTimingWheel(time.Second, 3600)
	staleTimingWheel.Start()
}

// StaleCheckBlock checks age of last update and sets tag state to InAlarm
type StaleCheckBlock struct {
	mu         sync.Mutex
	Timeout    *time.Duration `json:"timeout,omitempty"`
	timer      *tree.Timer
	selfUpdate atomic.Bool
}

func (b *StaleCheckBlock) GetType() string { return "stalecheck" }

func (b *StaleCheckBlock) Init(leaf tree.Leaf) {
	timeout := time.Second * 30 // default
	if b.Timeout != nil {
		timeout = *b.Timeout
	}
	b.timer = staleTimingWheel.AfterFunc(timeout, func() {
		if leaf != nil {
			leaf.SetState(tree.StatusStale)
		}
	})
}
func (b *StaleCheckBlock) Close(leaf tree.Leaf) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.timer != nil {
		b.timer.Stop()
		b.timer = nil
	}
}

func (b *StaleCheckBlock) Process(leaf tree.Leaf, value any) (any, error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	// Is this a self initiated update?
	// In which case we don't want to clear the stale status
	if b.selfUpdate.Load() {
		b.selfUpdate.Store(false)
		return value, nil
	}
	// Restart the stale timer
	if b.timer != nil {
		b.timer.Stop()
		b.timer = nil
	}
	b.timer = staleTimingWheel.AfterFunc(*b.Timeout, func() {
		if leaf != nil {
			leaf.SetState(tree.StatusStale)
			leaf.Publish(value)
		}
	})

	// A fresh value arrived - the tag is no longer stale.
	if leaf != nil {
		leaf.ClearState(tree.StatusStale)
	}

	return value, nil // pass value through unchanged
}
func (b *StaleCheckBlock) GetParameters() json.RawMessage {
	data, _ := json.Marshal(b)
	return data
}

func (b *StaleCheckBlock) SetParameters(data json.RawMessage) error {
	return json.Unmarshal(data, b)
}

func (b *StaleCheckBlock) Schema() tree.BlockSchema {
	return tree.BlockSchema{
		Type:        "stalecheck",
		Label:       "Stale Check",
		Description: "Sets STALE status if tag value not updated in timeout",
		Params: map[string]tree.ParamSchema{
			"Timeout": {Type: "number", Label: "Timeout (sec)", Required: true, Default: 30},
		},
	}
}
