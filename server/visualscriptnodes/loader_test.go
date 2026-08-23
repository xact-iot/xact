package visualscriptnodes

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/xact-iot/xact/visualscripts"
)

const testBackend = `package visualscriptplugin

import (
	"encoding/json"
	"strings"

	"github.com/xact-iot/xact/visualscripts"
)

func Register() string {
	return ` + "`" + `[{"Type":"acme.uppercase","TypeVersion":1}]` + "`" + `
}

func Compile(_, _ string, _ int, config string) (string, string) {
	var settings struct { Prefix string ` + "`json:\"prefix\"`" + ` }
	if err := json.Unmarshal([]byte(config), &settings); err != nil { return "", err.Error() }
	encoded, err := json.Marshal(settings)
	if err != nil { return "", err.Error() }
	return string(encoded), ""
}

func Handle(executionID, _, _, _ string, _ int, config, message string) (string, string) {
	if visualscripts.Cancelled(executionID) { return "", "cancelled" }
	var settings struct { Prefix string ` + "`json:\"prefix\"`" + ` }
	if err := json.Unmarshal([]byte(config), &settings); err != nil { return "", err.Error() }
	var msg map[string]interface{}
	if err := json.Unmarshal([]byte(message), &msg); err != nil { return "", err.Error() }
	if value, ok := msg["value"].(string); ok { msg["value"] = settings.Prefix + strings.ToUpper(value) }
	encoded, err := json.Marshal([]interface{}{map[string]interface{}{"port":"out", "message":msg}})
	if err != nil { return "", err.Error() }
	return string(encoded), ""
}
`

func TestLoadRegistersExecutableNodeAndEditorMetadata(t *testing.T) {
	root := t.TempDir()
	writeTestPlugin(t, root, "acme", "acme.uppercase", testBackend, true)
	registry := visualscripts.NewRegistry()

	loaded, errorsFound := Load(root, registry)
	if len(errorsFound) != 0 || len(loaded) != 1 {
		t.Fatalf("Load = %#v, errors %#v", loaded, errorsFound)
	}
	if loaded[0].Name != "acme" || loaded[0].Version != "1.2.3" || len(loaded[0].Hash) != 64 {
		t.Fatalf("loaded metadata = %#v", loaded[0])
	}
	definition, ok := registry.DefinitionVersion("acme.uppercase", 1)
	if !ok || definition.PluginName != "acme" || definition.PluginVersion != "1.2.3" || definition.PluginHash != loaded[0].Hash {
		t.Fatalf("registered definition = %#v, %v", definition, ok)
	}
	if definition.EditorModule != "/plugins/visual-script-nodes/acme/editor.js" || !registry.AllowsEditorModule("acme", definition.EditorModule) {
		t.Fatalf("editor module was not bound to registered node: %#v", definition)
	}
	if len(definition.Parameters) != 1 || definition.Parameters[0].Name != "prefix" || definition.Parameters[0].Default != "> " || !definition.Parameters[0].Required {
		t.Fatalf("schema-derived parameters = %#v", definition.Parameters)
	}

	implementation, ok := registry.NodeType("acme.uppercase", 1)
	if !ok {
		t.Fatal("registered implementation missing")
	}
	compiled, err := implementation.Compile(json.RawMessage(`{"prefix":"# "}`), visualscripts.CompileServices{})
	if err != nil {
		t.Fatal(err)
	}
	outputs, err := compiled.Handle(context.Background(), visualscripts.NodeInput{Port: "in", Message: visualscripts.Message{Value: "ready"}})
	if err != nil || len(outputs) != 1 || outputs[0].Port != "out" || outputs[0].Message.Value != "# READY" {
		t.Fatalf("plugin output = %#v, %v", outputs, err)
	}
}

func TestLoadRejectsDuplicateRegistrationAtomically(t *testing.T) {
	root := t.TempDir()
	writeTestPlugin(t, root, "alpha", "acme.uppercase", testBackend, false)
	writeTestPlugin(t, root, "beta", "acme.uppercase", testBackend, false)
	registry := visualscripts.NewRegistry()
	loaded, errorsFound := Load(root, registry)
	if len(loaded) != 1 || len(errorsFound) != 1 || !strings.Contains(errorsFound[0].Error(), "already registered") {
		t.Fatalf("Load duplicates = %#v, %#v", loaded, errorsFound)
	}
}

func TestLoadRejectsManifestBackendMismatch(t *testing.T) {
	root := t.TempDir()
	writeTestPlugin(t, root, "acme", "acme.different", testBackend, false)
	loaded, errorsFound := Load(root, visualscripts.NewRegistry())
	if len(loaded) != 0 || len(errorsFound) != 1 || !strings.Contains(errorsFound[0].Error(), "no matching backend") {
		t.Fatalf("Load mismatch = %#v, %#v", loaded, errorsFound)
	}
}

