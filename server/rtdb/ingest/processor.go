// Package ingest provides shared logic for writing device data into the RTDB.
// It is used by both the MQTT client and the REST ingest API.
package ingest

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strconv"
	"strings"
	"time"

	"github.com/xact-iot/xact/rtdb/blocks"
	"github.com/xact-iot/xact/rtdb/tree"
)

// limitsConfig holds all limit check configuration from a payload object tag's
// "limits" field. Used to provision or update a limitcheck block via ingest.
type limitsConfig struct {
	Hi       float64
	Lo       float64
	HiEvent  limitEventConfig
	RtnEvent limitEventConfig
	LowEvent limitEventConfig
}

// limitEventConfig mirrors the event fields configurable through ingest.
// NotificationID is stored as any so it can be either a string name
// ("none", "sysadmin", "manager", "tech") or a numeric ID; it is resolved
// to an integer ID during pipeline provisioning.
type limitEventConfig struct {
	Enabled        bool   `json:"enabled"`
	Severity       string `json:"severity,omitempty"`
	Message        string `json:"message,omitempty"`
	NotificationID any    `json:"notificationId"`
}

// scalingConfig holds linear scaling parameters from a payload object tag's
// "scaling" field: output = (input * scale) + offset, optionally clamped.
type scalingConfig struct {
	Scale  float64
	Offset float64
	Min    *float64
	Max    *float64
}

// ── Types ─────────────────────────────────────────────────────────────────────

// TagData holds a parsed device payload, distinguishing between grouped tags
// (nested under a group key) and direct scalar tags (no group level).
type TagData struct {
	// Groups holds tags nested under a named group: group → tagName → value.
	Groups map[string]map[string]any
	// DirectTags holds scalar tags placed directly under the device node.
	DirectTags map[string]any
	// Timestamp applies to all tags in this payload.
	TSUnixMilli int64
	// TemplateName optionally assigns a template to the concrete device node while
	// this payload is processed. The name is relative to the tenant, for example
	// "Templates.AirQualityStandard".
	TemplateName string
	// DeviceDescription optionally updates the concrete device node description.
	DeviceDescription string
}

// expandedTagValue holds the unwrapped scalar and optional provisioning metadata
// from an expanded tag object:
// {"value": x, "description": "...", "units": "...", "history": d, "limits": {...}}.
type expandedTagValue struct {
	value        any
	hasValue     bool // true when the "value" key was present
	isEnum       bool
	enumValues   map[int]string
	description  string
	units        string
	deadband     *float64
	history      bool           // use historyrecorder block
	limits       *limitsConfig  // limitcheck block config; nil = not specified
	persist      bool           // true when "persist" key is present and truthy
	scaling      *scalingConfig // scaling block config; nil = not specified
	staleTimeout *float64       // stalecheck timeout in seconds; nil = not specified
	publish      bool           // true when "publish" key is present and truthy
}

// ── Parsing ───────────────────────────────────────────────────────────────────

// ParsePayload parses a JSON device payload into TagData.
//
// Each top-level key is treated as:
//   - a scalar (number, string, bool) → DirectTag at {device}/{key}
//   - an object                       → Group whose fields become tags at {device}/{key}/{field}
//
// The optional "ts" field at any level is treated as a Unix millisecond
// timestamp rather than a tag value.
func ParsePayload(data []byte) (TagData, error) {
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		return TagData{}, fmt.Errorf("invalid JSON payload: %w", err)
	}
	result := TagData{
		Groups:     make(map[string]map[string]any),
		DirectTags: make(map[string]any),
	}

	for key, val := range raw {
		if key == "templateName" {
			templateName, ok := val.(string)
			if !ok {
				return TagData{}, fmt.Errorf("templateName must be a string")
			}
			result.TemplateName = strings.TrimSpace(templateName)
			continue
		}
		if key == "description" {
			description, ok := deviceDescriptionFromRaw(val)
			if !ok {
				return TagData{}, fmt.Errorf("description must be a string or an expanded tag object with a string value")
			}
			result.DeviceDescription = description
			continue
		}
		switch v := val.(type) {
		case map[string]any:
			// Object → group. Extract its timestamp if present.
			if tsVal, ok := v["ts"]; ok {
				if ts, ok := parseTS(tsVal); ok && result.TSUnixMilli == 0 {
					result.TSUnixMilli = ts
				}
				delete(v, "ts")
			}
			result.Groups[key] = v
		default:
			// Scalar. Top-level "ts" sets the payload timestamp.
			if key == "ts" {
				if ts, ok := parseTS(val); ok {
					result.TSUnixMilli = ts
				}
			} else {
				result.DirectTags[key] = val
			}
		}
	}

	// Fall back to current time if no ts was found.
	if result.TSUnixMilli == 0 {
		result.TSUnixMilli = time.Now().UnixMilli()
	}

	return result, nil
}

