# Public bus map: missing layers and browser CPU

## Findings

The public data endpoint silently limited each map layer to 1,000 devices
and the entire dashboard to 5,000 tags. Buses occupied 4,000 tags, stops used
the remaining 1,000 (including incomplete latitude/longitude pairs), and no
route geometry reached the browser.

Public widgets also attempted authenticated tree hydration whenever the
browser retained an `xact_auth_user` record. An expired or absent token then
produced the reported `/nodes` HTTP 401 responses.

Every five-second public poll called both `setShared` and `setStatus` for every
tag, even when nothing had changed. Both setters notify subscribers. Reparsed
coordinate arrays also looked changed because the comparison used object
identity. For buses, each resulting marker refresh called
`applySelectedMarker`, which scanned **every marker**. This made a poll's
selection work grow approximately with the square of the fleet size.

## Changes

- Public snapshots explicitly disable protected tree/tag hydration, including
  when a saved login belongs to a different organisation.
- The endpoint now supports 20,000 devices per pattern and 100,000 tag paths
  per dashboard. Exceeding the work limits returns HTTP 413 instead of a
  silently truncated snapshot. Disabled layers and unconfigured tags remain
  excluded. Device names accompany map coordinates.
- Native route arrays are read directly from their numbered children rather
  than repeatedly walking the tree from its root for each coordinate.
- Unchanged values and metadata do not notify widgets. Equal route arrays
  retain their existing geometry. Timestamp-only changes do not redraw icons.
- Updating a marker applies selection styling only to that marker. Changing
  the selected device still updates the whole selection once.
- Full public snapshots notify the existing map subscriptions about device
  additions/removals after all coordinates are loaded. A new bus no longer
  triggers a complete layer rebuild. Missing-tag subscription placeholders
  cannot keep a departed bus on the map.
- The public poll interval remains five seconds. Dashboard layer zoom settings
  remain in effect: this dashboard shows routes from zoom 11, buses from 15,
  and stops from 16. Dense stops use the existing viewport clustering.

## Verification

Activated the server and UI changes locally. XACT shut down gracefully and
public-bus restarted to republish its non-persisted values. Both health
endpoints returned HTTP 200 after initialization and synchronization.

An actual anonymous dashboard sample at zoom 16 rendered **233 route lines**,
**1,046 buses**, and **122 individual stops plus clusters**, with all **8,588
stops** present in the clustering layer. No protected API requests or uncaught
page errors occurred, including with a saved login record.

The original live browser profile spent 7.42 seconds doing renderer main-thread
work in a 12-second sample. A subsequent live sample with the complete routes
and stops spent 0.63 seconds. These live samples have different fleet/stop
counts and animation states, so they are not a controlled percentage comparison.

A separate comparison used the **same fixed snapshot of 1,000 buses and 333
stops**, the same viewport, zoom 16, and 80 pulse animations on both versions.
The old UI spent **3.13 seconds** on renderer main-thread work per 12 seconds;
the updated UI spent **0.019 seconds**. Protected requests fell from six to
zero. This isolates unchanged-poll overhead; it does not measure all Chrome
processes or predict CPU while buses move or the user pans/zooms.

Validation: all 354 UI tests passed; targeted snapshot tests passed again after
the missing-tag removal check; TypeScript and the production UI build passed.
Public API regression tests passed under the Go race detector. The Go suite
passed with the previously identified unrelated
`TestOpenAPIDocumentSchemasForDatabaseRoutes` failure excluded.

Private/local measurements are in `profiling/results/cpu-current/`:
`public-map-before-zoom16.json`, `public-map-after-stable.json`, and
`public-map-controlled.json`. Old hashed assets remain available for tabs
already open; reload the public dashboard to load the new code.

The endpoint still sends a full allowed snapshot every five seconds (about
6 MB with complete stop/route data before the fleet finishes repopulating).
Reducing repeated static geometry transfer is a possible further improvement;
the redraw fix above does not require a new public update protocol.
