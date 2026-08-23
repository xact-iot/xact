// Package visualscriptnodes loads trusted operator-installed visual-script
// node plugins from plugins/visual-script-nodes at server startup.
package visualscriptnodes

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/traefik/yaegi/interp"
	"github.com/traefik/yaegi/stdlib"
	"github.com/xact-iot/xact/visualscripts"
)

const (
	pluginSubdir = "visual-script-nodes"
	manifestFile = "manifest.json"
	backendFile  = "backend.go"
)

var (
	pluginNamePattern = regexp.MustCompile(`^[a-z][a-z0-9-]*$`)
	typeNamePattern   = regexp.MustCompile(`^[a-z][a-z0-9-]*(?:\.[a-z][a-z0-9-]*)+$`)
)

type Manifest struct {
	Name    string         `json:"name"`
	Version string         `json:"version"`
	Nodes   []ManifestNode `json:"nodes"`
}

type ManifestNode struct {
	Type             string                         `json:"type"`
	TypeVersion      int                            `json:"typeVersion"`
	Name             string                         `json:"name"`
	Description      string                         `json:"description"`
	Category         string                         `json:"category"`
	Icon             string                         `json:"icon"`
	Inputs           []visualscripts.PortDefinition `json:"inputs"`
	Outputs          []visualscripts.PortDefinition `json:"outputs"`
	ParameterSchema  json.RawMessage                `json:"parameterSchema"`
	RequiredServices []string                       `json:"requiredServices"`
	FrontendModule   string                         `json:"frontendModule,omitempty"`
	OutputNode       bool                           `json:"outputNode,omitempty"`
	SimulationSafe   bool                           `json:"simulationSafe,omitempty"`
}

type LoadedPlugin struct {
	Name      string   `json:"name"`
	Version   string   `json:"version"`
	Hash      string   `json:"hash"`
	NodeTypes []string `json:"nodeTypes"`
	Directory string   `json:"-"`
}

// Load discovers plugin directories in deterministic name order. A bad plugin
// is reported without preventing independent plugins from loading.
func Load(pluginRoot string, registry *visualscripts.Registry) ([]LoadedPlugin, []error) {
	if strings.TrimSpace(pluginRoot) == "" {
		return []LoadedPlugin{}, []error{}
	}
	info, err := os.Stat(pluginRoot)
	if err != nil {
		return []LoadedPlugin{}, []error{fmt.Errorf("checking visual-script node plugin root: %w", err)}
	}
	if !info.IsDir() || info.Mode().Perm()&0o002 != 0 {
		return []LoadedPlugin{}, []error{fmt.Errorf("visual-script node plugin root must be a directory and must not be world-writable")}
	}
	root := filepath.Join(pluginRoot, pluginSubdir)
	entries, err := os.ReadDir(root)
	if err != nil {
		if os.IsNotExist(err) {
			return []LoadedPlugin{}, []error{}
		}
		return []LoadedPlugin{}, []error{fmt.Errorf("reading visual-script node plugin directory: %w", err)}
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })
	loaded := make([]LoadedPlugin, 0)
	errorsFound := make([]error, 0)
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		plugin, err := loadOne(filepath.Join(root, entry.Name()), entry.Name(), registry)
		if err != nil {
			errorsFound = append(errorsFound, err)
			continue
		}
		loaded = append(loaded, plugin)
	}
	return loaded, errorsFound
}