func parseTS(v any) (int64, bool) {
	switch ts := v.(type) {
	case string:
		if ms, err := strconv.ParseInt(ts, 10, 64); err == nil {
			return ms, true
		}
	case float64:
		return int64(ts), true
	case int64:
		return ts, true
	case int:
		return int64(ts), true
	}
	return 0, false
}

// DetermineType infers the RTDB scalar type from a Go value.
func DetermineType(value any) tree.ScalarType {
	switch value.(type) {
	case bool:
		return tree.TypeBoolean
	case string:
		return tree.TypeString
	case int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64:
		return tree.TypeInteger
	case float32, float64:
		// if f, ok := v.(float64); ok && f == float64(int64(f)) {
		// 	return tree.TypeInteger
		// }
		return tree.TypeFloat
	default:
		return tree.TypeFloat
	}
}

func extractTagValue(raw any) expandedTagValue {
	m, ok := raw.(map[string]any)
	if !ok {
		return expandedTagValue{value: raw, hasValue: true}
	}

	// An object tag must have at least one known provisioning field to be treated
	// as an expanded tag; otherwise treat it as a scalar pass-through.
	_, hasValue := m["value"]
	_, hasUnits := m["units"]
	_, hasDeadband := m["deadband"]
	_, hasHistory := m["history"]
	_, hasLimits := m["limits"]
	_, hasPersist := m["persist"]
	_, hasScaling := m["scaling"]
	_, hasStalecheck := m["stalecheck"]
	_, hasPublish := m["publish"]
	_, hasType := m["type"]
	_, hasEnumValues := m["enumValues"]
	_, hasDescription := m["description"]
	if !hasValue && !hasUnits && !hasDeadband && !hasHistory && !hasLimits && !hasPersist && !hasScaling && !hasStalecheck && !hasPublish && !hasType && !hasEnumValues && !hasDescription {
		return expandedTagValue{value: raw, hasValue: true}
	}

	ev := expandedTagValue{value: m["value"], hasValue: hasValue}
	if typ, _ := m["type"].(string); strings.EqualFold(typ, "enum") {
		ev.isEnum = true
	}
	if hasEnumValues {
		if enumValues := parseEnumValues(m["enumValues"]); len(enumValues) > 0 {
			ev.enumValues = enumValues
			ev.isEnum = true
		}
	}
	if hasPersist {
		ev.persist, _ = m["persist"].(bool)
	}
	if hasPublish {
		ev.publish, _ = m["publish"].(bool)
	}
	ev.description, _ = m["description"].(string)
	ev.units, _ = m["units"].(string)

	if hasDeadband {
		if t, ok := toFloat64Any(m["deadband"]); ok {
			ev.deadband = &t
		}
	}

	if hasHistory {
		ev.history, _ = m["history"].(bool)
	}
	if hasLimits {
		if lm, ok := m["limits"].(map[string]any); ok {
			ev.limits = parseLimitsConfig(lm)
		}
	}
	if hasScaling {
		if sc, ok := m["scaling"].(map[string]any); ok {
			ev.scaling = parseScalingConfig(sc)
		}
	}
	if hasStalecheck {
		if t, ok := toFloat64Any(m["stalecheck"]); ok {
			ev.staleTimeout = &t
		}
	}
	return ev
}

