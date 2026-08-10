package visualscripts

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"reflect"
	"strings"
	"sync"
	"time"
)

type compiledPlan struct {
	graph    GraphDocument
	nodes    map[string]GraphNode
	outgoing map[string]map[string][]GraphEdge
}

type Engine struct {
	store    Store
	registry *Registry
	mu       sync.RWMutex
	plans    map[string]*compiledPlan
	statuses map[string]RuntimeStatus
	context  map[string]any
	sequence uint64
}

func New(store Store) *Engine {
	return &Engine{store: store, registry: NewRegistry(), plans: make(map[string]*compiledPlan), statuses: make(map[string]RuntimeStatus), context: make(map[string]any)}
}

func (e *Engine) Registry() *Registry { return e.registry }

func (e *Engine) Validate(graph GraphDocument) ValidationResult {
	return ValidateGraph(e.registry, graph)
}

func (e *Engine) Deploy(ctx context.Context, org, scriptID string, revision int) (*RuntimeStatus, error) {
	rev, err := e.store.GetVisualScriptRevision(ctx, org, scriptID, revision)
	if err != nil {
		return nil, err
	}
	if rev == nil {
		return nil, ErrNotFound
	}
	validation := e.Validate(rev.Graph)
	if !validation.Valid {
		return nil, fmt.Errorf("revision %d is invalid", revision)
	}
	plan := compile(validation.Graph)
	if err := e.store.SetVisualScriptActiveRevision(ctx, org, scriptID, &revision); err != nil {
		return nil, err
	}
	e.mu.Lock()
	e.plans[scriptKey(org, scriptID)] = plan
	e.clearContextLocked(org, scriptID)
	e.sequence++
	e.mu.Unlock()
	status, err := e.Status(ctx, org, scriptID)
	return &status, err
}

func (e *Engine) Undeploy(ctx context.Context, org, scriptID string) (*RuntimeStatus, error) {
	if err := e.store.SetVisualScriptDesiredState(ctx, org, scriptID, "stopped"); err != nil {
		return nil, err
	}
	if err := e.store.SetVisualScriptActiveRevision(ctx, org, scriptID, nil); err != nil {
		return nil, err
	}
	e.mu.Lock()
	delete(e.plans, scriptKey(org, scriptID))
	e.clearContextLocked(org, scriptID)
	e.sequence++
	e.mu.Unlock()
	status, err := e.Status(ctx, org, scriptID)
	return &status, err
}

func (e *Engine) SetDesiredState(ctx context.Context, org, scriptID, state string) (*RuntimeStatus, error) {
	if state != "stopped" && state != "running" && state != "paused" {
		return nil, fmt.Errorf("invalid desired state %q", state)
	}
	script, err := e.store.GetVisualScript(ctx, org, scriptID)
	if err != nil {
		return nil, err
	}
	if script == nil {
		return nil, ErrNotFound
	}
	if state != "stopped" && script.ActiveRevision == nil {
		return nil, errors.New("deploy a valid revision before starting the script")
	}
	if err := e.store.SetVisualScriptDesiredState(ctx, org, scriptID, state); err != nil {
		return nil, err
	}
	if state == "stopped" {
		e.mu.Lock()
		e.clearContextLocked(org, scriptID)
		e.mu.Unlock()
	}
	status, err := e.Status(ctx, org, scriptID)
	return &status, err
}

