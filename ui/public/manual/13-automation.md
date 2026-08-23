# Automation

Automation uses visual scripts to respond to live data, make decisions, transform values, retain temporary state, and carry out actions. A visual script is assembled as a graph: a trigger starts a run, connected nodes process its message, and action nodes produce the required result.

The editor is intended for engineers, technicians, and domain specialists who understand the process being automated. Most scripts can be built without writing code. Some settings accept JSON values or lists; examples are included below.

> Automation can change tags, send device controls, create events, and notify people. Build and test with **Simulate** enabled, include **Debug** nodes at important points, and verify the result before allowing live outputs.

## Choosing the Right XACT Tool

| Requirement | Recommended tool |
|---|---|
| Continuously calculate one tag from other tags | **Tag Calcs** |
| Run a report, backup, command, or server task on a calendar schedule | **Scheduler** |
| Respond to tag changes or edges and route through several decisions or actions | **Automation** |
| Run a workflow at a simple repeating interval | **Automation** with a Timer trigger |

Visual scripts are organisation-level resources. The dashboard widget holds a reference to a script; the executable script is not stored inside the dashboard layout. More than one dashboard may therefore display the same script.

## Permissions and Setup

The Visual Scripts permission group has two permissions:

| Permission | Access |
|---|---|
| `visual-scripts.view` | View a graph, its runtime state, and run summaries |
| `visual-scripts.edit` | Create, edit, save, test, back up, restore, and control scripts |

To add an automation to a dashboard:

1. Put the dashboard into edit mode.
2. Drag a **Visual Script** widget from the **System** widget category onto the dashboard.
3. Save or exit dashboard edit mode.
4. In the empty widget, enter a name and select **Create script**, or select an existing script and choose **Attach**.
5. Select **Edit** in the script widget to open the focused editor.

The widget properties can show the graph or a compact overview, and can show or hide runtime status. Script editing is separate from dashboard editing so moving a graph node cannot accidentally move the dashboard widget.

## How a Visual Script Works

Four concepts are important:

- A **trigger** starts a run. Manual, timer, startup, and tag-based triggers are available.
- A **message** travels along connections. It contains a main value, optional named fields, and system metadata identifying the trigger and script instance.
- A **node** reads or changes the message, chooses an output, stores temporary data, or performs an action.
- A **script instance** separates work for different devices matched by a wildcard trigger. Each instance has its own variable values and execution queue.

Connections describe both execution order and message flow. When a node sends the message through an output, every node connected to that output receives its own copy. Conditions use named **True** and **False** outputs to select a route.

An input may receive connections from more than one output. Each arrival is a separate execution; inputs do not automatically wait for, combine, or synchronise messages. If values must be combined, first place the required values in message fields or variables and then read them from one run.

Cycles are not permitted. Use variables and a later trigger when state must carry from one run to another.

## Build and Test a First Script

This example sends a configured value to the debug trace without creating an external side effect:

1. Open the editor with **Edit**.
2. Drag **Manual** from **Triggers** onto the canvas.
3. Drag **Set Field** from **Transforms** onto the canvas.
4. Drag **Debug** from **Actions** onto the canvas.
5. Drag from the Manual **Output** port to the Set Field **Input** port.
6. Drag from the Set Field **Output** port to the Debug **Input** port.
7. Select Set Field. Leave **Field** blank and enter this in **Value**:

   ```json
   {
     "mode": "test",
     "setpoint": 42
   }
   ```

8. Select Debug and give it a label such as `Configured test value`.
9. Select **Start**. The editor saves any changes, validates the graph, clears the previous trace, and starts the script.
10. Select **Trigger** on the Manual node.
11. Check **Debug trace**. The entry should contain the object configured in Set Field. Select **Format JSON** for an expanded view.

This Manual → Set Field → Debug pattern is useful for checking transforms and conditions before connecting a live trigger or action.

## Editing the Graph

### Add, Select, and Configure Nodes

- Drag a node from the left sidebar to the required position on the canvas.
- Use the search box to filter the node list.
- Select a category heading to collapse or expand it.
- Select a node to show its settings in the inspector on the right.
- Drag a node body to reposition it.
- Select **Delete node** in the inspector to remove the node and its connections.
- Use **Undo** and **Redo** for changes made during the current editing session.

