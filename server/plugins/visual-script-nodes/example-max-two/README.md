# Max of Two example node

This example implements `example.max-two` version 1. It reads two numeric
message fields selected by `firstField` and `secondField` (defaulting to
`inputA` and `inputB`) and writes the larger number to `message.value`.

Example input payload:

```json
{
  "fields": {
    "inputA": -4,
    "inputB": 12.5
  }
}
```

The output message has `value: 12.5` and retains the input fields. Missing or
non-numeric inputs fail the current message with a descriptive node error.

The source is kept under `server/plugins` as a tested example and is not loaded
by a normal installation automatically. To enable it, copy the entire
`example-max-two` directory into the configured operator plugin directory:

```text
plugins/visual-script-nodes/example-max-two/
```

Then restart XACT. Plugins are discovered only during server startup.

The lifecycle is documented inline in `backend.go`:

1. `Register` declares the backend's stable node type and version.
2. `Compile` validates configuration and creates immutable per-node state.
3. `Handle` processes each incoming message and returns routed outputs.
4. `Close` releases resources owned by the compiled node instance.
