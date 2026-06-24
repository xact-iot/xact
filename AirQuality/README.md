# AirQuality Driver

Standalone Los Angeles - Long Beach air-quality IoT simulation for testing RTDB templates and device variations.

The driver creates two reusable templates under `default.Templates`:

- `AirQualityStandard`
- `AirQualityBackup`

It then provisions 25 devices by default under one `AirQuality` device branch: one device per configured intersection, alternating between standard and battery-backup variations. Templates and telemetry are both published to XACT over MQTT. Device messages include top-level `templateName` metadata so XACT can select the reusable RTDB template for each device node without creating a separate device branch for every variation.

## Run

```bash
go run . -mqtt-url tcp://127.0.0.1:1883
```

Useful flags:

- `-devices 25` controls how many intersections are provisioned; it cannot exceed the configured intersection count.
- `-period 5m` sets each device's update interval.
- `-startup-spread 0s` publishes initial device creation messages as fast as MQTT acknowledges them; set a non-zero duration to throttle startup.
- `-template-wait 3s` lets MQTT template provisioning settle before devices are created.
- `-provision-only` creates templates/devices once and exits.

Environment defaults mirror the other XACT tools: `MQTT_BROKER`, `MQTT_USERNAME`, and `MQTT_BROKER_PASSWORD`.
For MQTT over TLS, use `mqtts://...` in `MQTT_BROKER`. Publicly trusted broker certificates work like HTTPS; for a local self-signed XACT broker, set `MQTT_TLS_CA_FILE` to the broker certificate, for example `../server/certs/server.crt`.
