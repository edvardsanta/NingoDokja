# Optional PostgreSQL application journal adapter

Step `08.2`, on the [journal transaction port](knowledge-journal.md). The
[adapter](../dokja_orch/internal/infrastructure/application_journal/postgres.go)
implements that port using an explicitly supplied `database/sql` connection.
It is not registered in the running application. No operator database, existing
Memory records, environment settings or Compose stack is changed by this batch.

## Initialization and ownership

`Initialize` explicitly creates schema `dokja_application_journal`, version 1,
inside the supplied database. `Open` checks an existing version and creates or
upgrades nothing. Both reject an unsupported version. Each transaction rechecks
the version under a shared row lock; this prevents a concurrent version-marker
update from racing a transaction that examined the old version.

The caller owns connection configuration, TLS/authentication, pool lifetime and
schema privileges. Production driver/configuration selection remains unwired.
The pgx driver is pinned for the integration fixtures; the adapter itself uses
`database/sql` and has no DSN parser or implicit fallback. No new SQLite authority
or migration of existing Memory is introduced. This is a journal adapter, not
pgvector retrieval or completion of the Memory PostgreSQL adoption step.

## Storage mapping

| Table | Content and constraints |
| --- | --- |
| `schema_version` | One version marker; unsupported/missing schema is refused |
| `episodes` | Scoped current revision and unique scoped attempt ownership; optional forgotten fence |
| `revisions` | Immutable bounded snapshot bytes per scoped episode/revision; at most 64 KiB each |
| `operations` | Unique scoped operation key, request digest and original result revision; forgotten keys retain no digest/result reference |

Snapshot bytes encode the existing Go record values; this is version-1 internal
storage encoding, not a new public JSON API. Revisions use positive PostgreSQL
signed `BIGINT`; out-of-range values fail rather than truncate. No trigger
interprets observation meaning or decides episode eligibility. Those remain
Memory-domain responsibilities.

All data values are bound SQL parameters. Namespaced table names are fixed.
Scoped transaction-level advisory locks serialize operations within a scope,
including absent-row creation and deletion. Hash collisions can cause extra
serialization, never shared access: every query still filters the actual scope.
This intentionally coarse boundary is not a throughput claim. The SQL update
also compares the old revision, and uniqueness protects attempt/operation claims.

The adapter supplies detached state to the domain callback once, validates the
returned write bindings, and commits head/revision/operation changes together.
Original operation results reference immutable revisions. Reopening a connection
or restarting the database does not turn replay into a fresh mutation. A result
returned for replay remains historical; it is not relabelled as current state.

## Failure and ambiguous commit

Validation errors, callback rejection, failed statements and confirmed rollback
leave no partial mutation or reserved operation key. Cancellation before commit
rolls back. Native SQL diagnostics are not included in returned errors.

A connection can report an error after the server committed. `ErrCommitUnknown`
therefore explicitly means the outcome is unknown, not confirmed rollback.
Resolve it by submitting the original operation key and command after access and
admission are checked again. Never repeat an external action to discover whether
its journal write succeeded. The domain port comment is corrected to state this
limit; no external exactly-once guarantee is claimed.

## Forgetting and replay fences

`Forget` is a mechanical storage operation; application authorization must precede
it. It removes every content-bearing revision and clears operation digests/result
references in the same scoped transaction. Episode, attempt and operation identity
fences remain, so old commands cannot restore the removed snapshots or reuse the
forgotten attempt under another episode. Reads and replays of forgotten records
return `ErrForgotten`. Forgetting an absent episode is a no-op; it does not invent
a fence for work that never existed.

These are opaque identity tombstones, not a TTL cache. No tombstone garbage
collection or replay-expiry policy is enabled. Scope/reference generation and
retention must be reviewed with the public writer; raw private text must never
be used as an identity. Logical deletion does not erase earlier caller-held
copies, PostgreSQL WAL, old snapshots, disk blocks, database logs or backups. Backup retention
and application-level deletion propagation remain deployment work.

## Reproducible integration checks

From the repository root, with the declared Go toolchain and a container engine:

```sh
bash scripts/test_application_journal_postgres.sh
```

The script creates a disposable `postgres:17` container on a random loopback-only
port, uses synthetic records, runs integration-tagged race tests, restarts that
same server, then verifies persisted state and replay from a fresh Go process.
The container and its anonymous data volume are removed afterward. It never
reads `.env` or an operator DSN. Trust authentication and disabled TLS apply only
to this loopback fixture; production TLS is neither selected nor tested here.

`DOKJA_JOURNAL_TEST_DSN` and `DOKJA_JOURNAL_TEST_PHASE` are test-only inputs. The
fixtures refuse any database whose name is not `journal_test`. The script supplies
both inputs itself; do not point the fixtures at application data. Ordinary
`go test ./...` compiles the adapter but does not run database tests without the
`integration` build tag and explicit disposable connection.

The real-database suite covers concurrent creation and revision races across
connections, duplicate replay, original results after reopen/restart, late receipt
isolation, scope filters, parameter binding, failed-statement/cancellation rollback,
malformed write rejection, forgotten-record fences, unsupported schema and corrupt
snapshot rejection. A test-only database/sql wrapper commits successfully and then
returns a lost acknowledgment to verify unknown-commit recovery. No production
fault hooks exist.

A controlled restart is not a power-loss, failover, performance or cross-engine
evaluation. Admission facts in these tests are fakes, not live authentication or
registered receipt verification. No comparison corpus was rerun or tuned.

## Rollback and next integration

The adapter is opt-in library code with no application entry-point wiring, so
this PR needs no runtime cutover or data rollback. Retain any journal created by
an explicit caller; removing adapter registration in a later build must not drop
its data or rebuild operation identity from a cache. A newer schema is refused,
not silently downgraded. Backup/restore tooling and a production rollout/rollback
procedure are not delivered or exercised here; review them before activation.

Next: connect one reviewed opt-in application receipt/report consumer to this
store with authenticated scope, independently verified attempt/producer facts,
criterion mapping, deletion propagation and explicit connection configuration.
Only its action-owning domain can authorize external execution. Live observed
outcome capture, eligible durable reuse, concepts/relationships and `08.3`
causal/reliability evaluation remain incomplete.


The subsequent [supervision contract](knowledge-supervision.md) selects automatic
receipts with incremental user steering, starting with an opt-in native source-read
consumer. Attributed reports remain secondary. This selection adds no runtime
activation to the adapter.

The [native source-read consumer](knowledge-source-receipts.md) now exercises
this port through the existing preparation route, including a disposable
PostgreSQL integration test. It remains an explicit host binding with synthetic
authorization in tests; startup configuration and public admission are pending.

The [native source supervisor](knowledge-source-steering.md) now verifies in-flight
redirects and retained original attempts against this adapter. Its control state
and replay acknowledgments remain volatile; journal persistence does not imply
durable supervisor or authenticated-ingress recovery.