func parseEnumValues(raw any) map[int]string {
	result := map[int]string{}

	switch vals := raw.(type) {
	case map[string]any:
		for key, label := range vals {
			idx, err := strconv.Atoi(key)
			if err != nil {
				continue
			}
			result[idx] = fmt.Sprint(label)
		}
	case map[string]string:
		for key, label := range vals {
			idx, err := strconv.Atoi(key)
			if err != nil {
				continue
			}
			result[idx] = label
		}
	case map[int]string:
		for key, label := range vals {
			result[key] = label
		}
	}

	return result
}

// toFloat64Any converts a JSON-unmarshalled numeric value to float64.
func toFloat64Any(v any) (float64, bool) {
	switch n := v.(type) {
	case float64:
		return n, true
	case int:
		return float64(n), true
	case int64:
		return float64(n), true
	}
	return 0, false
}

// parseLimitsConfig parses a "limits" object from an ingest payload into a
// limitsConfig struct, extracting hi/lo limits and all event settings.
func parseLimitsConfig(lm map[string]any) *limitsConfig {
	cfg := &limitsConfig{}
	if hi, ok := toFloat64Any(lm["hi"]); ok {
		cfg.Hi = hi
	}
	if lo, ok := toFloat64Any(lm["lo"]); ok {
		cfg.Lo = lo
	}
	if hiEvent, ok := lm["hiEvent"].(map[string]any); ok {
		cfg.HiEvent = parseLimitEvent(hiEvent)
	}
	if rtnEvent, ok := lm["rtnEvent"].(map[string]any); ok {
		cfg.RtnEvent = parseLimitEvent(rtnEvent)
	}
	if lowEvent, ok := lm["lowEvent"].(map[string]any); ok {
		cfg.LowEvent = parseLimitEvent(lowEvent)
	}
	return cfg
}

// parseLimitEvent extracts notification fields from an event map.
// NotificationID may be a string name ("none", "sysadmin", "manager", "tech") or
// a numeric ID; the caller resolves string names during pipeline provisioning.
func parseLimitEvent(em map[string]any) limitEventConfig {
	ec := limitEventConfig{}
	if enabled, ok := em["enabled"].(bool); ok {
		ec.Enabled = enabled
	}
	if sev, ok := em["severity"].(string); ok {
		ec.Severity = sev
	}
	if msg, ok := em["message"].(string); ok {
		ec.Message = msg
	}
	if nid, ok := em["notificationId"]; ok {
		ec.NotificationID = nid // kept as any; resolved to int during provisioning
	}
	return ec
}

// parseScalingConfig parses a "scaling" object from an ingest payload into a
// scalingConfig struct: output = (input * scale) + offset, with optional clamp.
func parseScalingConfig(sc map[string]any) *scalingConfig {
	cfg := &scalingConfig{Scale: 1, Offset: 0} // defaults match ScalingBlock
	if scale, ok := toFloat64Any(sc["scale"]); ok {
		cfg.Scale = scale
	}
	if offset, ok := toFloat64Any(sc["offset"]); ok {
		cfg.Offset = offset
	}
	if min, ok := toFloat64Any(sc["min"]); ok {
		cfg.Min = &min
	}
	if max, ok := toFloat64Any(sc["max"]); ok {
		cfg.Max = &max
	}
	return cfg
}

// ── Processor ─────────────────────────────────────────────────────────────────

// NotificationResolver resolves a notification profile name to its integer ID.
// Names map to seeded default profiles: "sysadmin" → 1, "manager" → 2, "tech" → 3.
// The special name "none" resolves to 0 (no notification).
type NotificationResolver interface {
	ResolveNotificationID(ctx context.Context, org, name string) (int, error)
}

