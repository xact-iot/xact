# XACT Ingest Benchmark

This standalone tool stress-tests the three XACT ingest methods:

- `mqtt`: publishes device payload JSON to the MQTT broker.
- `rest`: posts device payload JSON to the REST ingest API.
- `nats`: sends normalized `IngestEvent` JSON over NATS request/reply to `xact.internal.ingest_request.*`.

The tool first provisions deterministic benchmark devices under the selected organisation, then runs either a burst or sustained benchmark. The primary metric is accepted ingest messages per second from the benchmark client's point of view. NATS mode counts a message only after XACT replies that it was accepted into the bounded ingest queue.

## Build

```sh
cd benchmark
go build .
```

## Examples

Provision 100 devices and run a maximum-rate NATS burst:

```sh
NATS_INTERNAL_PASSWORD=... ./benchmark -method nats -messages 50000 -devices 100 -concurrency 8
```

Run a 60 second maximum-rate MQTT test:

```sh
./benchmark -method mqtt -mode sustained -duration 60s -mqtt-url tcp://127.0.0.1:1883 -mqtt-password xact
```

Run REST ingest at a target rate:

```sh
./benchmark -method rest -mode sustained -duration 2m -rate 500 -api-key ... -rest-url https://127.0.0.1:8443/xact
```

If `-api-key` is omitted, the REST sender logs in with `-username` and `-password` and creates or reuses an API key named `XACT Benchmark`.

Provision only:

```sh
./benchmark -method nats -provision-only -devices 1000
```

## Important Flags

- `-method mqtt|rest|nats`: ingest path under test.
- `-mode burst|sustained`: fixed-message burst or duration-based run.
- `-messages N`: burst message count.
- `-duration 30s`: sustained test duration.
- `-rate N`: target messages per second. Use `0` for maximum possible.
- `-concurrency N`: parallel sender workers.
- `-devices N`: number of benchmark devices to cycle through.
- `-tenant default`, `-zone`, `-device-type BENCH`: ingest route.
- `-provision`: send deterministic tag configuration before the run.
- `-provision-only`: send tag configuration and exit.

## Provisioned Tag Mix

Each device gets the same tag groups and tag configuration:

- `meta`: `name`, `lat`, `lon`.
- `analog`: `temperature`, `pressure`, `flow`, `vibration`.
- `status`: `online`, `state`, `mode` enum.
- `counters`: `cycle_count`, `quality`.

The provision payload includes a repeatable mix of processing blocks:

- `scaling`
- `limitcheck`
- `historyrecorder`
- `stalecheck`
- `persist`
- `publish`

After provisioning, benchmark messages send value-only payloads so repeated tests compare steady-state ingest and processing cost rather than first-seen tag creation cost.
