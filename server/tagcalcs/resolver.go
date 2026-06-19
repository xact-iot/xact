package tagcalcs

import (
	"math"
	"path"
	"sort"
	"strconv"
	"strings"

	"github.com/xact-iot/xact/rtdb/tree"
)

// ListEntry is one row returned by listHighest/listLowest and written to an
// object-array output tag.
type ListEntry struct {
	DeviceName       string  `json:"deviceName"`
	DeviceDescriptor string  `json:"deviceDescriptor"`
	TagName          string  `json:"tagName"`
	TagValue         float64 `json:"tagValue"`
}

// runtimeEnv implements the expression environment at evaluation time.
// It holds the tree reference and org name so Tag() and aggregation
// functions can resolve live values.
type runtimeEnv struct {
	treeOps *tree.TreeWithOperations
	org     string
}

// dotToSlash converts an org-relative dot-notation path to a tree slash-path.
// "SYSTEM.LoopTime.Current" → "/org/SYSTEM/LoopTime/Current"
func (e *runtimeEnv) dotToSlash(dotPath string) string {
	return "/" + e.org + "/" + strings.ReplaceAll(dotPath, ".", "/")
}

// Tag returns the numeric value of a single tag by org-relative dot-path.
// Returns 0 if the tag does not exist or is not numeric.
func (e *runtimeEnv) Tag(dotPath string) float64 {
	leaf, err := e.treeOps.FindLeaf(e.dotToSlash(dotPath))
	if err != nil {
		return 0
	}
	return toFloat(leaf)
}

// matchingLeaves returns all numeric leaves whose org-relative dot-path
// matches the given glob pattern (e.g. "SYSTEM.LoopTime.*").
func (e *runtimeEnv) matchingLeaves(pattern string) []float64 {
	dotPattern := "." + e.org + "." + pattern

	var vals []float64
	e.treeOps.WalkLeaves(func(p string, leaf tree.Leaf) {
		if !matchDotPattern(dotPattern, p) {
			return
		}
		vals = append(vals, toFloat(leaf))
	})
	return vals
}

func (e *runtimeEnv) matchingLeafValues(pattern string) []any {
	dotPattern := "." + e.org + "." + pattern

	var vals []any
	e.treeOps.WalkLeaves(func(p string, leaf tree.Leaf) {
		if !matchDotPattern(dotPattern, p) {
			return
		}
		vals = append(vals, leafValue(leaf))
	})
	return vals
}

func (e *runtimeEnv) matchingListEntries(pattern string) []ListEntry {
	dotPattern := "." + e.org + "." + pattern

	var entries []ListEntry
	e.treeOps.WalkLeaves(func(p string, leaf tree.Leaf) {
		if !matchDotPattern(dotPattern, p) {
			return
		}
		deviceName, deviceDescriptor := deviceInfo(leaf)
		entries = append(entries, ListEntry{
			DeviceName:       deviceName,
			DeviceDescriptor: deviceDescriptor,
			TagName:          leaf.GetMetricPath(),
			TagValue:         toFloat(leaf),
		})
	})
	return entries
}

func deviceInfo(leaf tree.Leaf) (string, string) {
	devicePath := leaf.GetTreeDevice()
	deviceName := devicePath
	if devicePath != "" {
		parts := strings.Split(devicePath, ".")
		deviceName = parts[len(parts)-1]
	}

	for p := leaf.GetParent(); p != nil; p = p.GetParent() {
		if p.GetNodeType() == tree.NodeTypeDevice {
			if deviceName == "" {
				deviceName = p.GetName()
			}
			return deviceName, p.GetDescription()
		}
	}

	if deviceName == "" {
		deviceName = leaf.GetName()
	}
	return deviceName, ""
}

func matchDotPattern(pattern, value string) bool {
	patternParts := strings.Split(pattern, ".")
	valueParts := strings.Split(value, ".")
	if len(patternParts) != len(valueParts) {
		return false
	}
	for i := range patternParts {
		matched, err := path.Match(patternParts[i], valueParts[i])
		if err != nil || !matched {
			return false
		}
	}
	return true
}

func (e *runtimeEnv) Avg(pattern string) float64 {
	vals := e.matchingLeaves(pattern)
	if len(vals) == 0 {
		return 0
	}
	sum := 0.0
	for _, v := range vals {
		sum += v
	}
	return sum / float64(len(vals))
}

func (e *runtimeEnv) Sum(pattern string) float64 {
	vals := e.matchingLeaves(pattern)
	sum := 0.0
	for _, v := range vals {
		sum += v
	}
	return sum
}

func (e *runtimeEnv) AggMin(pattern string) float64 {
	vals := e.matchingLeaves(pattern)
	if len(vals) == 0 {
		return 0
	}
	m := vals[0]
	for _, v := range vals[1:] {
		if v < m {
			m = v
		}
	}
	return m
}

func (e *runtimeEnv) AggMax(pattern string) float64 {
	vals := e.matchingLeaves(pattern)
	if len(vals) == 0 {
		return 0
	}
	m := vals[0]
	for _, v := range vals[1:] {
		if v > m {
			m = v
		}
	}
	return m
}

