# Knowledge application: attempts, outcomes and episode eligibility

Status: first pure-domain validation/eligibility batch implemented for step
`08.2`; receipt integration and persistence remain pending. The record semantics
below guide that integration. There is no new live action, event, store, model
call or migration. It follows the
[frozen comparison results](knowledge-comparison-results.md), not a successful
acceptance of either strategy. `08.3` remains a required gate.

## The distinction the application must preserve

A document or a Feed item supplies evidence. A proposal describes a possible
method. An attempt records what the application actually tried. An observation
states what was seen afterward, with an identifiable observer and evidence.
An episode links these records for possible later use. None substitutes for the
others; a persuasive answer, model review or predicted result is not an observed
outcome. Feeds remains the external intake wing of Knowledge.

The first consumer is the existing opt-in cross-document application path.
Before enabling an executor, it can only prepare proposals and assessments with
`can_execute=false`. These contracts do not turn that flag on. The first recording
implementation must consume an existing registered capability's typed receipt,
or explicitly attributed operator feedback about an actual attempt. It must not
invent an executor or perform an external action merely to obtain an outcome.

## Ownership and compatibility

| Responsibility | Owner |
| --- | --- |
| Source versions, method references and applicability | Knowledge domain |
| Authorization and whether the proposed action may run | Existing action-owning domain, using trusted request identity |
| Logical application identity, dispatch correlation, cancellation and bounded transitions | Application orchestrator |
| Outcome interpretation, prediction target and later episode eligibility | Memory domain policy, through orchestrator workflows |
| Observable external result | Registered integration/service or an explicitly attributed reporting person |
| Mechanical storage and atomic version checks | Persistence adapter behind the owning domain's port |

Preserve `Interface -> Event -> Orchestrator -> Domain -> Service`. Domains do
not call one another. Services return observations; they do not decide that an
episode is reusable or that an action was authorized. Model/provider output is
an untrusted proposal, including a provider's claim that something succeeded.

The following `*.v1` names identify proposed record contracts, not registered
routes or new public envelope fields. Implementation must review its additive
API and persistence mapping before exposing any writer. Existing chat checkpoints,
conversation archives, Memory records and Knowledge schemas stay unchanged.

## Record contracts

All records carry `contract`, an application-generated record reference, a trusted
scope reference, and server-assigned acceptance time. References are opaque;
they do not encode private text, URLs, credentials or authority. Scope comes from
application authorization, never from the model or a caller-supplied record field.

| Contract | Required immutable content |
| --- | --- |
| `knowledge.application.attempt.v1` | `application_ref`, `attempt_ref`, `workspace_task_ref`, `workspace_attempt_ref`, `state_revision`, `proposal_ref`, `method_ref` and version, selected source ID/hash pairs, normalized action/input digest, prerequisite evidence references, success-criterion reference/version, dispatch mode, and authorization-decision reference |
| `knowledge.application.observation.v1` | `observation_ref`, `application_ref`, `attempt_ref`, observer kind and trusted identity, criterion reference/version, reported occurrence time when available, receipt/evidence references and hashes, observation kind, structured result, and optional superseded-observation reference |
| `knowledge.application.prediction.v1` | `prediction_ref`, `application_ref`, `attempt_ref`, state revision, criterion reference/version, target and horizon, evidence/episode references, predictor identity/version, forecast status and probability when supported, optional matching baseline, and application acceptance time before dispatch |
| `knowledge.application.episode.v1` | `episode_ref`, `application_ref`, `attempt_ref`, method/input/source versions, observation references, derived outcome, applicable context constraints, optional prior prediction reference, eligibility status/reasons, and record revision |

A receipt may be stored as a bounded reference and digest; these contracts do not
require copying raw documents, provider reasoning, private conversation text or
credentials into Memory. A hash proves identity of bytes, not semantic truth or
read permission. Existing Knowledge is not scope-aware: an episode's private
scope must never relabel a globally stored source as private.

### Identity and duplicate delivery

`application_ref` is a new logical application identity when an integration is
implemented. Do not reuse the current workspace `TaskID`: `RunTask` creates a
fresh task and attempt for every invocation. Store the mapping explicitly, as
required by the [task identity baseline](task-lifecycle-compatibility.md).

A real retry receives a new `attempt_ref`; duplicate transport delivery does not.
Use an explicit scoped operation key and content digest at the future write port.
The same key and content is idempotent; the same key with different content is a
conflict. `event_id`, a provider thread, or a conversation ID is not that key.
An immutable old-attempt receipt must never be relabelled as a newer attempt.