// Processor writes device payload data into the RTDB.
type Processor struct {
	treeOps       *tree.TreeWithOperations
	notifResolver NotificationResolver
}

// NewProcessor returns a Processor backed by the given tree.
func NewProcessor(treeOps *tree.TreeWithOperations) *Processor {
	return &Processor{treeOps: treeOps}
}

// SetNotificationResolver injects the resolver used to convert notification
// profile names to integer IDs during pipeline provisioning.
func (p *Processor) SetNotificationResolver(r NotificationResolver) {
	p.notifResolver = r
}

// resolveLimitEvent converts a parsed limitEventConfig (potentially holding a
// string notification name) into a fully-resolved event config with an integer
// notification ID. "none" → 0, "sysadmin" → 1, "manager" → 2, "tech" → 3.
func (p *Processor) resolveLimitEvent(src *limitEventConfig, org string) *blocks.EventConfig {
	if src == nil {
		return nil
	}
	ec := &blocks.EventConfig{
		Enabled:  src.Enabled,
		Severity: src.Severity,
		Message:  src.Message,
	}
	switch v := src.NotificationID; v.(type) {
	case nil:
		ec.NotificationID = 0
	case float64:
		ec.NotificationID = int(v.(float64))
	case int:
		ec.NotificationID = v.(int)
	case string:
		s := v.(string)
		if s == "" || s == "none" {
			ec.NotificationID = 0
		} else if p.notifResolver != nil {
			id, _ := p.notifResolver.ResolveNotificationID(context.Background(), org, s)
			ec.NotificationID = id
		}
	}
	return ec
}