Node IDs are internal and are deliberately not shown. Use the node type, configured values, and Debug labels to make the graph understandable.

### Create and Edit Connections

- Drag from an output port on the right side of a node to a compatible input port on the left side of another node.
- One output can feed several downstream nodes. Each branch receives a separate copy of the message.
- Select a connection line to make it prominent and show its endpoints in the inspector.
- Press **Delete** or **Backspace** to remove the selected connection.
- Drag either selected endpoint away from its node and drop it on another valid port to reconnect it.

The editor rejects duplicate connections, incompatible port types, connections back to the same node, and connections that would create a cycle.

## Script States and Controls

| Control or state | Behaviour |
|---|---|
| **Idle** | The script is present but accepts no triggers. |
| **Running** | Triggers are active and runs can execute. |
| **Paused** | New triggers are suspended. Work already in progress is allowed to finish. Tag changes while paused are not replayed. |
| **Edit / Exit edit** | Opens or closes graph authoring. Unsaved work is saved before the editor closes. |
| **Save** | Quickly saves an in-progress graph without validation. If the script is running, it is stopped first. Save is disabled when nothing has changed. |
| **Start** | Saves an edited graph if required, validates it, clears the debug trace and temporary variables, and starts it. From Paused, Start resumes operation using the current saved graph. |
| **Stop** | Stops trigger intake, cancels queued work, requests cancellation of work in progress, and clears temporary variables. |
| **Pause** | Suspends new trigger intake without discarding the saved graph. |

Starting always validates the complete graph. The script does not run if validation reports an error. Warnings identify suspicious but executable arrangements, such as unreachable nodes.

Only one current version of a script is presented to the user. Internal revision numbers are used to prevent two editors from silently overwriting each other, but they are not part of the normal operating workflow.

## Script Menu

The **Script** menu is available in edit mode:

### Backup and Restore

- **Backup** saves dirty work if necessary, then records the current script as its manually managed backup.
- **Restore** confirms the operation, overwrites the current script from that backup, and leaves the script Idle.
- Creating another backup replaces the previous manual backup.

Create a backup before a substantial change or before replacing a proven control sequence.

### Simulate

**Simulate** allows the graph to run while suppressing external side effects from these output nodes:

- Set Tag
- Send Control
- Send Notification
- Log Event

The message continues through the suppressed node, so downstream logic can still be tested. Debug remains active because it only writes to the editor trace. Place a Debug node before or after an action to inspect the message that would be used. Set Simulate before starting the script so the next Start uses the required mode.

Simulation is a strong safety aid, but it is not a substitute for checking paths, limits, permissions, and device behaviour before live operation.

### Activate

**Activate** marks a script to start automatically when XACT starts. At server startup, XACT loads each activated script, validates its current saved graph, clears incomplete previous runs, and starts its triggers. A script that no longer validates remains stopped and records an error in the server logs.

Use Activate only after the script has been tested with the same trigger and output configuration intended for normal operation.

## Debug Trace

Only **Debug** nodes add entries to the Debug trace. This makes Debug equivalent to a controlled print statement placed at a chosen point in the graph.

Each entry contains:

- a local timestamp in `hh:mm:ss.ddd` format;
- the Debug node name and optional label;
- run status;
- the message's main value and named fields as compact JSON.

Select **Format JSON** to open an indented view. **Clear trace** immediately removes the displayed entries without a confirmation. Starting the script also clears the trace; subsequent trigger runs append new Debug entries until it is started or cleared again.

If a Debug node produces no entry, check that:

- the script is Running;
- the trigger actually fired;
- the Debug node is connected to the output route that was taken;
- an earlier node did not fail; and
- the Manual node's **Trigger** button is enabled.

## Message Values and Fields

Most nodes work with either the message's main **value** or a named **field**.

| Setting | Meaning |
|---|---|
| Field left blank | Read or replace the main message value |
| Field supplied | Read or replace that named entry in the message fields |
| Configured value | A fixed JSON value stored with the node |
| Message value | The value supplied by the trigger or a previous node |

JSON values may be strings, numbers, booleans, `null`, arrays, or objects. Enter strings with quotes, for example `"AUTO"`. Enter field lists as JSON arrays, for example:

```json
["pressure", "temperature", "flow"]
```

