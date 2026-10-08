# Tag publication batching

XACT collects a device snapshot's processed tag values and publishes one message
per changed group when ingest finishes. There is no added one-second delay.
Device/session changes and deletions are processed immediately. Ingest only
acknowledges success after the grouped publications succeed. Whole-tree
checkpoint behavior was unchanged by batching. Subsequent incremental
checkpoint work is described in [NODE_CHECKPOINTS.md](NODE_CHECKPOINTS.md).

The retained message contains a complete latest snapshot of the group's
published values, including unchanged fields from earlier partial updates. Its
`changed` list identifies which entries live consumers should dispatch. This
preserves replay information without firing scripts and callbacks for unchanged
fields. Failed publications remain dirty for the next device synchronization,
even when the leaf pipeline suppresses unchanged input values.

The browser, mobile realtime consumer and visual-script dispatcher understand
the grouped protocol. Browser subscriptions remain limited to selected groups
or branches; individual-tag and complete-array consumers still receive their
requested values. Values retain their status, timestamps and numeric precision.
Individual scalar RTDB leaves, their pipelines and REST APIs are preserved.

## Wire format

Subjects are `xact.internal.bcast.tagbatch.<organisation>.<group path>.<partition>`.
They are retained in the existing memory-backed `tagvalue` JetStream stream,
with one latest message per subject and the existing 24-hour maximum age.
Browser authorization permits only the session's organisation and requires tag
read access.

```json
{
  "values": {
    "default.PUBLIC_BUS.BUSES.Bus1.meta.lat": {
      "type": "value",
      "value": 49.283456,
      "status": "",
      "timestamp": 1750000000123
    },
    "default.PUBLIC_BUS.BUSES.Bus1.meta.online": {
      "type": "value",
      "value": true,
      "status": "",
      "timestamp": 1750000000000
    }
  },
  "changed": ["default.PUBLIC_BUS.BUSES.Bus1.meta.lat"]
}
```

Live consumers dispatch `changed`. Replay consumers can hydrate every entry in
`values`. Existing consumers that subscribe only to individual `tagvalue`
subjects must be updated to consume batches before activating the new server.
The updated web/mobile consumers also accept the previous single-tag protocol.

Large groups split into stable hash partitions targeting 256 KiB per message.
All replacement partitions are published before an obsolete parent partition
is purged. A single large tag still has the broker's existing payload limit;
its value is not fragmented. Complete array snapshots remain authoritative,
and transient array-start markers are not retained as group values.

Deletion removes cached fields and purges or replaces the affected retained
group messages. Device deletion also releases its batch scope. Recursive tree
deletion callbacks avoid rescanning the cache for every descendant. Persistence
restore seeds known, explicitly restored published values into the group cache;
undefined leaves are not exposed merely because their configuration exists.

## Validation and activation

Backend regressions cover group publication counts, partial-update replay,
processed/scaled values, failure/retry, concurrent devices, deletion/device
reuse, partition migration and large integer values. Browser tests cover
selected-coordinate subscriptions, change-only dispatch, complete arrays and
late array metadata. Mobile tests exercise actual batched NATS frames and reject
foreign group/organisation entries.

The server and production web assets were activated on 2026-10-07 using the
previously authorized service restarts. The built binary is at
`/tmp/xact-tag-batches`, installed as `server/bin/xact`; the UI build under
`profiling/results/tag-batching-ui/` is installed in `ui/dist/`. The served web
index matches that build. Reload existing browser sessions to load the batch
consumer, and rebuild/update native mobile clients using realtime tag values.

Public-bus was restarted afterward to reconcile non-persisted values. It
submitted all 233 routes and 8,588 stops with zero reported failures. Both
service health endpoints returned HTTP 200. A read from the live retained
stream confirmed an eight-field route batch containing all 366 coordinates,
beginning with `49.131263`. Both services remain running; private logs and
process metadata are under `profiling/results/cpu-current/`. These checks
verify activation and recovery; no post-activation CPU comparison was taken.

The full Go suite currently has the previously documented unrelated failure
`TestOpenAPIDocumentSchemasForDatabaseRoutes`: the public-bus upload route lacks
an OpenAPI request-body schema. The changed backend packages and their race
checks pass. The UI suite passes with limited worker concurrency (350 tests),
and its separate Node test passes using Node's test runner. The TypeScript and
production UI builds pass; mobile analysis and all 38 mobile tests pass.

`BenchmarkDeviceTagPublications` compares 64 changed tags in four groups against
the previous per-leaf publication path, using an isolated embedded NATS broker.
It measures publication latency and allocations, not total live XACT CPU or
checkpoint work. It also reports the publication count: 64 individual messages
versus four grouped messages, a 93.75% reduction for this fixture.

With 100 snapshots per case, the final run measured:

| Publication path | Time per snapshot | Allocated bytes per snapshot | Allocations per snapshot |
| --- | ---: | ---: | ---: |
| Individual | 6.019 ms | 253,351 | 3,145 |
| Batched | 0.793 ms | 80,240 | 653 |

That is about 7.6 times faster for this publication-only fixture. Workload,
browser subscriptions and checkpoints will affect the improvement in the live
application. The benchmark can be reproduced from `server/` with
`go test -run '^$' -bench BenchmarkDeviceTagPublications -benchtime=100x ./rtdb/nats`.
