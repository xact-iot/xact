package tree

import (
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"
)

// NATS publisher injected by main
var TagValuePublisher TagValuePublish

// Leaf is the interface for all leaf nodes in the tree
type Leaf interface {
	TreeNode
	ValueType() ScalarType
	GetAnyValue() any
	SetAnyValue(any) error
	// Type-safe getters - return ErrWrongType if called on wrong type
	GetInt() (int64, error)
	GetFloat() (float64, error)
	GetString() (string, error)
	GetBool() (bool, error)
	GetEnum() (int, error)
	// Type-safe setters - return ErrWrongType if called on wrong type
	SetInt(int64) error
	SetFloat(float64) error
	SetString(string) error
	SetBool(bool) error
	SetEnum(int) error
	// Config and shared accessors
	GetConfig() TagConfig
	GetShared() TagShared
	GetRuntime() TagRuntime
	// Runtime state. An empty string means Normal.
	GetState() string
	SetState(string)   // adds the given status flag (idempotent)
	ClearState(string) // removes the given status flag
	GetUpdatedTime() time.Time
	SetUpdatedTime(time.Time)
	// Description (template-aware)
	GetDescription() string
	// Template
	GetTemplate() Leaf
	SetTemplate(Leaf)
	// Pipeline
	GetPipeline() []ProcessBlock
	// Setters for config/shared
	SetShared(TagShared)
	// Tree context accessors
	GetTreeOrg() string
	GetTreeDevice() string
	GetMetricPath() string
	GetParent() *Node
	Publish(value any) error
}
type TagValuePublish interface {
	TagValuePublish(tagPath string, data []byte) error
}

// baseLeaf contains common fields for all leaf types
type baseLeaf struct {
	mu       sync.RWMutex // protects shared, runtime, and template
	name     string
	config   TagConfig
	shared   TagShared
	runtime  TagRuntime
	template Leaf  // resolved template ref (nil if none)
	parent   *Node // parent node in the tree; nil until AddChild is called
}

func (l *baseLeaf) setParent(p *Node) { l.parent = p }

// GetTreeOrg returns the name of the nearest NodeTypeOrganisation ancestor.
func (l *baseLeaf) GetTreeOrg() string {
	p := l.parent
	for p != nil {
		if p.nodeType == NodeTypeOrganisation {
			return p.Name
		}
		p = p.parent
	}
	return ""
}

// GetTreeDevice returns the dot-path from the org's child down to the nearest
// NodeTypeDevice ancestor (e.g. "building.floor1"), excluding the org name.
// Returns "" if no DeviceNode ancestor exists.
func (l *baseLeaf) GetTreeDevice() string {
	var device *Node
	p := l.parent
	for p != nil {
		if p.nodeType == NodeTypeDevice {
			device = p
			break
		}
		p = p.parent
	}
	if device == nil {
		return ""
	}
	var segments []string
	curr := device
	for curr != nil && curr.nodeType != NodeTypeOrganisation {
		segments = append([]string{curr.Name}, segments...)
		curr = curr.parent
	}
	return strings.Join(segments, ".")
}

func (l *baseLeaf) GetParent() *Node { return l.parent }
func (l *baseLeaf) GetName() string  { return l.name }
func (l *baseLeaf) IsNode() bool     { return false }

// GetMetricPath returns the dot-path from the DeviceNode to this leaf
// (exclusive of device, inclusive of leaf). e.g. "group.tag" or "group.batteries.0".
func (l *baseLeaf) GetMetricPath() string {
	var parts []string
	parts = append(parts, l.name)
	p := l.parent
	for p != nil && p.nodeType != NodeTypeDevice {
		if p.nodeType == NodeTypeOrganisation {
			break
		}
		parts = append([]string{p.Name}, parts...)
		p = p.parent
	}
	return strings.Join(parts, ".")
}

func (l *baseLeaf) GetConfig() TagConfig { return l.config }

func (l *baseLeaf) GetShared() TagShared {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return l.shared
}

func (l *baseLeaf) GetRuntime() TagRuntime {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return l.runtime
}

func (l *baseLeaf) GetState() string {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return l.runtime.Status
}

// SetState adds s to the status set (idempotent).
func (l *baseLeaf) SetState(s string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if !strings.Contains(l.runtime.Status, s) {
		l.runtime.Status += s
	}
}