System metadata such as the organisation, script, trigger timestamp, tag path, device path, run ID, and instance identity is preserved by the engine. Processing nodes cannot accidentally replace that identity.

## Node Reference

### Triggers

| Node | Purpose and important settings |
|---|---|
| **Manual** | Starts a run when an authorised user selects **Trigger**. The script must already be Running. Useful for controlled testing. |
| **Timer** | Starts a run repeatedly. Set Interval, optional Initial delay, and optional Jitter. Durations use forms such as `500ms`, `30s`, `5m`, or `2h`. |
| **Startup** | Starts one run when the script starts, optionally after a Delay. It fires on a normal Start as well as automatic startup activation. |
| **Tag Changed** | Starts whenever an exact or wildcard RTDB tag path changes. The incoming tag value becomes the message value. |
| **Rising Edge** | Starts when the matched value changes from false to true. Configure strict, truthy, or numeric Boolean conversion and optional Debounce. The first observed value establishes state and does not fire an edge. |
| **Falling Edge** | Starts when the matched value changes from true to false, with the same conversion and debounce options as Rising Edge. |

For edge conversion, **strict** accepts only JSON booleans, **numeric** treats zero as false and other finite numbers as true, and **truthy** also accepts common non-empty values. Prefer strict when the source tag is correctly typed.

### Conditions

Every condition sends the unchanged message through either its **True** or **False** output.

| Node | Purpose |
|---|---|
| **Compare** | Compares the main value or a selected field with a configured JSON value using `<`, `<=`, `==`, `!=`, `>=`, or `>`. |
| **In Range** | Tests a number against minimum and maximum limits. Boundaries can be inclusive or exclusive. |
| **NOT** | Inverts a Boolean value. |
| **AND** | True only when every field named in its JSON Fields array is Boolean true. |
| **OR** | True when at least one field named in its JSON Fields array is Boolean true. |
| **Compare Times** | Compares a timestamp with the current time, another message field, or a configured time using before/equal/after choices. |

### Transforms

| Node | Purpose |
|---|---|
| **Set Field** | Replaces the main value or writes a named field with a configured JSON value. |
| **Select Field** | Copies one named field into the main message value. |
| **Multiply / Divide** | Applies a factor or divisor to the main value or selected numeric field. Division by zero fails the run. |
| **Average** | Averages a numeric array in the main value, or the numeric fields listed in its Fields setting. |
| **Clamp** | Limits a number to a configured minimum and maximum. |
| **Scale** | Maps a number linearly from one input range to another output range. |
| **Current Time** | Writes the current Unix epoch time in milliseconds to the main value or a selected output field. |
| **Time Since** | Calculates elapsed milliseconds, seconds, minutes, or hours from a timestamp in the main value or a selected field. |

Numeric transforms require finite numbers. A missing field, non-numeric value, invalid range, or division by zero fails that run and prevents its later nodes from executing.

### Variables

Variables retain temporary values between trigger runs of the same active script instance.

| Node | Purpose |
|---|---|
| **Set Variable** | Stores the incoming message value or a configured JSON value under a key. |
| **Get Variable** | Reads a key and places the stored value into the message's main value. The editor suggests keys written elsewhere in the graph. |
| **Delete Variable** | Removes a key. |
| **Increment Variable** | Atomically adds a configured amount to a numeric key and places the new value in the message. A missing key starts at zero. |
| **Set Time Variable** | Stores the current time, message value, named field, or configured time under a script key. |
| **Get Time Variable** | Reads a stored time into the main value or a named output field. |

For general variables, **Script** scope shares a key between variable nodes in the same script instance. **Node** scope is private to the individual node. Choose Script when another node must read the value.

Variables are isolated by organisation, script, active graph, and script instance. They are held in memory and are cleared on Start or Resume, when the script stops, when its active graph is replaced, and when the server restarts. Pausing alone does not clear them. Do not use them as permanent records.

### Actions

| Node | Live behaviour |
|---|---|
| **Set Tag** | Writes the incoming or configured value to an RTDB tag path. |
| **Send Control** | Sends the incoming or configured value to a device and control path, waiting up to the configured timeout for a response. |
| **Send Notification** | Sends a message through an existing notification profile at the selected severity. |
| **Log Event** | Writes a message and severity to the XACT Events log. |
| **Debug** | Records the incoming message in the editor trace and then passes it onward. It remains active in simulation. |