// WriteDeviceData writes parsed tag data into the RTDB under the given device.
// It is safe to call concurrently from multiple goroutines.
func (p *Processor) WriteDeviceData(tenant, zone, deviceType, deviceName string, data TagData) error {
	var devicePath, deviceTypePath string
	if zone != "" {
		devicePath = tenant + "." + zone + "." + deviceType + "." + deviceName
		deviceTypePath = tenant + "." + zone + "." + deviceType
	} else {
		devicePath = tenant + "." + deviceType + "." + deviceName
		deviceTypePath = tenant + "." + deviceType
	}
	if err := p.treeOps.EnsureDeviceNode(tenant, deviceTypePath, devicePath); err != nil {
		log.Printf("ingest:352 device node %s, %v\n", devicePath, err)
		return fmt.Errorf("Cannot make device node %s: %w", devicePath, err)
	}

	// Payload template names apply to this device only. If absent, use the
	// device node's current template, then fall back to the device-type default.
	templateName := data.TemplateName
	if templateName == "" {
		if deviceNode, err := p.treeOps.FindNode(devicePath); err == nil {
			templateName = deviceNode.GetTemplateName()
		}
	}
	if templateName == "" {
		if dtNode, err := p.treeOps.FindNode(deviceTypePath); err == nil {
			templateName = dtNode.GetTemplateName()
		}
	}
	if templateName != "" {
		p.applyDeviceTemplateName(devicePath, templateName)
	}
	if data.DeviceDescription != "" {
		p.applyDeviceDescription(devicePath, data.DeviceDescription)
	}
	// Direct tags: path = {tenant}.{deviceType}.{deviceName}.{tagName}
	for tagName, raw := range data.DirectTags {
		if tagName == "" {
			continue
		}
		// Check for bare array value
		if arr, ok := raw.([]any); ok {
			parentPath := fmt.Sprintf("%s.%s", devicePath, tagName)
			p.writeArrayTag(parentPath, "", tagName, arr, nil, templateName, tenant, data.TSUnixMilli)
			continue
		}
		extracted := extractTagValue(raw)

		// Check if expanded tag has array value (e.g. {"value": [...], "units": "V"})
		if arr, ok := extracted.value.([]any); ok {
			parentPath := fmt.Sprintf("%s.%s", devicePath, tagName)
			p.writeArrayTag(parentPath, "", tagName, arr, &extracted, templateName, tenant, data.TSUnixMilli)
			continue
		}
		rtdbPath := fmt.Sprintf("%s.%s", devicePath, tagName)
		if err := p.writeTag(rtdbPath, "", tagName, extracted, templateName, tenant, data.TSUnixMilli); err != nil {
			log.Printf("ingest:383 %s: %v", rtdbPath, err)
		}
	}

	// Grouped tags: path = /{tenant}/{deviceType}/{deviceName}/{group}/{tagName}
	for tagGroup, tags := range data.Groups {
		if tagGroup == "" {
			continue
		}
		for tagName, raw := range tags {

			if tagName == "" {
				continue
			}
			// Check for bare array value
			if arr, ok := raw.([]any); ok {
				parentPath := fmt.Sprintf("%s.%s.%s", devicePath, tagGroup, tagName)
				p.writeArrayTag(parentPath, tagGroup, tagName, arr, nil, templateName, tenant, data.TSUnixMilli)
				continue
			}
			extracted := extractTagValue(raw)
			// If the extracted value is still a map, it's a sub-group (two-level nesting).
			// e.g. {"pumps": {"1": {"flow_m3h": 1.2, "speed_rpm": 1450}}}
			if subMap, ok := extracted.value.(map[string]any); ok {
				subGroup := tagGroup + "." + tagName
				for subTagName, subVal := range subMap {
					if subTagName == "" {
						continue
					}
					if arr, ok := subVal.([]any); ok {
						parentPath := fmt.Sprintf("%s.%s.%s.%s", devicePath, tagGroup, tagName, subTagName)
						p.writeArrayTag(parentPath, subGroup, subTagName, arr, nil, templateName, tenant, data.TSUnixMilli)
						continue
					}
					subExtracted := extractTagValue(subVal)
					rtdbPath := fmt.Sprintf("%s.%s.%s.%s", devicePath, tagGroup, tagName, subTagName)
					if err := p.writeTag(rtdbPath, subGroup, subTagName, subExtracted, templateName, tenant, data.TSUnixMilli); err != nil {
						log.Printf("ingest writetag: %s: %v", rtdbPath, err)
					}
				}
				continue
			}
			// Check if expanded tag has array value
			if arr, ok := extracted.value.([]any); ok {
				parentPath := fmt.Sprintf("%s.%s.%s", devicePath, tagGroup, tagName)
				p.writeArrayTag(parentPath, tagGroup, tagName, arr, &extracted, templateName, tenant, data.TSUnixMilli)
				continue
			}
			rtdbPath := fmt.Sprintf("%s.%s.%s", devicePath, tagGroup, tagName)
			if err := p.writeTag(rtdbPath, tagGroup, tagName, extracted, templateName, tenant, data.TSUnixMilli); err != nil {
				log.Printf("ingest:432 %s: %v", rtdbPath, err)
			}
		}
	}

	return nil
}

func (p *Processor) applyDeviceTemplateName(devicePath, templateName string) {
	deviceNode, err := p.treeOps.FindNode(devicePath)
	if err != nil {
		return
	}
	if deviceNode.GetTemplateName() == templateName {
		return
	}
	deviceNode.SetTemplateName(templateName)
	p.treeOps.NotifyChange(devicePath, deviceNode)
}

func (p *Processor) applyDeviceDescription(devicePath, description string) {
	description = strings.TrimSpace(description)
	if description == "" {
		return
	}
	deviceNode, err := p.treeOps.FindNode(devicePath)
	if err != nil {
		return
	}
	if deviceNode.GetDescription() == description {
		return
	}
	deviceNode.SetDescription(description)
	p.treeOps.NotifyChange(devicePath, deviceNode)
}