// ClearState removes s from the status set.
func (l *baseLeaf) ClearState(s string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.runtime.Status = strings.ReplaceAll(l.runtime.Status, s, "")
}

func (l *baseLeaf) GetUpdatedTime() time.Time {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return l.runtime.UpdatedTime
}

func (l *baseLeaf) SetUpdatedTime(t time.Time) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.runtime.UpdatedTime = t
}

func (l *baseLeaf) GetTemplate() Leaf {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return l.template
}

func (l *baseLeaf) SetTemplate(t Leaf) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.template = t
}

func (l *baseLeaf) SetShared(s TagShared) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.shared = s
}

// GetDescription returns the description, falling through to template if empty
func (l *baseLeaf) GetDescription() string {
	l.mu.RLock()
	desc := l.shared.Description
	tmpl := l.template
	l.mu.RUnlock()

	if desc != "" {
		return desc
	}
	if tmpl != nil {
		return tmpl.GetDescription()
	}
	return ""
}

// GetPipeline returns the pipeline, falling through to template if empty
func (l *baseLeaf) GetPipeline() []ProcessBlock {
	l.mu.RLock()
	pipeline := l.shared.Pipeline
	tmpl := l.template
	l.mu.RUnlock()

	if len(pipeline) > 0 {
		return pipeline
	}
	if tmpl != nil {
		return tmpl.GetPipeline()
	}
	return nil
}

// newBaseLeaf initialises the non-mutex fields of a baseLeaf.
// The caller must pass a pointer to an already-allocated baseLeaf so that the
// embedded sync.RWMutex is never copied.
func newBaseLeaf(bl *baseLeaf, name string, config TagConfig, shared ...TagShared) {
	bl.name = name
	bl.config = config
	if len(shared) > 0 {
		bl.shared = shared[0]
	}
}

// Value is the sealed interface for leaf values.
type Value interface{ isValue() }

// IntValue holds a 64-bit integer leaf value.
type IntValue int64

// FloatValue holds a 64-bit float leaf value.
type FloatValue float64

// StringValue holds a string leaf value.
type StringValue string

// BoolValue holds a boolean leaf value.
type BoolValue bool

// EnumValue holds an integer enum value with an optional set of allowed values.
type EnumValue struct {
	val           int
	allowedValues map[int]string
}

func (IntValue) isValue()    {}
func (FloatValue) isValue()  {}
func (StringValue) isValue() {}
func (BoolValue) isValue()   {}
func (EnumValue) isValue()   {}

// LeafNode is the single concrete implementation of the Leaf interface.
// The value field is a sealed Value, discriminated by type-switch.
type LeafNode struct {
	baseLeaf
	value Value
}

// ValueType returns the scalar type of this leaf.
func (l *LeafNode) ValueType() ScalarType {
	l.mu.RLock()
	defer l.mu.RUnlock()
	switch l.value.(type) {
	case IntValue:
		return TypeInteger
	case FloatValue:
		return TypeFloat
	case StringValue:
		return TypeString
	case BoolValue:
		return TypeBoolean
	case EnumValue:
		return TypeEnum
	default:
		return 0
	}
}

// GetAnyValue returns the current value as its native Go type.
func (l *LeafNode) GetAnyValue() any {
	l.mu.RLock()
	defer l.mu.RUnlock()
	switch v := l.value.(type) {
	case IntValue:
		return int64(v)
	case FloatValue:
		return float64(v)
	case StringValue:
		return string(v)
	case BoolValue:
		return bool(v)
	case EnumValue:
		return v.val
	default:
		return nil
	}
}

func (l *LeafNode) GetInt() (int64, error) {
	l.mu.RLock()
	defer l.mu.RUnlock()
	if v, ok := l.value.(IntValue); ok {
		return int64(v), nil
	}
	return 0, ErrWrongType
}

func (l *LeafNode) GetFloat() (float64, error) {
	l.mu.RLock()
	defer l.mu.RUnlock()
	if v, ok := l.value.(FloatValue); ok {
		return float64(v), nil
	}
	return 0, ErrWrongType
}

func (l *LeafNode) GetString() (string, error) {
	l.mu.RLock()
	defer l.mu.RUnlock()
	if v, ok := l.value.(StringValue); ok {
		return string(v), nil
	}
	return "", ErrWrongType
}