Action nodes require the corresponding server capability and permission. A failure stops the current run at that node. It does not automatically stop the complete script; later triggers may still create new runs.

## Time Values

Time nodes store and calculate with **Unix epoch milliseconds**. For example, `1786797000000` represents one instant independent of timezone. Configured time fields also accept ISO-8601 values such as:

```text
2026-08-15T14:30:00Z
```

Use ISO-8601 with an explicit timezone when entering a fixed instant. Use Current Time or the `now` source when the value should be evaluated during the run.

Debug can add human-readable representations without changing the stored number:

1. Set **Time display** to `utc` or `local`.
2. In **Time fields**, enter a JSON array of timestamp fields.
3. Use `$value` in that array to format the main message value.

For example:

```json
["$value", "startedAt", "lastServiceTime"]
```

The formatted values appear in the Debug JSON under `formattedTimes`; the original epoch values remain available for calculations.

## Wildcard Tag Triggers and Script Instances

Tag Changed, Rising Edge, and Falling Edge accept exact paths or segment-based patterns. XACT treats `.` and `/` as path separators and normalises them to dots.

- `*` matches any number of characters inside one path segment.
- `?` matches exactly one character inside one path segment.
- Wildcards do not cross a path separator.
- The concrete tag path must contain the same number of segments as the pattern.

Example:

```text
SITE.*.Status.Running
```

This matches `SITE.Pump01.Status.Running` and `SITE.Pump02.Status.Running`, but not a path with an extra segment. XACT creates an instance identity from the wildcard segment, so Pump01 and Pump02 have separate variables, edge state, queues, and runs while using the same graph.

With more than one wildcard segment, their concrete segment values are combined into the instance identity. This lets one script safely monitor a family of similarly structured devices without mixing their temporary state.

## Execution and Operational Safety

Each trigger creates a run. Runs for different scripts and different wildcard instances are handled independently, so a slow device control or external notification in one instance does not stop unrelated scripts from executing. Runs for the same instance are queued and, by default, processed one at a time to preserve a useful ordering.

If a node fails, the current run stops and records the first failing node and message. The script remains available for later triggers unless an operator stops it or the runtime itself becomes unavailable.

Automation is leader-gated in a clustered XACT installation, with best-effort de-duplication. It cannot promise universal exactly-once delivery to external systems: a device or service may complete an action just before a network or server failure prevents XACT from recording the result. Design important controls to be idempotent where possible, include safe limits in the receiving device, and use Events for an audit trail.

## Troubleshooting

| Symptom | Checks |
|---|---|
| Start does not complete | Review **Problems** for missing settings, invalid JSON, cycles, or an absent trigger. Correct the highlighted node and try Start again. |
| Manual Trigger is disabled | The script is not Running, Start is still in progress, or the selected node is not a Manual trigger. |
| No Debug entry appears | Confirm the trigger fired, the taken branch reaches Debug, and no earlier node failed. Only Debug nodes are traced. |
| A condition always takes the wrong branch | Inspect the incoming value with Debug, check whether Field should be blank, and confirm JSON types. `"1"`, `1`, and `true` are different values. |
| Get Variable reports a missing key | Confirm the Set or Increment node executed first, the key spelling matches, Script scope is selected when crossing nodes, and the same wildcard instance is involved. |
| Tag trigger does not fire | Check the normalised path, wildcard segment count, script Running state, and Boolean conversion for edge triggers. |
| An action has no external effect | Check whether Simulate is enabled, then verify the target path, notification profile, device response, and required permission. |
| Changes appear lost after a restart | Graph changes must be saved. Temporary variables deliberately do not persist across restarts. |

## Design Practices

- Begin with Manual and Debug, then substitute the live trigger after the message shape is understood.
- Keep Simulate enabled until every external target has been checked.
- Give each Debug node a label that describes its position, such as `After pressure check`.
- Prefer several small, clearly named scripts over one graph that controls unrelated processes.
- Put safety interlocks in the device or controller as well as in XACT automation.
- Use Script variables only for temporary operating state; use tags, events, or another durable store for records that must survive a restart.
- Back up a proven graph before a substantial edit, and test Restore before relying on it as an operating procedure.
- Treat wildcard scripts as one design running for many independent devices; verify the pattern against representative device paths before activation.