func deviceDescriptionFromRaw(raw any) (string, bool) {
	switch v := raw.(type) {
	case string:
		description := strings.TrimSpace(v)
		return description, description != ""
	case map[string]any:
		extracted := extractTagValue(v)
		description, ok := extracted.value.(string)
		if !ok {
			return "", false
		}
		description = strings.TrimSpace(description)
		return description, description != ""
	default:
		return "", false
	}
}

// writeTag creates the tag (if absent) and sets its value.
func (p *Processor) writeTag(
	rtdbPath, tagGroup, tagName string,
	extracted expandedTagValue,
	templateName, tenant string,
	unixMillis int64,
) error {
	scalarType := DetermineType(extracted.value)
	if extracted.isEnum {
		scalarType = tree.TypeEnum
	}
	config := tree.TagConfig{Name: tagName, Type: scalarType}
	sharedToCreate := tree.TagShared{
		Description: extracted.description,
		EnumValues:  extracted.enumValues,
	}

	created := false
	if existingLeaf, err := p.treeOps.FindLeaf(rtdbPath); err == nil && extracted.isEnum && existingLeaf.ValueType() != tree.TypeEnum {
		if err := p.treeOps.DeleteTag(rtdbPath); err != nil {
			return err
		}
	}
	if templateName != "" {
		var tmplLeafPath string
		if tagGroup != "" {
			tmplLeafPath = tenant + "/" + strings.ReplaceAll(templateName, ".", "/") + "/" + tagGroup + "/" + tagName
		} else {
			tmplLeafPath = tenant + "/" + strings.ReplaceAll(templateName, ".", "/") + "/" + tagName
		}
		if tmplLeaf, err := p.treeOps.FindLeaf(tmplLeafPath); err == nil {
			config.TemplateName = templateName
			if err := p.treeOps.CreateTagWithTemplateLeaf(rtdbPath, scalarType, config, tmplLeaf); err == nil {
				created = true
			}
		}
	}
	if !created {
		if err := p.treeOps.CreateTag(rtdbPath, scalarType, config, sharedToCreate); err != nil {
			fmt.Printf("writeTag:468 %+v\n", err)
			return err
		}
	}

	leaf, err := p.treeOps.FindLeaf(rtdbPath)
	if err != nil {
		fmt.Printf("writeTag:479 %+v\n", err)
		return err
	}
	shared := leaf.GetShared()
	if extracted.description != "" {
		shared.Description = extracted.description
		leaf.SetShared(shared)
	}
	if extracted.units != "" {
		shared.Units = extracted.units
		leaf.SetShared(shared)
	}
	if len(extracted.enumValues) > 0 {
		shared.EnumValues = extracted.enumValues
		leaf.SetShared(shared)
		if enumLeaf, ok := leaf.(interface {
			AddEnum(int, string)
		}); ok {
			for value, display := range extracted.enumValues {
				enumLeaf.AddEnum(value, display)
			}
		}
	}

	if extracted.deadband != nil {
		shared.Deadband = *extracted.deadband
		leaf.SetShared(shared)
	}

	if extracted.history || extracted.limits != nil || extracted.persist || extracted.scaling != nil || extracted.staleTimeout != nil {
		p.provisionPipeline(leaf, extracted, tenant)
	}

	if extracted.hasValue && extracted.value != nil {
		if err := leaf.SetAnyValue(extracted.value); err != nil {
			fmt.Printf("writeTag:494 %+v\n", err)
			return err
		}
	}
	leaf.SetUpdatedTime(time.UnixMilli(unixMillis))
	return nil
}

