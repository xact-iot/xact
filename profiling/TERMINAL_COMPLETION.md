Terminal arrival investigation, October 6, 2026

The route 250 Vancouver GTFS trips end with Homer Street stop 12596
(last passenger drop-off, pickup prohibited), followed about 35 metres later
by Hamilton Street stop 10114 (pickup and drop-off prohibited). Both belong
to the terminal area. A bus can stop producing new positions at either point.
The previous completion rule required three fresh terminal observations over
90 seconds, so a frozen final timestamp left a pulsing offline marker until
the feed eventually removed its entity.

The adjacent fleet-tracking implementation now recognises terminal evidence
using prior plausible trip progress, an accuracy-aware stop geofence, and
the provider's final stop sequence when present. Without a sequence, it
requires a current geometry match into the terminal along-route window.
Contiguous trailing no-pickup stops near the endpoint form the terminal area.
Poor GPS, deviation, explicitly moving buses, early stop sequences and
terminal proximity without observed approach do not qualify. Existing
three-observation completion remains; a qualifying last terminal observation
followed by silence completes with reason `terminal_stale` when both source
freshness and terminal dwell have elapsed, before the normal stale transition.
The publisher wakes at that deadline, including when dwell exceeds freshness.

Completion is persisted before RTDB deletion through the existing lifecycle
path. Storage failures retry, completed trips remain suppressed after restart,
and the vehicle's next trip can create a new session.

Read-only replay used the actual active GTFS dataset and 219 distinct recorded
position samples for vehicles 81207, 16131 and 80702. It ran entirely in memory,
using observation timestamps as processing timestamps, without changing live
services or databases. All times below are October 6, UTC−4.

| Vehicle | Last recorded position | Completion in replay | Actual old-rule removal |
|---|---|---|---|
| 81207 | 14:45:51 | 14:47:34 | 14:56:03 |
| 16131 | 14:52:18 | 14:54:05 | 15:02:48 |
| 80702 | 14:59:14 | 15:00:45 | 15:09:18 |

Vehicle 80702's geometry progress was stuck with approximately 14.6 km
remaining despite a final-stop observation. Its provider stop sequence and
actual terminal coordinates allowed completion without fabricating a corrected
progress value or ETA. Fixing the underlying geometry matcher is separate work.
Replay completion times reflect the next available timer/observation check;
production timing also depends on the publisher's ten-second tick and work queue.

The full fleet-tracking test suite and race checks for arrivals/publicbus pass.
Regressions cover silence, terminal stop areas, lagging geometry, mid-route
freshness loss, poor GPS, moving buses, loop starts, disabled inference,
configured dwell, persistence failure/retry, restart and the next trip.
Restart public-bus from its updated source to activate this change. The live
process and database were left running throughout the investigation.