func (l *LeafNode) GetBool() (bool, error) {
	l.mu.RLock()
	defer l.mu.RUnlock()
	if v, ok := l.value.(BoolValue); ok {
		return bool(v), nil
	}
	return false, ErrWrongType
}

func (l *LeafNode) GetEnum() (int, error) {
	l.mu.RLock()
	defer l.mu.RUnlock()
	if v, ok := l.value.(EnumValue); ok {
		return v.val, nil
	}
	return 0, ErrWrongType
}

func (l *LeafNode) SetInt(v int64) error {
	l.mu.RLock()
	_, ok := l.value.(IntValue)
	l.mu.RUnlock()
	if !ok {
		return ErrWrongType
	}
	return l.SetAnyValue(v)
}

func (l *LeafNode) SetFloat(v float64) error {
	l.mu.RLock()
	_, ok := l.value.(FloatValue)
	l.mu.RUnlock()
	if !ok {
		return ErrWrongType
	}
	return l.SetAnyValue(v)
}

func (l *LeafNode) SetString(v string) error {
	l.mu.RLock()
	_, ok := l.value.(StringValue)
	l.mu.RUnlock()
	if !ok {
		return ErrWrongType
	}
	return l.SetAnyValue(v)
}

func (l *LeafNode) SetBool(v bool) error {
	l.mu.RLock()
	_, ok := l.value.(BoolValue)
	l.mu.RUnlock()
	if !ok {
		return ErrWrongType
	}
	return l.SetAnyValue(v)
}

// setEnumLocked validates and stores an enum value; must be called with l.mu held for writing.
func (l *LeafNode) setEnumLocked(v int) error {
	ev, ok := l.value.(EnumValue)
	if !ok {
		return ErrWrongType
	}
	if ev.allowedValues != nil {
		if _, ok := ev.allowedValues[v]; !ok {
			return fmt.Errorf("enum value %d is not in allowed values", v)
		}
	}
	ev.val = v
	l.value = ev
	return nil
}

// SetEnum validates the value against allowed values and stores it.
func (l *LeafNode) SetEnum(v int) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.setEnumLocked(v)
}

// toNative coerces v to the native Go type expected by the current Value kind.
// JSON numbers arrive as float64; MQTT payloads may carry various numeric types.
func toNative(current Value, v any) (any, error) {
	switch current.(type) {
	case IntValue:
		switch n := v.(type) {
		case int64:
			return n, nil
		case int:
			return int64(n), nil
		case float64:
			return int64(n), nil
		}
		return nil, fmt.Errorf("cannot coerce %T to int64", v)
	case FloatValue:
		switch n := v.(type) {
		case float64:
			return n, nil
		case float32:
			return float64(n), nil
		case int:
			return float64(n), nil
		case int64:
			return float64(n), nil
		}
		return nil, fmt.Errorf("cannot coerce %T to float64", v)
	case StringValue:
		if s, ok := v.(string); ok {
			return s, nil
		}
		return nil, fmt.Errorf("cannot coerce %T to string", v)
	case BoolValue:
		if b, ok := v.(bool); ok {
			return b, nil
		}
		return nil, fmt.Errorf("cannot coerce %T to bool", v)
	case EnumValue:
		switch n := v.(type) {
		case int:
			return n, nil
		case int64:
			return int(n), nil
		case float64:
			return int(n), nil
		}
		return nil, fmt.Errorf("cannot coerce %T to int (enum)", v)
	}
	return nil, fmt.Errorf("unknown Value type %T", current)
}