// writeArrayTag expands an array value into individually addressable scalar leaves
// under an array parent node. Supports both scalar arrays ([12.6, 12.9]) and
// object arrays ([{"P": 1.0, "I": 0.5}, {"P": 2.0}]).
func (p *Processor) writeArrayTag(
	arrayParentPath, tagGroup, tagName string,
	elements []any,
	metadata *expandedTagValue, // nil for bare arrays
	templateName, tenant string,
	unixMilli int64,
) {
	// Ensure array parent node exists and mark it
	if err := p.treeOps.CreateNode(arrayParentPath, ""); err != nil {
		log.Printf("ingest: create array node %s: %v", arrayParentPath, err)
		return
	}
	if node, err := p.treeOps.FindNode(arrayParentPath); err == nil {
		node.SetIsArray(true)
	}

	for i, elem := range elements {
		idxStr := strconv.Itoa(i)

		switch v := elem.(type) {
		case map[string]any:
			// Object element → sub-group of scalar tags
			elemNodePath := arrayParentPath + "/" + idxStr
			if err := p.treeOps.CreateNode(elemNodePath, ""); err != nil {
				log.Printf("ingest: create array element node %s: %v", elemNodePath, err)
				continue
			}
			for fieldName, fieldVal := range v {
				extracted := extractTagValue(fieldVal)
				if metadata != nil {
					if extracted.description == "" {
						extracted.description = metadata.description
					}
					if extracted.units == "" {
						extracted.units = metadata.units
					}
					if extracted.history {
						extracted.history = metadata.history
					}
					if extracted.limits == nil {
						extracted.limits = metadata.limits
					}
					if !extracted.persist {
						extracted.persist = metadata.persist
					}
				}
				leafPath := elemNodePath + "/" + fieldName
				// Template group: tagGroup/tagName/idx for template lookup
				var elemTagGroup string
				if tagGroup != "" {
					elemTagGroup = tagGroup + "/" + tagName + "/" + idxStr
				} else {
					elemTagGroup = tagName + "/" + idxStr
				}
				if err := p.writeTag(leafPath, elemTagGroup, fieldName, extracted, templateName, tenant, unixMilli); err != nil {
					log.Printf("ingest:548 %s: %v", leafPath, err)
				}
			}
		default:
			// Scalar element → leaf directly under array parent
			extracted := expandedTagValue{value: v, hasValue: true}
			if metadata != nil {
				extracted.description = metadata.description
				extracted.units = metadata.units
				extracted.history = metadata.history
				extracted.limits = metadata.limits
				extracted.persist = metadata.persist
			}
			leafPath := arrayParentPath + "/" + idxStr
			var elemTagGroup string
			if tagGroup != "" {
				elemTagGroup = tagGroup + "/" + tagName
			} else {
				elemTagGroup = tagName
			}
			if err := p.writeTag(leafPath, elemTagGroup, idxStr, extracted, templateName, tenant, unixMilli); err != nil {
				log.Printf("ingest:568 %s: %v", leafPath, err)
			}
		}
	}
}

// canonicalBlockOrder defines the required execution order for pipeline blocks.
// When auto-provisioning, new blocks are inserted at the position that respects
// this order relative to any blocks already present.
var canonicalBlockOrder = []string{
	"scaling",
	"limitcheck",
	"historyrecorder",
	"stalecheck",
	"persist",
	"publish",
}

// blockRank returns the position of blockType in canonicalBlockOrder, or a
// large sentinel value for unknown types (appended after all known blocks).
func blockRank(blockType string) int {
	for i, t := range canonicalBlockOrder {
		if t == blockType {
			return i
		}
	}
	return len(canonicalBlockOrder)
}

// insertOrdered inserts block into pipeline at the position dictated by
// canonicalBlockOrder. Existing blocks retain their relative order.
func insertOrdered(pipeline []tree.ProcessBlock, block tree.ProcessBlock) []tree.ProcessBlock {
	rank := blockRank(block.GetType())
	for i, b := range pipeline {
		if blockRank(b.GetType()) > rank {
			// Insert before position i.
			pipeline = append(pipeline, nil)
			copy(pipeline[i+1:], pipeline[i:])
			pipeline[i] = block
			return pipeline
		}
	}
	return append(pipeline, block)
}

