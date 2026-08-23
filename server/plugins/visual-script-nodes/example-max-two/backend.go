// Package visualscriptplugin is evaluated by Yaegi when XACT starts.
//
// This example implements a pure transform node. It reads two configured
// numeric fields from each incoming message, puts the larger number in
// message.value, and routes the updated message through the "out" port.
//
// Visual-script plugins are trusted, in-process server code. They are loaded
// only at startup; there is no upload or hot-reload lifecycle.
package visualscriptplugin

import (
	"encoding/json"
	"fmt"
	"strings"
)

const (
	nodeType    = "example.max-two"
	nodeVersion = 1
)

// compiledConfig is the immutable state produced by Compile for one configured
// graph node. A real plugin can put pre-parsed expressions, lookup tables, or
// other read-only state here so Handle does not repeat that work per message.
//
// The Yaegi boundary uses JSON strings, so Compile serializes this value and
// Handle deserializes it. Keep compiled state JSON-compatible.
type compiledConfig struct {
	FirstField  string `json:"firstField"`
	SecondField string `json:"secondField"`
}

// message contains only the payload fields this plugin changes. XACT restores
// authoritative identity and tenancy fields (message ID, organisation, script,
// revision, trigger, and timestamp) after Handle returns, so plugin code cannot
// replace them.
type message struct {
	Value  interface{}            `json:"value,omitempty"`
	Fields map[string]interface{} `json:"fields,omitempty"`
}

// output is one routed result. Its Port must match an output port declared in
// manifest.json. A handler may return zero, one, or several outputs, subject to
// the engine's fan-out limits.
type output struct {
	Port    string  `json:"port"`
	Message message `json:"message"`
}

// Register is the first lifecycle function called during server startup.
//
// It declares the stable type/version pairs implemented by this backend. XACT
// cross-checks this list against manifest.json and rejects missing, extra, or
// duplicate registrations. Register should be deterministic and should not
// start goroutines or acquire per-node resources.
func Register() string {
	return `[{"type":"example.max-two","typeVersion":1}]`
}

// Compile runs once for every instance of this node in a compiled graph plan.
//
// This is the right place to parse and validate node configuration and prepare
// immutable state. Returning a non-empty errorMessage rejects deployment before
// XACT swaps the active plan; the previously active plan remains in service.
// The nodeType and typeVersion checks make this backend safe to extend with
// additional node implementations later.
func Compile(requestedType string, requestedVersion int, configJSON string) (compiledJSON, errorMessage string) {
	if requestedType != nodeType || requestedVersion != nodeVersion {
		return "", fmt.Sprintf("unsupported node %s version %d", requestedType, requestedVersion)
	}

	var config compiledConfig
	if err := json.Unmarshal([]byte(configJSON), &config); err != nil {
		return "", "invalid Max of Two configuration: " + err.Error()
	}
	config.FirstField = strings.TrimSpace(config.FirstField)
	config.SecondField = strings.TrimSpace(config.SecondField)
	if config.FirstField == "" || config.SecondField == "" {
		return "", "firstField and secondField are required"
	}
	if config.FirstField == config.SecondField {
		return "", "firstField and secondField must identify different inputs"
	}

	encoded, err := json.Marshal(config)
	if err != nil {
		return "", "encoding compiled configuration: " + err.Error()
	}
	return string(encoded), ""
}

// Handle runs once for each message routed into the compiled node.
//
// executionID identifies this invocation to XACT's token-scoped service bridge.
// This pure calculation is short and needs no services. A long-running handler
// should periodically call visualscripts.Cancelled(executionID), and handlers
// performing I/O should use only services declared in requiredServices.
//
// Returning a non-empty errorMessage fails the current message under the
// script's normal error policy. Returning an empty output list deliberately
// consumes a message without routing it onward.
func Handle(executionID, requestedType string, requestedVersion int, compiledJSON, messageJSON string) (outputsJSON, errorMessage string) {
	// This example does not need executionID, but naming it documents where the
	// cancellation/service token is supplied in the lifecycle.
	_ = executionID

	if requestedType != nodeType || requestedVersion != nodeVersion {
		return "", fmt.Sprintf("unsupported node %s version %d", requestedType, requestedVersion)
	}

	var config compiledConfig
	if err := json.Unmarshal([]byte(compiledJSON), &config); err != nil {
		return "", "invalid compiled Max of Two state: " + err.Error()
	}
	var msg message
	if err := json.Unmarshal([]byte(messageJSON), &msg); err != nil {
		return "", "invalid input message: " + err.Error()
	}

	first, err := numericField(msg.Fields, config.FirstField)
	if err != nil {
		return "", err.Error()
	}
	second, err := numericField(msg.Fields, config.SecondField)
	if err != nil {
		return "", err.Error()
	}
	if second > first {
		first = second
	}
	msg.Value = first

	encoded, err := json.Marshal([]output{{Port: "out", Message: msg}})
	if err != nil {
		return "", "encoding Max of Two output: " + err.Error()
	}
	return string(encoded), ""
}

// Close is the final lifecycle function for a compiled node instance.
//
// XACT calls it when the owning execution plan is closed (for example during
// server shutdown). Plugins should release files, clients, subscriptions, and
// goroutines created for that compiled instance. This pure node owns no such
// resources, so successful cleanup is represented by an empty error string.
// Close is optional, but included here to demonstrate the complete lifecycle.
func Close(requestedType string, requestedVersion int, compiledJSON string) (errorMessage string) {
	if requestedType != nodeType || requestedVersion != nodeVersion {
		return fmt.Sprintf("unsupported node %s version %d", requestedType, requestedVersion)
	}
	if !json.Valid([]byte(compiledJSON)) {
		return "compiled Max of Two state is not valid JSON"
	}
	return ""
}

// numericField applies the example node's input contract. JSON numbers decode
// as float64 at the plugin boundary, so integer and fractional inputs share the
// same representation. Missing, null, string, and boolean values are rejected
// instead of being silently coerced.
func numericField(fields map[string]interface{}, name string) (float64, error) {
	value, ok := fields[name]
	if !ok {
		return 0, fmt.Errorf("input field %q is missing", name)
	}
	number, ok := value.(float64)
	if !ok {
		return 0, fmt.Errorf("input field %q must be a number", name)
	}
	return number, nil
}
