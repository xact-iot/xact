# LTA VMS Driver

A standalone Go program that interfaces with the Singapore Land Transport Authority (LTA) DataMall API to retrieve Variable Message Sign (VMS) data and publish it to the XACT server via MQTT.

## Overview

This driver polls the LTA DataMall API for VMS (Variable Message Sign) data and sends it to XACT using the MQTT protocol. VMS signs display traffic information and messages on Singapore roads.

## API Reference

- LTA DataMall API Documentation: https://datamall.lta.gov.sg/
- VMS Endpoint: `http://datamall2.mytransport.sg/ltaodataservice/VMS`

## Installation

```bash
cd server/drivers/lta
go build -o lta-driver .
```

## Configuration

The driver is configured via environment variables:

| Variable | Required | Default | Description |
|----------|----------|---------|-------------|
| `LTA_API_KEY` | Yes | - | Your LTA DataMall API key (AccountKey) |
| `MQTT_BROKER` | No | `tcp://127.0.0.1:1883` | MQTT broker address |
| `MQTT_BROKER_PASSWORD` | No | `xact` | MQTT authentication password |
| `POLL_INTERVAL` | No | `5m` | Polling interval (e.g., `5m`, `30s`, `1h`) |

## Running

```bash
# Set required environment variable
export LTA_API_KEY="your-api-key-here"

# Optional: customize MQTT broker
export MQTT_BROKER="tcp://192.168.1.100:1883"

# Run the driver
./lta-driver
```

## Data Mapping

The LTA VMS data is mapped to XACT's MQTT topic structure as follows:

| LTA Field | XACT Mapping | Value |
|-----------|--------------|-------|
| - | Tenant | `default` |
| - | Device Type | `VMS` |
| EquipmentID | Device Name | Equipment ID from LTA |
| - | Tag Group | `sign` |
| Latitude | Payload Field | `latitude` |
| Longitude | Payload Field | `longitude` |
| Message | Payload Field | `message` |

### MQTT Topic Format

```
xact/default/VMS/{EquipmentID}/sign
```

### Example Payload

```json
{
  "latitude": 1.3521,
  "longitude": 103.8198,
  "message": "TRAFFIC MOVING SLOWLY AHEAD",
  "ts": "1704067200000"
}
```

## Behavior

- **Startup**: Polls the LTA API immediately and publishes all VMS data
- **Polling**: Polls every 5 minutes (configurable via `POLL_INTERVAL`)
- **Auto-reconnect**: Automatically reconnects to MQTT broker if connection is lost
- **Error handling**: Continues operation even if individual VMS records fail to publish

## Getting an LTA API Key

1. Visit https://datamall.lta.gov.sg/
2. Register for an account
3. Request API access
4. Your AccountKey will be provided for use with the API

## Logging

The driver logs to stdout:
- Connection status
- Polling operations
- Number of records retrieved and published
- Errors (with continuation)

Example output:
```
LTA VMS Driver starting...
MQTT Broker: tcp://127.0.0.1:1883
Poll Interval: 5m0s
LTA driver: connected to MQTT broker
Polling LTA API for VMS data...
Retrieved 47 VMS records
Published 47 VMS data points
```

## Troubleshooting

### "LTA_API_KEY environment variable is required"
Set the `LTA_API_KEY` environment variable with your LTA DataMall API key.

### "Failed to connect to MQTT broker"
- Verify the MQTT broker is running
- Check the `MQTT_BROKER` address is correct
- Ensure the MQTT password matches the server configuration

### No data appearing in XACT
- Check the driver logs for errors
- Verify the MQTT interface is enabled in XACT (`MQTT_CLIENT_ENABLED=yes`)
- Check XACT metrics at `/system/metrics/mqtt`
- Ensure the LTA API key is valid and has VMS access
