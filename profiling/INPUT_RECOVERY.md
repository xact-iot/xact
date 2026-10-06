The `_INBOX.app_translink...` violations concern public-bus acknowledgments to
the adapter. XACT already dynamically permits one reply to each received
request, with a 30-second lifetime. The public-bus handler responds once;
processing or callback-queue delays beyond that lifetime explain the reported
bursts. Broad inbox publish permissions were not added.

Investigation also found a separate map starvation problem: full stop
synchronization submitted thousands of devices before bus positions. The
initial live validation accepted feed batches while its BUSES projection
remained empty until the static upload finished.

Changes in the adjacent fleet-tracking checkout:

- Stop boards and display HTML build from copied input state outside the shared
  application lock. Position-history rows commit in the same transaction as
  input journaling, instead of one database commit per position.
- Adapter requests use a 25-second budget and carry a deadline header.
  Public-bus caps that deadline, discards expired queued requests, bounds
  processing database operations, omits replies after timeout, and logs slow
  acknowledgments. Requests from older clients retain a bounded processing
  deadline; upgrading the adapter is required to cover time spent in its queue.
- Bus positions publish before static devices. During bulk route/stop uploads,
  the serialized publisher checks for current bus state between submissions
  with a ten-second target interval. An individual slow submission can extend
  that interval. Interleaved refresh persists lifecycle decisions before
  retiring devices and uses the normal acknowledged/delta publication path.

Regression coverage verifies atomic history rollback and idempotent retry,
concurrent input/snapshot construction under the race detector, deadline
validation, bus-first publication, latest coordinates, and retirement history.
The complete fleet-tracking test suite passes. XACT's in-process broker test
also verifies that public-bus can answer an adapter request once, while duplicate
and unsolicited inbox publishes remain denied.

Final live verification uses copied SQLite databases, real Translink data and
separate loopback ports in `results/input-recovery-final/`. The original running
services and databases were not restarted or changed. The generated binaries
are under `results/`; the existing services were launched with `go run` and
must be restarted from the updated fleet-tracking source to apply the fixes.
No XACT permission-manifest change or restart is needed for this correction.

The 30-second API sampling check observed 1,662 coordinate changes while the
bulk synchronization had uploaded only approximately 3,250 of 8,588 stops.
The next interval recorded another 1,618 changes. During its 60-second capture,
Chrome received 3,275 frames containing bus coordinate subjects, rendered 38
route overlays, and logged no browser errors. Neither final service log contains
a reply-permission violation. The isolated stack was stopped after verification,
while the deliberately forced static refresh was still in progress.
Final samples and browser network counters are retained in the result directory.
The browser capture records bus coordinate frames separately from general
realtime traffic. Earlier captures verified the direct-served UI connects and
renders route overlays without browser errors. Reproduce API sampling with:

```bash
node profiling/check-bus-updates.mjs profiling/results/RUN/state.json
```

The initial corrected runs exposed starvation and were superseded by the final
interleaving implementation. They should not be treated as successful repeated
coordinate-update tests.

Remaining offline marker investigation: the acknowledged app snapshot for
`bus_2504_8c0da908476a4044fbcc2627` continued to report `meta.online=true`
while its coordinates changed. The map could skip an icon-rule subscription
when the rule field was still unknown but its parent group was already known.
Projected hydration and new-device events can produce that ordering. Explicit
icon-rule and heading references now subscribe even before their fields arrive.
A regression creates a marker from coordinates in a known meta group, then
delivers online=true and false and verifies both icon transitions and cleanup.
All 56 map tests pass. Reload the direct-served UI after the production build
to recreate marker subscriptions.
