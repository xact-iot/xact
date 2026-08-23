package visualscripts

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func TestCompiledPluginNodeRunsThroughEngineAndPreservesAuthority(t *testing.T) {
	engine := New(newRuntimeTestStore(Script{}, Revision{}))
	defer engine.Close()
	plugin := PluginNode{
		DefinitionValue: NodeDefinition{Type: "acme.replace", TypeVersion: 1, Name: "Replace", Category: "Custom", Inputs: []PortDefinition{{Name: "in", Label: "Input", DataType: "message"}}, Outputs: []PortDefinition{{Name: "out", Label: "Output", DataType: "message"}}, Available: true},
		CompileFunc: func(json.RawMessage, CompileServices) (PluginHandler, error) {
			return PluginHandler{HandleFunc: func(_ context.Context, msg Message) ([]Output, error) {
				msg.OrgName = "attacker"
				msg.Value = "changed"
				return []Output{{Port: "out", Message: msg}}, nil
			}}, nil
		},
	}
	if err := engine.Registry().Register(plugin); err != nil {
		t.Fatal(err)
	}
	graph := NormalizeGraph(GraphDocument{Nodes: []GraphNode{
		{ID: "manual", Type: "core.manual", TypeVersion: 1, Config: json.RawMessage(`{}`)},
		{ID: "plugin", Type: "acme.replace", TypeVersion: 1, Config: json.RawMessage(`{}`)},
		{ID: "debug", Type: "core.debug", TypeVersion: 1, Config: json.RawMessage(`{"label":"result"}`)},
	}, Edges: []GraphEdge{
		{ID: "a", From: EdgeEndpoint{NodeID: "manual", Port: "out"}, To: EdgeEndpoint{NodeID: "plugin", Port: "in"}},
		{ID: "b", From: EdgeEndpoint{NodeID: "plugin", Port: "out"}, To: EdgeEndpoint{NodeID: "debug", Port: "in"}},
	}})
	plan, err := compileWithRegistry(graph, false, engine.Registry(), engine.services)
	if err != nil {
		t.Fatal(err)
	}
	defer plan.close(context.Background())
	run := &Run{}
	input := Message{ID: "message", OrgName: "acme", ScriptID: "script", ActiveRevision: 1, Value: "original"}
	if err := engine.execute(context.Background(), plan, graph.Nodes[0], input, run); err != nil {
		t.Fatal(err)
	}
	if len(run.Trace) != 1 || run.Trace[0].Value != "changed" {
		t.Fatalf("trace = %#v", run.Trace)
	}
	output := preserveAuthoritativeMessage(input, Message{OrgName: "attacker"})
	if output.OrgName != "acme" || output.ID != "message" {
		t.Fatalf("authority was not preserved: %#v", output)
	}
}

func TestPluginCompilationRejectsUnavailableRequiredService(t *testing.T) {
	registry := NewRegistry()
	node := PluginNode{
		DefinitionValue: NodeDefinition{Type: "acme.writer", TypeVersion: 1, Name: "Writer", Category: "Custom", RequiredCaps: []string{"tag-write"}, Available: true},
		CompileFunc:     func(json.RawMessage, CompileServices) (PluginHandler, error) { return PluginHandler{}, nil },
	}
	if err := registry.Register(node); err != nil {
		t.Fatal(err)
	}
	graph := NormalizeGraph(GraphDocument{Nodes: []GraphNode{{ID: "writer", Type: "acme.writer", TypeVersion: 1, Config: json.RawMessage(`{}`)}}})
	if _, err := compileWithRegistry(graph, false, registry, RuntimeServices{}); err == nil || !strings.Contains(err.Error(), "tag-write") {
		t.Fatalf("missing service compilation error = %v", err)
	}
}
