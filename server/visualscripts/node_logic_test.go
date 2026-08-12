package visualscripts

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
)

func TestEveryCatalogNodeHasWorkingExecutionLogic(t *testing.T) {
	engine := New(newRuntimeTestStore(Script{}, Revision{}))
	defer engine.Close()
	base := Message{OrgName: "org", ScriptID: "script", ActiveRevision: 1, InstanceKey: "Pump01"}
	tests := []struct {
		name       string
		nodeType   string
		config     string
		message    Message
		wantPort   string
		wantValue  any
		wantFields map[string]any
		wantAction bool
	}{
		{name: "manual passes the message through", nodeType: "core.manual", config: `{}`, message: messageWith(base, 7, nil), wantPort: "out", wantValue: 7},
		{name: "compare routes numeric fields", nodeType: "core.compare", config: `{"field":"temperature","operator":">=","compareTo":20}`, message: messageWith(base, nil, map[string]any{"temperature": 21}), wantPort: "true", wantFields: map[string]any{"temperature": 21}},
		{name: "in range includes boundaries by default", nodeType: "core.in-range", config: `{"minimum":0,"maximum":10}`, message: messageWith(base, 0, nil), wantPort: "true", wantValue: 0},
		{name: "not negates a boolean", nodeType: "core.not", config: `{}`, message: messageWith(base, false, nil), wantPort: "true", wantValue: false},
		{name: "and requires every field", nodeType: "core.and", config: `{"fields":["ready","enabled"]}`, message: messageWith(base, nil, map[string]any{"ready": true, "enabled": false}), wantPort: "false", wantFields: map[string]any{"ready": true, "enabled": false}},
		{name: "or accepts any field", nodeType: "core.or", config: `{"fields":["ready","enabled"]}`, message: messageWith(base, nil, map[string]any{"ready": false, "enabled": true}), wantPort: "true", wantFields: map[string]any{"ready": false, "enabled": true}},
		{name: "set field creates a nested path", nodeType: "core.set-field", config: `{"field":"state.level","value":9}`, message: messageWith(base, nil, nil), wantPort: "out", wantFields: map[string]any{"state": map[string]any{"level": float64(9)}}},
		{name: "select field copies into value", nodeType: "core.select-field", config: `{"field":"state.level"}`, message: messageWith(base, nil, map[string]any{"state": map[string]any{"level": 5}}), wantPort: "out", wantValue: 5, wantFields: map[string]any{"state": map[string]any{"level": 5}}},
		{name: "multiply updates a selected field", nodeType: "core.multiply", config: `{"field":"temperature","factor":2}`, message: messageWith(base, nil, map[string]any{"temperature": 3}), wantPort: "out", wantFields: map[string]any{"temperature": float64(6)}},
		{name: "divide updates value", nodeType: "core.divide", config: `{"divisor":3}`, message: messageWith(base, 9, nil), wantPort: "out", wantValue: float64(3)},
		{name: "average handles value arrays", nodeType: "core.average", config: `{}`, message: messageWith(base, []any{2, 4, 6}, nil), wantPort: "out", wantValue: float64(4)},
		{name: "clamp limits value", nodeType: "core.clamp", config: `{"minimum":0,"maximum":100}`, message: messageWith(base, 150, nil), wantPort: "out", wantValue: float64(100)},
		{name: "scale maps ranges", nodeType: "core.scale", config: `{"inputMin":0,"inputMax":100,"outputMin":0,"outputMax":1}`, message: messageWith(base, 50, nil), wantPort: "out", wantValue: float64(.5)},
		{name: "debug records an action", nodeType: "core.debug", config: `{"label":"checkpoint"}`, message: messageWith(base, "seen", nil), wantPort: "out", wantValue: "seen", wantAction: true},
	}
	covered := make(map[string]bool)
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			covered[test.nodeType] = true
			port, message, action, err := engine.handleNode(context.Background(), testNode(test.nodeType, test.config), test.message)
			if err != nil {
				t.Fatal(err)
			}
			if port != test.wantPort || action != test.wantAction || !reflect.DeepEqual(message.Value, test.wantValue) || !reflect.DeepEqual(message.Fields, test.wantFields) {
				t.Fatalf("result = port %q, action %v, value %#v, fields %#v", port, action, message.Value, message.Fields)
			}
		})
	}

	contextTypes := testContextNodes(t, engine, base)
	for _, nodeType := range contextTypes {
		covered[nodeType] = true
	}
	for _, definition := range engine.Registry().Catalog() {
		if !covered[definition.Type] {
			t.Errorf("catalog node %s has no execution test", definition.Type)
		}
	}
}

func TestNodeExecutionReportsInvalidInputs(t *testing.T) {
	engine := New(newRuntimeTestStore(Script{}, Revision{}))
	defer engine.Close()
	base := Message{OrgName: "org", ScriptID: "script", ActiveRevision: 1, InstanceKey: "Pump01"}
	tests := []struct {
		nodeType string
		config   string
		message  Message
		contains string
	}{
		{"core.compare", `{"field":"missing","operator":"==","compareTo":1}`, messageWith(base, nil, nil), `field "missing" does not exist`},
		{"core.in-range", `{"minimum":10,"maximum":0}`, messageWith(base, 5, nil), "maximum must be"},
		{"core.and", `{"fields":["ready"]}`, messageWith(base, nil, map[string]any{"ready": "yes"}), "is not boolean"},
		{"core.divide", `{"divisor":0}`, messageWith(base, 5, nil), "division by zero"},
		{"core.average", `{}`, messageWith(base, []any{1, "bad"}, nil), "must be numeric"},
		{"core.scale", `{"inputMin":1,"inputMax":1}`, messageWith(base, 5, nil), "zero width"},
	}
	for _, test := range tests {
		_, _, _, err := engine.handleNode(context.Background(), testNode(test.nodeType, test.config), test.message)
		if err == nil || !strings.Contains(err.Error(), test.contains) {
			t.Errorf("%s error = %v, want containing %q", test.nodeType, err, test.contains)
		}
	}

	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, _, _, err := engine.handleNode(cancelled, testNode("core.manual", `{}`), base); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled node error = %v", err)
	}
}

