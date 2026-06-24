# Data Ingest

This chapter is for developers and integrators who need to send data into XACT from external devices, PLCs, gateways, or software systems.

XACT provides three ingest methods: **NATS** (the most efficient option, with explicit admission into the shared ingest queue), **MQTT** (for persistent device connections via the embedded broker or a configured broker), and the **REST API** (for HTTP-based integrations and batch uploads).

Simple benchmarks show a performance difference between these ingest methods. On a mid range laptop with a single server instance the ballpark sustained ingest rate is

  - NATS: 1.8K msgs/sec
  - MQTT: 1.2K msgs/sec
  - REST: 640 msgs/sec

Each message updates 12 tags. The REST message rate can be improved with a connection pool on the sender, but even 640 msgs/sec could be enough for many systems.

---
## Custom Devices and Protocols

It is not practical for the core XACT code to embed every possible protocol used to communicate with external devices. The approach taken is to either 

 - Implement one of the XACT native protocols directly in the device.
 - Use a stand alone program to convert the protocol from the device to one of the XACT native protocols.

The stand alone option can be written in your language of choice.

The demo section in the repo has examples that show:
  - An example Python3 driver for sending data using the HTTP REST API.
  - LTA VMS and traffic images drivers fetch data using HTTP REST and publish this to XACT with MQTT.
  - Water driver simulates a small town water reticulation system and publishes to XACT with the REST API.
  - ISS driver fetches data with a proprietary protocol and publishes to XACT with NATS.


---

## MQTT Ingest

XACT includes an embedded MQTT broker. External devices publish messages to this broker, and XACT automatically ingests the data into the tag tree.

### Connection Details

| Setting | Default Value |
|---------|--------------|
| Embedded broker listen address | `mqtt://127.0.0.1:1883` (`MQTT_URL`) |
| Ingest client broker address | `tcp://127.0.0.1:1883` (`MQTT_CLIENT_URL`) |
| Device authentication | Password-based (`MQTT_BROKER_PASSWORD`, default `xact`) |
| XACT ingest client authentication | Optional username plus broker password (`MQTT_CLIENT_USERNAME`, `MQTT_BROKER_PASSWORD`) |
| Protocol | MQTT v3.1.1 or v5 |

The embedded broker starts by default unless `EMBEDDED_MQTT_SERVER=no`. The MQTT ingest client starts by default when an embedded broker is running or `MQTT_URL` is set, unless `MQTT_CLIENT_ENABLED=no`.

### Topic Format

XACT supports two MQTT topic formats: **zoneless** (the simple form) and **zoned** (which includes an optional zone grouping between tenant and device type).

Publish to one of these topic structures:

#### Zoneless Format
```
xact/{msgtype}/{tenant}/{devicetype}/{devicename}
```
| Segment | Description | Example |
|---------|-------------|---------|
| `xact` | Fixed prefix - always `xact` | `xact` |
| `{msgtype}` | Message type - use `data` for ingest | `data` |
| `{tenant}` | Organisation name. Use `default` if not multi-tenant | `default`, `AcmeCorp` |
| `{devicetype}` | The type of device - creates a grouping node | `VMS`, `PLC`, `Pump`, `Meter` |
| `{devicename}` | Unique device name within the organisation | `Sign001`, `Pump03`, `Meter_Building_A` |

#### Zoned Format
```
xact/{msgtype}/{tenant}/zone/{zone}/{devicetype}/{devicename}
```
| Segment | Description | Example |
|---------|-------------|---------|
| `xact` | Fixed prefix - always `xact` | `xact` |
| `{msgtype}` | Message type - use `data` for ingest | `data` |
| `{tenant}` | Organisation name | `default`, `AcmeCorp` |
| `zone` | Literal marker - identifies the next segment as a zone name | `zone` |
| `{zone}` | Zone identifier - groups devices within a physical or logical area | `building_a`, `floor2`, `outdoor` |
| `{devicetype}` | The type of device | `VMS`, `PLC`, `Pump` |
| `{devicename}` | Unique device name within the organisation | `Sign001`, `Pump03` |

**Example topics (zoneless):**
```
xact/data/default/VMS/Sign001
xact/data/AcmeCorp/Pump/Pump03
xact/data/default/Meter/Meter_Building_A
```

