# RTDB Tree Package - Public API Documentation

## Overview
The `tree` package provides a hierarchical tree data structure for the real-time database (RTDB), supporting both internal nodes and typed leaf nodes (tags).

## Types

### ScalarType
```go
type ScalarType int

const (
    TypeInteger ScalarType = iota
    TypeFloat
    TypeString
    TypeBoolean
    TypeEnum
)
```

### Node Types
- **`TreeNode`** - Interface for both Node and Leaf
  - `GetName() string`
  - `IsNode() bool`

- **`Node`** - Internal tree node
  - `Name string`
  - `Description string`
  - `TemplateName string`
  - `AddChild(child TreeNode) error`
  - `RemoveChild(name string) error`
  - `GetChild(name string) (TreeNode, bool)`
  - `GetChildren() map[string]TreeNode`
  - `MarkDeleted()`
  - `IsDeleted() bool`

- **`Leaf`** - Interface for typed value nodes
  - Value accessors: `GetAnyValue() any`, `SetAnyValue(any) error`
  - Type-safe getters: `GetInt()`, `GetFloat()`, `GetString()`, `GetBool()`, `GetEnum()`
  - Type-safe setters: `SetInt(int64)`, `SetFloat(float64)`, `SetString(string)`, `SetBool(bool)`, `SetEnum(int)`
  - `ValueType() ScalarType`
  - `GetConfig() TagConfig`, `GetShared() TagShared`, `GetRuntime() TagRuntime`
  - `GetState() TagState`, `SetState(TagState)`
  - `GetUpdatedTime() time.Time`, `SetUpdatedTime(time.Time)`
  - `GetDescription() string`
  - `GetPipeline() []ProcessBlock`
  - `RunPipeline(rawValue any) (any, error)`
  - `SetShared(TagShared)`

### Leaf Types
- **`IntegerLeaf`** - 64-bit integer values
  - `NewIntegerLeaf(name string, config TagConfig, shared ...TagShared) *IntegerLeaf`

- **`FloatLeaf`** - 64-bit floating point values
  - `NewFloatLeaf(name string, config TagConfig, shared ...TagShared) *FloatLeaf`

- **`StringLeaf`** - String values
  - `NewStringLeaf(name string, config TagConfig, shared ...TagShared) *StringLeaf`

- **`BoolLeaf`** - Boolean values
  - `NewBoolLeaf(name string, config TagConfig, shared ...TagShared) *BoolLeaf`

- **`EnumLeaf`** - Integer enum values with display strings
  - `NewEnumLeaf(name string, config TagConfig, shared ...TagShared) *EnumLeaf`
  - `AddEnum(value int, display string)`
  - `GetEnumDisplay() (string, error)`
  - `GetAllowedValues() map[int]string`

### Metadata Types
- **`TagConfig`** - Persisted tag configuration
  - `Type ScalarType`
  - `Name string`
  - `TemplateName string`
  - `EnumValues map[int]string`

- **`TagShared`** - Template-inheritable data
  - `Description string`
  - `Pipeline []ProcessBlock`

- **`TagRuntime`** - Non-persisted runtime state
  - `State TagState`
  - `UpdatedTime time.Time`
  - `ProcessBlocksData []any`

- **`TagState`** - Runtime state enum
  - `StateUndefined`, `StateNormal`, `StateStale`, `StateInAlarm`

### Tree Types
- **`Tree`** - Root of the hierarchical data structure
  - `Root *Node`
  - `FindNode(path string) (*Node, error)` - Returns node at path
  - `FindLeaf(path string) (Leaf, error)` - Returns leaf at path
  - `FindNodeOrLeaf(path string) (TreeNode, error)` - Returns any TreeNode
  - `Lock()`, `Unlock()` - Write lock for structure changes
  - `RLock()`, `RUnlock()` - Read lock for traversals

- **`TreeWithOperations`** - Tree with create/delete operations
  - `CreateNode(path string, templateRef string) error` - Create node
  - `DeleteNode(path string) error` - Cascade delete node
  - `CreateTag(path string, scalarType ScalarType, config TagConfig, shared ...TagShared) error` - Create tag
  - `DeleteTag(path string) error` - Delete tag/leaf
  - `SetOnChange(fn func(path string, node TreeNode))` - Set change callback

### ProcessBlock Types
- **`ProcessBlock`** - Interface for processing pipeline steps
  - `GetType() string`
  - `Init(leaf Leaf)`
  - `Process(leaf Leaf, value any) (any, error)`
  - `GetParameters() json.RawMessage`
  - `SetParameters(json.RawMessage) error`

- **`ProcessBlockEnvelope`** - JSON serialization wrapper
  - `Type string`
  - `Parameters json.RawMessage`

## Functions

### Tree Operations
- **`NewTree() *Tree`** - Create new empty tree
- **`NewTreeWithOperations(onChange func(path string, node TreeNode)) *TreeWithOperations`** - Create tree with operations
- **`ResolvePath(path string) []string`** - Split path into components
- **`NewNode(name string) *Node`** - Create new internal node

### ProcessBlock Operations
- **`RegisterProcessBlock(name string, factory ProcessBlockFactory)`** - Register block factory
- **`NewProcessBlockByType(name string) (ProcessBlock, error)`** - Create block by type
- **`MarshalPipeline(pipeline []ProcessBlock) ([]ProcessBlockEnvelope, error)`** - Serialize pipeline
- **`UnmarshalPipeline(envelopes []ProcessBlockEnvelope) ([]ProcessBlock, error)`** - Deserialize pipeline
- **`RunPipeline(leaf Leaf, pipeline []ProcessBlock, rawValue any) (any, error)`** - Execute pipeline

## Errors
```go
var (
    ErrNotFound         = errors.New("not found")
    ErrLeafAtPath       = errors.New("leaf found at path")
    ErrCannotDeleteRoot = errors.New("cannot delete root")
    ErrIsLeaf           = errors.New("path is a leaf, not a node")
    ErrIsNode           = errors.New("path is a node, not a leaf")
    ErrWrongType        = errors.New("wrong value type for leaf")
    ErrNodeDeleted      = errors.New("node has been deleted")
)
```

## Usage Example
```go
// Create tree with change callback
tree := tree.NewTreeWithOperations(func(path string, node tree.TreeNode) {
    log.Printf("Changed: %s", path)
})

// Create nodes
tree.CreateNode("/building/floor1", "")
tree.CreateNode("/devices/device1", "")

// Create tags
tree.CreateTag("/building/floor1/temperature", tree.TypeFloat, tree.TagConfig{
    Name: "Temperature",
}, tree.TagShared{
    Description: "Floor 1 temperature",
})

// Set values
if leaf, err := tree.FindLeaf("/building/floor1/temperature"); err == nil {
    if floatLeaf, ok := leaf.(*tree.FloatLeaf); ok {
        floatLeaf.SetFloat(22.5)
    }
}

// Traverse
node, err := tree.FindNode("/building/floor1")
leaf, err := tree.FindLeaf("/building/floor1/temperature")
```
