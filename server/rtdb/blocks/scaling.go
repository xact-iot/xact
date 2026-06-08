package blocks

import (
	"encoding/json"
	"fmt"

	"github.com/xact-iot/xact/rtdb/tree"
)

func init() {
	tree.RegisterProcessBlock("scaling", func() tree.ProcessBlock { return &ScalingBlock{} })
}

// ScalingBlock applies linear scaling: output = (input * Scale) + Offset, optionally clamped
type ScalingBlock struct {
	Scale  float64  `json:"scale"`
	Offset float64  `json:"offset"`
	Min    *float64 `json:"min,omitempty"` // nil = no clamp
	Max    *float64 `json:"max,omitempty"` // nil = no clamp
}

func (b *ScalingBlock) GetType() string { return "scaling" }

func (b *ScalingBlock) Init(leaf tree.Leaf)  {}
func (b *ScalingBlock) Close(leaf tree.Leaf) {}

func (b *ScalingBlock) Process(leaf tree.Leaf, value any) (any, error) {
	v, err := toFloat64(value)
	if err != nil {
		return value, nil // pass through non-numeric values
	}

	result := v*b.Scale + b.Offset

	if b.Min != nil && result < *b.Min {
		result = *b.Min
	}
	if b.Max != nil && result > *b.Max {
		result = *b.Max
	}
	return result, nil
}

func (b *ScalingBlock) GetParameters() json.RawMessage {
	data, _ := json.Marshal(b)
	return data
}

func (b *ScalingBlock) SetParameters(data json.RawMessage) error {
	return json.Unmarshal(data, b)
}

func (b *ScalingBlock) Schema() tree.BlockSchema {
	return tree.BlockSchema{
		Type:        "scaling",
		Label:       "Scaling",
		Description: "Linear: output = (input × scale) + offset, with optional clamp",
		Params: map[string]tree.ParamSchema{
			"scale":  {Type: "number", Label: "Scale", Required: true, Default: 1},
			"offset": {Type: "number", Label: "Offset", Required: true, Default: 0},
			"min":    {Type: "number", Label: "Min Clamp"},
			"max":    {Type: "number", Label: "Max Clamp"},
		},
	}
}

// toFloat64 converts numeric types to float64
func toFloat64(v any) (float64, error) {
	switch n := v.(type) {
	case float64:
		return n, nil
	case float32:
		return float64(n), nil
	case int:
		return float64(n), nil
	case int64:
		return float64(n), nil
	case int32:
		return float64(n), nil
	case bool:
		if n {
			return 1.0, nil
		} else {
			return 0.0, nil
		}
	default:
		return 0, fmt.Errorf("cannot convert %T to float64", v)
	}
}
