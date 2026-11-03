# Automatic receipts with incremental application supervision

Owner decision, 2026-11-03: use automatic execution receipts as the first outcome
consumer, with incremental supervision and an authenticated way to redirect the
active task. Operator reports remain an attributed secondary input; they are not
the default proof that an action succeeded.

Status: application integration contract. The subsequent
[native source-read receipt integration](knowledge-source-receipts.md) is
implemented as an opt-in host binding. A subsequent
[native source supervisor](knowledge-source-steering.md) implements revision-bound
controls for that bounded path. No supervisor, control endpoint or receipt
producer is activated in application startup. It builds on
[domain outcome policy](knowledge-outcomes.md), the [journal](knowledge-journal.md)
and its [optional PostgreSQL adapter](knowledge-journal-postgres.md).

## What the user experiences

Dokja works in small registered operations within an authorized capability set.
After an operation, the application records the actual receipt and checks the
result against the goal, evidence, constraints and remaining budget. A checkpoint
is an opportunity to inspect or change direction, not a mandatory approval dialog
for every small read. Standing authorization does not extend to new capabilities,
private scopes or external effects.

For example, a task combines an API contract and a retry guide. Dokja starts to
use a rule for the wrong API version. The user can say:

> You are taking the wrong path: this is version 1, the previous result is unknown,
> and the version-2 deduplication guarantee does not apply.

The application accepts the authenticated correction, invalidates the old plan
revision and stops dispatching its next operation. Knowledge reassesses the
method against the corrected constraints. Work resumes from a fresh reviewed
plan, if eligible. Historical observations remain what they were. A correction
changes the task's direction; it is not an execution receipt or a retroactive
fact about whether a previous action succeeded.

The API example explains the control model. It does not register an arbitrary
HTTP tester, allow generated shell commands or authorize an external retry.

## Responsibilities

| Concern | Owner |
| --- | --- |
| Feedback/control submission and progress display | Interface, through an event/request |
| Authenticated actor, permitted task scope and control authority | Application admission using trusted identity and domain policy |
| Revision ordering, checkpoint scheduling, cancellation and aggregate bounds | Orchestrator's bounded application coordinator |
| Interpretation of a correction, method/source applicability and whether the next action is eligible | Relevant domains |
| Native execution and actual observable acknowledgment | Registered service/capability integration |
| Observation interpretation, provenance and episode eligibility | Memory domain policy |
| Atomic record changes and original-command replay | Journal port and storage adapter |

Natural language can propose a goal or constraint revision. It cannot manufacture
capability registration, authority, a verified producer or a successful observation.
Automatic semantic review is also a proposal, subject to domain checks. The current
reviewer's rejection/acceptance behavior is not established as reliable by the
[frozen comparison](knowledge-comparison-results.md).

## Proposed global observation spine

The owner asked whether one observer could cover Ningo across domains because
application work converges on orchestration. The proposed extension is a common,
passive operational observation stream, correlated with the supervised application.
It is not an already installed global collector or permission to record every
message body. Collection, scope, retention and deployment still require review.

The main orchestrator provides the spine at request admission, routing, workflow
planning, domain dispatch and terminal transitions. Typed task execution adds
per-operation progress. Runtime/provider adapters and interface-native executors
must supply correlated native progress/receipts through existing orchestration
ports or events; wrapping `ProcessWithResult` alone cannot see every internal
operation or a late result discarded by the task runner.

Keep three projections distinct:

| Projection | What it may establish |
| --- | --- |
| Operational observations | Request/operation identity, actual observed stage, producer, revision, elapsed time, cancellation/error/unknown state and bounded references |
| Domain-admitted outcomes and episodes | The named criterion's supported outcome after receipt/provenance checks; purpose-specific eligibility remains domain policy |
| Supervision state | Current goal/plan revision, accepted steering, next permitted operation and pending clarification/reconciliation |

For example, a music workflow could show command acceptance, queue mutation,
voice-connection failure and absence of confirmed playback. A queue result alone
would not become a receipt that audio played. A user correction adds a control
observation and a plan transition; it does not rewrite that execution history.

The observer preserves correlation and chronology, without interpreting success
for every domain. A common request-completed marker is not universally a successful
book extraction, delivered message or applied method. Domains supply the relevant
criterion and admissible producer. All service/interface observations still enter
through the orchestrator; no domain-to-domain or interface-to-service shortcut is
introduced by this observer.

Use a separate operational observation contract/projection rather than coercing
every trace item into the existing application episode tables. The PostgreSQL
adapter currently stores bounded application snapshots and replay metadata, not
a general trace-event log. Review a versioned observation contract and persistence
mapping before collecting this broader stream. Operational errors can aid diagnosis
without automatically becoming learning examples or binary prediction labels.

Emit metadata/references from trusted instrumentation, not arbitrary caller maps.
Authentication denial and other early exits need redacted observations too:
credentials must be stripped before observation payload construction. Private
source/chat contents require their existing access rules. Never mix private task
bodies into an unrestricted global dashboard. Observed participant text is not
verified identity, and a broad timeline grants no new action authority.

Bound event size/rate, retained history and per-task projections; version their
redaction policy. Observability loss must be visible as a coverage gap. For a
required action journal, failed/unknown recording must block false completion;
optional telemetry loss must not invent a failed real-world effect or silently
claim a complete audit. Neither observer nor supervisor may recursively journal
its own ingestion forever. A mandatory write-ahead/receipt policy needs explicit
failure semantics before an effectful consumer is activated.

Start with the selected supervised source-read consumer and a small event taxonomy.
Extend capture to other domains after checking their native receipt semantics and
coverage. Global instrumentation must not become a new centralized domain for all
business rules or a claim that every subsystem is already observed.