An attempt binds one method, input, criterion and examined state revision. A
material change requires a new attempt and a fresh authorization decision.
Preparation and a journal entry alone never prove that dispatch happened.
A future executor needs a recoverable dispatch/receipt protocol: writing a
`dispatched` marker before sending and receiving an acknowledgment after sending
both leave crash windows. Neither permits an automatic duplicate external action.

## Lifecycle and what counts as success

Keep execution phase and observed outcome separate:

| Dimension | Values and meaning |
| --- | --- |
| Phase | `prepared`, `dispatched`, `settled`, `interrupted`; settlement requires an admitted observation, not just a finished model call |
| Observation kind | `executor_receipt`, `operator_report`, or `simulation`; report attribution and simulation status are immutable |
| Observed outcome | `unknown`, `succeeded`, `failed`, `partial`, `conflicted`; success/failure applies only to the named criterion and observed attempt |

Preparation can end in clarification, abstention, denial or cancellation without
an attempted procedure. Do not manufacture a failed or successful episode from
those decisions. Provider/transport failure while preparing a proposal is a
proposal failure, not evidence that the proposed method would fail.

A receipt saying "request accepted" can establish dispatch acceptance, not the
requested real-world effect. A timeout, cancellation or crash after possible
dispatch leaves the outcome `unknown` until an admissible receipt or observation
resolves it. Missing feedback and an elapsed observation horizon do not mean
failure. Preserve the explicit uncertainty even when the task UI is terminal.

An executor receipt is admissible only after correlating its authenticated
producer, attempt, action/input digest and criterion. Operator feedback is an
attributed report with the reporter's permitted scope; it is not silently
upgraded to an independently measured result. A provider's assessment or human
agreement with a recommendation is neither kind of execution receipt.

Late observations attach to their original attempt. They may resolve that
attempt's uncertainty, but cannot settle a different current attempt. Duplicates
are idempotent; conflicting evidence sets `conflicted` and keeps the competing
references. Corrections append a superseding observation with provenance;
original observations are not overwritten. A future store must atomically check
scope, attempt and expected record revision when updating the derived state.

## Prediction is a prior estimate

Define a criterion before dispatch, such as whether an observed result satisfies
a named check within its declared window. A forecast refers to that exact target,
method, input, state revision and attempt. Record it before dispatch using
application-controlled acceptance order, not a provider-supplied timestamp.

Use `estimated` with a finite probability in `[0,1]`, or
`insufficient_evidence` with no probability. Missing usage, evidence or probability
is not zero. Retrieval similarity, proposal confidence and action acceptance are
different quantities. A baseline is optional and only comparable when it names
the same target and horizon.

Only eligible observations that resolve that target may score its prior forecast.
Retain unresolved counts and feedback coverage. Partial, conflicting, simulated
or operator-reported results need an explicit target/scoring policy; they must
not be collapsed to binary labels merely to compute a metric. No fabricated
baseline, retrospective forecast, automatic calibration or learning claim.

## Eligibility for later use

Store eligibility separately from outcome: `candidate`, `eligible`, or
`ineligible`, with stable reasons and a revision. Admission is a domain decision,
not the fact that a row exists or has a nearby embedding.

Before an episode informs a later method choice, Memory policy must check:

1. The current caller can read its original scope and evidence; owner-only
   conversational treatment never changes these checks or factual labels.
2. Its attempt and observations have valid bindings and permitted provenance.
3. The proposed use is compatible with its observation kind. Simulations are
   excluded from observed-outcome prediction and execution evidence. An operator
   report stays visibly attributed and is not interchangeable with measurement.
4. Method version, source scope/effective dates, material context and unresolved
   preconditions are applicable now. Similar wording cannot override a mismatch.
5. The requested use matches the outcome. Success can support only its recorded
   criterion; failures and partial results can inform cautions, never masquerade
   as demonstrated success. Unknown/conflicting outcomes cannot train a resolved
   success target, though a separately labeled diagnostic view may expose them.
6. Deleted, inaccessible, superseded or invalid evidence withdraws eligibility
   for that use. Prior decisions retain their original evidence references for
   audit, subject to deletion policy; no automatic promotion into Knowledge.

Selection returns explicit exclusions and `insufficient_evidence` when nothing
qualifies. A source update or outcome correction can change future eligibility;
it must not retroactively rewrite what an earlier decision knew. Generalizing
an episode into a reusable claim requires a separate reviewed Knowledge step.

## Bounded first implementation and test gate

