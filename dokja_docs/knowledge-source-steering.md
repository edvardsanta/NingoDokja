# Authenticated steering of source preparation

Status: implemented as an opt-in native host API for one bounded application.
`handlers.SourceSupervisor` builds on the [native receipt consumer](knowledge-source-receipts.md).
It has no startup registration, network endpoint, Discord command or Desktop UI.
Live authentication and production activation are not demonstrated by its tests.

## Supported behavior

A host creates one supervisor with `NewSourceSupervisor`, supplying an application
lifetime, Knowledge handler, scoped journal port and independent current control
and source-authorization policies. `Start` accepts the existing
`knowledge.application.prepare` event and validates all selected source pins.
The application automatically reads eligible sources through the existing
preparation workflow and Knowledge service port. No model or method executor is
added. This is not the general task registry proposed in step `10.6`.

The host API exposes `Start`, `Status` and `Control`. Every call checks current
authenticated authorization, including duplicate controls and status reads.
The policy must resolve identity independently of the candidate event: event user
IDs, display names, `is_owner` and natural-language claims grant no authority.
Only bounded event correlation fields enter the workspace; candidate context maps
and display names are discarded. The host must enforce the exact application/scope
and allowed action, respect
context and return bounded opaque actor/decision references. These callbacks are
a trusted integration boundary, not an authentication implementation themselves.

`Start` retains the authenticated actor and decision reference. Each accepted
control retains its actor, decision, reasons and resulting plan digest. Feedback
is private scoped application state, not a factual observation or receipt. Source
IDs keep their existing meaning; a journal scope does not make their data private.

| Control | Transition |
| --- | --- |
| `redirect` | Requires a reason and explicit replacement goal/constraints/pinned sources; advances control and plan revisions; cancels old planning and revalidates before automatic progression |
| `pause` | Advances control revision, blocks new reads and requests cancellation; leaves plan revision unchanged |
| `resume` | Requires paused state, no pending native work and no uncertain result; advances control revision and revalidates the same plan |
| `cancel` | Advances control revision and ends further planning; preserves actual or uncertain original results |

A redirect while paused replaces the candidate plan but remains paused. A redirect
while running progresses automatically after the previous native operation
settles and the replacement passes current authorization and Knowledge validation.
An accepted acknowledgment reports revision/state only; it does not claim the
revised preparation has finished. `Status` returns a detached snapshot.

Redirect reasons alone are insufficient to define a plan. An interface must
provide a structured candidate; Knowledge reuses its existing goal, constraint,
source identity, count, pin and size validation. There is no automatic semantic
interpretation of free-text feedback yet. These checks establish read eligibility,
not understanding, source truth or method applicability.

## Revision and operation ordering

Controls carry an explicit `OperationRef` and `ExpectedRevision`. Acceptance,
operation replay and state changes share the supervisor's application lock.
Concurrent controls for one revision cannot overwrite each other. Same-actor,
identical commands replay the original acknowledgment before the revision check;
changed commands or changed actor bindings conflict. Replays still require current
authorization, and do not charge the budget again.

The supervisor checks revisions before claiming each source step, rechecks current
source permission before the native reader proceeds, and checks the current plan
before publishing prepared context. A correction accepted before a claim blocks
the old read. If it races already claimed work, cancellation is requested; it
cannot undo an operation or assert that no read happened.

Each plan invocation has fresh actual workspace IDs. Native attempts retain the
control revision in `Attempt.StateRevision`; the pinned read input digest also
binds the plan revision. The host's typed settlement projection carries both
revisions and the original workspace/episode/attempt references. Omitting the
optional plan revision preserves the prior unbound consumer's digest format.
There is no journal record/schema or event-envelope change.

## Native settlement and uncertainty

The original native receipt channel continues independently of planning
cancellation. Confirmed old receipts remain on their original journal attempts;
old text, context and workspace results never advance the replacement plan.
No new reads start while an earlier step is pending, including its journal
settlement. Native results cannot be supplied by control payloads.

An unconfirmed source response or unconfirmed observation write marks the
application uncertain. Resume is refused and automatic progress stops. A
structured redirect does not clear this uncertainty. Cancel remains available;
no uncertain read or effect is replayed as if it proved the old outcome.
There is no automatic reconciliation or operator override for that uncertainty
in this batch. The receipt body's full validation metadata remains ephemeral,
as documented for the original consumer.

## Aggregate bounds

Default ceilings, which a host may lower but cannot raise through this API:

- 32 claimed source-read attempts, including denied or abandoned attempts.
- 32 accepted controls; the final available control ends progression if necessary.
- 512 KiB cumulative request, accepted-control and returned-source bytes.
- 60 seconds aggregate application lifetime; each underlying preparation retains
  its existing shorter workflow deadline.

Corrections, pause/resume and fresh workspace IDs do not reset these counters.
Native source bytes are charged even for stale results or failed journal
settlement. A read that crosses the byte ceiling is retained but stops subsequent
dispatch. Cancellation metadata may cross the byte ceiling so cancellation itself
is not blocked. It remains bounded by the control count and field-size ceilings.
There are no provider calls or newly granted capabilities in this workflow.

The supervisor retains one bounded volatile state and one worker. Request
cancellation after successful `Start` does not end the application; the separate
host lifetime and fixed deadline do. Trusted authentication/service callbacks
must honor their contexts. As with the existing task runner, a native operation
that ignores cancellation can return late; its pending work is not replaced by
an unbounded retry worker.

## Persistence and activation boundary

Native attempts and admitted receipts can use the existing PostgreSQL journal.
Supervisor revisions, command replay, feedback, pending-work tracking and prepared
context are in memory only. They are not reconstructed after a process restart.
A new supervisor is a new logical application, not recovery or an implicit budget
grant for a lost session. There is no durable control acknowledgment or cross-node
coordination guarantee.

Before public activation, implement authenticated ingress mapping, per-actor host
quotas, durable control/replay/checkpoint recovery and unknown-result reconciliation.
Review connection/TLS selection, coordinated deletion, retention and restart/
backup/rollout behavior. No environment, Compose, startup migration or operator
database change is introduced here. The proposed global observer remains separate;
steps `08.2` and `08.3` remain open.

## Verification

Knowledge regressions cover structured correction validation. Orchestrator race
tests cover automatic progress, denial of untrusted claims, current authorization
on replay, pre-claim corrections, in-flight redirects, original late receipts,
changed source pins, pause/resume/cancel, competing controls, unknown settlements,
source-access refusal, isolated snapshots and cumulative read/control/byte/time
bounds.

`scripts/test_application_journal_postgres.sh` additionally runs a supervised
redirect during a blocked native read against disposable PostgreSQL and reopens
both original and revised journal attempts, alongside existing native-consumer,
fault/replay and controlled restart tests. Source transport and authentication
are synthetic fixtures. This does not prove live user authentication, durable
control recovery, method execution or improved cognitive results.
