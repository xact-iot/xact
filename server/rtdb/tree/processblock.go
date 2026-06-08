package tree

import (
	"encoding/json"
	"fmt"
	"sync"
)

// ParamSchema describes a single parameter accepted by a ProcessBlock.
type ParamSchema struct {
	Type     string   `json:"type"` // "number", "boolean", "string", "select"
	Label    string   `json:"label"`
	Options  []string `json:"options,omitempty"` // valid values for "select" type
	Required bool     `json:"required,omitempty"`
	Default  any      `json:"default,omitempty"`
}

// BlockSchema is the self-description of a ProcessBlock type.
type BlockSchema struct {
	Type        string                 `json:"type"`
	Label       string                 `json:"label"`
	Description string                 `json:"description"`
	Params      map[string]ParamSchema `json:"params"`
}

// ProcessBlock is the interface for a single processing step in a tag's pipeline
type ProcessBlock interface {
	GetType() string
	Init(leaf Leaf)
	Close(leaf Leaf)
	Process(leaf Leaf, value any) (any, error)
	GetParameters() json.RawMessage
	SetParameters(json.RawMessage) error
	Schema() BlockSchema
}

// ProcessBlockEnvelope is the JSON serialization wrapper for a ProcessBlock
type ProcessBlockEnvelope struct {
	Type       string          `json:"type"`
	Parameters json.RawMessage `json:"parameters"`
}

// ProcessBlockFactory creates a new instance of a ProcessBlock
type ProcessBlockFactory func() ProcessBlock

// processBlockRegistry maps type names to factories
var (
	processBlockRegistry   = map[string]ProcessBlockFactory{}
	processBlockRegistryMu sync.RWMutex
)

// RegisterProcessBlock registers a ProcessBlock factory by type name
func RegisterProcessBlock(name string, factory ProcessBlockFactory) {
	processBlockRegistryMu.Lock()
	defer processBlockRegistryMu.Unlock()
	processBlockRegistry[name] = factory
}

// GetRegisteredSchemas returns the BlockSchema for every registered block type.
func GetRegisteredSchemas() []BlockSchema {
	processBlockRegistryMu.RLock()
	defer processBlockRegistryMu.RUnlock()
	schemas := make([]BlockSchema, 0, len(processBlockRegistry))
	for _, factory := range processBlockRegistry {
		schemas = append(schemas, factory().Schema())
	}
	return schemas
}

// NewProcessBlockByType creates a ProcessBlock from its type name
func NewProcessBlockByType(name string) (ProcessBlock, error) {
	processBlockRegistryMu.RLock()
	defer processBlockRegistryMu.RUnlock()
	factory, ok := processBlockRegistry[name]
	if !ok {
		return nil, fmt.Errorf("unknown process block type: %s", name)
	}
	pb := factory()
	return pb, nil
}

// MarshalPipeline serializes a pipeline to JSON envelopes
func MarshalPipeline(pipeline []ProcessBlock) ([]ProcessBlockEnvelope, error) {
	envelopes := make([]ProcessBlockEnvelope, len(pipeline))
	for i, block := range pipeline {
		params := block.GetParameters()
		envelopes[i] = ProcessBlockEnvelope{
			Type:       block.GetType(),
			Parameters: params,
		}
	}
	return envelopes, nil
}

// UnmarshalPipeline deserializes JSON envelopes into a pipeline
func UnmarshalPipeline(envelopes []ProcessBlockEnvelope) ([]ProcessBlock, error) {
	pipeline := make([]ProcessBlock, len(envelopes))
	for i, env := range envelopes {
		block, err := NewProcessBlockByType(env.Type)
		if err != nil {
			return nil, fmt.Errorf("block %d: %w", i, err)
		}
		if env.Parameters != nil {
			if err := block.SetParameters(env.Parameters); err != nil {
				return nil, fmt.Errorf("block %d parameters: %w", i, err)
			}
		}
		pipeline[i] = block
	}
	return pipeline, nil
}

// ProcessValue runs the leaf's pipeline on rawValue and returns the processed result.
// It is the correct entry point for raw incoming values; call it inside SetAnyValue
// so that the concrete type passes itself (which satisfies the Leaf interface) rather
// than nil.  If the pipeline is empty the raw value is returned unchanged.
func ProcessValue(leaf Leaf, rawValue any) (any, error) {
	pipeline := leaf.GetPipeline()
	if len(pipeline) == 0 {
		return rawValue, nil
	}
	return RunPipeline(leaf, pipeline, rawValue)
}

// InitPipelineBlocks calls Init on every block in the pipeline with the owning leaf.
// Must be called after the leaf is attached to the tree so that tree-context
// accessors (GetTreeOrg, GetTreeDevice, etc.) return correct values.
func InitPipelineBlocks(leaf Leaf, pipeline []ProcessBlock) {
	for _, block := range pipeline {
		block.Init(leaf)
	}
}

// ClosePipelineBlocks calls Close on every block in the pipeline.
// Must be called when the owning leaf is removed from the tree so that blocks
// can release any resources acquired during Init.
func ClosePipelineBlocks(leaf Leaf, pipeline []ProcessBlock) {
	for _, block := range pipeline {
		block.Close(leaf)
	}
}

// RunPipeline executes a pipeline of ProcessBlocks on a value
func RunPipeline(leaf Leaf, pipeline []ProcessBlock, rawValue any) (any, error) {
	// log.Printf("RunPipeline: leaf=%s, blocks=%d", leaf.GetName(), len(pipeline))
	value := rawValue
	for _, block := range pipeline {
		// log.Printf("RunPipeline: calling block[%d].Process type=%s, value=%v", i, block.GetType(), value)
		var err error
		value, err = block.Process(leaf, value)
		if err != nil {
			return nil, fmt.Errorf("block %s: %w", block.GetType(), err)
		}
	}
	return value, nil
}