func loadOne(directory, directoryName string, registry *visualscripts.Registry) (LoadedPlugin, error) {
	manifestData, err := os.ReadFile(filepath.Join(directory, manifestFile))
	if err != nil {
		return LoadedPlugin{}, fmt.Errorf("visual-script node plugin %q: reading manifest: %w", directoryName, err)
	}
	backendData, err := os.ReadFile(filepath.Join(directory, backendFile))
	if err != nil {
		return LoadedPlugin{}, fmt.Errorf("visual-script node plugin %q: reading backend: %w", directoryName, err)
	}
	var manifest Manifest
	decoder := json.NewDecoder(strings.NewReader(string(manifestData)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&manifest); err != nil {
		return LoadedPlugin{}, fmt.Errorf("visual-script node plugin %q: parsing manifest: %w", directoryName, err)
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return LoadedPlugin{}, fmt.Errorf("visual-script node plugin %q: manifest must contain exactly one JSON document", directoryName)
	}
	if err := validateManifest(manifest, directoryName, directory); err != nil {
		return LoadedPlugin{}, fmt.Errorf("visual-script node plugin %q: %w", directoryName, err)
	}

	pluginHash, err := hashPluginFiles(directory, manifestData, backendData, manifest)
	if err != nil {
		return LoadedPlugin{}, fmt.Errorf("visual-script node plugin %q: hashing files: %w", directoryName, err)
	}
	backend, err := evaluateBackend(backendData)
	if err != nil {
		return LoadedPlugin{}, fmt.Errorf("visual-script node plugin %q: %w", directoryName, err)
	}
	if len(backend.registrations) != len(manifest.Nodes) {
		return LoadedPlugin{}, fmt.Errorf("manifest declares %d nodes but backend registers %d", len(manifest.Nodes), len(backend.registrations))
	}
	byKey := make(map[string]visualscripts.PluginRegistration, len(backend.registrations))
	for _, registration := range backend.registrations {
		key := nodeKey(registration.Type, registration.TypeVersion)
		if _, duplicate := byKey[key]; duplicate {
			return LoadedPlugin{}, fmt.Errorf("backend registers %s more than once", key)
		}
		byKey[key] = registration
	}

	nodes := make([]visualscripts.NodeType, 0, len(manifest.Nodes))
	nodeNames := make([]string, 0, len(manifest.Nodes))
	for _, declared := range manifest.Nodes {
		key := nodeKey(declared.Type, declared.TypeVersion)
		registration, ok := byKey[key]
		if !ok {
			return LoadedPlugin{}, fmt.Errorf("manifest node %s has no matching backend implementation", key)
		}
		definition, err := manifestDefinition(manifest, declared, pluginHash)
		if err != nil {
			return LoadedPlugin{}, err
		}
		nodes = append(nodes, visualscripts.WithDefinition(interpretedNodeType{registration: registration, backend: backend}, definition))
		nodeNames = append(nodeNames, key)
	}
	if err := registry.RegisterBatch(nodes); err != nil {
		return LoadedPlugin{}, err
	}
	sort.Strings(nodeNames)
	return LoadedPlugin{Name: manifest.Name, Version: manifest.Version, Hash: pluginHash, NodeTypes: nodeNames, Directory: directory}, nil
}

type interpretedBackend struct {
	registrations []visualscripts.PluginRegistration
	compile       func(string, string, int, string) (string, string)
	handle        func(string, string, string, string, int, string, string) (string, string)
	close         func(string, string, int, string) string
	bridge        *runtimeBridge
	gate          chan struct{}
}

type interpretedNodeType struct {
	registration visualscripts.PluginRegistration
	backend      interpretedBackend
}

func (n interpretedNodeType) Definition() visualscripts.NodeDefinition {
	return visualscripts.NodeDefinition{Type: n.registration.Type, TypeVersion: n.registration.TypeVersion}
}

func (n interpretedNodeType) Compile(config json.RawMessage, services visualscripts.CompileServices) (visualscripts.CompiledNode, error) {
	n.backend.gate <- struct{}{}
	defer func() { <-n.backend.gate }()
	instanceID := "node-" + strconv.FormatUint(n.backend.bridge.sequence.Add(1), 36)
	compiledConfig, errorText := n.backend.compile(instanceID, n.registration.Type, n.registration.TypeVersion, string(config))
	if errorText != "" {
		return nil, errors.New(errorText)
	}
	if !json.Valid([]byte(compiledConfig)) {
		return nil, fmt.Errorf("Compile returned invalid JSON")
	}
	return interpretedCompiledNode{instanceID: instanceID, registration: n.registration, backend: n.backend, config: compiledConfig, services: services}, nil
}

type interpretedCompiledNode struct {
	instanceID   string
	registration visualscripts.PluginRegistration
	backend      interpretedBackend
	config       string
	services     visualscripts.CompileServices
}

func (n interpretedCompiledNode) Handle(ctx context.Context, input visualscripts.NodeInput) ([]visualscripts.Output, error) {
	messageJSON, err := json.Marshal(input.Message)
	if err != nil {
		return nil, fmt.Errorf("encoding plugin message: %w", err)
	}
	token := n.backend.bridge.add(ctx, n.services, input.Message)
	defer n.backend.bridge.remove(token)
	select {
	case n.backend.gate <- struct{}{}:
		defer func() { <-n.backend.gate }()
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	encoded, errorText := n.backend.handle(token, n.instanceID, input.Port, n.registration.Type, n.registration.TypeVersion, n.config, string(messageJSON))
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if errorText != "" {
		return nil, errors.New(errorText)
	}
	if len(encoded) > visualscripts.MaxGraphBytes {
		return nil, fmt.Errorf("plugin output exceeds %d bytes", visualscripts.MaxGraphBytes)
	}
	var outputs []visualscripts.Output
	if err := json.Unmarshal([]byte(encoded), &outputs); err != nil {
		return nil, fmt.Errorf("decoding plugin outputs: %w", err)
	}
	return outputs, nil
}

func (n interpretedCompiledNode) Close(ctx context.Context) error {
	if n.backend.close == nil {
		return nil
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	select {
	case n.backend.gate <- struct{}{}:
		defer func() { <-n.backend.gate }()
	case <-ctx.Done():
		return ctx.Err()
	}
	if errorText := n.backend.close(n.instanceID, n.registration.Type, n.registration.TypeVersion, n.config); errorText != "" {
		return errors.New(errorText)
	}
	return nil
}

type bridgeExecution struct {
	ctx      context.Context
	services visualscripts.CompileServices
	message  visualscripts.Message
}

type runtimeBridge struct {
	sequence atomic.Uint64
	mu       sync.RWMutex
	active   map[string]bridgeExecution
}

func newRuntimeBridge() *runtimeBridge {
	return &runtimeBridge{active: make(map[string]bridgeExecution)}
}

func (b *runtimeBridge) add(ctx context.Context, services visualscripts.CompileServices, message visualscripts.Message) string {
	token := strconv.FormatUint(b.sequence.Add(1), 36)
	b.mu.Lock()
	b.active[token] = bridgeExecution{ctx: ctx, services: services, message: message}
	b.mu.Unlock()
	return token
}
func (b *runtimeBridge) remove(token string) { b.mu.Lock(); delete(b.active, token); b.mu.Unlock() }
func (b *runtimeBridge) execution(token string) (bridgeExecution, bool) {
	b.mu.RLock()
	defer b.mu.RUnlock()
	item, ok := b.active[token]
	return item, ok
}
func (b *runtimeBridge) cancelled(token string) bool {
	item, ok := b.execution(token)
	return !ok || item.ctx.Err() != nil
}
func (b *runtimeBridge) nowUnixMilli(token string) int64 {
	item, ok := b.execution(token)
	if !ok {
		return 0
	}
	if item.services.Now != nil {
		return item.services.Now().UnixMilli()
	}
	return time.Now().UnixMilli()
}
func (b *runtimeBridge) readTags(token, pattern string) (string, string) {
	item, ok := b.execution(token)
	if !ok {
		return "", "plugin execution is no longer active"
	}
	if item.services.ReadTags == nil {
		return "", "tag-read service is unavailable"
	}
	changes, err := item.services.ReadTags(item.ctx, item.message.OrgName, pattern)
	if err != nil {
		return "", err.Error()
	}
	encoded, err := json.Marshal(changes)
	if err != nil {
		return "", err.Error()
	}
	return string(encoded), ""
}
func (b *runtimeBridge) setTag(token, nodeID, path, valueJSON string) string {
	item, ok := b.execution(token)
	if !ok {
		return "plugin execution is no longer active"
	}
	if item.services.SetTag == nil {
		return "set-tag service is unavailable"
	}
	var value any
	if err := json.Unmarshal([]byte(valueJSON), &value); err != nil {
		return err.Error()
	}
	if err := item.services.SetTag(item.ctx, item.message, nodeID, path, value); err != nil {
		return err.Error()
	}
	return ""
}
func (b *runtimeBridge) sendControl(token, nodeID, device, path, valueJSON string, timeoutMillis int64) string {
	item, ok := b.execution(token)
	if !ok {
		return "plugin execution is no longer active"
	}
	if item.services.SendControl == nil {
		return "send-control service is unavailable"
	}
	var value any
	if err := json.Unmarshal([]byte(valueJSON), &value); err != nil {
		return err.Error()
	}
	if err := item.services.SendControl(item.ctx, item.message, nodeID, device, path, value, time.Duration(timeoutMillis)*time.Millisecond); err != nil {
		return err.Error()
	}
	return ""
}
func (b *runtimeBridge) sendNotification(token, nodeID, profile, severity, message, device string) string {
	item, ok := b.execution(token)
	if !ok {
		return "plugin execution is no longer active"
	}
	if item.services.SendNotification == nil {
		return "send-notification service is unavailable"
	}
	if err := item.services.SendNotification(item.ctx, item.message, nodeID, profile, severity, message, device); err != nil {
		return err.Error()
	}
	return ""
}
func (b *runtimeBridge) logEvent(token, nodeID, severity, message, device string) string {
	item, ok := b.execution(token)
	if !ok {
		return "plugin execution is no longer active"
	}
	if item.services.LogEvent == nil {
		return "log-event service is unavailable"
	}
	if err := item.services.LogEvent(item.ctx, item.message, nodeID, severity, message, device); err != nil {
		return err.Error()
	}
	return ""
}

func evaluateBackend(source []byte) (interpretedBackend, error) {
	i := interp.New(interp.Options{})
	bridge := newRuntimeBridge()
	if err := i.Use(stdlib.Symbols); err != nil {
		return interpretedBackend{}, fmt.Errorf("loading Yaegi standard library: %w", err)
	}
	if err := i.Use(visualScriptSymbols(bridge)); err != nil {
		return interpretedBackend{}, fmt.Errorf("loading XACT node API: %w", err)
	}
	if _, err := i.Eval(string(source)); err != nil {
		return interpretedBackend{}, fmt.Errorf("evaluating backend.go: %w", err)
	}
	value, err := i.Eval("visualscriptplugin.Register")
	if err != nil {
		return interpretedBackend{}, fmt.Errorf("backend.go must declare package visualscriptplugin and Register, Compile, and Handle functions: %w", err)
	}
	register, ok := value.Interface().(func() string)
	if !ok {
		return interpretedBackend{}, fmt.Errorf("Register has type %T, want func() string", value.Interface())
	}
	compileValue, err := i.Eval("visualscriptplugin.Compile")
	if err != nil {
		return interpretedBackend{}, fmt.Errorf("resolving Compile: %w", err)
	}
	compile, ok := compileValue.Interface().(func(string, string, int, string) (string, string))
	if !ok {
		return interpretedBackend{}, fmt.Errorf("Compile has an incompatible signature")
	}
	handleValue, err := i.Eval("visualscriptplugin.Handle")
	if err != nil {
		return interpretedBackend{}, fmt.Errorf("resolving Handle: %w", err)
	}
	handle, ok := handleValue.Interface().(func(string, string, string, string, int, string, string) (string, string))
	if !ok {
		return interpretedBackend{}, fmt.Errorf("Handle has an incompatible signature")
	}
	var closeFn func(string, string, int, string) string
	if closeValue, closeErr := i.Eval("visualscriptplugin.Close"); closeErr == nil {
		closeFn, ok = closeValue.Interface().(func(string, string, int, string) string)
		if !ok {
			return interpretedBackend{}, fmt.Errorf("Close has an incompatible signature")
		}
	}
	var registrations []visualscripts.PluginRegistration
	if err := json.Unmarshal([]byte(register()), &registrations); err != nil {
		return interpretedBackend{}, fmt.Errorf("decoding Register result: %w", err)
	}
	return interpretedBackend{registrations: registrations, compile: compile, handle: handle, close: closeFn, bridge: bridge, gate: make(chan struct{}, 1)}, nil
}

func visualScriptSymbols(bridge *runtimeBridge) interp.Exports {
	return interp.Exports{
		"github.com/xact-iot/xact/visualscripts/visualscripts": {
			"Cancelled":        reflect.ValueOf(bridge.cancelled),
			"NowUnixMilli":     reflect.ValueOf(bridge.nowUnixMilli),
			"ReadTags":         reflect.ValueOf(bridge.readTags),
			"SetTag":           reflect.ValueOf(bridge.setTag),
			"SendControl":      reflect.ValueOf(bridge.sendControl),
			"SendNotification": reflect.ValueOf(bridge.sendNotification),
			"LogEvent":         reflect.ValueOf(bridge.logEvent),
		},
	}
}

func validateManifest(manifest Manifest, directoryName, directory string) error {
	manifest.Name = strings.TrimSpace(manifest.Name)
	if !pluginNamePattern.MatchString(manifest.Name) || manifest.Name != directoryName {
		return fmt.Errorf("manifest name must match directory %q and use lowercase letters, digits, or hyphens", directoryName)
	}
	if strings.TrimSpace(manifest.Version) == "" {
		return fmt.Errorf("manifest version is required")
	}
	if len(manifest.Nodes) == 0 {
		return fmt.Errorf("manifest must declare at least one node")
	}
	seen := make(map[string]struct{}, len(manifest.Nodes))
	for _, node := range manifest.Nodes {
		key := nodeKey(node.Type, node.TypeVersion)
		if !typeNamePattern.MatchString(node.Type) || strings.HasPrefix(node.Type, "core.") {
			return fmt.Errorf("node type %q must be a namespaced non-core stable type", node.Type)
		}
		if node.TypeVersion < 1 || strings.TrimSpace(node.Name) == "" || strings.TrimSpace(node.Category) == "" {
			return fmt.Errorf("node %s requires a positive version, display name, and category", key)
		}
		if node.Category == "Triggers" {
			return fmt.Errorf("node %s uses the trigger category; executable trigger plugins require a separate lifecycle contract", key)
		}
		if _, duplicate := seen[key]; duplicate {
			return fmt.Errorf("node %s is declared more than once", key)
		}
		seen[key] = struct{}{}
		if err := validatePorts(node.Inputs, "input", key); err != nil {
			return err
		}
		if err := validatePorts(node.Outputs, "output", key); err != nil {
			return err
		}
		if _, err := parametersFromSchema(node.ParameterSchema); err != nil {
			return fmt.Errorf("node %s parameterSchema: %w", key, err)
		}
		if node.RequiredServices == nil {
			return fmt.Errorf("node %s requiredServices must be present (use [] when none are needed)", key)
		}
		for _, service := range node.RequiredServices {
			switch service {
			case "clock", "tag-read", "tag-write", "device-control", "notifications", "event-log":
			default:
				return fmt.Errorf("node %s declares unsupported required service %q", key, service)
			}
		}
		if node.FrontendModule != "" {
			if node.FrontendModule != "editor.js" {
				return fmt.Errorf("node %s frontendModule must be editor.js", key)
			}
			info, err := os.Stat(filepath.Join(directory, node.FrontendModule))
			if err != nil || info.IsDir() {
				return fmt.Errorf("node %s frontend module %q is missing", key, node.FrontendModule)
			}
		}
	}
	return nil
}

func validatePorts(ports []visualscripts.PortDefinition, kind, key string) error {
	seen := make(map[string]struct{}, len(ports))
	for _, port := range ports {
		if strings.TrimSpace(port.Name) == "" || strings.TrimSpace(port.Label) == "" || strings.TrimSpace(port.DataType) == "" {
			return fmt.Errorf("node %s has an incomplete %s port", key, kind)
		}
		if _, duplicate := seen[port.Name]; duplicate {
			return fmt.Errorf("node %s repeats %s port %q", key, kind, port.Name)
		}
		seen[port.Name] = struct{}{}
	}
	return nil
}

func manifestDefinition(manifest Manifest, node ManifestNode, hash string) (visualscripts.NodeDefinition, error) {
	parameters, err := parametersFromSchema(node.ParameterSchema)
	if err != nil {
		return visualscripts.NodeDefinition{}, fmt.Errorf("node %s parameterSchema: %w", nodeKey(node.Type, node.TypeVersion), err)
	}
	editorModule := ""
	if node.FrontendModule != "" {
		editorModule = fmt.Sprintf("/plugins/%s/%s/%s", pluginSubdir, manifest.Name, node.FrontendModule)
	}
	return visualscripts.NodeDefinition{
		Type: node.Type, TypeVersion: node.TypeVersion, Name: node.Name,
		Description: node.Description, Category: node.Category, Icon: node.Icon,
		Inputs: node.Inputs, Outputs: node.Outputs, Parameters: parameters,
		RequiredCaps: node.RequiredServices, Available: true,
		OutputNode: node.OutputNode, SimulationSafe: node.SimulationSafe,
		ParameterSchema: append(json.RawMessage(nil), node.ParameterSchema...),
		EditorModule:    editorModule, PluginName: manifest.Name,
		PluginVersion: manifest.Version, PluginHash: hash,
	}, nil
}

type schemaDocument struct {
	Type       string                    `json:"type"`
	Properties map[string]schemaProperty `json:"properties"`
	Required   []string                  `json:"required"`
}

type schemaProperty struct {
	Type        string          `json:"type"`
	Title       string          `json:"title"`
	Description string          `json:"description"`
	Format      string          `json:"format"`
	UIType      string          `json:"x-ui-type"`
	Enum        []string        `json:"enum"`
	Default     json.RawMessage `json:"default"`
}

func parametersFromSchema(raw json.RawMessage) ([]visualscripts.ParameterDefinition, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return nil, fmt.Errorf("an object JSON Schema is required")
	}
	var schema schemaDocument
	if err := json.Unmarshal(raw, &schema); err != nil {
		return nil, err
	}
	if schema.Type != "object" {
		return nil, fmt.Errorf("root type must be object")
	}
	required := make(map[string]bool, len(schema.Required))
	for _, name := range schema.Required {
		if _, exists := schema.Properties[name]; !exists {
			return nil, fmt.Errorf("required property %q is not declared", name)
		}
		required[name] = true
	}
	names := make([]string, 0, len(schema.Properties))
	for name := range schema.Properties {
		names = append(names, name)
	}
	sort.Strings(names)
	parameters := make([]visualscripts.ParameterDefinition, 0, len(names))
	for _, name := range names {
		property := schema.Properties[name]
		parameterType := property.UIType
		if parameterType == "" && property.Format == "tag-path" {
			parameterType = "tag-path"
		}
		if parameterType == "" && len(property.Enum) > 0 {
			parameterType = "select"
		}
		if parameterType == "" {
			switch property.Type {
			case "string":
				parameterType = "string"
			case "number", "integer":
				parameterType = "number"
			case "boolean":
				parameterType = "boolean"
			case "object", "array":
				parameterType = "json"
			default:
				return nil, fmt.Errorf("property %q has unsupported type %q", name, property.Type)
			}
		}
		if parameterType != "string" && parameterType != "number" && parameterType != "boolean" && parameterType != "select" && parameterType != "json" && parameterType != "tag-path" {
			return nil, fmt.Errorf("property %q has unsupported x-ui-type %q", name, parameterType)
		}
		label := strings.TrimSpace(property.Title)
		if label == "" {
			label = name
		}
		parameter := visualscripts.ParameterDefinition{Name: name, Label: label, Type: parameterType, Description: property.Description, Required: required[name], Options: property.Enum}
		if len(property.Default) > 0 && string(property.Default) != "null" {
			if err := json.Unmarshal(property.Default, &parameter.Default); err != nil {
				return nil, fmt.Errorf("property %q default: %w", name, err)
			}
		}
		parameters = append(parameters, parameter)
	}
	return parameters, nil
}

func hashPluginFiles(directory string, manifest, backend []byte, parsed Manifest) (string, error) {
	hash := sha256.New()
	for _, item := range []struct {
		name string
		data []byte
	}{{manifestFile, manifest}, {backendFile, backend}} {
		hash.Write([]byte(item.name))
		hash.Write([]byte{0})
		hash.Write(item.data)
		hash.Write([]byte{0})
	}
	editorNames := make(map[string]struct{})
	for _, node := range parsed.Nodes {
		if node.FrontendModule != "" {
			editorNames[node.FrontendModule] = struct{}{}
		}
	}
	names := make([]string, 0, len(editorNames))
	for name := range editorNames {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		data, err := os.ReadFile(filepath.Join(directory, name))
		if err != nil {
			return "", err
		}
		hash.Write([]byte(name))
		hash.Write([]byte{0})
		hash.Write(data)
		hash.Write([]byte{0})
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

func nodeKey(nodeType string, version int) string { return fmt.Sprintf("%s@%d", nodeType, version) }
