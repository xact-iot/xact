# MQTT Device Interface

## Overview

This document describes how to connect devices to the XACT MQTT broker for data ingress. Devices publish telemetry data via MQTT messages, which are automatically ingested into the RTDB (Real-Time Database).

## Connection Details

### Broker Address

```
Host: <server-ip>
Port: 1883 (default MQTT port)
Protocol: MQTT v3.1.1 or v5.0
```

The broker URL can be configured via the `MQTT_URL` environment variable on the server (default: `mqtt://127.0.0.1:1883`).

### Authentication

All devices must authenticate using password-based authentication.

**Username:** (optional, leave empty if not required)

**Password:** the value of `MQTT_PASSWORD`

Development builds fall back to `xact` when `MQTT_PASSWORD` is unset. Production mode requires an explicit non-default password.

### Connection Parameters

- **Keep Alive:** 60 seconds (recommended)
- **Clean Session:** true
- **QoS Level:** 1 (recommended for reliable delivery)

## Topic Structure

Devices must publish to topics following this pattern:

```
xact/{tenant}/{msgtype}/{devicetype}/{devicename}
```

### Components

| Component | Description | Example |
|-----------|-------------|---------|
| `xact` | Fixed prefix - must be exactly "xact" | `xact` |
| `{tenant}` | Organization name. Use `default` for single-tenant setups | `acme-corp`, `default` |
| `{msgtype}` | Message type. Either `data` or `control` |
| `{devicetype}` | Device category/type | `CTLR`, `VMS`, `PLC`, `SENSOR`, `METER` |
| `{devicename}` | Unique device identifier within the tenant | `boiler-01`, `floor-3-hvac` |

### Topic Examples

```
xact/default/data/CTLR/boiler-01
xact/acme-corp/data/SENSOR/floor-3-temp
xact/manufacturing/data/PLC/line-7-controller
```

## Payload Format

The message payload must be a JSON object containing taggroups with their tag names and their values. Taggroup is a logical grouping of tags.


### Basic Payload

```json
{
  "grp1": {
    "supplyTemp": 72.4,
    "returnTemp": 68.9,
    "flowRate": 120.2,
    "pumpStatus": true
  },
  "mygrp": {
    "count": 34,
    "status": "Normal"
  }
}
```
### Tag value format

A tag value can either be a scalar value

```json
  "flowRate": 120
```

or an object

```json
  "flowRate": {
    "value": 120,
    "description": "Flow rate through the main outlet",
    "units": "l/sec"
  }
```
The following fields are predefined for object formats
  - **value**: The tags value
  - **description**: The tag description shown in the tag tree and tag editor
  - **units**: The units e.g. V, C etc

### Payload with Timestamp

Include an optional `ts` field within any taggroup to specify when the data was collected:

```json
{
  "grp1": {
    "supplyTemp": 72.4,
    "returnTemp": 68.9,
    "flowRate": 120.2,
    "ts": "1704067200000"
  }
}
```

**Timestamp Format:**
- Field name: `ts`
- Format: Unix timestamp in milliseconds (as number)
- Optional: If omitted, the server uses the current time

### Supported Data Types

The following data types are automatically detected and supported:

| Type | Examples | RTDB Type |
|------|----------|-----------|
| Integer | `42`, `-17`, `0` | `integer` |
| Float | `72.4`, `-3.14` | `float` |
| String | `"online"`, `"alarm"` | `string` |
| Boolean | `true`, `false` | `boolean` |

### Field Naming

- Tag names should be alphanumeric with optional underscores
- Avoid special characters and spaces
- Examples: `temperature`, `flow_rate`, `Motor1Speed`

## Data Flow

When a message is received:

1. **Authentication** - Client password is verified
2. **Topic Validation** - Topic format is validated (`xact/{tenant}/{msgtype}/{devicetype}/{devicename}`)
3. **Message Type Filter** - Only messages with `msgtype=data` are processed (control messages are ignored)
4. **Payload Parsing** - Tag groups and their tags are extracted from the JSON payload
5. **Path Construction** - RTDB path built as `/{tenant}/{devicetype}/{devicename}/{taggroup}/{tagname}`
6. **Auto-provisioning** - Database nodes and tags from devicename down are created automatically if they don't exist
7. **Value Storage** - Tag values are written to the RTDB with timestamps

## Error Handling

Devices should handle these common scenarios:

- **Connection refused** - Check broker address and port
- **Authentication failure** - Verify password matches server configuration
- **Publish failures** - Implement retry logic with exponential backoff
- **Network interruptions** - Use MQTT keep-alive and auto-reconnect

## Best Practices

1. **Use QoS 1** for critical telemetry to ensure delivery
2. **Include timestamps** when device time is accurate and important
3. **Batch related tags** in a single message to ensure consistency
4. **Use descriptive tag names** that indicate the measurement
5. **Group related tags** under appropriate `taggroup` names
6. **Maintain unique device names** within each tenant

## Server-Side Configuration

The following environment variables control the MQTT interface:

| Variable | Default | Description |
|----------|---------|-------------|
| `MQTT_URL` | `mqtt://127.0.0.1:1883` | Broker listen address |
| `MQTT_PASSWORD` | development fallback: `xact` | Client authentication password. Production mode requires a non-default value. |
| `EMBEDDED_MQTT_SERVER` | `yes` | Enable embedded broker |
| `MQTT_CLIENT_ENABLED` | `yes` | Enable MQTT client for data ingress |
| `MQTT_CLIENT_WORKERS` | `4` | Number of processing workers |
| `MQTT_CLIENT_QUEUE_SIZE` | `1000` | Message queue capacity |

## Monitoring

Server-side metrics are available at the RTDB path `/system/metrics/mqtt`:

- `queueLength` - Current message queue depth
- `activeWorkers` - Currently processing workers
- `totalProcessed` - Total messages processed
- `avgLatencyMs` - Average processing latency in milliseconds

## Troubleshooting

### Connection Issues

1. Verify the broker is running: `EMBEDDED_MQTT_SERVER=yes`
2. Check firewall rules for port 1883
3. Verify password matches server configuration

### Data Not Appearing

1. Check topic format matches pattern exactly
2. Verify payload is valid JSON
3. Review server logs for parsing errors
4. Check `/system/metrics/mqtt` for processing stats

### High Latency

1. Increase `MQTT_CLIENT_WORKERS` for higher throughput
2. Increase `MQTT_CLIENT_QUEUE_SIZE` to handle bursts
3. Check network connectivity between device and server
