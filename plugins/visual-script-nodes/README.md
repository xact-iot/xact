# Visual-script node plugins

This directory is operator-managed. Each immediate child directory is one
trusted plugin loaded once when XACT starts:

```text
visual-script-nodes/acme-tools/
  manifest.json
  backend.go
  editor.js       # optional
```

There is no upload, source-editing, install, or reload API. Treat `backend.go`
as having the same trust as the XACT server process.

## Manifest

`manifest.json` owns the catalog definition. A plugin may declare more than one
node in `nodes`. Each node requires a namespaced non-`core` `type`, positive
`typeVersion`, display metadata, input/output ports, an object
`parameterSchema`, and `requiredServices`. An optional frontend module must be
named `editor.js`.

JSON Schema properties are mapped to the built-in inspector. Supported property
types are `string`, `number`, `integer`, `boolean`, `object`, and `array`.
String enums render as selects, `format: "tag-path"` renders the tag picker, and
`x-ui-type` may explicitly select `string`, `number`, `boolean`, `select`,
`json`, or `tag-path`.

## Backend ABI

`backend.go` declares `package visualscriptplugin` and these top-level
functions. The ABI uses JSON strings at the Yaegi boundary; XACT validates and
converts them into native `NodeType`, `CompiledNode`, `Message`, and `Output`
values.

```go
package visualscriptplugin

func Register() string
func Compile(instanceID, nodeType string, typeVersion int, configJSON string) (compiledJSON, errorMessage string)
func Handle(executionID, instanceID, inputPort, nodeType string, typeVersion int, compiledJSON, messageJSON string) (outputsJSON, errorMessage string)
func Close(instanceID, nodeType string, typeVersion int, compiledJSON string) (errorMessage string) // optional
```

`Register` returns a JSON array such as:

```json
[{"type":"acme.uppercase","typeVersion":1}]
```

`Handle` returns a JSON array of routed outputs:

```json
[{"port":"out","message":{"value":"READY","fields":{}}}]
```

The engine restores message identity, tenant, script, revision, trigger, and
timestamp fields after every plugin call.

`instanceID` is stable for one compiled graph-node instance and lets a backend
isolate resources or state belonging to different instances of the same type.
`inputPort` is the destination port named by the graph edge that delivered the
message. `executionID` is short-lived and is used only with the service bridge.

The current plugin ABI covers nodes that process an incoming message. Custom
`Triggers` are rejected because autonomous trigger plugins require a separate
start/stop lifecycle contract; use the built-in triggers ahead of custom nodes.

Backend code can import `github.com/xact-iot/xact/visualscripts` and use only
the token-scoped service bridge below:

```go
visualscripts.Cancelled(executionID) bool
visualscripts.NowUnixMilli(executionID) int64
visualscripts.ReadTags(executionID, pathPattern) (changesJSON, errorMessage string)
visualscripts.SetTag(executionID, nodeID, path, valueJSON) string
visualscripts.SendControl(executionID, nodeID, device, path, valueJSON string, timeoutMillis int64) string
visualscripts.SendNotification(executionID, nodeID, profile, severity, message, device string) string
visualscripts.LogEvent(executionID, nodeID, severity, message, device string) string
```

An empty returned string means success; otherwise it is the node error message.
Long operations must periodically call `Cancelled`.

## Optional editor

`editor.js` must export:

```js
export function mount(container, context) {
  // context: { node, definition, updateConfig(patch), replaceConfig(config) }
  // Return an optional cleanup callback.
}
```

The module is served only when its matching backend type was successfully
registered. It may edit configuration only; executable behavior always comes
from `backend.go`.
