# Application outcome journal: admission and transaction boundary

Step `08.2`, following the [record and eligibility contracts](knowledge-outcomes.md).
The [Memory-domain journal](../dokja_domain/dokja_memory/application/journal.go)
implements bounded record mutations through a typed transaction port. There is
no public writer, executor or registered receipt producer in this batch. A
subsequent [optional PostgreSQL adapter](knowledge-journal-postgres.md) implements
the storage port, without application activation. This is a prerequisite for that integration, not actual
durable outcome capture or completed episode reuse.

## Domain operations

| Operation | Records | Does not establish |
| --- | --- | --- |
| `Begin` | One prepared attempt, context constraints and optional prior prediction; revision 1 | Actual dispatch, success or permission to perform an external action |
| `MarkDispatched` | A possible-dispatch marker and application-controlled dispatch time | Delivery acknowledgment or the action's real-world effect |
| `MarkInterrupted` | An interrupted prepared/dispatched attempt, retaining any dispatch marker | Failure of the method, permission to replay, or an invented result |
| `Observe` | An admitted executor receipt or attributed operator report tied to one attempt/input/criterion | Semantic applicability, general correctness or automatic Knowledge promotion |

These are Go domain methods, not event names or newly accepted HTTP/CLI commands.
An observation before possible dispatch is refused. Simulations and model claims
cannot enter this recording path. Unknown observations preserve uncertainty;
non-unknown observations can settle the execution phase, while later eligibility
still derives the actual outcome from the complete journal, including conflicts.
An update resolving a previously recorded unknown observation names it explicitly
with `Supersedes`, using the same observation kind and producer. The old record
remains in history; unrelated new evidence must not silently erase it.

Each mutation uses a scoped episode reference, attempt reference, operation key
and expected snapshot revision. Attempts cannot be reassigned to a second episode
in that scope. A new real attempt has its own reference and episode; a late receipt
updates only its original one. The application reference can connect those attempts
without overwriting either record. Existing workspace IDs retain their lifetimes.

## Trusted admission and time

`Admission.Access` verifies current scoped record read/write rights before a
transaction. Attempt and observation admission are bounded pure decisions over
independently verified facts supplied by the owning domains. They cannot make
external calls under the transaction lock. Missing admission functions fail
closed. Replays recheck current access and attempt admission; receipt replays also
recheck observation admission. An old operation key is not continuing authority.

Admission remains unwired: a caller's producer string, scope, authorization
reference or timestamp is not proof. Before a public writer exists, the application
must map authenticated identity, registered capability/report provenance and
criterion bindings into this port. The orchestrator coordinates these decisions;
it must not implement domain authorization or evidence interpretation itself.

The journal clock assigns acceptance times before validating a new snapshot.
Caller-supplied acceptance times for new attempts, predictions and observations
are refused. A clock going backward relative to the current snapshot fails without
writing. A prediction is recorded at preparation and must precede dispatch and
cover it. Reported occurrence time remains separate; recording it does not certify
its clock provenance or qualify it for scoring.

## Replay and atomic storage contract

The domain hashes a versioned action plus the original typed command before
assigning server time. The same scoped operation key and command returns the
original result with `Replayed=true`, even when the current revision advanced.
A changed command under that key conflicts. Expected revision is part of command
content: a caller cannot silently update it while retaining the operation key.
Operation results are historical snapshots, not a claim about current state.

This is typed-command JSON identity, not a newly standardized wire canonicalization.
Callers must retain the original command; distinctions such as nil/empty slices
are not silently normalized. A future public request mapper must define its
normalization before exposing retries. Hashing snapshots after assigning a fresh
time would break replay and is explicitly avoided.

`JournalStore.Transact` must perform the following as one transaction:

1. Read/lock the scoped operation key, episode revision and attempt-owner claim,
   including concurrent creation when no episode row exists.
2. Supply detached state to the domain callback once. Check an existing operation
   before compare-and-swap, so a legitimate retry can return its old result.
3. Refuse a stale revision, conflicting operation payload or attempt-owner claim.
4. On a changed snapshot, write the next revision and its attempt claim together
   with the operation result. An identical receipt under a new operation key
   records replay metadata without adding evidence or advancing the revision.
