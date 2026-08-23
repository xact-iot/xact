package visualscripts

import (
	"context"
	"encoding/json"
	"errors"
	"time"
)

// NodeType is the executable contract shared by built-in and operator-installed
// visual-script nodes. Plugin implementations are compiled once per graph
// revision rather than interpreting configuration for every message.
type NodeType interface {
	Definition() NodeDefinition
	Compile(config json.RawMessage, services CompileServices) (CompiledNode, error)
}

// CompiledNode handles messages for one configured graph node. Implementations
// must honor cancellation. Close is called during engine shutdown.
type CompiledNode interface {
	Handle(context.Context, Message) ([]Output, error)
	Close(context.Context) error
}

// CompileServices is the narrow service surface available to trusted node
// plugins. It intentionally excludes the database, HTTP server, and RTDB tree.
type CompileServices struct {
	Now              func() time.Time
	ReadTags         func(context.Context, string, string) ([]TagChange, error)
	SetTag           func(context.Context, Message, string, string, any) error
	SendControl      func(context.Context, Message, string, string, string, any, time.Duration) error
	SendNotification func(context.Context, Message, string, string, string, string, string) error
	LogEvent         func(context.Context, Message, string, string, string, string) error
}

// PluginRegistration is returned by a Yaegi backend's top-level Register
// function. The loader binds each declared type/version to the manifest-owned
// definition and adapts the backend's Compile/Handle functions to NodeType.
type PluginRegistration struct {
	Type        string `json:"type"`
	TypeVersion int    `json:"typeVersion"`
}

func compileServices(services RuntimeServices) CompileServices {
	return CompileServices{
		Now:              services.Now,
		ReadTags:         services.ReadTags,
		SetTag:           services.SetTag,
		SendControl:      services.SendControl,
		SendNotification: services.SendNotification,
		LogEvent:         services.LogEvent,
	}
}

// PluginNode is a callback-backed NodeType adapter. It is useful for native
// integrations and tests; the Yaegi loader adapts its JSON-only boundary to
// the same NodeType contract.
type PluginNode struct {
	DefinitionValue NodeDefinition
	CompileFunc     func(json.RawMessage, CompileServices) (PluginHandler, error)
}

func (n PluginNode) Definition() NodeDefinition { return n.DefinitionValue }

func (n PluginNode) Compile(config json.RawMessage, services CompileServices) (CompiledNode, error) {
	if n.CompileFunc == nil {
		return nil, errors.New("plugin node has no compile function")
	}
	handler, err := n.CompileFunc(config, services)
	if err != nil {
		return nil, err
	}
	return handler, nil
}

// PluginHandler is the Yaegi-friendly concrete CompiledNode returned by a
// PluginNode compile callback.
type PluginHandler struct {
	HandleFunc func(context.Context, Message) ([]Output, error)
	CloseFunc  func(context.Context) error
}

func (h PluginHandler) Handle(ctx context.Context, msg Message) ([]Output, error) {
	if h.HandleFunc == nil {
		return nil, errors.New("plugin node has no handle function")
	}
	return h.HandleFunc(ctx, msg)
}

func (h PluginHandler) Close(ctx context.Context) error {
	if h.CloseFunc == nil {
		return nil
	}
	return h.CloseFunc(ctx)
}

type manifestNodeType struct {
	definition NodeDefinition
	delegate   NodeType
}

func (n manifestNodeType) Definition() NodeDefinition { return n.definition }
func (n manifestNodeType) Compile(config json.RawMessage, services CompileServices) (CompiledNode, error) {
	return n.delegate.Compile(config, services)
}

// WithDefinition binds operator-reviewed manifest metadata to executable
// behavior. Plugin source may identify its type/version but cannot replace the
// catalog schema or frontend-module association declared by the manifest.
func WithDefinition(node NodeType, definition NodeDefinition) NodeType {
	return manifestNodeType{definition: definition, delegate: node}
}