**Example topics (zoned):**
```
xact/data/acme/zone/building_a/VMS/Sign001
xact/data/acme/zone/floor2/HVAC/AHU01
xact/data/default/zone/outdoor/meteo/Weather001
```

The two formats are distinguished by the number of topic parts: 5 parts = zoneless, 7 parts = zoned (with the literal `zone` marker at position 3). RTDB tag paths follow the same structure: `tenant.zone.devicetype.devicename.tagdata` when zone is present, or `tenant.devicetype.devicename.tagdata` without it.

### Payload Format

The payload is a JSON object. Each top-level key is either a **scalar tag** (written directly under the device) or a **tag group** (a nested object containing multiple tags).

#### Scalar Tags

Keys that map to a simple value (number, string, or boolean):

```json
{
    "lat": 51.5074,
    "lon": -0.1278,
    "online": true,
    "status": "Normal"
}
```

This creates tags at:
```
{tenant}.{devicetype}.{devicename}.lat = 51.5074
{tenant}.{devicetype}.{devicename}.lon = -0.1278
{tenant}.{devicetype}.{devicename}.online = true
{tenant}.{devicetype}.{devicename}.status = "Normal"
```

#### Tag Groups

Keys that map to a nested object create a sub-group:

```json
{
    "metrics": {
        "temperature": 72.4,
        "pressure": 3.2,
        "flowRate": 15.7
    },
    "status": {
        "online": true,
        "mode": "Auto"
    }
}
```

This creates tags at:
```
{device}.metrics.temperature = 72.4
{device}.metrics.pressure = 3.2
{device}.metrics.flowRate = 15.7
{device}.status.online = true
{device}.status.mode = "Auto"
```

#### Object Tags with Metadata

Tags inside a group can be sent as objects to include metadata alongside the value. This is useful for initial provisioning - the metadata fields are typically only sent once on first connection:

```json
{
    "metrics": {
        "supplyTemp": {
            "value": 72.4,
            "description": "Supply air temperature",
            "units": "C",
            "deadband": 0.5,
            "history": true,
            "limits": {"hi": 80, "lo": 20},
            "persist": true
        }
    }
}
```

