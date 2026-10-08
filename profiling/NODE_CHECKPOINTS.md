# Incremental node configuration checkpoints

RTDB configuration is stored in `rtdb_config_nodes`, with one row per node.
Organisation (snapshot owner), path, parent path, node type, description,
template reference, lock and array flags are standard columns. `tags` contains
only the node's immediate leaf configurations: names, types, descriptions,
units, deadbands, enums, template references and local pipeline definitions.
PostgreSQL stores this array as JSONB; SQLite uses JSON text. Descendant nodes
have independent rows. Live values retain their existing opt-in persistence
and history behavior.

## Checkpoint behavior

The startup structural callback now passes its path to the persistence manager.
Leaf edits mark their containing node; node edits mark that node. Repeated edits
coalesce. Node serialization does not recurse into descendants. Ordinary value
updates do not mark configuration dirty.

The existing debounce bounds remain: a five-second debounce, generally at least
30 seconds between save attempts, and a target attempt within 60 seconds of the
first pending change. Shutdown flushes pending changes. Database/processing
delays can extend completion beyond that target.

Each checkpoint atomically deletes affected subtrees and then upserts changed
node records. Deletion paths are compacted to their highest deleted ancestors.
Literal, case-sensitive path ranges prevent underscore/percent characters or
similarly named siblings from matching a deletion. Deletes are preserved across
recreation of a path so obsolete descendants cannot survive. Changes arriving
while a batch is being written remain pending; failures merge the batch back
for retry. Database I/O does not hold the tree lock.

PostgreSQL sends upserts in chunks of 256 records within one transaction. SQLite
reuses a prepared upsert statement within one transaction. This avoids one
database round trip per tag, while both drivers provide the same semantics.
Checkpoint logs include upsert count, delete count, replacement flag and elapsed
time, allowing subsequent CPU captures to distinguish checkpoint work.

## Migration and recovery

`rtdb_config_snapshots` records initialization and format version independently
of node count. An initialized empty tree never falls back to a stale legacy
snapshot.

On first startup, if no initialized node snapshot exists, XACT restores the
legacy `system_config/rtdb_tree` document and atomically saves the reconstructed
configuration as node rows. The legacy document is retained and is no longer
updated by ordinary checkpoints. Subsequent startups restore node rows, parents
first, and reapply locks and template links. Restore also preserves saved
metadata on mandatory device tags that constructors provision automatically.

Unsupported formats or restore/migration failures stop startup before the API
accepts traffic. The application must not replace a valid snapshot with a tree
that failed to restore. Existing database backup adapters discover tables and
therefore include both new tables.

`SerializeTree` remains available for full exports. An explicit manager
`MarkDirty()` requests a full replacement; normal callbacks use
`MarkStructureDirty`. Legacy database implementations without `TreeConfigStore`
retain the old snapshot path; both supplied database drivers implement the new
interface.

The retained legacy document is a migration-time snapshot, not an automatically
updated rollback copy. A later rollback to an older executable needs a compatible
export of current configuration or a suitable database backup to preserve edits
made after migration.

## Validation

SQLite integration tests cover legacy migration, incremental writes, unchanged
static branches, value-only updates, deletion/recreation, renaming, empty trees,
successful and failed saves with concurrent edits, shutdown flush, templates,
enums, array flags, locks and transaction rollback. PostgreSQL's production DDL
and SQL were tested in a disposable PostgreSQL 17 cluster on loopback port 15432,
including multi-chunk writes, owner isolation, literal subtree deletion, rollback
and future-format rejection. The temporary server was stopped afterward.

Persistence, both database drivers and startup race tests pass. All backend
packages pass except the existing unrelated
`TestOpenAPIDocumentSchemasForDatabaseRoutes` failure: the public-bus upload
route lacks a request-body schema. Remaining API tests pass when that test is
excluded. The server build and whitespace checks pass.

`BenchmarkCheckpointSerialization` uses 2,001 nodes with 32,000 scalar tags and
one changed node. A final 10-iteration run measured:

| Preparation path | Time | Allocated bytes | Encoded bytes | Nodes |
| --- | ---: | ---: | ---: | ---: |
| Whole tree | 99.77 ms | 37,650,576 | 3,300,939 | 2,001 |
| One dirty node | 0.072 ms | 17,232 | 1,743 | 1 |

This measures serialization and batch preparation, excluding database writes,
live ingest and total application CPU. Benefits depend on the number and size
of dirty nodes. A very large individual tag group still rewrites its own tag
configuration array; separate tag rows remain an option if that becomes costly.

Run from `server/`:

```sh
go test -race ./rtdb/persistence ./sqldb/sqlite ./sqldb/psql ./startup
go test -run '^$' -bench BenchmarkCheckpointSerialization -benchtime=10x ./rtdb/persistence
# Use only a disposable database; this test creates and drops an isolated schema.
XACT_TEST_POSTGRES_URL=... go test -run '^TestPostgresTreeNodesIntegration$' ./sqldb/psql
```

## Local activation

Activated on 2026-10-07 using the previously authorized service restarts.
XACT gracefully flushed its last legacy checkpoint before activation. The old
executable is preserved at `/tmp/xact-before-node-rows`; a private custom-format
dump of `system_config` is at
`profiling/results/cpu-current/system-config-before-node-rows.dump`.

Startup migrated 63,709 nodes. Observed normal checkpoints included two upserts
in 3 ms, a cleanup batch with one upsert and 818 subtree deletes in 427 ms,
3,012 upserts in 322 ms, and 40 upserts in 8 ms. These are individual checkpoint
timings, not a measurement of overall CPU improvement.

Public-bus was restarted afterward and submitted all 233 routes and 8,588 stops
with zero reported failures. Both health endpoints returned HTTP 200. A retained
route batch contained all 366 coordinates, starting with `49.131263`. Both
services remain running. Their current process metadata and private logs are
under `profiling/results/cpu-current/`.
