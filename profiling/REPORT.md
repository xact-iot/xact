Implementation follow-up: [changes and validation](IMPLEMENTATION.md).

Fixture correction: warm captures retained application acknowledgment hashes
after RTDB restored nonpersistent route coordinates as zeroes. They measure
mirror/subscription costs but omit actual route rendering. The final validated
implementation capture resets hashes in the copied database and verifies drawn
routes; see the follow-up for its measurements and comparison limits.

Profiling reproduced substantial XACT CPU usage and a large Chrome memory
footprint with live Translink positions, trips and alerts. The dominant browser
cost is the organisation-wide mirror tree and its hydration/subscriptions,
rather than the number of visible map elements. Whole-tree persistence and
per-tag ingestion/NATS processing are concrete XACT hotspots.

Tests used an isolated SQLite XACT database, copied bus configuration, copied
dashboard definitions, and real Translink data. The active dataset has 233
routes, 8,588 stops and 57,273 trips. The cold live test's saved tree contained
61,421 container nodes and 694,550 scalar leaves, including 97,868 scalar array
elements. Its configuration snapshot was 65.6 MiB before JSON formatting.
These are snapshot counts; bus lifecycle changes alter the live count.

The baseline, live, warm and deployed-UI comparisons ran on 6 October 2026.
Runtime versions were Go 1.26.5, Chrome 154.0.8037.92 and Node.js 22.22.2.
All isolated services and Chrome instances were stopped after capture. The
original databases and deployed UI files were not changed. Only the profiling
endpoint, harness and reports were added for this investigation.

| Browser capture | Post-GC JS heap | Peak Chrome RSS sum | Peak interval CPU |
|---|---:|---:|---:|
| Fresh UI, map opened during startup; network recording on | 268.2 MiB | 1,874.4 MiB | 268.6% |
| Fresh UI, map opened on populated tree; network recording off | 393.1 MiB | 1,787.9 MiB | 144.5% |
| Fresh UI, expanded bus group in Tags Manager | 338.4 MiB | 1,803.2 MiB | 147.4% |
| Fresh UI, public-bus configuration widget | 376.1 MiB | 1,819.2 MiB | 169.1% |
| Existing deployed UI, populated bus map | 345.6 MiB | 1,607.3 MiB | 167.5% |
| Fresh UI, Buses + Tags Manager in XACT tabs, two switches | 416.1 MiB | 2,304.1 MiB | 142.5% |

100% CPU means one core. Peak intervals include hydration and widget setup.
Chrome RSS is summed across its processes and double-counts shared pages; it
is not private memory. The populated-tree captures are separate, short tests
at different feed phases, so their numbers are not a controlled performance
ranking of the widgets or builds. Post-GC heap is retained JavaScript data,
not proof of an unbounded leak. The deployed files predate the current source;
the organisation-wide mirror cost appears in both.

The user confirmed Buses and Tags Manager were open in internal XACT tabs.
The combined-dashboard follow-up reproduced that tab arrangement. It opened
Buses and Tags Manager, expanded a bus metadata group, then switched every
20 seconds. It hit the 2,300 MiB RSS safeguard after two switches, ending
measurement at 40.1 seconds rather than the requested 140. Heap rose from
401.0 to 677.1 MiB during measurement and fell to 416.1 MiB after forced GC.
DOM nodes fell from 46,499 before GC to 14,214 after GC, indicating substantial
temporary/detached DOM allocation. This short run does not prove a sustained
leak. CPU/allocation profiles also include login and dashboard setup.

In this combined profile, `countLeaves` took 10.3% of active self-time,
node type/status lookups 20.5%, `removeNode` 9.6%, metadata hydration 7.6%,
and GC 7.3%. Internal tab activation detaches the previous dashboard and
reloads widgets; Tags Manager's reconnect triggers another full-depth
`loadTreeFromAPI('', -1)`. Its leaf-value subscription callbacks are not
unsubscribed on disconnect, so subscriber retention deserves a focused
repeat-switch/heap-retaining-path test. Separate Chrome tabs would each
create an application store, whereas these internal tabs share one store.
The concurrent 30-second XACT profile measured 57.8% of one core, with
whole-tree `persistence.Manager.Save` accounting for 24.9% cumulative sampled
CPU. Failed harness setup attempts are excluded from this comparison.

The live startup map capture observed 392,319 WebSocket frames while its DOM
stayed around 2,100 nodes. Its first counter recorded 612.9 MiB of CDP frame
payload representations, including base64 overhead for binary frames; that
is not a raw network-byte measurement. The harness now counts decoded binary
payload bytes. Recording these frames adds overhead, so the later comparisons
disable Network recording. The no-Network map capture still spent substantial
CPU in mirror hydration, NATS handling, and garbage collection.

Browser evidence and code paths:

- `ui/src/main.ts:initializeStore` fetches the entire organisation with
  `loadTreeFromAPI('', -1)` on every application startup, including when the
  visible widget is only the configuration panel.
- `ui/src/store/store.ts:processChildrenRecursive` and
  `applyTagMetadataToNode` allocate the mirror and populate the hydration-path
  set for the hundreds of thousands of tags. The populated-map allocation
  sample attributed about 95.7 MiB to recursive hydration, 85 MiB to Set adds,
  and 62.8 MiB to Maps. Sampling locations can include their callees and are
  estimates, not exact independently attributable retained-object totals.