func (e *Engine) Status(ctx context.Context, org, scriptID string) (RuntimeStatus, error) {
	script, err := e.store.GetVisualScript(ctx, org, scriptID)
	if err != nil {
		return RuntimeStatus{}, err
	}
	if script == nil {
		return RuntimeStatus{}, ErrNotFound
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	key := scriptKey(org, scriptID)
	status := e.statuses[key]
	status.ScriptID = scriptID
	status.DesiredState = script.DesiredState
	status.ActiveRevision = script.ActiveRevision
	status.LatestRevision = script.LatestRevision
	if script.ActiveRevision == nil {
		status.RuntimeState = "draft"
	} else {
		status.RuntimeState = script.DesiredState
	}
	e.sequence++
	status.Sequence = e.sequence
	e.statuses[key] = status
	return status, nil
}

func (e *Engine) RunManual(ctx context.Context, org, scriptID string, request RunRequest) (*Run, error) {
	script, err := e.store.GetVisualScript(ctx, org, scriptID)
	if err != nil {
		return nil, err
	}
	if script == nil || script.ActiveRevision == nil {
		return nil, errors.New("no active revision")
	}
	plan, err := e.plan(ctx, org, scriptID, *script.ActiveRevision)
	if err != nil {
		return nil, err
	}
	triggerID := request.TriggerNodeID
	if triggerID == "" {
		for _, node := range plan.graph.Nodes {
			if node.Type == "core.manual" {
				triggerID = node.ID
				break
			}
		}
	}
	trigger, ok := plan.nodes[triggerID]
	if !ok || trigger.Type != "core.manual" {
		return nil, errors.New("select a Manual trigger")
	}
	now := time.Now().UTC()
	runID := newID("run")
	run := &Run{RunID: runID, OrgName: org, ScriptID: scriptID, ActiveRevision: *script.ActiveRevision, TriggerNodeID: triggerID, StartedAt: now, Status: "running"}
	message := Message{ID: newID("msg"), CorrelationID: runID, OrgName: org, ScriptID: scriptID, ActiveRevision: *script.ActiveRevision, TriggerNodeID: triggerID, TriggerTimestamp: now, Value: request.Value, Fields: cloneFields(request.Fields)}
	if message.Fields == nil {
		message.Fields = make(map[string]any)
	}
	if encoded, marshalErr := json.Marshal(message); marshalErr != nil || len(encoded) > 256<<10 {
		return nil, errors.New("manual message must be JSON-compatible and no larger than 256 KiB")
	}
	if err := e.store.AppendVisualScriptRun(ctx, run); err != nil {
		return nil, err
	}
	e.mu.Lock()
	status := e.statuses[scriptKey(org, scriptID)]
	status.LastTriggerAt = &now
	e.statuses[scriptKey(org, scriptID)] = status
	e.mu.Unlock()

	execErr := e.execute(ctx, plan, trigger, message, run)
	completed := time.Now().UTC()
	run.CompletedAt = &completed
	run.DurationMS = completed.Sub(now).Milliseconds()
	if execErr != nil {
		run.Status = "error"
		run.Message = execErr.Error()
	} else {
		run.Status = "ok"
		run.Message = "Run completed"
	}
	if err := e.store.CompleteVisualScriptRun(ctx, run); err != nil {
		return nil, err
	}
	e.mu.Lock()
	status = e.statuses[scriptKey(org, scriptID)]
	status.LastCompleteAt = &completed
	status.ErrorSummary = ""
	if execErr != nil {
		status.ErrorSummary = execErr.Error()
	}
	e.statuses[scriptKey(org, scriptID)] = status
	e.mu.Unlock()
	return run, nil
}

func (e *Engine) plan(ctx context.Context, org, scriptID string, revision int) (*compiledPlan, error) {
	key := scriptKey(org, scriptID)
	e.mu.RLock()
	plan := e.plans[key]
	e.mu.RUnlock()
	if plan != nil {
		return plan, nil
	}
	rev, err := e.store.GetVisualScriptRevision(ctx, org, scriptID, revision)
	if err != nil || rev == nil {
		return nil, fmt.Errorf("loading active revision: %w", err)
	}
	validation := e.Validate(rev.Graph)
	if !validation.Valid {
		return nil, errors.New("active revision is invalid")
	}
	plan = compile(validation.Graph)
	e.mu.Lock()
	e.plans[key] = plan
	e.mu.Unlock()
	return plan, nil
}

func compile(graph GraphDocument) *compiledPlan {
	plan := &compiledPlan{graph: graph, nodes: make(map[string]GraphNode), outgoing: make(map[string]map[string][]GraphEdge)}
	for _, node := range graph.Nodes {
		plan.nodes[node.ID] = node
	}
	for _, edge := range graph.Edges {
		if plan.outgoing[edge.From.NodeID] == nil {
			plan.outgoing[edge.From.NodeID] = make(map[string][]GraphEdge)
		}
		plan.outgoing[edge.From.NodeID][edge.From.Port] = append(plan.outgoing[edge.From.NodeID][edge.From.Port], edge)
	}
	return plan
}

type queuedNode struct {
	node GraphNode
	msg  Message
}

func (e *Engine) execute(ctx context.Context, plan *compiledPlan, trigger GraphNode, message Message, run *Run) error {
	queue := []queuedNode{{node: trigger, msg: message}}
	hops := 0
	for len(queue) > 0 {
		if err := ctx.Err(); err != nil {
			return err
		}
		item := queue[0]
		queue = queue[1:]
		hops++
		if hops > 1000 {
			return errors.New("maximum execution hops exceeded")
		}
		port, nextMessage, action, err := e.handleNode(item.node, item.msg)
		run.NodesExecuted++
		if action {
			run.ActionsAttempted++
		}
		trace := TraceEvent{Sequence: run.NodesExecuted, Timestamp: time.Now().UTC(), NodeID: item.node.ID, NodeType: item.node.Type, Port: port, Status: "ok", Value: nextMessage.Value, Fields: cloneFields(nextMessage.Fields)}
		if err != nil {
			trace.Status = "error"
			trace.Message = err.Error()
			run.FirstErrorNodeID = item.node.ID
		}
		if len(run.Trace) < 200 {
			run.Trace = append(run.Trace, trace)
		} else {
			run.DroppedTraces++
		}
		if err != nil {
			return fmt.Errorf("node %s: %w", item.node.ID, err)
		}
		for _, edge := range plan.outgoing[item.node.ID][port] {
			next, ok := plan.nodes[edge.To.NodeID]
			if ok {
				copyMessage := nextMessage
				copyMessage.Fields = cloneFields(nextMessage.Fields)
				queue = append(queue, queuedNode{node: next, msg: copyMessage})
			}
		}
	}
	return nil
}

func (e *Engine) handleNode(node GraphNode, msg Message) (string, Message, bool, error) {
	var config map[string]any
	if err := json.Unmarshal(node.Config, &config); err != nil {
		return "", msg, false, err
	}
	value := selectedValue(msg, stringConfig(config, "field"))
	switch node.Type {
	case "core.manual":
		return "out", msg, false, nil
	case "core.compare":
		result, err := compareValues(value, config["compareTo"], stringConfig(config, "operator"))
		return boolPort(result), msg, false, err
	case "core.in-range":
		n, ok := asFloat(value)
		min, minOK := asFloat(config["minimum"])
		max, maxOK := asFloat(config["maximum"])
		if !ok || !minOK || !maxOK {
			return "", msg, false, errors.New("in-range requires numeric values")
		}
		inclusive, _ := config["inclusive"].(bool)
		inside := n > min && n < max
		if inclusive {
			inside = n >= min && n <= max
		}
		return boolPort(inside), msg, false, nil
	case "core.not":
		truth, ok := value.(bool)
		if !ok {
			return "", msg, false, errors.New("NOT requires a boolean")
		}
		return boolPort(!truth), msg, false, nil
	case "core.and", "core.or":
		fields, ok := config["fields"].([]any)
		if !ok || len(fields) == 0 {
			return "", msg, false, errors.New("logical node requires a non-empty fields array")
		}
		result := node.Type == "core.and"
		for _, fieldName := range fields {
			fieldValue, exists := getField(msg.Fields, fmt.Sprint(fieldName))
			boolean, isBoolean := fieldValue.(bool)
			if !exists || !isBoolean {
				return "", msg, false, fmt.Errorf("field %q is not boolean", fieldName)
			}
			if node.Type == "core.and" {
				result = result && boolean
			} else {
				result = result || boolean
			}
		}
		return boolPort(result), msg, false, nil
	case "core.set-field":
		setSelectedValue(&msg, stringConfig(config, "field"), config["value"])
	case "core.select-field":
		field := stringConfig(config, "field")
		selected, ok := getField(msg.Fields, field)
		if !ok {
			return "", msg, false, fmt.Errorf("field %q does not exist", field)
		}
		msg.Value = selected
	case "core.multiply", "core.divide", "core.clamp", "core.scale":
		output, err := transformNumber(node.Type, value, config)
		if err != nil {
			return "", msg, false, err
		}
		setSelectedValue(&msg, stringConfig(config, "field"), output)
	case "core.average":
		values := []any{}
		if fields, ok := config["fields"].([]any); ok && len(fields) > 0 {
			for _, fieldName := range fields {
				fieldValue, exists := getField(msg.Fields, fmt.Sprint(fieldName))
				if !exists {
					return "", msg, false, fmt.Errorf("field %q does not exist", fieldName)
				}
				values = append(values, fieldValue)
			}
		} else if list, ok := msg.Value.([]any); ok {
			values = list
		} else {
			return "", msg, false, errors.New("average requires a numeric value array or configured fields")
		}
		total := 0.0
		for _, item := range values {
			number, ok := asFloat(item)
			if !ok {
				return "", msg, false, errors.New("average inputs must be numeric")
			}
			total += number
		}
		if len(values) == 0 {
			return "", msg, false, errors.New("average requires at least one value")
		}
		msg.Value = total / float64(len(values))
	case "core.get-context":
		contextValue, ok := e.getContext(msg, node.ID, stringConfig(config, "scope"), stringConfig(config, "key"))
		if !ok {
			return "", msg, false, errors.New("context key does not exist")
		}
		msg.Value = contextValue
	case "core.set-context":
		contextValue := msg.Value
		if stringConfig(config, "source") == "configured" {
			contextValue = config["value"]
		}
		e.setContext(msg, node.ID, stringConfig(config, "scope"), stringConfig(config, "key"), contextValue)
	case "core.delete-context":
		e.deleteContext(msg, node.ID, stringConfig(config, "scope"), stringConfig(config, "key"))
	case "core.increment-context":
		amount, _ := asFloat(config["amount"])
		incremented, err := e.incrementContext(msg, node.ID, stringConfig(config, "scope"), stringConfig(config, "key"), amount)
		if err != nil {
			return "", msg, false, err
		}
		msg.Value = incremented
	case "core.debug":
		return "out", msg, true, nil
	default:
		return "", msg, false, fmt.Errorf("node type %q cannot execute", node.Type)
	}
	return "out", msg, false, nil
}

func transformNumber(nodeType string, value any, config map[string]any) (float64, error) {
	n, ok := asFloat(value)
	if !ok || math.IsNaN(n) || math.IsInf(n, 0) {
		return 0, errors.New("transform requires a finite numeric value")
	}
	var result float64
	switch nodeType {
	case "core.multiply":
		factor, _ := asFloat(config["factor"])
		result = n * factor
	case "core.divide":
		divisor, _ := asFloat(config["divisor"])
		if divisor == 0 {
			return 0, errors.New("division by zero")
		}
		result = n / divisor
	case "core.clamp":
		minimum, _ := asFloat(config["minimum"])
		maximum, _ := asFloat(config["maximum"])
		result = math.Max(minimum, math.Min(maximum, n))
	case "core.scale":
		inputMin, _ := asFloat(config["inputMin"])
		inputMax, _ := asFloat(config["inputMax"])
		outputMin, _ := asFloat(config["outputMin"])
		outputMax, _ := asFloat(config["outputMax"])
		if inputMin == inputMax {
			return 0, errors.New("input range has zero width")
		}
		result = outputMin + (n-inputMin)*(outputMax-outputMin)/(inputMax-inputMin)
	}
	if math.IsNaN(result) || math.IsInf(result, 0) {
		return 0, errors.New("numeric overflow")
	}
	return result, nil
}

func compareValues(left, right any, operator string) (bool, error) {
	if l, ok := asFloat(left); ok {
		if r, rightOK := asFloat(right); rightOK {
			switch operator {
			case "<":
				return l < r, nil
			case "<=":
				return l <= r, nil
			case "==":
				return l == r, nil
			case "!=":
				return l != r, nil
			case ">=":
				return l >= r, nil
			case ">":
				return l > r, nil
			}
		}
	}
	if operator == "==" {
		return reflect.DeepEqual(left, right), nil
	}
	if operator == "!=" {
		return !reflect.DeepEqual(left, right), nil
	}
	leftString, leftOK := left.(string)
	rightString, rightOK := right.(string)
	if leftOK && rightOK {
		switch operator {
		case "<":
			return leftString < rightString, nil
		case "<=":
			return leftString <= rightString, nil
		case ">=":
			return leftString >= rightString, nil
		case ">":
			return leftString > rightString, nil
		}
	}
	return false, errors.New("comparison operands are incompatible")
}

func asFloat(value any) (float64, bool) {
	switch n := value.(type) {
	case float64:
		return n, true
	case float32:
		return float64(n), true
	case int:
		return float64(n), true
	case int64:
		return float64(n), true
	case int32:
		return float64(n), true
	case json.Number:
		v, err := n.Float64()
		return v, err == nil
	default:
		return 0, false
	}
}

func selectedValue(msg Message, field string) any {
	if field == "" {
		return msg.Value
	}
	value, _ := getField(msg.Fields, field)
	return value
}

func setSelectedValue(msg *Message, field string, value any) {
	if field == "" {
		msg.Value = value
		return
	}
	if msg.Fields == nil {
		msg.Fields = make(map[string]any)
	}
	setField(msg.Fields, field, value)
}

func getField(fields map[string]any, path string) (any, bool) {
	parts := strings.Split(path, ".")
	var current any = fields
	for _, part := range parts {
		mapping, ok := current.(map[string]any)
		if !ok {
			return nil, false
		}
		current, ok = mapping[part]
		if !ok {
			return nil, false
		}
	}
	return current, true
}

func setField(fields map[string]any, path string, value any) {
	parts := strings.Split(path, ".")
	current := fields
	for _, part := range parts[:len(parts)-1] {
		next, ok := current[part].(map[string]any)
		if !ok {
			next = make(map[string]any)
			current[part] = next
		}
		current = next
	}
	current[parts[len(parts)-1]] = value
}

func cloneFields(fields map[string]any) map[string]any {
	if fields == nil {
		return nil
	}
	copy := make(map[string]any, len(fields))
	for key, value := range fields {
		copy[key] = value
	}
	return copy
}

func stringConfig(config map[string]any, key string) string {
	value, _ := config[key].(string)
	return value
}

func boolPort(value bool) string {
	if value {
		return "true"
	}
	return "false"
}

func scriptKey(org, scriptID string) string { return org + "\x00" + scriptID }

func contextKey(msg Message, nodeID, scope, key string) string {
	prefix := scriptKey(msg.OrgName, msg.ScriptID) + fmt.Sprintf("\x00%d\x00", msg.ActiveRevision)
	if scope == "node" {
		prefix += nodeID + "\x00"
	} else {
		prefix += "script\x00"
	}
	return prefix + key
}

func (e *Engine) getContext(msg Message, nodeID, scope, key string) (any, bool) {
	e.mu.RLock()
	defer e.mu.RUnlock()
	value, ok := e.context[contextKey(msg, nodeID, scope, key)]
	return value, ok
}
func (e *Engine) setContext(msg Message, nodeID, scope, key string, value any) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.context[contextKey(msg, nodeID, scope, key)] = value
}
func (e *Engine) deleteContext(msg Message, nodeID, scope, key string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	delete(e.context, contextKey(msg, nodeID, scope, key))
}
func (e *Engine) incrementContext(msg Message, nodeID, scope, key string, amount float64) (float64, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	fullKey := contextKey(msg, nodeID, scope, key)
	current := 0.0
	if value, exists := e.context[fullKey]; exists {
		var ok bool
		current, ok = asFloat(value)
		if !ok {
			return 0, errors.New("context value is not numeric")
		}
	}
	current += amount
	e.context[fullKey] = current
	return current, nil
}

func (e *Engine) clearContextLocked(org, scriptID string) {
	prefix := scriptKey(org, scriptID) + "\x00"
	for key := range e.context {
		if strings.HasPrefix(key, prefix) {
			delete(e.context, key)
		}
	}
}

func newID(prefix string) string {
	var bytes [12]byte
	_, _ = rand.Read(bytes[:])
	return prefix + "_" + hex.EncodeToString(bytes[:])
}
