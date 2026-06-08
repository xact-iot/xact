# Tags & Real-Time Data

XACT uses a hierarchical **tag tree** to organise all real-time data. Tags are the fundamental data points - each tag represents a single measurement, status, or control value from a connected device or computed source.

## Tag Tree Structure

Tags are organised in a tree hierarchy, similar to a file system:

```
Organisation
└── DeviceType
    └── DeviceName
        ├── TagGroup
        │   ├── Temperature
        │   ├── Pressure
        │   └── Status
        └── meta
            ├── online
            ├── name
            └── description
```

The tree has three types of **nodes**:

| Node Type | Description |
|-----------|-------------|
| **Organisation** | The root node - one per organisation, created automatically |
| **Device** | Represents a physical or logical device. Device nodes are the level at which metrics are recorded. |
| **Standard** | A general-purpose container for grouping. Can represent sites, areas, device types, or tag groups. |

Leaf nodes at the bottom of the tree are **tags** - they hold values. Branch nodes are containers that group related tags and child nodes.

## Tag Paths

Every tag has a unique **path** that describes its location in the tree. Paths use dot notation internally and forward slashes in the REST API:

```
VMS.Sign001.status.brightness
Pumps.Pump01.metrics.flowRate
WaterNetwork.Zone1.Reservoir.level
```

Tag paths are used throughout XACT - in widget configurations, report templates, tag calcs, API calls, and MQTT topic mappings.

> **Note:** The organisation name is not included in tag paths - it is added automatically based on your session context.

## Tag Data Types

Each tag has a data type that determines what values it can hold:

| Type | Description | Examples |
|------|-------------|---------|
| **Integer** | Whole numbers | `42`, `0`, `-17` |
| **Float** | Decimal numbers | `72.4`, `3.14159`, `-0.5` |
| **String** | Text values | `"Normal"`, `"Offline"`, `"Error: timeout"` |
| **Boolean** | True/false | `true`, `false` |
| **Enum** | Predefined set of values | `"Running"`, `"Stopped"`, `"Fault"` |

## Tag Metadata

Each tag carries metadata alongside its value:

- **Description** - human-readable explanation of what the tag represents
- **Units** - measurement unit (e.g. "C", "bar", "kWh", "%")
- **Min / Max** - expected value range (used by gauges and colour bands)
- **Precision** - number of decimal places for display
- **Pipeline** - ordered list of processing blocks (see below)
- **Deadband** - minimum change required before a new value is recorded or published

## Tag Status

Tags have status flags that indicate their current health:

| Status | Indicator | Meaning |
|--------|-----------|---------|
| **Normal** | (none) | Value is current and within limits |
| **Stale** | **S** | No new value received within the configured timeout |
| **Alarm** | **A** | Value has exceeded a configured limit threshold |
| **Unavailable** | **U** | Tag exists but has never received a value |
| **Disabled** | **D** | Tag has been manually disabled |

Status flags are set automatically by processing blocks (stalecheck and limitcheck) or manually by administrators.

## Live Values

Tag values are updated in real time as data arrives from connected devices. The data flow is:

1. Data arrives via **MQTT** or the **REST ingest API** (see [Data Ingest Methods](#data-ingest) in the developer section).
2. The server writes the value into the **RTDB** (Real-Time Database) - an in-memory tree.
3. The value passes through the tag's **processing pipeline** (see below).
4. Updated values are published to the **NATS** messaging system.
5. Connected browsers receive the update and widgets refresh automatically.

This entire chain typically completes in milliseconds, providing near-instant feedback on dashboards.

## Tag Manager

The **Tags Manager** widget (System category) provides a full tree browser and editor. From this widget you can:

- **Browse** the complete tag tree with expand/collapse navigation
- **Search** for tags by name or path
- **View** current values with status indicators and timestamps
- **Create** new nodes and tags
- **Edit** tag metadata, data types, and configuration
- **Delete** nodes and tags (with cascade delete for branches)
- **Configure pipelines** - add, remove, and reorder processing blocks
- **Debug pipelines** - test a value through the pipeline and see step-by-step results

### Creating a Node

1. Navigate to the parent location in the tree.
2. Click **Add Node**.
3. Enter the node name, description, and select the type (Standard, Device, or Organisation).
4. Optionally assign a **template** - a reusable blueprint that pre-populates child tags when devices are created.
5. Click Save.

### Creating a Tag

1. Navigate to the parent node.
2. Click **Add Tag**.
3. Enter the tag name, description, units, and select the data type.
4. Optionally configure min/max values and precision.
5. Click Save.

## Array Tags

Array tags allow a single tag name to hold multiple indexed values - for example, a bank of battery voltages or a list of PID controller parameters.

### How Array Tags Are Created

Array tags are created **automatically** when an MQTT payload (or REST ingest) contains a JSON array value. You do not create them manually in the Tags Manager.

**Scalar array** - a simple list of values:

```json
{
  "batteries": [12.6, 12.9, 11.8]
}
```

This creates an array container node `batteries` with three child tags named `0`, `1`, and `2`, each holding the corresponding value.

**Object array** - a list of structured objects:

```json
{
  "pids": [
    { "P": 1.5, "I": 0.3, "D": 0.01 },
    { "P": 2.0, "I": 0.1, "D": 0.05 }
  ]
}
```

This creates an array container `pids` with child nodes `0` and `1`, each containing tags `P`, `I`, and `D`.

### Tree Structure

Array containers are marked with an `isArray` flag and appear in the Tags Manager with a **[ ]** badge. Child elements use numeric names and are displayed as `[0]`, `[1]`, etc.

```
Device
└── batteries [ ]       ← array container node
    ├── [0]  → 12.6
    ├── [1]  → 12.9
    └── [2]  → 11.8

Device
└── pids [ ]            ← array container node
    ├── [0]
    │   ├── P  → 1.5
    │   ├── I  → 0.3
    │   └── D  → 0.01
    └── [1]
        ├── P  → 2.0
        ├── I  → 0.1
        └── D  → 0.05
```

### Referencing Array Elements

Array elements are referenced by their numeric index as part of the tag path, just like any other path segment:

| Context | Scalar array example | Object array example |
|---------|---------------------|---------------------|
| **Tag path** | `Device.batteries.0` | `Device.pids.1.P` |
| **REST API** | `/api/v1/tags/Device/batteries/0` | `/api/v1/tags/Device/pids/1/P` |

Array element paths can be used anywhere a tag path is accepted - widget configurations, report template variables, tag calcs, and pattern matching.

### Creating Array Tags Manually

Array tags can also be created directly in the Tags Manager:

1. Navigate to the parent node.
2. Click **Add Tag**.
3. Tick the **Array** checkbox - a **Size** field appears.
4. Enter the number of elements (1–256).
5. Select the element type, description, and units as usual.
6. Click **Create Tag**.

This creates the array container node and the numbered child tags (`[0]`, `[1]`, …) in one step. You can edit individual elements afterwards to set different descriptions or pipelines.

### Dynamic Sizing

Array tags resize automatically. If a subsequent payload contains more (or fewer) elements than the previous one, the tree is updated accordingly - new child nodes are added as needed. Metadata such as units, limits, and deadband from the ingested payload is applied to each element.

## Templates

Templates are reusable tag structure blueprints. When a template is assigned to a node, any new device created under that node automatically inherits the template's tag structure - including tag names, types, units, and pipeline configurations.

Templates ensure consistency when many devices share the same tag layout. Instead of manually creating dozens of identical tags for each new device, you define the structure once as a template.

## Processing Blocks

Processing blocks form a **pipeline** attached to each tag. When a new value arrives, it passes through the pipeline in sequence - each block may transform the value, set status flags, trigger side effects, or record data.

Pipelines are configured in the Tags Manager widget using the pipeline editor. Blocks can be added, removed, and reordered by dragging.

### Scale Block

Applies a linear transformation to numeric values: `output = (input x scale) + offset`.

| Parameter | Description | Default |
|-----------|-------------|---------|
| Scale | Multiplication factor | 1 |
| Offset | Addition offset | 0 |
| Min | Lower clamp bound (optional) | - |
| Max | Upper clamp bound (optional) | - |

Non-numeric values pass through unchanged. Use this for unit conversion (e.g. converting raw sensor counts to engineering units).

**Example:** A temperature sensor that reports in tenths of a degree - set scale to `0.1` to convert `254` to `25.4`.

### Limit Check Block

Checks the value against high and low thresholds. Sets the **Alarm** status flag when a limit is breached; clears it when the value returns to normal. The value itself passes through unchanged.

| Parameter | Description |
|-----------|-------------|
| Hi Limit | Upper alarm threshold |
| Low Limit | Lower alarm threshold |
| Hi Event | Event emitted when value exceeds high limit (severity, message) |
| Return Event | Event emitted when value returns to normal |
| Low Event | Event emitted when value falls below low limit (severity, message) |

Each event can be individually enabled/disabled and assigned a severity level (DEBUG, INFO, WARN, ERROR, CRITICAL). Events are sent to the notification system and logged.

**Example:** A tank level tag with Hi Limit = 90 and Low Limit = 10 - the tag enters alarm state when the level exceeds 90% or drops below 10%.

### Stale Check Block

Detects when a tag has not received a new value within a configurable timeout. Sets the **Stale** status flag when the timer expires; clears it when any new value arrives.

| Parameter | Description | Default |
|-----------|-------------|---------|
| Timeout | Maximum time between updates | 30 seconds |

**Example:** A device that reports every 60 seconds - set timeout to 120 seconds so the tag is marked stale only if two consecutive updates are missed.

### Publish Block

Publishes tag value changes to the NATS broadcast stream for real-time distribution to connected clients. Changes smaller than the tag's deadband are suppressed to reduce network traffic.

No parameters - behaviour is controlled by the tag's deadband setting.

### History Recorder Block

Records tag value snapshots to the time-series database for historical charting and reporting. Applies deadband filtering so that insignificant changes are not recorded.

No parameters - deadband is read from the tag configuration. Only numeric values on device tags are recorded.

See the [Historical Data](#historical-data) chapter for more about time-series storage.

### Persist Block

Saves the last-known tag value to durable storage (NATS KV store). On server restart, the persisted value is restored so tags do not start empty. Deadband filtering suppresses writes for trivial changes.

No parameters - deadband is read from the tag configuration.

### Flap Filter Block

Detects rapid alarm-state oscillation (flapping) and suppresses further toggling until the state stabilises. This prevents notification storms when a value hovers near a limit threshold.

| Parameter | Description | Default |
|-----------|-------------|---------|
| Threshold | Number of alarm transitions that trigger flapping detection | 1 |
| Window | Rolling time window for counting transitions | 60 seconds |
| Cooldown | How long the state must be stable before flapping clears | 120 seconds |
| Flap Event | Event emitted when flapping is detected | - |
| Return Event | Event emitted when flapping clears | - |

While flapping, the alarm flag is locked and a **Deviation** flag is set on the tag. Values always pass through unchanged - downstream blocks continue to receive every update.

> **Pipeline placement:** The flap filter must be placed **after** the limit check block so it can observe alarm transitions.

### Pipeline Order

The order of blocks in the pipeline matters. A typical pipeline might be:

1. **Scale** - convert raw sensor values to engineering units
2. **Limit Check** - check for alarm conditions
3. **Flap Filter** - suppress rapid alarm toggling
4. **Publish** - broadcast changes to connected clients
5. **History Recorder** - record values to the time-series database
6. **Persist** - save last-known value for recovery after restart

### Debugging a Pipeline

The Tags Manager includes a pipeline debugger. Enter a test value and click **Debug** to see the step-by-step output of each block in the pipeline. This is invaluable for verifying that scaling, limits, and event generation are configured correctly before deploying to production.

## Data Ingestion

Data enters XACT through **ingest connectors**. Two methods are available:

- **MQTT** - external devices publish data to the embedded MQTT broker using structured topics and JSON payloads
- **REST API** - devices or integrations POST data to the REST ingest endpoint using an API key for authentication

Both methods automatically create tag tree structure as needed - if a device or tag doesn't exist yet, it is created on first data arrival.

See the [Data Ingest Methods](#data-ingest) chapter in the developer section for detailed integration instructions.