## Control and revision boundary

Keep a logical application reference and a revision for accepted control changes.
Keep plan revision separately, so changing pause state does not pretend the method
changed. Bind each dispatched operation to its application revision, plan revision,
workspace mapping, attempt, capability, input digest and criterion. Do not replace
existing per-invocation workspace IDs or equate public `event_id` with an operation
key; see the [identity baseline](task-lifecycle-compatibility.md).

| Control | Required transition |
| --- | --- |
| Redirect with reasons/constraints | Compare expected application revision; accept once; advance control and plan revisions; invalidate the old plan and block its next dispatch pending reassessment |
| Pause | Advance control revision and block new dispatch; request safe cancellation of in-flight work |
| Resume | Advance control revision only after current evidence, authorization and uncertain prior effects are reconciled; reassess before the next dispatch |
| Cancel | End further planning/dispatch; keep original-attempt observations and any unknown external outcome |

The feedback itself retains its authenticated attribution and reason text. The
accepted-feedback acknowledgment identifies the resulting revision and current
state. It does not claim that replanning or execution has completed. Duplicate
control commands use explicit scoped operation identity; changed payloads conflict.
Concurrent corrections to one expected revision cannot silently overwrite one
another. Bound correction count/text and include them in total task-state limits.

The goal/constraints a model suggests from informal feedback are candidates.
Domains must check their interpretation before the coordinator commits a new plan.
The supervisor should ask a focused clarification when the intended change is
materially ambiguous. It must not repeatedly interrogate an already clear correction.

Control authority belongs to the currently authenticated, authorized task
supervisor. A message mentioning the owner, a display name, quoted text, an
`is_owner` value supplied over an untrusted request path, or historical feedback
is not authentication. Configured-owner conversational treatment does not grant
new data access or make the owner's claims factual observations.

## Checkpoints and actual receipts

Check the current revision and domain eligibility before first dispatch, after
each admitted result, before changing method/capability/scope, before retrying,
and whenever evidence, authorization or outcome becomes uncertain. The task may
continue automatically through eligible registered operations. It stops for a
needed user decision, unresolved contradiction, exhausted budget or absent authority.

At dispatch, freeze the operation's binding and possible-dispatch marker. A user
correction accepted before that intent is claimed prevents it. A correction
racing an already claimed/dispatched action requests cancellation; it cannot
promise to undo an external effect or prove that the action did not occur.

A late native receipt still belongs to its original attempt and must be retained.
It cannot advance the new plan, settle another attempt or be relabelled with the
latest revision. This is why "discard stale results" means refusing their use in
current-plan progression, not deleting evidence of an action that happened.

Receipt settlement needs its own bounded path when planning is cancelled. The
existing task runner returns promptly on cancellation and can discard a late
step result; that result alone cannot be the journal's only observation channel.
The native producer/consumer integration must settle or reconcile the original
receipt independently, with appropriate scope and admission checks. A crash or
unrecoverable acknowledgment leaves the outcome unknown. Do not rerun an effect
or repeat a changed source read and call that proof of the old attempt's result.

A provider's "done", a completed model call, a supervisor verdict and a user
agreeing with the answer do not qualify as native execution receipts. Receipt
criteria describe only what the integration actually observed. Replanning retains
original aggregate time/call/evidence/step budgets; feedback is not a reset button
for unlimited execution. A new budget grant requires a separate explicit decision.

## Proposed first bounded application slice

Start within the selected cross-document use case, using the existing
`knowledge.application.prepare` source-read path. It already calls Knowledge
`LoadApplicationSource`, which reads `knowledge.list` and validates actual source
identity, text version and bounds. Use that registered, read-only integration for
the first automatic producer rather than introducing arbitrary code execution.

The receipt criterion is narrowly "the selected source was returned and validated
against its identity/pin", or an observed read/validation failure. Record native
source hash, bounded references and attempt correlation. This does not establish
that the documents were understood, that the proposed method was applied, or that
an eventual real-world task succeeded. Knowledge sources retain their existing
scope model; a scoped journal entry does not relabel them as private.

A supervised wrapper should checkpoint between source reads and before the next
application operation. Redirecting source/version selection invalidates the old
plan. Only a native read result can produce its receipt. The wrapper remains
opt-in and preserves the existing preparation route's behavior. Before exposure,
review its control payloads, authenticated scope, producer admission, journal
configuration/TLS, deletion propagation and rollout/rollback together.

A later method executor needs a separately registered capability and a concrete
observable success criterion. The API retry example must not become an implicit
permission to contact arbitrary endpoints. Live outcome capture and eligible
reuse remain incomplete until those actual consumers are integrated and evaluated.
This slice does not bring forward the general task registry in `10.6` or remove
`08.3` causal/reliability gates.

## Required implementation regressions

- Eligible read operations progress automatically without per-read owner approval.
- Unauthenticated, foreign-scope or stale-revision controls cannot change a task.
- A redirect accepted before dispatch prevents the next old-plan read.
- A redirect during a read cancels planning safely; a late receipt is recorded for
  the old attempt without advancing the revised plan.
- Pause/resume/cancel preserve unknown outcomes and never replay an uncertain effect.
- Duplicate feedback replays once; conflicting concurrent changes are explicit.
- Corrections cannot fabricate successful receipts, broaden authority or reset budgets.
- Changed source pins stop obsolete evidence from entering the new context.
- Journal failure or unknown commit cannot produce a false completion acknowledgment.

The [native source supervisor](knowledge-source-steering.md) now exercises these
ordering, admission and settlement behaviors through an opt-in host API and
synthetic authentication policy. Public authenticated controls, durable control
recovery and live runtime activation remain pending; these regressions do not
establish those capabilities or semantic understanding.
