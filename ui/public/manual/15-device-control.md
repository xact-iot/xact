# Device Control

Device Control lets XACT send commands back to connected devices. A command can start or stop a pump, update a sign message, change a setpoint, switch a device mode, or perform any other action that a driver understands.

Commands are intentionally simple: XACT sends a JSON payload to a device driver, the driver performs the work, and the driver replies with a success or failure result. Custom widgets and custom drivers can use their own protocol when needed, but the built-in control flow uses NATS request/reply.

## Where Commands Come From

Commands can be initiated in two main ways:

- **Status Table widget** - add command columns to a status table to create a compact control panel with switches, text inputs, numeric fields, enum dropdowns, sliders, and an **Execute** button.
- **Scheduler** - create a scheduled task of type **Command** to send a command at a fixed time, such as changing a setpoint overnight or stopping equipment on a recurring schedule.

When a command completes, XACT records the result in the Events table. Use the **Events Viewer** widget to audit command successes, failures, and driver timeouts.

## Status Table Controls

The built-in control panel is the **Status Table** widget. Each row can show live values, command inputs, or both. This lets an operator see the current device state beside the control that changes it.

A command column is configured with:

| Field | Description |
|-------|-------------|
| **Tag path** | Relative path sent in the command payload, such as `pumps.1.status` |
| **Input field** | Control type: switch, text, number, enum, or slider |
| **Default value** | Initial value shown when the widget loads |
| **Enum options** | Comma-separated values for enum dropdowns |
| **Min / Max** | Numeric limits for number and slider controls |
| **Timeout** | How long the UI waits for the driver response |

When the operator clicks **Execute**, the widget shows a confirmation dialog, sends the command, waits for the driver response, then shows the final result.

## Scheduled Commands

The **Scheduler** can send the same kind of command without an operator clicking a dashboard control. Choose task type **Command**, then set the device path, tag path, value, timeout, and cron schedule.

Scheduled commands are useful for routine operating actions, such as changing an operating mode at shift change or issuing a periodic reset command. The scheduler logs the result to the server console and to the Events table.

## Driver Protocol

The standard XACT command protocol uses NATS request/reply over the `commands` JetStream stream. The stream is created by the server on startup and uses work-queue retention for subjects below:

```text
xact.command.>
```

The command subject identifies the organisation and target device:

```text
xact.command.{org}.{device-path}
```

For example:

```text
xact.command.myorg.WaterWorks.PUMP_STATION.RAW_WATER_PS
```

A driver subscribes to the device paths it can control. A VMS driver might subscribe to `xact.command.myorg.traffic.VMS.>`, while a camera driver might subscribe to `xact.command.myorg.traffic.cameras.>`. If multiple instances of the same driver are running, NATS work-queue delivery lets one driver instance handle each command request.

The command payload is JSON. It includes a unique command ID plus one or more relative tag paths and values:

```json
{
  "id": "abcd1234",
  "pumps.1.status": true
}
```

The driver should:

1. Receive the request on the matching `xact.command...` subject.
2. Validate that the command path and value are allowed.
3. Send the command to the PLC, device API, controller, or simulator.
4. Publish any resulting live tag changes back into RTDB through the normal ingest path.
5. Reply to the NATS request with a command result.

The response payload is also JSON:

```json
{
  "id": "abcd1234",
  "success": true,
  "message": "The command succeeded"
}
```

The response `id` should match the request `id`. If the driver reports `success: false`, returns an invalid response, or does not respond before the timeout, XACT treats the command as failed and records an event.