Start with pure domain validation/eligibility and fake-port application tests,
then review the persistence and registered receipt integration as separate work.
Initial record limits: 64 KiB serialized, 8 source references (matching current
application preparation), 16 prerequisite/criterion checks, 16 evidence references,
and one forecast/criterion per attempt. References are at most 256 bytes; free-text
annotations at most 2000 bytes. Reject excess content explicitly, without silent
truncation or another provider call. An application-run budget bounds the number
of attempts/transitions; it must not rely on a per-record size limit alone.

The first regression matrix must prove these observable results:

| Input or event | Required result |
| --- | --- |
| Accepted proposal, no action dispatched | No observed-success episode |
| Simulated success or provider saying "done" | Cannot settle an actual attempt or score its forecast |
| Late receipt for attempt 1 after attempt 2 starts | Only attempt 1 can change; no relabelling |
| Duplicate observation / conflicting operation key | Idempotent result / explicit conflict |
| Timeout or interrupted dispatch | Unknown outcome; restart does not replay the action |
| Conflicting or superseded observations | Preserved evidence and recomputed eligibility |
| Cross-scope, stale-method or invalidated source | Excluded from later reuse |
| Valid failure episode | Available only for a compatible caution/failure use |
| Forecast after dispatch or different criterion | Refused for prior-prediction scoring |
| Missing feedback or unsupported forecast | Unknown/unscored, never invented zero/success |
| No eligible episodes | Explicit cold start; source-based reasoning remains possible |

No step is complete merely because these contracts are written. `08.2` still
needs the bounded application integration and actual observed procedure/outcome
recording. `08.3` must evaluate removal of experience, stale/inapplicable episodes,
restart/reuse behavior, source-scope comparability, and semantic-review reliability
before broader adoption. The evaluated corpus cannot become unseen again; new
held-out cases must be frozen before measuring any tuned method.


## Implemented pure policy

The [Memory application package](../dokja_domain/dokja_memory/application/records.go)
adds bounded Go record values and structural validators for attempts, observations,
prior predictions and episode snapshots. These are domain values, not new public
JSON payloads or registered actions. The generic stamp reference identifies each
record; nested attempt/observation fields retain their own bindings.

[Eligibility policy](../dokja_domain/dokja_memory/application/eligibility.go) derives
an observed outcome from the active, attributed observation journal. Repeated
identical observation references are idempotent; different content under one
reference conflicts. A correction must name an earlier observation of the same
kind and observer. Competing current outcomes remain conflicted. Input records
are not changed. Snapshot revision/acceptance time describes the supplied
projection; no append-only store or atomic compare-and-swap is implemented here.

The trusted query supplies current access/evidence versions, an independently
verified attempt, observation provenance, applicability and context facts. The
application must obtain these facts through the relevant domain policies; the
orchestrator must not implement their business decisions itself. A supplied
`MethodApplicable` flag is not this package performing semantic applicability
review. Missing verification callbacks or evidence versions fail closed. No
adapter supplying real identity, receipts, effective-date interpretation or
state revisions is wired by this batch. Server-controlled ordering and hashes
must be established by that future integration; typed fields alone do not
prove authenticity, dispatch or clock provenance.

Use-specific decisions distinguish `success_support`, `caution`, `prediction`
and `diagnostic`. Simulations can appear only in diagnostics and never yield an
observed success. Attributed operator reports require explicit policy permission;
this first implementation excludes them from prediction scoring. Scoring covers
only a prior `criterion_satisfied` estimate and a verified executor observation
of that terminal criterion within its horizon. An absent score/baseline remains
absent, not zero. Other targets and report-derived scoring remain unimplemented.

Selection evaluates at most 32 candidate snapshots, deduplicates identical
scope/reference identities and refuses mixed snapshots of one identity. With no
eligible candidate it returns `insufficient_evidence`, including exclusion reasons
when candidates exist. It never fabricates a remembered successful method.

Regression tests exercise the pure decision boundary with fake verification facts:
prepared proposals, simulations, interrupted/late attempts, receipt/input/criterion
mismatches, duplicates/conflicts/corrections, scope and evidence invalidation,
method/context mismatches, attributed reports, missing/retrospective estimates,
unknown outcomes, target horizons, record/candidate bounds and input immutability.
They do not exercise a durable journal, startup recovery, actual external effects,
live authorization, real retrieval, or cross-document learning. In particular,
these tests do not complete the application-level restart/settlement controls in
`08.3` or the general lifecycle work in `10.6`.

Next: connect these policies through a reviewed opt-in application path using a
registered capability receipt or attributed operator report, and explicitly review
its storage mapping and atomic settlement before persisting or reusing episodes.
Existing Memory acceptance labels and prediction targets are unchanged; there is
no automatic conversion from `accepted`/`replaced` into method success/failure.
