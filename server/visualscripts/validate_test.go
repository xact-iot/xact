package visualscripts

import (
	"encoding/json"
	"testing"
)

func node(id, nodeType string, config string) GraphNode {
	return GraphNode{ID: id, Type: nodeType, TypeVersion: 1, Config: json.RawMessage(config)}
}

func TestValidateGraphAcceptsManualPureFlow(t *testing.T) {
	graph := GraphDocument{Nodes: []GraphNode{
		node("manual", "core.manual", `{}`),
		node("set", "core.set-field", `{"field":"temperature","value":21}`),
		node("compare", "core.compare", `{"field":"temperature","operator":">=","compareTo":20}`),
		node("debug", "core.debug", `{}`),
	}, Edges: []GraphEdge{
		{ID: "e1", From: EdgeEndpoint{NodeID: "manual", Port: "out"}, To: EdgeEndpoint{NodeID: "set", Port: "in"}},
		{ID: "e2", From: EdgeEndpoint{NodeID: "set", Port: "out"}, To: EdgeEndpoint{NodeID: "compare", Port: "in"}},
		{ID: "e3", From: EdgeEndpoint{NodeID: "compare", Port: "true"}, To: EdgeEndpoint{NodeID: "debug", Port: "in"}},
	}}
	result := ValidateGraph(NewRegistry(), graph)
	if !result.Valid {
		t.Fatalf("expected valid graph, got %#v", result.Diagnostics)
	}
	if result.Diagnostics == nil {
		t.Fatal("expected successful validation diagnostics to be an empty array, not nil")
	}
	if result.GraphHash == "" {
		t.Fatal("expected a stable graph hash")
	}
	if result.Graph.Settings.MaxConcurrency != 1 || result.Graph.Settings.QueueLimit != 100 {
		t.Fatalf("defaults not normalized: %#v", result.Graph.Settings)
	}
}

func TestValidateGraphRejectsCyclesAndBadPorts(t *testing.T) {
	graph := GraphDocument{Nodes: []GraphNode{node("manual", "core.manual", `{}`), node("a", "core.debug", `{}`), node("b", "core.debug", `{}`)}, Edges: []GraphEdge{
		{ID: "e1", From: EdgeEndpoint{NodeID: "manual", Port: "missing"}, To: EdgeEndpoint{NodeID: "a", Port: "in"}},
		{ID: "e2", From: EdgeEndpoint{NodeID: "a", Port: "out"}, To: EdgeEndpoint{NodeID: "b", Port: "in"}},
		{ID: "e3", From: EdgeEndpoint{NodeID: "b", Port: "out"}, To: EdgeEndpoint{NodeID: "a", Port: "in"}},
	}}
	result := ValidateGraph(NewRegistry(), graph)
	if result.Valid {
		t.Fatal("expected graph to be invalid")
	}
	wanted := map[string]bool{"invalid_output_port": false, "cycle_detected": false}
	for _, item := range result.Diagnostics {
		if _, ok := wanted[item.Code]; ok {
			wanted[item.Code] = true
		}
	}
	for code, found := range wanted {
		if !found {
			t.Errorf("missing diagnostic %s: %#v", code, result.Diagnostics)
		}
	}
}

func TestValidateGraphRejectsInvalidNumericConfiguration(t *testing.T) {
	graph := GraphDocument{Nodes: []GraphNode{node("manual", "core.manual", `{}`), node("divide", "core.divide", `{"divisor":0}`)}, Edges: []GraphEdge{{ID: "e", From: EdgeEndpoint{NodeID: "manual", Port: "out"}, To: EdgeEndpoint{NodeID: "divide", Port: "in"}}}}
	result := ValidateGraph(NewRegistry(), graph)
	if result.Valid {
		t.Fatal("division by zero must fail validation")
	}
}
