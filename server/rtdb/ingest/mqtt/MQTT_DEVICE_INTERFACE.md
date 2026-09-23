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

The broker URL can be configured via the `MQTT_BROKER_URL` environment variable on the server (default: `mqtt://127.0.0.1:1883`).

### Authentication

The embedded broker requires an organisation ingest API key, created by an authorised administrator. The raw key is shown only on creation; store it in the device's secret store.

- **Username:** the organisation slug, for example `default`.
- **Password:** an ingest API key belonging to that organisation.
- **Client ID:** `<organisation>:<device-id>`, for example `default:boiler-01`.

There is no shared password or development fallback. `MQTT_BROKER_PASSWORD` configures only the server's connection to an external broker. The embedded server creates a private, process-local credential for its own ingest subscriber; do not configure devices as `xact:ingest`.

Devices can publish data only under their own organisation and subscribe only to `xact/data/<organisation>/...` and `xact/control/<organisation>/...`. Wildcards are allowed only after the fixed organisation prefix. Cross-tenant subscriptions, shared subscription prefixes, foreign client IDs, and foreign last-will topics are rejected. API-key deletion takes effect on subsequent authorization checks, including message delivery.

Use TLS when sending credentials over an untrusted network. With an external broker, configure equivalent tenant ACLs and separate credentials for the privileged ingest subscriber.

### Connection Parameters

- **Keep Alive:** 60 seconds (recommended)
- **Clean Session:** true
- **QoS Level:** 1 (recommended for reliable delivery)

## Topic Structure

Devices must publish to one of these patterns (the tenant must match the authenticated username):

```
xact/data/{tenant}/{devicetype}/{devicename}
xact/data/{tenant}/zone/{zone}/{devicetype}/{devicename}
```

### Components

| Component | Description | Example |
|-----------|-------------|---------|
| `xact` | Fixed prefix - must be exactly "xact" | `xact` |
| `{tenant}` | Organization name. Use `default` for single-tenant setups | `acme-corp`, `default` |
| `{msgtype}` | Device publishing uses `data`; subscriptions may also use `control`. |
| `{devicetype}` | Device category/type | `CTLR`, `VMS`, `PLC`, `SENSOR`, `METER` |
| `{devicename}` | Unique device identifier within the tenant | `boiler-01`, `floor-3-hvac` |

### Topic Examples

```
xact/data/default/CTLR/boiler-01
xact/data/acme-corp/SENSOR/floor-3-temp
xact/data/manufacturing/PLC/line-7-controller
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

1. **Authentication** - API-key ownership, tenant username, and client-ID namespace are verified
2. **Topic Validation** - Topic format is validated (`xact/{msgtype}/{tenant}/{devicetype}/{devicename}`)
3. **Message Type Filter** - Only messages with `msgtype=data` are processed (control messages are ignored)
4. **Payload Parsing** - Tag groups and their tags are extracted from the JSON payload
5. **Path Construction** - RTDB path built as `/{tenant}/{devicetype}/{devicename}/{taggroup}/{tagname}`
6. **Auto-provisioning** - Database nodes and tags from devicename down are created automatically if they don't exist
7. **Value Storage** - Tag values are written to the RTDB with timestamps

## Error Handling

Devices should handle these common scenarios:

- **Connection refused** - Check broker address and port
- **Authentication failure** - Verify the API key belongs to the username organisation and the client ID has that organisation prefix
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
| `MQTT_BROKER_URL` | `mqtt://127.0.0.1:1883` | Broker listen address |
| `MQTT_BROKER_PASSWORD` | none | External broker ingest-client password only; embedded devices use tenant API keys. |
| `EMBEDDED_MQTT_SERVER` | `yes` | Enable embedded broker |
| `MQTT_CLIENT_ENABLED` | `yes` | Enable MQTT client for data ingress |
| `MQTT_CLIENT_TLS_CA_FILE` | unset | CA or self-signed certificate file trusted by the XACT ingest client for MQTT over TLS. Defaults to `server.crt` under `HTTP_CERTS_DIR`/`HTTPS_CERTS_DIR` when available. |
| `MQTT_CLIENT_TLS_SERVER_NAME` | unset | Optional server name used when verifying the broker certificate. |
| `MQTT_CLIENT_TLS_INSECURE_SKIP_VERIFY` | `false` | Disable broker certificate verification for local development only. |
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
3. Verify the API key belongs to the username organisation and the client ID has that organisation prefix

### Data Not Appearing

1. Check topic format matches pattern exactly
2. Verify payload is valid JSON
3. Review server logs for parsing errors
4. Check `/system/metrics/mqtt` for processing stats

### High Latency

1. Increase `MQTT_CLIENT_WORKERS` for higher throughput
2. Increase `MQTT_CLIENT_QUEUE_SIZE` to handle bursts
3. Check network connectivity between device and server
