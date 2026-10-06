The performance fixes now hydrate browser data on demand and release live
subscriptions when widgets stop using them. The original full organisation
snapshot is no longer part of application startup. The deployed UI and
original databases were left untouched; the rebuilt UI was tested in the
isolated profiling stack.

Changes:

- Tags Manager loads immediate children when a group opens, shares completed
  and concurrent branch loads, and watches expanded branches so they recover
  after deletion/recreation. It releases value callbacks on collapse and
  disconnect. Counts describe direct tags in each group, avoiding recursive
  whole-tree recounts. Ordinary updates in unopened branches do not redraw it.
- Search/status filtering runs on the server and returns matching branches,
  limited to 1,000 tags. The widget prompts users to refine truncated searches.
- Map layers request only their coordinate, name, icon-rule, rotation and
  template fields. Route arrays arrive as complete snapshots; numbered array
  elements remain available through normal node/tag APIs. Array-layout widgets
  and the tree picker also load their chosen branches explicitly.
  Dashboard wildcard prefixes load their immediate device list before selecting
  a fallback device, including prefixes in nested widget configurations.
- NATS value subscriptions use exact subjects or required coordinate patterns.
  Reference counts preserve shared consumers, and subscription reconciliation
  processes changed paths rather than rescanning all marker subscriptions.
  Complete route-array consumers do not subscribe to every numbered element.
- Tree metadata is parsed once. Events for unopened, unobserved branches are
  discarded before parsing. Deletion cleanup visits the removed subtree rather
  than scanning every hydrated tag in the organisation.
- Map initialization is cancelled when a tab removes or replaces its widget.
  Queued value hydration is discarded when its consumer has disconnected.
- XACT reuses its debounce timer and coalesces full configuration checkpoints.
  With the startup setting of five seconds, checkpoints have a minimum interval
  of 30 seconds and an upper dirty-delay target of 60 seconds. Explicit saves
  and clean shutdown still flush pending changes; failed saves stay dirty and
  retry. Shutdown waits for an in-progress checkpoint. Long serialization or
  database operations can extend the dirty-delay target. Unexpected termination
  can lose structural changes since the last checkpoint.

This keeps the existing full-snapshot database format. It does not migrate
configuration to an incremental persistence schema. The existing leaf publish
pipeline already suppresses unchanged values; individually addressable values
and their pipelines are preserved.

The final live capture is in `results/validated/buses-and-tags-soak/` and uses
the exact source maps in `results/ui-verified/`. It completed 359.5 seconds of
measurement and 17 internal tab switches with the live Translink adapter.
The functional check found 42 nonempty route overlays, and the final screenshot
confirms the Vancouver routes are drawn. Retained heaps with Tags Manager active
after switches 4, 8, 12 and 16 were 32.38, 32.77, 32.67 and 32.85 MiB. Final heap
with Buses active was 51.7 MiB after GC. Chrome's aggregate RSS peaked at
1,388.4 MiB, below the 2,300 MiB guard that stopped the original combined capture
after 40 seconds. Median interval CPU after the first 30 seconds was 9.47% of
one core; the peak was 174.2%, so tab-switch bursts remain. Forced GC changes
CPU timing and six minutes is not a long-duration leak guarantee.

The original combined capture retained 416.1 MiB with Tags Manager active.
That comparison supports the reduced browser mirror and callback retention,
but it is not a controlled map CPU comparison: investigation found that warm
fixtures preserved public-bus acknowledgment hashes while restored nonpersistent
route coordinates were zero. Earlier warm captures, including the first
optimized soak, therefore omitted actual route rendering. The harness now
resets acknowledgment hashes only in its copied application database and
requires drawn routes. The validated final run uses that correction.

The final capture logged five repeated fetch attempts for one restored expanded
bus that the live feed had deleted (HTTP 404). There were no uncaught map
initialization exceptions. Stale expanded selections can still produce these
harmless missing-node fetches.

XACT's final 30-second sample used 22.46 CPU seconds (74.9% of one core).
Snapshot Save accounted for 15.9% of sampled cumulative CPU, versus 24.9% in the
original combined sample. Feed volume and restored fixtures differ, so this
does not establish an overall server CPU speedup. Garbage collection, ingest,
embedded NATS and full snapshots remain significant. Incremental persistence
would be a separate schema/design change; this implementation reduces checkpoint
frequency while retaining the existing storage format.

Validation includes the production TypeScript/Vite build, the UI regression
suite, the public-bus plugin tests, and Go race tests for API/persistence.
Application, ingest, NATS and startup tests also pass. The API race run excludes
the existing `TestOpenAPIDocumentSchemasForDatabaseRoutes` failure: the dynamic
public_bus upload route lacks an OpenAPI request-body schema. That failure was
reproduced using the original node handler, independently of these changes.
The final suite passed 342 UI tests across 33 files. The production build passed;
its output is in `results/ui-release/` and includes the additional wildcard
prefix compatibility fix. That fix was tested after the live soak and does not
change the Buses/Tags Manager workloads measured by `ui-verified`. The isolated
services have been stopped and their test ports released.