- `setupTreeSubscription` listens to the entire tenant tree. Once any tag
  value is needed, `watchTagValuePath` subscribes to the tenant's entire
  `xact.internal.bcast.tagvalue.<tenant>.>` stream. Unneeded values are rejected
  only after receiving and parsing their NATS routing information.
- `handleTreeChange` decodes/parses metadata, then `processIncomingNats` decodes
  and parses the same bytes again. The live startup allocation sample attributed
  about 179.8 MiB of sampled live allocations to `processIncomingNats`.
- Tags Manager's initial `countLeaves` traverses the whole subtree even when
  most groups are collapsed. Its warm capture attributed about 10.1% of active
  self-time to this traversal and another 18.4% to node-type/status lookups.
- The configuration widget's own `render` accounted for about 0.2% of active
  self-time; its roughly 376 MiB retained heap comes predominantly from the
  shared store, not the small configuration table.

XACT evidence:

- Cold live process sampling peaked near 395% CPU and 2,164 MiB RSS. The
  30-second ingest/startup profile attributed about 24.7% cumulative sampled CPU
  to the ingest worker path, 20.5% to `writeTag`, and 16.9% to background GC.
  Nested cumulative percentages overlap and must not be added.
- Warm steady profiles used about 57–61% of one core on average over their
  30-second recording windows. `persistence.Manager.Save` accounted for about
  12% in one sample and 22% in another. It serializes/saves the entire tree,
  making small structural changes disproportionately expensive at this size.
- The RTDB scalar leaves, restored configuration, NATS memory stream, and
  persistence buffers dominate live heap samples. The warm sample showed
  about 380 MiB cumulatively under tree deserialization and a 192 MiB buffer
  allocation; another sample showed about 231 MiB cumulatively under NATS
  memory-message storage. Heap profiles need a retained-object/GC analysis
  before interpreting temporary buffers as a leak.
- The public-bus process peaked around 1,420 MiB RSS and one CPU core while
  loading/initially synchronizing the full GTFS dataset. The Translink adapter
  itself remained small (roughly 20–40 MiB RSS and low interval CPU). The source
  public-bus SQLite file is approximately 13 GiB; its history/retention deserves
  separate analysis, but database file size alone does not establish a CPU or
  browser-memory cause.

Recommended implementation order, based on this evidence:

1. Replace unconditional full-depth browser hydration with lazy/paged tree
   loading and targeted metadata/value snapshots for visible widgets. Avoid
   fetching the full RTDB at all for configuration-only pages. This targets the
   largest retained browser allocation across every dashboard.
2. Narrow live value subscriptions to the required tags or minimal prefixes;
   release them when widgets disconnect. Suppress unnecessary per-element
   browser updates when a complete array snapshot is sufficient. Preserve
   individually addressable arrays and tag access for consumers that need them.
3. Decode tree events once and avoid expensive full-tree recounts for ordinary
   updates. Consolidate initial hydration so Tags Manager does not independently
   fetch a second full tree while the application is already hydrating it.
4. Persist changed configuration incrementally or reduce full snapshot frequency
   after bursts, with a bounded checkpoint interval for recovery. Avoid redundant
   per-tag/NATS work, especially for static route geometry.
5. Repeat these captures after each change, then run a longer soak and repeated
   dashboard-switch test. Compare forced-GC heap across cycles and capture a
   heap snapshot for retaining paths if the baseline keeps increasing.

Limits: XACT ran with `GOMEMLIMIT=2GiB`, so GC cost can be inflated by the
safeguard. Chrome captures also had memory budgets. These were short diagnostic
runs, not production sizing measurements; no OOM termination was reproduced.
The test XACT backend was SQLite rather than the original PostgreSQL database.
Application, RTDB and NATS paths are shared, but database I/O/persistence costs
can differ. Main-thread `(program)` CPU samples are not assigned to an
application function and should not be presented as a specific JSON/parser
cost without a Chrome performance trace.

Reproduction commands and ports are in [README.md](README.md). Raw local
artifacts are git-ignored but retained for review:

- [Fresh map during live startup](results/live/buses/summary.md)
- [Fresh map on populated tree](results/live/buses-steady/summary.md)
- [Tags Manager](results/warm/tags-manager/summary.md)
- [Public-bus configuration widget](results/warm/buses-configuration/summary.md)
- [Existing deployed map UI](results/deployed/buses/summary.md)
- [Combined Buses and Tags Manager](results/combined/buses-and-tags-final/summary.md)
- [Combined-run XACT CPU functions](results/combined/server-cpu-top.txt)
- [Warm XACT CPU functions](results/warm/server-cpu-top.txt)
- [Warm XACT heap functions](results/warm/server-heap-top.txt)

Each browser capture folder contains `cpu.cpuprofile`,
`allocations.heapprofile`, `metrics.jsonl`, `after-gc.json`, `errors.json`, and a
dashboard screenshot. Stack folders contain `.pprof` files, `processes.csv`,
logs and isolated databases. Source maps and the profiled XACT binary are in
`results/ui/assets/` and `results/xact`.