// Binary min/max - return the larger or smaller of two numeric values.
// These handle expressions like max(1, count(...)) and min(0, avg(...)).
func (e *runtimeEnv) Max(a, b float64) float64 {
	if a > b {
		return a
	}
	return b
}

func (e *runtimeEnv) Min(a, b float64) float64 {
	if a < b {
		return a
	}
	return b
}

func (e *runtimeEnv) Count(pattern string) float64 {
	return float64(len(e.matchingLeaves(pattern)))
}

// CountWhere counts leaves matching pattern whose value equals the given value.
func (e *runtimeEnv) CountWhere(pattern string, value any) float64 {
	n := 0
	for _, v := range e.matchingLeafValues(pattern) {
		if valuesEqual(v, value) {
			n++
		}
	}
	return float64(n)
}

func (e *runtimeEnv) ListHighest(pattern string, count int) []ListEntry {
	return e.listByValue(pattern, count, true)
}

func (e *runtimeEnv) ListLowest(pattern string, count int) []ListEntry {
	return e.listByValue(pattern, count, false)
}

func (e *runtimeEnv) listByValue(pattern string, count int, highest bool) []ListEntry {
	if count <= 0 {
		return []ListEntry{}
	}
	entries := e.matchingListEntries(pattern)
	sort.SliceStable(entries, func(i, j int) bool {
		if entries[i].TagValue == entries[j].TagValue {
			if entries[i].DeviceName == entries[j].DeviceName {
				return entries[i].TagName < entries[j].TagName
			}
			return entries[i].DeviceName < entries[j].DeviceName
		}
		if highest {
			return entries[i].TagValue > entries[j].TagValue
		}
		return entries[i].TagValue < entries[j].TagValue
	})
	if len(entries) > count {
		entries = entries[:count]
	}
	return entries
}

// Math functions
func (e *runtimeEnv) Abs(v float64) float64 { return math.Abs(v) }
func (e *runtimeEnv) Round(v float64, dec int) float64 {
	return math.Round(v*math.Pow(10, float64(dec))) / math.Pow(10, float64(dec))
}
func (e *runtimeEnv) Sqrt(v float64) float64        { return math.Sqrt(v) }
func (e *runtimeEnv) Pow(base, exp float64) float64 { return math.Pow(base, exp) }
func (e *runtimeEnv) Floor(v float64) float64       { return math.Floor(v) }
func (e *runtimeEnv) Ceil(v float64) float64        { return math.Ceil(v) }
func (e *runtimeEnv) Log(v float64) float64         { return math.Log(v) }
func (e *runtimeEnv) Log10(v float64) float64       { return math.Log10(v) }
func (e *runtimeEnv) Sin(v float64) float64         { return math.Sin(v) }
func (e *runtimeEnv) Cos(v float64) float64         { return math.Cos(v) }
func (e *runtimeEnv) Tan(v float64) float64         { return math.Tan(v) }

// Conditional: if(cond, trueVal, falseVal)
func (e *runtimeEnv) If(cond bool, a, b float64) float64 {
	if cond {
		return a
	}
	return b
}

func (e *runtimeEnv) IfStr(cond bool, a, b string) string {
	if cond {
		return a
	}
	return b
}

// toFloat extracts a float64 from any numeric leaf value.
func toFloat(leaf tree.Leaf) float64 {
	switch leaf.ValueType() {
	case tree.TypeFloat:
		v, _ := leaf.GetFloat()
		return v
	case tree.TypeInteger:
		v, _ := leaf.GetInt()
		return float64(v)
	case tree.TypeBoolean:
		v, _ := leaf.GetBool()
		if v {
			return 1
		}
		return 0
	}
	return 0
}

func leafValue(leaf tree.Leaf) any {
	switch leaf.ValueType() {
	case tree.TypeFloat:
		v, _ := leaf.GetFloat()
		return v
	case tree.TypeInteger:
		v, _ := leaf.GetInt()
		return float64(v)
	case tree.TypeBoolean:
		v, _ := leaf.GetBool()
		return v
	case tree.TypeString:
		v, _ := leaf.GetString()
		return v
	case tree.TypeEnum:
		v, _ := leaf.GetEnum()
		return float64(v)
	}
	return nil
}

func valuesEqual(a, b any) bool {
	if as, ok := a.(string); ok {
		bs, ok := b.(string)
		return ok && as == bs
	}
	if ab, ok := a.(bool); ok {
		switch bv := b.(type) {
		case bool:
			return ab == bv
		case int:
			return boolAsFloat(ab) == float64(bv)
		case int64:
			return boolAsFloat(ab) == float64(bv)
		case float64:
			return boolAsFloat(ab) == bv
		}
	}
	af, aok := asFloat(a)
	bf, bok := asFloat(b)
	return aok && bok && af == bf
}

func boolAsFloat(v bool) float64 {
	if v {
		return 1
	}
	return 0
}

func asFloat(v any) (float64, bool) {
	switch t := v.(type) {
	case float64:
		return t, true
	case float32:
		return float64(t), true
	case int:
		return float64(t), true
	case int64:
		return float64(t), true
	case string:
		n, err := strconv.ParseFloat(t, 64)
		return n, err == nil
	}
	return 0, false
}
