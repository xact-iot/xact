Vehicle label verification, October 7, 2026

A read-only fetch of TransLink's live `gtfsposition` protobuf feed at
11:44:18 UTC contained 105 vehicle descriptors. All 105 supplied a non-empty
`vehicle.label`; every label in this particular snapshot equalled its
`vehicle.id`. Examples included 2179, 2530, 16044, 2117 and 8134. The existing
adapter discarded the label rather than forwarding it.

The adjacent fleet-tracking source now carries `vehicle_label` separately from
`vehicle_id` in position and trip-update messages. Bus snapshots publish the
string tag `meta.vehicle_label` from the latest accepted position, with an
empty string when that observation has no label. Device names continue to use
the internal vehicle ID. Labels are preserved literally, including spaces and
Unicode characters; they are not treated as device-address tokens.

Tests exercise protobuf decoding and JSON transport with different labels and
IDs, absent labels, and literal display strings. Snapshot tests verify that
the tag changes and clears without renaming the device. The full fleet-tracking
suite and race checks for translink/publicbus/arrivals pass.

Restart translink-adapter and public-bus from the updated source. Subsequent
fresh position observations populate `PUBLIC_BUS.BUSES.<vehicle_id>.meta.vehicle_label`.
No live process or database was modified during verification.

GTFS defines the passenger-visible label separately from its internal vehicle
identifier: https://gtfs.org/documentation/realtime/reference/#message-vehicledescriptor
