# Native source-read receipts

Status: opt-in library integration, not enabled in application startup or any
public ingress. This is the first bounded automatic receipt consumer from the
[supervision contract](knowledge-supervision.md). Incremental pause, redirect,
resume and cancel endpoints, durable supervised-session state and the proposed
global observer remain unimplemented.

## Existing execution path

The registered `knowledge.application.prepare` route still runs a bounded typed
task. Each selected source is read through Knowledge's existing `knowledge.list`
service port. Knowledge returns a typed native observation alongside the
transient source data:

| Native result | Journal outcome | Meaning |
| --- | --- | --- |
| `validated` | `succeeded` | Identity, text/provenance bounds and requested text pin passed |
| `validation_failed` | `failed` | A native response arrived but failed a source validation check |
| `unconfirmed` | `unknown` | The native read returned an error without a confirmed source response |

The criterion is `knowledge.application.source.validated`, version `1`. It says
nothing about source truth, understanding, cross-document reasoning or successful
application of a proposed method. A failed validation is not a failed method.
Normal preparation still returns `applicability: unassessed` and does not consult
or reuse episodes.

## Trusted host binding

`handlers.WithSourceReadJournal` installs a private Go context value containing a
journal port, trusted scope/application identity, positive state revision, clock
and authorization callback. It accepts no HTTP or event-context representation.
Claimed owner flags, producer identities or outcomes in event JSON grant no
recording or execution authority.

The binding supports preparation only. Reusing it with other Knowledge actions
is refused before source I/O. Without the binding, all existing actions retain
their previous behavior, including unpinned preparation and no journal writes.

With the binding, every selected source must have a text-version pin before any
source or journal I/O. Before each read, the host must independently authorize
`execute` for the exact capability, source/pin, application revision, task,
workspace attempt and episode. Each journal operation also rechecks `record`
rights outside its transaction. The host must distinguish permission to record
an original attempt from permission to dispatch more work after a correction.
A planning revision change must not automatically erase permission to retain
an actual old receipt; an actual record-access revocation still denies writing.
No current ingress installs this policy or binding.

Each read gets server-generated episode, native attempt, intent, operation and
receipt identities. The real workspace task and attempt IDs are propagated
through internal `core.StepInput`; public event IDs keep their existing meaning.
The attempt records the source pin, native capability/criterion versions,
authorization decision reference and digest of the canonical source selection.
A native intent reference in `ProposalRef` identifies this registered read; it
is not a model-authored method proposal.

`Begin` and the possible-dispatch marker must both be confirmed before the
service read starts. Memory's existing journal owns lifecycle and record
validation. Pure admission checks compare the immutable native attempt and the
sealed observation actually produced by this read. The producer name
`knowledge.source-reader.v1` is assigned by code, never accepted from a provider.

## Cancellation and failure behavior

The native step settles its observation with a fresh two-second context that
removes planning cancellation but retains trusted context values. Record
permissions are rechecked. If the source returns after `RunTask` has cancelled,
its actual receipt remains on its original attempt. The cancelled task receives
no late output, new prepared context or receipt projection. No new unbounded
settlement worker is created; the native call is still subject to the existing
provider's cancellation behavior.

Cancellation noticed after the marker but before starting the read records an
interruption and starts no source call. A transport error records `unknown`.
A journal failure prevents a successful preparation result; unconfirmed begin or
dispatch prevents the source read. Failure or lost acknowledgment while storing
the observation leaves durable settlement uncertain and never retries the source.
There is no automatic replay/recovery worker in this integration.

Each invocation creates fresh operation identities. Repeating a public request
is not deduplicated across fresh task invocations. Public retry identity,
restart reconciliation and recovery of unfinished native receipt writes must
be defined before exposure. The journal's original-operation replay contract
continues to apply to callers that retain that original command.

## Stored content and activation limits

The producer does not copy source text, title, `source_ref`, goal, raw provider
diagnostics or credentials into the journal. It retains scoped
application/workspace identity, selected source ID/pin and native observation outcome.
Source IDs retain their existing meaning and may themselves identify a location;
receipt evidence contains an opaque reference and a digest of bounded typed
validation facts. The full validation metadata is not stored as a separate
receipt body. Its admission seal is local to the running native step; there is
no cross-process producer-verification or receipt-reconstruction API yet.
Existing journal retention/deletion fences apply to these records; coordinated
source deletion propagation remains pending.

The host can supply the existing PostgreSQL port. No DSN, environment variable,
Compose change, startup migration, operator database write or production cutover
is introduced. Live authentication/admission, explicit TLS/connection selection,
control ordering, revision-bound checkpoints, cumulative supervision limits,
restart/backup recovery and rollout/rollback still require implementation and
review before runtime activation. Step `08.2` remains in progress and `08.3`
remains pending. The global observation spine is a separate proposed extension.

## Verification

Domain and handler regressions exercise native result classification, legacy
behavior, required pins, denial before I/O, journal failures, revoked recording
rights, event-context forgery and late receipt settlement after cancellation.

`scripts/test_application_journal_postgres.sh` includes the actual registered
orchestrator preparation path with this producer and the real journal adapter.
It verifies the scoped receipt through a new PostgreSQL connection without
repeating the source action, alongside the existing journal fault/replay tests
and controlled server restart. The source service and authorization policy are
synthetic fixtures; this is not live user authentication or method execution.
