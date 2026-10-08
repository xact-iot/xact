# Live XACT CPU investigation — 7 October 2026

The spikes come from bursts of message processing and delivery, with additional
work from full RTDB configuration checkpoints and garbage collection. XACT is
running the embedded NATS/JetStream broker as well as ingest and the RTDB; its
process CPU includes all of those services.

## Measurements

The original process was sampled once per second for 45 seconds: average CPU
64.88%, peak 363.82%, and nine intervals at or above 100%. There were nearly idle
intervals between bursts. Busy message-delivery intervals made tens of thousands
of read/write system calls per second. Another busy interval had very few I/O
calls, so message traffic alone does not explain every burst.

With the user's approval, the same executable was restarted with the existing
loopback profiler enabled at `127.0.0.1:6060`. No rebuild or application setting
changes were made. Three subsequent 30-second steady CPU captures averaged
59.57%, 54.70%, and 67.53%. Simultaneous one-second measurements during the third
capture peaked at 271.88%, with ten intervals at or above 100%.

100% here means approximately one fully occupied core. Values above 100% reflect
work on multiple cores, including concurrent broker and GC goroutines.

| Sampled CPU path | Capture 1 | Capture 2 | Capture 3 |
| --- | ---: | ---: | ---: |
| Embedded NATS server goroutine wrapper (includes client read/write loops) | 22.89% | 24.74% | 24.14% |
| RTDB ingest worker | 13.43% | 14.20% | 13.77% |
| Whole-tree configuration save | 7.55% | 6.70% | 8.74% |
| Background garbage collector | 10.52% | 6.09% | 7.60% |
| Go scheduler | 12.98% | 16.39% | 13.62% |
| Visual-script tag dispatch | 4.31% | 4.33% | 4.79% |

These are cumulative percentages of each profile's sampled CPU time, not machine
utilization. Nested paths must not be added together. The NATS wrapper is not a
complete accounting of every broker/JetStream goroutine; client-library loops
and stream processing also appear in the profiles. Dispatch cost does not by
itself demonstrate expensive user scripts.

## Why a forwarding application has this much work

1. **Each incoming snapshot becomes many individual operations.**
   [Ingest](../server/rtdb/ingest/processor.go) parses device data and updates
   each scalar tag through its pipeline. Changed values are JSON-encoded in
   [LeafNode.Publish](../server/rtdb/tree/leaf.go) and sent using synchronous
   [JetStream publication](../server/rtdb/nats/publisher.go). The broker stores
   the latest message per subject and routes messages and acknowledgements.
   This work occurs even without a browser receiving every value. Arrays also
   become individually addressable leaves; scalar arrays additionally publish
   start/completion messages. Unchanged leaf values are already suppressed.

2. **The RTDB is large.** The saved configuration immediately before restart
   contained 65,145 nodes and 721,439 scalar tags. Startup restored 65,129 nodes;
   live device creation/deletion changes these counts. A later read measured
   82,876,097 bytes (79.04 MiB) of PostgreSQL JSON text for `rtdb_tree`. A tag is a
   runtime object with metadata and pipeline state, not just a scalar value.

3. **A structural change can save the entire tree.**
   [The persistence manager](../server/rtdb/persistence/manager.go) walks all
   nodes/tags, constructs configuration objects, JSON-encodes the complete tree,
   and upserts that document. Adding/removing a bus or changing tag configuration
   can therefore checkpoint the large static tree too. Value-only updates do
   not trigger this save. The current five-second debounce coalesces changes,
   generally limits attempts to one per 30 seconds, and targets a checkpoint
   within 60 seconds of the first pending change. Processing/DB delays can make
   completion later. Every steady capture included a save; it is a confirmed
   contributor, not the sole CPU cost.

4. **Allocations create more GC and scheduling work.** The first two sampled
   heaps contained about 917 MiB and 1,102 MiB of in-use allocations. Restored-tree
   allocations accounted for about 466 MiB cumulatively in the first sample;
   JSON buffer allocations under configuration saves accounted for about
   304 MiB and 480 MiB respectively. Heap profiles can include allocations not
   yet collected; these samples do not prove a memory leak. The original
   process's resident memory approached 2.9 GiB. Numerous small acknowledged
   publications also create scheduler and network-system-call work.

PostgreSQL runs in a separate process. Its own JSONB/storage CPU is not included
in XACT's CPU percentage. XACT pays for preparing/encoding/sending snapshots.
The current publish lock is a no-op in single-instance mode, so per-tag lock
database transactions are not an explanation.

## Changes most likely to help

- Batch changed values per device/group so ingest does fewer acknowledged
  publications and creates fewer transient objects. Preserve consumers'
  snapshot/ordering guarantees and latest-value replay semantics.
- Avoid repeatedly checkpointing the entire static tree for transient bus
  lifecycle changes: consider incremental configuration persistence, separate
  static/dynamic snapshots, or reconstructing app-owned dynamic nodes. Durable
  administrative configuration and retired-session guards still need protection.
- Represent route geometry as a compact array/blob where consumers permit it,
  rather than duplicating every coordinate as a scalar RTDB leaf and message.
- Re-profile after those changes before adjusting GC or concurrency settings.
  Merely moving public-bus runtime SQL work to memory does not remove these XACT
  costs.

No CPU optimizations were applied during this investigation. The live feed
profiles identify contributors to bursts; they do not establish a function for
each individual one-second peak or isolate fleet traffic from browser traffic.

Following this investigation, tag-group batching was implemented separately.
See [TAG_BATCHING.md](TAG_BATCHING.md) for its protocol, validation and activation
notes. The measurements above describe the earlier running executable.

## Artifacts and restart follow-up

Profiles, cumulative text reports, and CPU samples are under
[`results/cpu-current/`](results/cpu-current/). The executable's build revision
was `53a0b8f19b9cf4b3e1cffe7a52132dd772cdfc0e`, built without race or coverage
instrumentation. XACT is running with the loopback profiler enabled; its process
IDs and log location are recorded in that directory. Its health endpoint
returned HTTP 200 after restart.

The restart also exposed a recovery issue: sampled public-bus route metadata
had publish-only pipelines, and the sampled route coordinate had no retained
message after restart. Public-bus's snapshot cache can consider these
non-persisted values synchronized until a full refresh. With separate user
approval, public-bus was gracefully restarted using a copy of its existing
binary and the same environment. It successfully submitted all 233 route and
8,588 stop snapshots with zero reported failures; both health endpoints returned
HTTP 200. A NATS
read confirmed that the sampled route coordinate returned (`49.131263`). This
recovery restart happened after all the CPU captures above. Both replacement
processes remain running; their private logs and process metadata are in the
artifact directory. This issue suggests adding automatic reconciliation when
the RTDB restarts, independently of CPU optimization.

Whole-tree configuration checkpoints were subsequently replaced by incremental
node rows and batched dirty-path persistence. Migration, validation and live
checkpoint timings are recorded in [NODE_CHECKPOINTS.md](NODE_CHECKPOINTS.md).
The CPU captures above predate that change.
