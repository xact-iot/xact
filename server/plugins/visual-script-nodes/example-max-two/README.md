# Max of Two example node

This example implements `example.max-two` version 1. It exposes two visual
inputs named `first` and `second`. Each input accepts a message whose `value` is
numeric. After both inputs receive values belonging to the same message run,
the node writes the larger number to `message.value` and emits through `out`.

Example input payload:

```json
{
  "msgId": "same-run-id",
  "value": -4
}
```

Send that message to `first` and another message with the same `msgId` and
`value: 12.5` to `second`. The node then emits `value: 12.5`. Non-numeric values
fail the current message with a descriptive node error.

The source is kept under `server/plugins` as a tested example and is not loaded
by a normal installation automatically. To enable it, copy the entire
`example-max-two` directory into the configured operator plugin directory:

```text
plugins/visual-script-nodes/example-max-two/
```

Then restart XACT. Plugins are discovered only during server startup.

The lifecycle is documented inline in `backend.go`:

1. `Register` declares the backend's stable node type and version.
2. `Compile` validates configuration and creates isolated per-instance state.
3. `Handle` receives the destination input port, joins both values by message
   correlation, and returns a routed output when the pair is complete.
4. `Close` releases resources owned by the compiled node instance.
