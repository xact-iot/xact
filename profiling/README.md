# XACT / Translink / Chrome profiling

The harness runs a separate stack with copies of the active public-bus GTFS
configuration and dashboard definitions. Original application databases are
not used for writes. Stop the original public-bus app before copying its SQLite
configuration; copying a database with an outstanding WAL is rejected.

Requirements: Go, Python 3, installed `ui/node_modules`, Chrome at
`/usr/bin/google-chrome`, the adjacent `fleet-tracking` checkout, its configured
Translink API key, and a desktop session for headed Chrome.

Build from the workspace root:

```bash
mkdir -p profiling/results
(cd server && GOCACHE=/tmp/xact-go-build /usr/local/go/bin/go build -o ../profiling/results/xact ./startup)
(cd ../fleet-tracking && GOCACHE=/tmp/xact-go-build /usr/local/go/bin/go build -o ../XACT/profiling/results/public-bus ./cmd/public-bus)
(cd ../fleet-tracking && GOCACHE=/tmp/xact-go-build /usr/local/go/bin/go build -o ../XACT/profiling/results/translink-adapter ./cmd/translink-adapter)
(cd ui && npm run build -- --outDir ../profiling/results/ui --sourcemap)
python3 profiling/export-dashboards.py
```

In one terminal, launch the stack:

```bash
python3 profiling/run-stack.py --run investigation --duration 600 --feed-delay 60
```

Wait for its `Ready` message, then run Chrome captures from another terminal:

```bash
node profiling/chrome-profile.mjs profiling/results/investigation/state.json Buses 180
node profiling/chrome-profile.mjs profiling/results/investigation/state.json 'Tags Manager' 60
node profiling/chrome-profile.mjs profiling/results/investigation/state.json 'Buses Configuration' 60
node profiling/summarize-chrome.mjs profiling/results/investigation/buses
```

Each capture uses a separate Chrome instance/profile and signs in normally as
the isolated test admin. Tags Manager expands a bus group. Captures include
login, widget data hydration, widget setup, and the requested measurement
window. CPU totals therefore include startup; compare interval metrics for
steady-state behavior. A `.cpuprofile` loads into Chrome DevTools' JavaScript
profiler, and `.heapprofile` into its allocation profiler. Source maps are
retained beside the test UI assets. `metrics.jsonl` contains JS heap, DOM,
listeners, CPU and aggregate Chrome RSS. RSS sums can count shared pages twice.

Network payload recording is off by default because mirroring all WebSocket
frames into DevTools can itself be expensive. Use it once to measure volume,
and compare with a recording without it:

```bash
XACT_PROFILE_NETWORK=1 XACT_PROFILE_LABEL=buses-network \
  node profiling/chrome-profile.mjs profiling/results/investigation/state.json Buses 180
```

XACT's CPU, live heap, cumulative allocations and goroutines are captured at
startup and three feed phases. To inspect a capture:

```bash
/usr/local/go/bin/go tool pprof -top -cum profiling/results/xact profiling/results/investigation/startup-cpu.pprof
/usr/local/go/bin/go tool pprof -top profiling/results/xact profiling/results/investigation/feed-180-heap.pprof
```

Profiles use a separate loopback-only listener on `127.0.0.1:16060`. Normal
XACT startup has no profiler unless `XACT_PPROF_ADDR` is set. The harness uses
HTTP 18080, NATS 14222, WebSocket 19223 and public-bus HTTP 18091.
Stop the test with Ctrl+C; its child services are terminated and reaped.

The harness sets XACT's `GOMEMLIMIT=2GiB`, ends if service RSS exceeds 5,000 MiB,
and stops Chrome capture above 1,200 MiB of JS heap or 2,300 MiB aggregate RSS.
These safeguards can increase garbage-collection cost; recorded CPU is not a
production sizing estimate. Profiles are diagnostic samples, not proof of an
unbounded leak or a long-duration soak test. Test credentials, configuration,
logs, databases and profiles remain in git-ignored `profiling/results/`.

To reuse a stopped run's warm tree:

```bash
python3 profiling/run-stack.py --run warm-test --duration 300 --feed-delay 15 \
  --resume-from profiling/results/investigation
```

The harness resets acknowledgment hashes in the copied public-bus database
so static routes and stops are republished. RTDB restores configuration but
not every nonpersistent value; retaining those hashes can leave restored
coordinate arrays full of zeroes until the application's periodic refresh.
The source database is not changed. Require actual drawn routes when validating
map performance by setting `XACT_PROFILE_REQUIRE_ROUTES=1`.

For a comparison with the currently deployed UI files, add
`--static-dir "$PWD/ui/dist"`. Do not mix source maps from different builds.

To reproduce Buses and Tags Manager open in XACT's internal dashboard tabs:

```bash
XACT_PROFILE_COMBINED=1 XACT_PROFILE_LABEL=buses-and-tags \
  node profiling/chrome-profile.mjs profiling/results/warm-test/state.json 'Tags Manager' 140
```

This opens Buses, adds an internal XACT tab, opens Tags Manager, expands a
bus's metadata group, and switches between the two tabs every 20 seconds.
The tabs share one page/store; this does not represent separate Chrome tabs,
which each create their own application store. Metrics include switch counts.

For the repeated-switch retained-memory soak, force GC every four switches:

```bash
XACT_PROFILE_COMBINED=1 XACT_PROFILE_GC_EVERY_SWITCHES=4 \
  XACT_PROFILE_REQUIRE_ROUTES=1 XACT_PROFILE_LABEL=buses-and-tags-soak \
  node profiling/chrome-profile.mjs profiling/results/warm-test/state.json 'Tags Manager' 360
```

`gc-cycles.json` records comparable retained heaps at each cycle. Forced GC
alters CPU timing, so treat this as a retention test. When profiling another
UI build, point the source-map summarizer to that exact build:

```bash
XACT_PROFILE_UI_DIR="$PWD/profiling/results/ui-final" \
  node profiling/summarize-chrome.mjs profiling/results/warm-test/buses-and-tags-soak
```

For feed starvation and delayed acknowledgment investigation, see
[INPUT_RECOVERY.md](INPUT_RECOVERY.md). With the isolated stack running, check
successive bus coordinate updates using its generated test admin:

```bash
node profiling/check-bus-updates.mjs profiling/results/investigation/state.json
```

Network-enabled Chrome captures include `busCoordinateFrames` in `network.json`
to distinguish map position traffic from general metadata traffic.