func TestBranchMessagesAndTracesAreDeeplyIsolated(t *testing.T) {
	engine := New(newRuntimeTestStore(Script{}, Revision{}))
	defer engine.Close()
	graph := NormalizeGraph(GraphDocument{
		Nodes: []GraphNode{
			testNode("core.manual", `{}`),
			{ID: "set", Type: "core.set-field", TypeVersion: 1, Config: json.RawMessage(`{"field":"device.level","value":9}`)},
			{ID: "debug", Type: "core.debug", TypeVersion: 1, Config: json.RawMessage(`{"label":"unchanged branch"}`)},
		},
		Edges: []GraphEdge{
			{ID: "to-set", From: EdgeEndpoint{NodeID: "core.manual", Port: "out"}, To: EdgeEndpoint{NodeID: "set", Port: "in"}},
			{ID: "to-debug", From: EdgeEndpoint{NodeID: "core.manual", Port: "out"}, To: EdgeEndpoint{NodeID: "debug", Port: "in"}},
		},
	})
	plan := compile(graph)
	run := &Run{}
	message := Message{Fields: map[string]any{"device": map[string]any{"level": float64(1)}}}
	if err := engine.execute(context.Background(), plan, plan.nodes["core.manual"], message, run); err != nil {
		t.Fatal(err)
	}
	var debug TraceEvent
	for _, event := range run.Trace {
		if event.NodeID == "debug" {
			debug = event
		}
	}
	if got, _ := getField(debug.Fields, "device.level"); got != float64(1) {
		t.Fatalf("sibling branch was mutated: %#v", debug.Fields)
	}
	if got, _ := getField(message.Fields, "device.level"); got != float64(1) {
		t.Fatalf("source message was mutated: %#v", message.Fields)
	}
}

func TestDebugTraceSnapshotsInputMessage(t *testing.T) {
	engine := New(newRuntimeTestStore(Script{}, Revision{}))
	defer engine.Close()
	debug := GraphNode{ID: "debug", Type: "core.debug", TypeVersion: 1, Config: json.RawMessage(`{}`)}
	plan := compile(NormalizeGraph(GraphDocument{Nodes: []GraphNode{debug}}))
	engine.nodeHandler = func(_ context.Context, _ GraphNode, message Message) (string, Message, bool, error) {
		message.Value = float64(99)
		message.Fields["stage"] = "output"
		return "out", message, true, nil
	}
	run := &Run{}
	input := Message{Value: float64(23), Fields: map[string]any{"stage": "input"}}
	if err := engine.execute(context.Background(), plan, debug, input, run); err != nil {
		t.Fatal(err)
	}
	if len(run.Trace) != 1 || run.Trace[0].Value != float64(23) || run.Trace[0].Fields["stage"] != "input" {
		t.Fatalf("debug trace did not snapshot its input: %#v", run.Trace)
	}
}

func testContextNodes(t *testing.T, engine *Engine, base Message) []string {
	t.Helper()
	set := testNode("core.set-context", `{"scope":"script","key":"threshold","source":"configured","value":{"level":4}}`)
	if _, _, _, err := engine.handleNode(context.Background(), set, base); err != nil {
		t.Fatal(err)
	}
	get := testNode("core.get-context", `{"scope":"script","key":"threshold"}`)
	_, message, _, err := engine.handleNode(context.Background(), get, base)
	if err != nil || !reflect.DeepEqual(message.Value, map[string]any{"level": float64(4)}) {
		t.Fatalf("get context = %#v, %v", message.Value, err)
	}
	increment := testNode("core.increment-context", `{"scope":"script","key":"attempts","amount":2}`)
	for _, want := range []float64{2, 4} {
		_, message, _, err = engine.handleNode(context.Background(), increment, base)
		if err != nil || message.Value != want {
			t.Fatalf("increment context = %#v, %v; want %v", message.Value, err, want)
		}
	}
	remove := testNode("core.delete-context", `{"scope":"script","key":"threshold"}`)
	if _, _, _, err = engine.handleNode(context.Background(), remove, base); err != nil {
		t.Fatal(err)
	}
	if _, _, _, err = engine.handleNode(context.Background(), get, base); err == nil {
		t.Fatal("deleted context remained readable")
	}
	return []string{"core.set-context", "core.get-context", "core.increment-context", "core.delete-context"}
}

func testNode(nodeType, config string) GraphNode {
	return GraphNode{ID: nodeType, Type: nodeType, TypeVersion: 1, Config: json.RawMessage(config)}
}

func messageWith(base Message, value any, fields map[string]any) Message {
	base.Value = value
	base.Fields = fields
	return base
}