// storeNative wraps a processed native value back into the appropriate Value type.
func (l *LeafNode) storeNative(v any) error {
	switch l.value.(type) {
	case IntValue:
		switch n := v.(type) {
		case int64:
			l.value = IntValue(n)
			return nil
		case int:
			l.value = IntValue(n)
			return nil
		case float64:
			l.value = IntValue(n)
			return nil
		}
		return fmt.Errorf("cannot store %T as IntValue", v)
	case FloatValue:
		switch n := v.(type) {
		case float64:
			l.value = FloatValue(n)
			return nil
		case float32:
			l.value = FloatValue(n)
			return nil
		case int:
			l.value = FloatValue(n)
			return nil
		case int64:
			l.value = FloatValue(n)
			return nil
		}
		return fmt.Errorf("cannot store %T as FloatValue", v)
	case StringValue:
		if s, ok := v.(string); ok {
			l.value = StringValue(s)
			return nil
		}
		return fmt.Errorf("cannot store %T as StringValue", v)
	case BoolValue:
		if b, ok := v.(bool); ok {
			l.value = BoolValue(b)
			return nil
		}
		return fmt.Errorf("cannot store %T as BoolValue", v)
	case EnumValue:
		switch n := v.(type) {
		case int:
			return l.setEnumLocked(n)
		case int64:
			return l.setEnumLocked(int(n))
		case float64:
			return l.setEnumLocked(int(n))
		}
		return fmt.Errorf("cannot store %T as EnumValue", v)
	}
	return fmt.Errorf("unknown Value type %T", l.value)
}

