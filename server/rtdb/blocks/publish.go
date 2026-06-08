package blocks

import (
	"encoding/json"
	"strings"
	"sync"

	"github.com/xact-iot/xact/rtdb/tree"
)

func init() {
	tree.RegisterProcessBlock("publish", func() tree.ProcessBlock { return &PublishBlock{} })
}

// PublishBlock controls whether a tag's value changes are published, with history depth and tracing
type PublishBlock struct {
	mu            sync.Mutex
	lastPubValue  any       `json:"-"`
	lastPubStatus string    `json:"-"`
	leaf          tree.Leaf `json:"-"`
	devPath       string    `json:"-"`
}

func (b *PublishBlock) GetType() string { return "publish" }

// tagPath builds a dot-separated NATS subject path, omitting empty segments
// (e.g. when GetTreeDevice returns "" for non-device leaves).
func tagPath(leaf tree.Leaf) string {
	parts := []string{"tagvalue", leaf.GetTreeOrg()}
	if dev := leaf.GetTreeDevice(); dev != "" {
		parts = append(parts, dev)
	}
	parts = append(parts, leaf.GetMetricPath())
	return strings.Join(parts, ".")
}

func (b *PublishBlock) Init(leaf tree.Leaf) {
	b.leaf = leaf
}

func (b *PublishBlock) Close(leaf tree.Leaf) {
	b.leaf = nil
}

func (b *PublishBlock) Process(leaf tree.Leaf, value any) (any, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	leaf.Publish(value)
	// if b.didChange(value, status) {
	// 	b.lastPubValue = value
	// 	b.lastPubStatus = status

	// 	metricPath := leaf.GetMetricPath()
	// 	msg := make(map[string]tree.TagValue, 1)
	// 	msg[metricPath] = tree.TagValue{
	// 		Type:      "value",
	// 		Value:     value,
	// 		Status:    status,
	// 		Timestamp: leaf.GetUpdatedTime().UnixMilli(),
	// 	}

	// 	data, err := json.Marshal(msg)
	// 	if err != nil {
	// 		return value, fmt.Errorf("failed to marshal value: %w", err)
	// 	}
	// 	bcast, err := nats.GetBroadcastStream(nats.TagValueStream)
	// 	if err != nil {
	// 		return value, fmt.Errorf("failed to marshal value: %w", err)
	// 	}
	// 	bcast.Publish(tagPath(leaf), data, 0)

	// 	// if rev, err := b.publisher.Publish(tagPath(leaf), data, b.lockRevision); err != nil {
	// 	// 	b.lockRevision = 0
	// 	// 	return value, fmt.Errorf("publish failed: %w", err)
	// 	// } else if rev != 0 {
	// 	// 	b.lockRevision = rev // only update when we won the lock; 0 means deduplicated
	// 	// }
	// }
	return value, nil
}

// func (b *PublishBlock) didChange(newValue any, status string) bool {
// 	if b.lastPubValue == nil {
// 		return true
// 	}
// 	if b.lastPubStatus != status {
// 		return true
// 	}
// 	deadband := b.leaf.GetShared().Deadband
// 	switch b.leaf.ValueType() {
// 	case tree.TypeInteger:
// 		intValue, ok := newValue.(int64)
// 		if !ok {
// 			return false
// 		}
// 		old, _ := b.lastPubValue.(int64)
// 		if Abs(float64(old)-float64(intValue)) > deadband {
// 			return true
// 		}
// 	case tree.TypeFloat:
// 		fltValue, ok := newValue.(float64)
// 		if !ok {
// 			return false
// 		}
// 		old, _ := b.lastPubValue.(float64)
// 		if Abs(old-fltValue) > deadband {
// 			return true
// 		}
// 	case tree.TypeString:
// 		strValue, ok := newValue.(string)
// 		if !ok {
// 			return false
// 		}
// 		old, _ := b.lastPubValue.(string)
// 		return strValue != old

// 	case tree.TypeBoolean:
// 		boolValue, ok := newValue.(bool)
// 		if !ok {
// 			return false
// 		}
// 		old, _ := b.lastPubValue.(bool)
// 		return boolValue != old

// 	case tree.TypeEnum:
// 		enumValue, ok := newValue.(int)
// 		if !ok {
// 			return false
// 		}
// 		old, _ := b.lastPubValue.(int)
// 		return enumValue != old
// 	}
// 	return false
// }

// func Abs[T ~int | ~int32 | ~int64 | ~float32 | ~float64](v T) T {
// 	if v < 0 {
// 		return -v
// 	}
// 	return v
// }

func (b *PublishBlock) GetParameters() json.RawMessage {
	data, _ := json.Marshal(b)
	return data
}

func (b *PublishBlock) SetParameters(data json.RawMessage) error {
	return json.Unmarshal(data, b)
}

func (b *PublishBlock) Schema() tree.BlockSchema {
	return tree.BlockSchema{
		Type:        "publish",
		Label:       "Publish",
		Description: "Publishes tag value changes via NATS",
		Params:      map[string]tree.ParamSchema{},
	}
}