| Field | Description |
|-------|-------------|
| `value` | The tag's current value |
| `description` | The tag description shown in the tag tree and tag editor |
| `units` | The tag's unit of measurement |
| `deadband` | Used by processing blocks to filter 'noise' |
| `history` | Boolean - adds a History Recorder processing block if `true` |
| `limits` | Object - configures the Limit Check processing block. See [Processing Blocks](#processing-blocks) |
| `persist` | Boolean - adds a Persist processing block if `true` |
| `scaling` | Object - configures a Scaling processing block |
| `stalecheck` | Number - configures stale timeout in seconds |
| `publish` | Boolean - adds a Publish processing block if `true` |
| `type` | Use `"enum"` to create or convert the tag to an enum |
| `enumValues` | Object mapping integer values to display labels for enum tags |

Not all fields are required. If `value` is omitted, only the configuration fields are applied. If a tag already exists, metadata such as `description` and `units` is updated, and existing processing block parameters are updated with the new values.

In practice, configuration fields are sent once on first connection. Subsequent messages send only the value:

```json
{
    "metrics": {
        "supplyTemp": 73.1
    }
}
```

#### Mixed Payloads

Scalar tags, tag groups, and object tags can be mixed in the same payload:

```json
{
    "lat": 51.5074,
    "lon": -0.1278,
    "metrics": {
        "supplyTemp": {"value": 72.4, "units": "C"},
        "returnTemp": 68.9
    }
}
```

#### Arrays and Nested Groups

JSON arrays are expanded into individually addressable array nodes. A scalar array creates leaves named `0`, `1`, `2`, and so on; an array of objects creates one node per array element with leaves for each object field.

```json
{
    "batteries": {
        "cellVoltage": {"value": [3.91, 3.88, 3.9], "units": "V"},
        "packs": [
            {"voltage": 48.2, "current": 12.1},
            {"voltage": 48.0, "current": 11.9}
        ]
    }
}
```

Nested object groups are supported one additional level below the first group. For example, `{"pumps":{"0":{"flow":12.4}}}` creates `pumps.0.flow`.

#### Timestamps

The `ts` field is a special key interpreted as a timestamp (Unix milliseconds), not as a tag value. XACT stores one timestamp per ingest payload:

- **Top-level `ts`** - applies to all tags in the payload.
- **Group-level `ts`** - used as the payload timestamp only if a timestamp has not already been set.

If `ts` is not present, the server uses its current time.

```json
{
    "ts": 1709312400000,
    "temperature": 22.5,
    "humidity": 65.3
}
```

#### Template Assignment

The top-level `templateName` field is interpreted as ingest metadata, not as a tag value. When present, XACT assigns the template to the concrete device node while processing the payload, so devices under the same device-type branch can use different reusable templates.

```json
{
    "templateName": "Templates.AirQualityStandard",
    "metrics": {
        "pm25": 88.4,
        "temperature": 35.2
    }
}
```

Template names are relative to the organisation root. For example, `Templates.AirQualityStandard` resolves to `{tenant}.Templates.AirQualityStandard`.

### Auto-Provisioning

When a message arrives for a device or tag that doesn't exist yet in the tag tree:

1. The organisation node is located (or created if it's the first device for that tenant).
2. A device-type node is created (e.g. `VMS`).
3. A device node is created (e.g. `Sign001`).
4. Tags are created with their types inferred from the JSON value types.
5. Processing blocks specified in object tags are configured.

This means you can start sending data immediately - no pre-configuration is required. The tag tree builds itself from the incoming data.

---

## Processing Blocks

Processing blocks are configured by including a named object inside an object tag. All fields within each object are optional - omit a field to leave the previous setting unchanged. Each block can be provisioned on first contact and updated independently on subsequent messages.

All processing blocks can be provisioned in a single message - include multiple block objects together inside one tag. Existing blocks are updated (not duplicated) when a new configuration arrives, so the full pipeline for a tag can be redefined over time without any reset.

### Limit Check Block

**JSON object name:** `limits`

The limit check block monitors a numeric tag value against `hi` and `lo` thresholds. When a threshold is crossed it sets the tag's **alarm state**, and optionally emits an event and sends a notification.

```json
    "supplyTemp": {
        "value": 72.4,
        "units": "°C",
        "limits": {
            "hi": 80,
            "lo": 20,
            "hiEvent": {
                "enabled": true,
                "severity": "WARN",
                "message": "High temperature alarm",
                "notificationId": "manager"
            },
            "lowEvent": {
                "enabled": true,
                "severity": "WARN",
                "message": "Low temperature alarm",
                "notificationId": "manager"
            },
            "rtnEvent": {
                "enabled": true,
                "severity": "INFO",
                "message": "Temperature returned to normal",
                "notificationId": "none"
            }
        }
    }
```

**`limits` fields**

| Field | Type | Description |
|-------|------|-------------|
| `hi` | number | High limit threshold |
| `lo` | number | Low limit threshold |
| `hiEvent` | object | Event emitted when value rises above `hi` |
| `lowEvent` | object | Event emitted when value falls below `lo` |
| `rtnEvent` | object | Event emitted when value returns to normal |

**`hiEvent` / `lowEvent` / `rtnEvent` fields**

| Field | Type | Description |
|-------|------|-------------|
| `enabled` | boolean | Whether to emit this event. Default `false` if omitted |
| `severity` | string | One of: `DEBUG`, `INFO`, `WARN`, `ERROR`, `CRITICAL`. Default `WARN` for alarm events, `INFO` for return-to-normal |
| `message` | string | Custom event message. Default is a built-in message |
| `notificationId` | string or number | Notification profile to trigger. See below |

**`notificationId` values**

String names are the most convenient way to specify a notification profile:

| String | Maps to | Description |
|--------|---------|-------------|
| `"none"` | ID `0` | No notification |
| `"sysadmin"` | ID `1` | SysAdmin notification profile |
| `"manager"` | ID `2` | Manager notification profile |
| `"tech"` | ID `3` | Technician notification profile |

A numeric ID can be used directly if you know the profile's internal ID.

**Provisioning behaviour**

- Sending the `limits` object for the first time creates the limit check block on that tag.
- Subsequent messages update the existing block's parameters - you only need to send what changed.
- The tag value (`value`) and the limits can be updated independently:

```json
    "supplyTemp": {
        "value": 82.1
    }
```

```json
    "supplyTemp": {
        "limits": {
            "hi": 85,
            "lo": 15
        }
    }
```

---

### Scaling Block

**JSON object name:** `scaling`

Applies a linear transformation to the value: `output = (input × scale) + offset`, with optional clamping to a `min`/`max` range.

```json
    "rawPressure": {
        "value": 1638,
        "units": "counts",
        "scaling": {
            "scale": 0.01,
            "offset": -10.5,
            "min": 0,
            "max": 100
        }
    }
```

**`scaling` fields**

| Field | Type | Description |
|-------|------|-------------|
| `scale` | number | Multiplier applied to the input value. Default `1` |
| `offset` | number | Added after scaling. Default `0` |
| `min` | number | Optional lower clamp - output will not go below this value |
| `max` | number | Optional upper clamp - output will not exceed this value |

**Example: raw 4–20 mA current loop sensor**

A pressure transducer with a 4–20 mA output, 0–250 bar range:

```json
    "pressure": {
        "value": 12.8,
        "units": "mA",
        "scaling": {
            "scale": 15.625,
            "offset": -62.5,
            "min": 0,
            "max": 250
        }
    }
```

`scale = 250 / (20 - 4) = 15.625`
`offset = -(4 × 15.625) = -62.5`
Result: 12.8 mA → `1638 × 0.01 - 10.5` = **5.88 bar** (for the first example)

---

### History Recorder Block

**JSON field name:** `history`

Records value changes that exceed a deadband. The tag is only written to history storage when the new value differs from the last recorded value by more than the deadband, reducing storage noise from stable sensors.

```json
    "temperature": {
        "value": 23.7,
        "units": "°C",
        "history": true
    }
```

**`history` field**

| Field | Type | Description |
|-------|------|-------------|
| `history` | boolean | If `true`, numeric value changes outside the tag deadband are written to metrics storage |


---

### Stale Check Block

**JSON field name:** `stalecheck`

Sets the **STALE** alarm state on the tag if no new value arrives within the configured timeout. The timer restarts each time a new value is received.

```json
    "level": {
        "value": 1.42,
        "units": "m",
        "stalecheck": 120
    }
```

**`stalecheck` field**

| Field | Type | Description |
|-------|------|-------------|
| `stalecheck` | number | Timeout in **seconds**. If no new value arrives within this window the tag enters STALE alarm state |

A value of `120` means: if no new reading arrives within 2 minutes, the tag is marked stale. Sending a new value resets the timer.

---

### Persist Block

**JSON field name:** `persist`

Caches the latest tag value to the NATS KV-backed persist store and restores it on startup. Writes are suppressed when numeric changes are within the tag deadband.

```json
    "energy_kwh": {
        "value": 8423.1,
        "units": "kWh",
        "persist": true
    }
```

**`persist` field**

| Field | Type | Description |
|-------|------|-------------|
| `persist` | boolean | `true` enables durable last-value storage in the NATS KV-backed persist store. `false` or absent leaves the persist block unchanged or unprovisioned |

---

### Publish Block

**JSON field name:** `publish`

Publishes the tag's current value and status to the real-time `tagvalue...` NATS stream used by dashboards and subscribers. Values are published after upstream processing blocks, so scaling and status changes are reflected.

```json
    "netPower": {
        "value": 315.2,
        "units": "kW",
        "publish": true
    }
```

**`publish` field**

| Field | Type | Description |
|-------|------|-------------|
| `publish` | boolean | `true` enables real-time tag-value publishing after processing |

Publishing is also triggered by stale-state changes and is filtered by value/status change detection and the tag deadband where applicable.

---

## REST API Ingest

The REST API provides an HTTP endpoint for sending device data. This is suitable for integrations that prefer HTTP over MQTT, or for batch data uploads.

### Endpoint

The REST API supports both zoneless and zoned paths:

```
POST /api/v1/ingest/{tenant}/{devicetype}/{devicename}
POST /api/v1/ingest/{tenant}/zone/{zone}/{devicetype}/{devicename}
DELETE /api/v1/ingest/{tenant}/{devicetype}/{devicename}
DELETE /api/v1/ingest/{tenant}/zone/{zone}/{devicetype}/{devicename}
```

The URL path segments mirror the MQTT topic structure. Zone is optional - omit it to use the simple path.
Use `POST` to create or update a device's tags. Use `DELETE` to remove a device node and all of its child tags when the external source knows the device no longer exists.

### Authentication

REST ingest uses **API keys** rather than user credentials. Include the key in the request header:

```
Authorization: ApiKey <your-api-key>
```

API keys are created and managed in the **Organisations** widget. Each key is bound to a specific organisation - the `{tenant}` in the URL must match the key's organisation. The full key is shown only when it is generated; save it in the device or integration secret store before closing the panel.

### Request Body

`POST` requests use the same JSON payload format as the MQTT payload described above:

```json
{
    "metrics": {
        "temperature": 72.4,
        "pressure": 3.2
    },
    "status": {
        "online": true
    }
}
```

`DELETE` requests do not use a request body.

### Example Request

```bash
curl -X POST \
  http://xact-server:8080/api/v1/ingest/default/Pump/Pump01 \
  -H "Authorization: ApiKey abc123def456" \
  -H "Content-Type: application/json" \
  -d '{
    "metrics": {
        "flowRate": 15.7,
        "pressure": 3.2,
        "temperature": 42.1
    },
    "status": {
        "running": true,
        "mode": "Auto"
    }
  }'
```

**Zoned example:**

```bash
curl -X POST \
  http://xact-server:8080/api/v1/ingest/acme/zone/building_a/HVAC/AHU01 \
  -H "Authorization: ApiKey abc123def456" \
  -H "Content-Type: application/json" \
  -d '{
    "metrics": {
        "supplyTemp": 18.5,
        "returnTemp": 24.2
    }
  }'
```

**Delete example:**

```bash
curl -X DELETE \
  http://xact-server:8080/api/v1/ingest/acme/zone/building_a/HVAC/AHU01 \
  -H "Authorization: ApiKey abc123def456"
```

### Differences from MQTT

- Authentication is via API key rather than MQTT credentials.
- REST currently supports both device updates and device deletes. MQTT and NATS ingest currently support updates; device delete parity is planned.
- REST is stateless - there is no persistent connection. Each request is independent.
- REST requests are accepted only after XACT admits the event into the shared bounded ingest queue. If the queue is full, XACT returns `503 Service Unavailable`.
- REST may be more suitable for low-frequency batch uploads, while MQTT is better for continuous real-time streaming.

---

## NATS Ingest

NATS is the most efficient ingest method: drivers connect directly to the XACT server's embedded NATS server using a standard NATS client library. This bypasses REST HTTP overhead and the MQTT broker, while still using the same bounded ingest relay as the other ingest paths.

External drivers should use the admitted request/reply subject:

```
xact.internal.ingest_request.{tenant}.{deviceType}.{deviceName}              # zoneless
xact.internal.ingest_request.{tenant}.zone.{zone}.{deviceType}.{deviceName} # zoned
```

For example: `xact.internal.ingest_request.acme.zone.building_a.VMS.Sign001` for a zoned device, or `xact.internal.ingest_request.default.NASA.ISS` for a zoneless one.

The older core subject prefix, `xact.internal.ingest.`, is the internal fan-out stream consumed by XACT servers. Use it only for trusted internal components that deliberately bypass request/reply admission and accept at-most-once publish semantics.

### Environment Variables

| Variable | Default | Description |
|----------|---------|-------------|
| `NATS_URL` | `nats://127.0.0.1:4222` | NATS server URL |
| `NATS_INTERNAL_PASSWORD` | *(none)* | Optional password for the `internal` NATS user. Set this when the server has authentication enabled |

### Driver Pattern

No XACT server internals are required. Any language with a NATS client library can send an `IngestEvent` JSON envelope and wait for an admission response:

```python
# Python example (using nats.py)
import json, os
import nats

async def publish():
    url     = os.getenv("NATS_URL", "nats://127.0.0.1:4222")
    event = {
        "tenant": "default",
        "zone": "",
        "device_type": "NASA",
        "device_name": "ISS",
        "tag_data": {
            "Groups": {
                "orbit": {"altitude": 408.5, "speed": 7.66},
                "env": {"cabin_temp": 22.5},
            },
            "DirectTags": {},
            "TSUnixMilli": 1709312400000,
        },
    }
    nc = await nats.connect(url)
    reply = await nc.request(
        "xact.internal.ingest_request.default.NASA.ISS",
        json.dumps(event).encode(),
        timeout=10,
    )
    print(reply.data.decode())
    await nc.close()
```

```javascript
// JavaScript/Node.js example (using nats.js)
import { connect } from "nats";

const nc = await connect({ servers: "nats://127.0.0.1:4222" });
// Zoneless: xact.internal.ingest_request.{tenant}.{deviceType}.{deviceName}
// Zoned:    xact.internal.ingest_request.{tenant}.zone.{zone}.{deviceType}.{deviceName}
const msg = await nc.request(
  "xact.internal.ingest_request.default.NASA.ISS",
  JSON.stringify({
    tenant: "default",
    zone: "",
    device_type: "NASA",
    device_name: "ISS",
    tag_data: {
      Groups: {
        orbit: { altitude: 408.5, speed: 7.66 },
        env: { cabin_temp: 22.5 },
      },
      DirectTags: {},
      TSUnixMilli: Date.now(),
    },
  }),
  { timeout: 10000 }
);
console.log(new TextDecoder().decode(msg.data));
await nc.close();
```

### NATS Driver Specification

```
Connection:
  - Protocol: NATS v1.x
  - URL: nats://<xact-host>:4222
  - Authentication: optional - set NATS_INTERNAL_PASSWORD env var
  - Auto-reconnect: yes (exponential backoff recommended)

Publishing:
  - Subject (zoneless): xact.internal.ingest_request.<organisation>.<device-type>.<device-name>
  - Subject (zoned):    xact.internal.ingest_request.<organisation>.zone.<zone>.<device-type>.<device-name>
  - Operation: request/reply
  - Payload: JSON IngestEvent envelope
  - Successful reply: {"status":"accepted"}
  - Busy reply: {"status":"busy","error":"..."}

IngestEvent envelope:
  {
    "tenant": "<organisation>",
    "zone": "<zone-or-empty>",
    "device_type": "<device-type>",
    "device_name": "<device-name>",
    "tag_data": {
      "Groups": {
        "<group-name>": {
          "<tag-name>": <value-or-expanded-tag-object>
        }
      },
      "DirectTags": {
        "<tag-name>": <value>
      },
      "TSUnixMilli": <unix-milliseconds>,
      "TemplateName": "<optional-template-name>"
    }
  }

The subject route and the envelope route should match. XACT processes the envelope fields.

Value types:
  - number (integer or float) → XACT integer or float tag
  - string → XACT string tag
  - boolean (true/false) → XACT boolean tag
  - object with "value" key → XACT tag with metadata (inside MQTT/REST groups, or inside NATS Groups/DirectTags)
  - object without "value" key → XACT tag group (recurse)
  - array → XACT array node with indexed children

Envelope fields:
  - "TSUnixMilli": Unix milliseconds timestamp for the whole event
  - "TemplateName": optional template assigned to the concrete device node

Object tag metadata fields inside Groups or DirectTags:
  - "value": tag value
  - "description": tag description string
  - "units": tag units string
  - "deadband": numeric deadband used by publish, history, and persist filtering
  - "history": boolean, enables history recording
  - "limits": {"hi": N, "lo": M, "hiEvent": {...}, "lowEvent": {...}, "rtnEvent": {...}}, enables and configures limit checking with full event and notification support
  - "scaling": {"scale": N, "offset": N, "min": N, "max": N}, enables linear scaling
  - "stalecheck": seconds before the tag is marked stale
  - "persist": boolean, enables durable last-value persistence
  - "publish": boolean, enables real-time tag-value publishing
  - "type": "enum", creates or converts the tag to an enum
  - "enumValues": {"0": "Off", "1": "On"}, maps enum values to display labels
```

### When to Use NATS

NATS is the preferred method when:
- The driver has access to the NATS port (default 4222) on the XACT server
- Maximum throughput and minimum latency are required
- The driver and XACT server run in the same data centre or VPC

Use MQTT or REST when:
- The device only supports MQTT or HTTP natively
- The driver must run outside the trusted network and cannot reach the NATS port

---

## Building a Driver - AI Reference

The following reference describes the XACT data ingest interface in a language-agnostic way. It can be used as a specification when building a driver or integration in any programming language.

### Goal

Build a component that reads data from a source system (PLC, sensor, API, database, etc.) and sends it to XACT via MQTT, REST, or directly via NATS.

### MQTT Driver Specification

```
Connection:
  - Protocol: MQTT v3.1.1 or v5
  - Broker: tcp://<xact-host>:1883
  - Authentication: password-based (default: "xact")
  - Clean session: true (recommended)
  - Keep-alive: 60 seconds (recommended)

Publishing:
  - Topic (zoneless):  xact/<msgtype>/<organisation>/<device-type>/<device-name>
  - Topic (zoned):     xact/<msgtype>/<organisation>/zone/<zone>/<device-type>/<device-name>
  - QoS: 1 (at least once) recommended
  - Payload: JSON object (UTF-8 encoded)
  - Retain: false (recommended)

Payload structure:
  {
    "<tag-or-group-name>": <value-or-nested-object>,
    ...
  }

Value types:
  - number (integer or float) → XACT integer or float tag
  - string → XACT string tag
  - boolean (true/false) → XACT boolean tag
  - object with "value" key inside a group → XACT tag with metadata
  - object without "value" key → XACT tag group (recurse)

Reserved keys:
  - "ts": Unix milliseconds timestamp (not stored as a tag)
  - "value": tag value (within an object tag)
  - "templateName": template assigned to the concrete device node while processing the payload
  - "description": tag description string (within an object tag)
  - "units": tag units string (within an object tag)
  - "deadband": numeric deadband used by publish, history, and persist filtering
  - "history": boolean, enables history recording
  - "limits": {"hi": N, "lo": M, "hiEvent": {...}, "lowEvent": {...}, "rtnEvent": {...}}, enables and configures limit checking with full event and notification support
  - "scaling": {"scale": N, "offset": N, "min": N, "max": N}, enables linear scaling
  - "stalecheck": seconds before the tag is marked stale
  - "persist": boolean, enables durable last-value persistence
  - "publish": boolean, enables real-time tag-value publishing
  - "type": "enum", creates or converts the tag to an enum
  - "enumValues": {"0": "Off", "1": "On"}, maps enum values to display labels

Recommended pattern:
  1. On first connection, send a payload with full metadata
     (value, description, units, deadband, history, limits, scaling, stalecheck, persist, publish) to provision tags.
  2. On subsequent updates, send values only (no metadata).
  3. Publish at a rate appropriate to the data source
     (typically 1-60 second intervals).
```

### REST Driver Specification

```
Update endpoint (zoneless): POST /api/v1/ingest/<organisation>/<device-type>/<device-name>
Update endpoint (zoned):    POST /api/v1/ingest/<organisation>/zone/<zone>/<device-type>/<device-name>
Delete endpoint (zoneless): DELETE /api/v1/ingest/<organisation>/<device-type>/<device-name>
Delete endpoint (zoned):    DELETE /api/v1/ingest/<organisation>/zone/<zone>/<device-type>/<device-name>

Headers:
  - Authorization: ApiKey <key>
  - Content-Type: application/json (POST only)

Body:
  - POST: Same JSON payload structure as MQTT (see above)
  - DELETE: No body

Response:
  - 204 No Content: data accepted or device deleted
  - 401 Unauthorized: invalid or missing API key
  - 403 Forbidden: API key belongs to a different organisation
  - 400 Bad Request: malformed payload or missing path parameters
  - 503 Service Unavailable: ingest queue full (POST)

Rate limiting:
  - No built-in rate limit, but respect server capacity
  - For high-frequency data, prefer NATS or MQTT over REST
```

### Driver Design Guidelines

1. **Reconnection** - MQTT and NATS drivers should implement automatic reconnection with exponential backoff.
2. **Buffering** - if the connection is lost, buffer messages locally and send when reconnected.
3. **Timestamps** - include `ts` if the source provides accurate timestamps. Omit it to use server time.
4. **Provisioning** - send metadata fields (description, units, history, limits) on first connection or when configuration changes. Do not send them on every message.
5. **Tag naming** - use consistent, descriptive tag names. Avoid spaces and special characters. Use camelCase or snake_case.
6. **Group structure** - organise related tags into groups (nested objects) for clarity in the tag tree.
7. **Use NATS when possible** - it has the lowest overhead and the simplest dependency list (any language with a NATS client SDK; no XACT server code needed).