// provisionPipeline adds or updates historyrecorder / limitcheck blocks on the
// leaf's local pipeline. New blocks are inserted at their canonical position.
func (p *Processor) provisionPipeline(leaf tree.Leaf, extracted expandedTagValue, tenant string) {
	shared := leaf.GetShared()
	pipeline := shared.Pipeline

	changed := false

	if extracted.history {
		changed = true
		if findBlockIndex(pipeline, "historyrecorder") < 0 {
			block, err := tree.NewProcessBlockByType("historyrecorder")
			if err != nil {
				log.Printf("ingest: provision historyrecorder: %v", err)
			} else {
				block.Init(leaf)
				pipeline = insertOrdered(pipeline, block)
			}
		}
	}

	if extracted.limits != nil {
		lm := extracted.limits
		params, _ := json.Marshal(map[string]any{
			"hiLimit":  lm.Hi,
			"lowLimit": lm.Lo,
			"hiEvent":  p.resolveLimitEvent(&lm.HiEvent, tenant),
			"rtnEvent": p.resolveLimitEvent(&lm.RtnEvent, tenant),
			"lowEvent": p.resolveLimitEvent(&lm.LowEvent, tenant),
		})
		if idx := findBlockIndex(pipeline, "limitcheck"); idx >= 0 {
			if err := pipeline[idx].SetParameters(params); err == nil {
				changed = true
			}
		} else {
			block, err := tree.NewProcessBlockByType("limitcheck")
			if err != nil {
				log.Printf("ingest: provision limitcheck: %v", err)
			} else if err := block.SetParameters(params); err == nil {
				block.Init(leaf)
				pipeline = insertOrdered(pipeline, block)
				changed = true
			}
		}
	}

	if extracted.persist && findBlockIndex(pipeline, "persist") < 0 {
		block, err := tree.NewProcessBlockByType("persist")
		if err != nil {
			log.Printf("ingest: provision persist: %v", err)
		} else {
			block.Init(leaf)
			pipeline = insertOrdered(pipeline, block)
			changed = true
		}
	}

	if extracted.scaling != nil {
		sc := extracted.scaling
		params, _ := json.Marshal(sc)
		if idx := findBlockIndex(pipeline, "scaling"); idx >= 0 {
			if err := pipeline[idx].SetParameters(params); err == nil {
				changed = true
			}
		} else {
			block, err := tree.NewProcessBlockByType("scaling")
			if err != nil {
				log.Printf("ingest: provision scaling: %v", err)
			} else if err := block.SetParameters(params); err == nil {
				block.Init(leaf)
				pipeline = insertOrdered(pipeline, block)
				changed = true
			}
		}
	}

	if extracted.staleTimeout != nil {
		timeoutSecs := *extracted.staleTimeout
		timeout := time.Duration(timeoutSecs) * time.Second
		params, _ := json.Marshal(map[string]any{"timeout": timeout})
		if idx := findBlockIndex(pipeline, "stalecheck"); idx >= 0 {
			if err := pipeline[idx].SetParameters(params); err == nil {
				changed = true
			}
		} else {
			block, err := tree.NewProcessBlockByType("stalecheck")
			if err != nil {
				log.Printf("ingest: provision stalecheck: %v", err)
			} else if err := block.SetParameters(params); err == nil {
				block.Init(leaf)
				pipeline = insertOrdered(pipeline, block)
				changed = true
			}
		}
	}

	if extracted.publish && findBlockIndex(pipeline, "publish") < 0 {
		block, err := tree.NewProcessBlockByType("publish")
		if err != nil {
			log.Printf("ingest: provision publish: %v", err)
		} else {
			block.Init(leaf)
			pipeline = insertOrdered(pipeline, block)
			changed = true
		}
	}

	if changed {
		shared.Pipeline = pipeline
		leaf.SetShared(shared)
	}
}

// findBlockIndex returns the index of the first block with the given type, or -1.
func findBlockIndex(pipeline []tree.ProcessBlock, blockType string) int {
	for i, b := range pipeline {
		if b.GetType() == blockType {
			return i
		}
	}
	return -1
}