// SetAnyValue coerces v to the leaf's native type, runs the pipeline, then stores the result.
func (l *LeafNode) SetAnyValue(v any) error {
	// Snapshot current value type under read lock so toNative can coerce correctly.
	l.mu.RLock()
	currentValue := l.value
	l.mu.RUnlock()
	l.SetUpdatedTime(time.Now())

	native, err := toNative(currentValue, v)
	if err != nil {
		fmt.Printf("SetAnyValue:532 %+v\n", err)
		return err
	}
	// ProcessValue calls back into the leaf (GetPipeline, GetFloat, etc.) which
	// re-acquire the read lock individually - no lock is held here to avoid deadlock.
	processed, err := ProcessValue(l, native)
	if err != nil {
		return err
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	err = l.storeNative(processed)
	return err
}

// AddEnum adds an allowed enum value with its display string.
// Panics silently (no-op) if called on a non-enum leaf.
func (l *LeafNode) AddEnum(value int, display string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	ev, ok := l.value.(EnumValue)
	if !ok {
		return
	}
	if ev.allowedValues == nil {
		ev.allowedValues = make(map[int]string)
	}
	ev.allowedValues[value] = display
	l.value = ev
}

// SetEnumValues replaces the allowed enum values and updates shared metadata.
func (l *LeafNode) SetEnumValues(values map[int]string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	ev, ok := l.value.(EnumValue)
	if !ok {
		return
	}
	ev.allowedValues = values
	l.value = ev
	l.shared.EnumValues = values
}

// GetEnumDisplay returns the display string for the current enum value.
func (l *LeafNode) GetEnumDisplay() (string, error) {
	l.mu.RLock()
	defer l.mu.RUnlock()
	ev, ok := l.value.(EnumValue)
	if !ok {
		return "", ErrWrongType
	}
	if display, ok := ev.allowedValues[ev.val]; ok {
		return display, nil
	}
	return "", fmt.Errorf("no display string for enum value %d", ev.val)
}

// GetAllowedValues returns the map of allowed enum values, or nil for non-enum leaves.
func (l *LeafNode) GetAllowedValues() map[int]string {
	l.mu.RLock()
	defer l.mu.RUnlock()
	ev, ok := l.value.(EnumValue)
	if !ok {
		return nil
	}
	return ev.allowedValues
}

// NewIntegerLeaf creates a new integer leaf.
func NewIntegerLeaf(name string, config TagConfig, shared ...TagShared) *LeafNode {
	l := &LeafNode{value: IntValue(0)}
	newBaseLeaf(&l.baseLeaf, name, config, shared...)
	return l
}

// NewFloatLeaf creates a new float leaf.
func NewFloatLeaf(name string, config TagConfig, shared ...TagShared) *LeafNode {
	l := &LeafNode{value: FloatValue(0)}
	newBaseLeaf(&l.baseLeaf, name, config, shared...)
	return l
}

// NewStringLeaf creates a new string leaf.
func NewStringLeaf(name string, config TagConfig, shared ...TagShared) *LeafNode {
	l := &LeafNode{value: StringValue("")}
	newBaseLeaf(&l.baseLeaf, name, config, shared...)
	return l
}

// NewBoolLeaf creates a new boolean leaf.
func NewBoolLeaf(name string, config TagConfig, shared ...TagShared) *LeafNode {
	l := &LeafNode{value: BoolValue(false)}
	newBaseLeaf(&l.baseLeaf, name, config, shared...)
	return l
}

// NewEnumLeaf creates a new enum leaf.
func NewEnumLeaf(name string, config TagConfig, shared ...TagShared) *LeafNode {
	var allowedValues map[int]string
	if len(shared) > 0 {
		allowedValues = shared[0].EnumValues
	}
	l := &LeafNode{value: EnumValue{allowedValues: allowedValues}}
	newBaseLeaf(&l.baseLeaf, name, config, shared...)
	return l
}

// NewLeaf creates a new leaf of the given scalar type.
func NewLeaf(t ScalarType, name string, config TagConfig, shared ...TagShared) *LeafNode {
	switch t {
	case TypeInteger:
		return NewIntegerLeaf(name, config, shared...)
	case TypeFloat:
		return NewFloatLeaf(name, config, shared...)
	case TypeString:
		return NewStringLeaf(name, config, shared...)
	case TypeBoolean:
		return NewBoolLeaf(name, config, shared...)
	case TypeEnum:
		return NewEnumLeaf(name, config, shared...)
	default:
		return nil
	}
}

// Publish. Broadcast leaf's status for browser and other updates
type TagValue struct {
	Type      string `json:"type"`
	Value     any    `json:"value"`
	Status    string `json:"status"`
	Timestamp int64  `json:"timestamp"`
}

func (l *LeafNode) Publish(value any) error {
	status := l.GetState()
	updatedTime := l.GetUpdatedTime()
	if l.didChange(value, status) {

		l.runtime.lastPubValue = value
		l.runtime.lastPubStatus = status
		l.runtime.lastPubUpdateTime = updatedTime

		metricPath := l.GetMetricPath()
		msg := make(map[string]TagValue, 1)
		msg[metricPath] = TagValue{
			Type:      "value",
			Value:     value,
			Status:    status,
			Timestamp: updatedTime.UnixMilli(),
		}

		data, err := json.Marshal(msg)
		if err != nil {
			return fmt.Errorf("failed to marshal value: %w", err)
		}
		return TagValuePublisher.TagValuePublish(tagPath(l), data)
		// bcast, err := nats.GetBroadcastStream(nats.TagValueStream)
		// if err != nil {
		// 	return fmt.Errorf("publish failed: %w", err)
		// }
		// _, err = bcast.Publish(tagPath(l), data, 0)
		// if err != nil {
		// 	return fmt.Errorf("publish failed: %w", err)
		// }
	}
	return nil
}
func tagPath(leaf Leaf) string {
	parts := []string{"tagvalue", leaf.GetTreeOrg()}
	if dev := leaf.GetTreeDevice(); dev != "" {
		parts = append(parts, dev)
	}
	parts = append(parts, leaf.GetMetricPath())
	return strings.Join(parts, ".")
}

func (l *LeafNode) didChange(newValue any, status string) bool {
	if l.runtime.lastPubValue == nil {
		return true
	}
	if l.runtime.lastPubStatus != status {
		return true
	}
	deadband := l.GetShared().Deadband
	switch l.ValueType() {
	case TypeInteger:
		intValue, ok := newValue.(int64)
		if !ok {
			return false
		}
		old, _ := l.runtime.lastPubValue.(int64)
		if Abs(float64(old)-float64(intValue)) > deadband {
			return true
		}
	case TypeFloat:
		fltValue, ok := newValue.(float64)
		if !ok {
			return false
		}
		old, _ := l.runtime.lastPubValue.(float64)
		if Abs(old-fltValue) > deadband {
			return true
		}
	case TypeString:
		strValue, ok := newValue.(string)
		if !ok {
			return false
		}
		old, _ := l.runtime.lastPubValue.(string)
		return strValue != old

	case TypeBoolean:
		boolValue, ok := newValue.(bool)
		if !ok {
			return false
		}
		old, _ := l.runtime.lastPubValue.(bool)
		return boolValue != old

	case TypeEnum:
		enumValue, ok := newValue.(int)
		if !ok {
			return false
		}
		old, _ := l.runtime.lastPubValue.(int)
		return enumValue != old
	}
	return false
}
func Abs[T ~int | ~int32 | ~int64 | ~float32 | ~float64](v T) T {
	if v < 0 {
		return -v
	}
	return v
}