5. On validation/callback error or confirmed rollback/cancellation before commit,
   preserve the prior snapshot and leave no operation key or attempt claim behind.
   A commit transport failure may have an unknown outcome; resolve it by replaying
   the original command, not by repeating an external action. Return detached results.

The domain callback never dispatches an external action. Clock assignment and
validation under this transaction do not create an external exactly-once protocol.
A timeout after possible dispatch remains uncertain. Recovery must not reset it
to preparation and resend merely because a receipt is absent.

## Restart recovery

`Journal.RecoverUnsettled` is the journal's restart rule, the same one archived chat uses.
At start, under one owner, every attempt begun before that instant and still prepared or
dispatched is marked interrupted through `MarkInterrupted`, at the revision it was read at,
with the operation key `<attempt>:interrupt`. An attempt settled meanwhile conflicts and is
left alone; one the admission does not cover is skipped. Nothing is redone: an interrupted
attempt's outcome stays unknown, and only its own late receipt can settle it.

It needs the optional store capability `UnsettledReader`, a read-only listing across scopes
that also counts records it could not read. Recovery reports what it interrupted and what was
unreadable; an unreadable record is left untouched for the operator.

The source-read path names each operation by its attempt and step (`<attempt>:begin`,
`:dispatch`, `:interrupt`, `:observe`), so asking for a step again replays it. Its writes go
through one record per read, which fixes the order and the revision each step expects.
Preparation and dispatch stay separate writes: a prediction must be recorded before dispatch,
and that ordering is what later scoring compares against.

## Proposed persistence mapping for review

No table or schema migration is created here. A backend implementation needs
these logical records and constraints:

| Record | Identity and atomic requirement |
| --- | --- |
| Current episode projection | Unique `(scope, episode_ref)`, current revision and attempt reference; unique `(scope, attempt_ref)` claim |
| Immutable episode revisions | `(scope, episode_ref, revision)` and bounded snapshot/evidence references; preserve prior observations and correction links |
| Operation results | Unique `(scope, operation_key)`, versioned request digest and original episode/result revision; commit with the projection/revision |

A backend must protect concurrent absent-row inserts, not just check an old read
before an unconditional write. Its locking/uniqueness strategy, failure handling,
reopen/restart behavior and immutable result lookup need real adapter tests.
Historical result snapshots may be normalized to revision references; a transient
cache is not the authority for operation identity or outcome history.

The project's PostgreSQL adoption direction is unchanged. This batch does not add
a new SQLite authority, choose a journal database, migrate Memory or bypass its
existing adoption/evaluation gates. The storage PR must state its ownership,
configuration, privacy/deletion and rollback policy explicitly. In particular,
forgetting or revoking access must not leave operation-result snapshots as a way
to retrieve deleted/private evidence. Replay retention and deletion require a
reviewed policy before durable operation keys are enabled.

## Validation and remaining integration

The [journal tests](../dokja_domain/dokja_memory/application/journal_test.go) use a
mutex-protected, copy-isolated transactional fake. They exercise:

- Original-time replay, conflicting payloads and detached returned values.
- Scoped operation keys and unique per-scope attempt claims.
- Commit failure/cancellation rollback without reserved operation keys.
- Competing revision writes and concurrent duplicate-operation replay.
- Observation admission, duplicate receipts and replay after admission revocation.
- Unknown results, prior predictions, late receipts, preserved conflicts and
  append-only corrections without overwriting an earlier observation.

These tests establish domain behavior against the declared port contract. They do
not prove a database transaction, disk durability, restart safety, live identity or
receipt authenticity. No observer or application workflow calls this journal in
production. No frozen comparison case was rerun, no provider was called and no
existing Memory record was converted.

Next integration: review and implement a production transaction adapter plus its
failure/restart tests, then connect one opt-in registered receipt or attributed
report consumer with independent admission facts. Keep execution disabled until
its owning domain authorizes a reviewed action path. Eligible retrieval and
prediction must still use the [purpose-specific policy](knowledge-outcomes.md#implemented-pure-policy).


## PostgreSQL adapter follow-up

The [optional PostgreSQL implementation](knowledge-journal-postgres.md) now covers
the transaction port with real-database concurrency, rollback, deletion-fence and
controlled restart/replay tests. It remains unregistered. Backup/restore tooling,
application-level admission, receipt production, deletion propagation and runtime
configuration still require the next integration review.