func TestLoadRejectsTrailingManifestDocument(t *testing.T) {
	root := t.TempDir()
	writeTestPlugin(t, root, "acme", "acme.uppercase", testBackend, false)
	manifestPath := filepath.Join(root, pluginSubdir, "acme", manifestFile)
	manifest, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatal(err)
	}
	manifest = append(manifest, []byte(` {}`)...)
	if err := os.WriteFile(manifestPath, manifest, 0o644); err != nil {
		t.Fatal(err)
	}
	loaded, errorsFound := Load(root, visualscripts.NewRegistry())
	if len(loaded) != 0 || len(errorsFound) != 1 || !strings.Contains(errorsFound[0].Error(), "exactly one JSON document") {
		t.Fatalf("Load trailing manifest = %#v, %#v", loaded, errorsFound)
	}
}

func TestExampleMaxTwoPluginLifecycle(t *testing.T) {
	// The checked-in example lives under server/plugins/visual-script-nodes.
	// Passing that installable-plugin root to Load exercises the same discovery
	// path used for operator plugins without mixing loader code into it.
	registry := visualscripts.NewRegistry()
	loaded, errorsFound := Load(filepath.Join("..", "plugins"), registry)
	if len(errorsFound) != 0 {
		t.Fatalf("loading examples: %#v", errorsFound)
	}
	found := false
	for _, plugin := range loaded {
		if plugin.Name == "example-max-two" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("example-max-two was not discovered: %#v", loaded)
	}

	implementation, ok := registry.NodeType("example.max-two", 1)
	if !ok {
		t.Fatal("example.max-two@1 was not registered")
	}
	definition := implementation.Definition()
	if len(definition.Inputs) != 2 || definition.Inputs[0].Name != "first" || definition.Inputs[1].Name != "second" {
		t.Fatalf("example inputs = %#v", definition.Inputs)
	}
	compiled, err := implementation.Compile(json.RawMessage(`{}`), visualscripts.CompileServices{})
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	defer func() {
		if err := compiled.Close(context.Background()); err != nil {
			t.Errorf("Close: %v", err)
		}
	}()

	for _, test := range []struct {
		name      string
		first     float64
		second    float64
		want      float64
		reverseIn bool
	}{
		{name: "second is larger", first: -4, second: 12.5, want: 12.5},
		{name: "first is larger and arrives last", first: 8, second: 3, want: 8, reverseIn: true},
		{name: "equal values", first: 7, second: 7, want: 7},
	} {
		t.Run(test.name, func(t *testing.T) {
			inputs := []visualscripts.NodeInput{
				{Port: "first", Message: visualscripts.Message{ID: test.name, Value: test.first}},
				{Port: "second", Message: visualscripts.Message{ID: test.name, Value: test.second}},
			}
			if test.reverseIn {
				inputs[0], inputs[1] = inputs[1], inputs[0]
			}
			outputs, err := compiled.Handle(context.Background(), inputs[0])
			if err != nil {
				t.Fatal(err)
			}
			if len(outputs) != 0 {
				t.Fatalf("first input emitted early: %#v", outputs)
			}
			outputs, err = compiled.Handle(context.Background(), inputs[1])
			if err != nil {
				t.Fatal(err)
			}
			if len(outputs) != 1 || outputs[0].Port != "out" || outputs[0].Message.Value != test.want {
				t.Fatalf("outputs = %#v, want value %v", outputs, test.want)
			}
		})
	}

	if _, err := compiled.Handle(context.Background(), visualscripts.NodeInput{Port: "first", Message: visualscripts.Message{ID: "bad", Value: "not a number"}}); err == nil || !strings.Contains(err.Error(), "must be a number") {
		t.Fatalf("non-numeric input error = %v", err)
	}
}

func writeTestPlugin(t *testing.T, root, name, nodeType, backend string, editor bool) {
	t.Helper()
	directory := filepath.Join(root, pluginSubdir, name)
	if err := os.MkdirAll(directory, 0o755); err != nil {
		t.Fatal(err)
	}
	frontend := ""
	if editor {
		frontend = `,"frontendModule":"editor.js"`
		if err := os.WriteFile(filepath.Join(directory, "editor.js"), []byte(`export function mount() {}`), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	manifest := `{"name":` + quote(name) + `,"version":"1.2.3","nodes":[{"type":` + quote(nodeType) + `,"typeVersion":1,"name":"Uppercase","description":"Uppercases text","category":"Custom","icon":"A","inputs":[{"name":"in","label":"Input","dataType":"message"}],"outputs":[{"name":"out","label":"Output","dataType":"message"}],"parameterSchema":{"type":"object","properties":{"prefix":{"type":"string","title":"Prefix","default":"> "}},"required":["prefix"]},"requiredServices":[]` + frontend + `}]}`
	if err := os.WriteFile(filepath.Join(directory, manifestFile), []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(directory, backendFile), []byte(backend), 0o644); err != nil {
		t.Fatal(err)
	}
}

func quote(value string) string { encoded, _ := json.Marshal(value); return string(encoded) }
