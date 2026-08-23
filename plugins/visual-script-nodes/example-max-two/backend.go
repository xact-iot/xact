// Package visualscriptplugin is evaluated by Yaegi when XACT starts.
//
// This example implements a two-input transform node. Each connected input
// supplies one number in message.value. The node waits until both "first" and
// "second" have arrived for the same routed message, then emits their maximum.
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
	nodeType       = "example.max-two"
	nodeVersion    = 1
	maxPendingRuns = 1024
)

// compiledConfig is the immutable state returned by Compile. Max of Two has no
// user configuration, but retaining a versioned JSON object demonstrates the
// compile-state portion of the ABI and leaves room for compatible additions.
type compiledConfig struct {
	StateVersion int `json:"stateVersion"`
}

// pendingInputs holds the values received by one compiled node during one
// message run. The loader serializes calls into a Yaegi backend, so this example
// does not need a mutex around the package-level instances map.
type pendingInputs struct {
	First      float64
	Second     float64
	HaveFirst  bool
	HaveSecond bool
}

type instanceState struct {
	Pending map[string]pendingInputs
}

// instances is keyed by the opaque instanceID supplied by XACT. Different Max
// of Two nodes therefore never share their pending input values.
var instances = map[string]*instanceState{}

// message includes the mutable payload plus routing identifiers used to pair
// both branches of the same run. XACT restores authoritative identity and
// tenancy fields after Handle returns.
type message struct {
	ID            string                 `json:"msgId"`
	CorrelationID string                 `json:"correlationId"`
	DevicePath    string                 `json:"devicePath,omitempty"`
	TagPath       string                 `json:"tagPath,omitempty"`
	Value         interface{}            `json:"value,omitempty"`
	Fields        map[string]interface{} `json:"fields,omitempty"`
}

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
// instanceID uniquely identifies that compiled graph-node instance and is
// supplied again to Handle and Close. Compile validates configuration, creates
// per-instance state, and returns immutable JSON passed to subsequent calls.
// A non-empty errorMessage rejects deployment before XACT swaps active plans.
func Compile(instanceID, requestedType string, requestedVersion int, configJSON string) (compiledJSON, errorMessage string) {
	if strings.TrimSpace(instanceID) == "" {
		return "", "compiled node instance ID is required"
	}
	if requestedType != nodeType || requestedVersion != nodeVersion {
		return "", fmt.Sprintf("unsupported node %s version %d", requestedType, requestedVersion)
	}
	var config map[string]interface{}
	if err := json.Unmarshal([]byte(configJSON), &config); err != nil {
		return "", "invalid Max of Two configuration: " + err.Error()
	}

	instances[instanceID] = &instanceState{Pending: map[string]pendingInputs{}}
	encoded, err := json.Marshal(compiledConfig{StateVersion: 1})
	if err != nil {
		delete(instances, instanceID)
		return "", "encoding compiled configuration: " + err.Error()
	}
	return string(encoded), ""
}

// Handle runs once for each message routed into one of the node's input ports.
//
// inputPort is the exact destination port from the graph edge. executionID is
// the short-lived token used by XACT's cancellation/service bridge, while
// instanceID identifies the longer-lived compiled node created by Compile.
// This pure calculation is short; long-running handlers should periodically
// call visualscripts.Cancelled(executionID).
//
// The first arriving input returns [] and is retained. The second input emits
// one message on "out" and clears that run's pending state. A non-empty error
// fails the current routed message under the script's normal error policy.
func Handle(executionID, instanceID, inputPort, requestedType string, requestedVersion int, compiledJSON, messageJSON string) (outputsJSON, errorMessage string) {
	_ = executionID
	if requestedType != nodeType || requestedVersion != nodeVersion {
		return "", fmt.Sprintf("unsupported node %s version %d", requestedType, requestedVersion)
	}
	var compiled compiledConfig
	if err := json.Unmarshal([]byte(compiledJSON), &compiled); err != nil || compiled.StateVersion != 1 {
		return "", "invalid compiled Max of Two state"
	}
	state, ok := instances[instanceID]
	if !ok {
		return "", "Max of Two instance is not compiled"
	}

	var msg message
	if err := json.Unmarshal([]byte(messageJSON), &msg); err != nil {
		return "", "invalid input message: " + err.Error()
	}
	value, ok := msg.Value.(float64)
	if !ok {
		return "", fmt.Sprintf("%s input value must be a number", inputPort)
	}
	runKey := msg.CorrelationID
	if runKey == "" {
		runKey = msg.ID
	}
	if runKey == "" {
		return "", "input message requires a correlationId or msgId"
	}
	values, alreadyPending := state.Pending[runKey]
	if !alreadyPending && len(state.Pending) >= maxPendingRuns {
		return "", "Max of Two pending-input limit reached"
	}
	switch inputPort {
	case "first":
		values.First, values.HaveFirst = value, true
	case "second":
		values.Second, values.HaveSecond = value, true
	default:
		return "", fmt.Sprintf("unknown input port %q", inputPort)
	}
	if !values.HaveFirst || !values.HaveSecond {
		state.Pending[runKey] = values
		return "[]", ""
	}
	delete(state.Pending, runKey)
	if values.Second > values.First {
		values.First = values.Second
	}
	msg.Value = values.First

	encoded, err := json.Marshal([]output{{Port: "out", Message: msg}})
	if err != nil {
		return "", "encoding Max of Two output: " + err.Error()
	}
	return string(encoded), ""
}

// Close is the final lifecycle function for a compiled node instance.
//
// XACT calls it when the owning execution plan closes. Plugins should release
// files, clients, subscriptions, goroutines, and pending state created by
// Compile. Close receives the same instanceID, so cleanup is isolated from
// other nodes implemented by this plugin. An empty string means success.
func Close(instanceID, requestedType string, requestedVersion int, compiledJSON string) (errorMessage string) {
	if requestedType != nodeType || requestedVersion != nodeVersion {
		return fmt.Sprintf("unsupported node %s version %d", requestedType, requestedVersion)
	}
	if !json.Valid([]byte(compiledJSON)) {
		return "compiled Max of Two state is not valid JSON"
	}
	delete(instances, instanceID)
	return ""
}
