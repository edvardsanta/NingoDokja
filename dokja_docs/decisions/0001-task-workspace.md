# ADR 0001: scoped task workspace owned by orchestration

Date: 2026-10-18. Status: accepted for prototype. Implementation: pending.
Related: [decision index](./README.md), [plugin lifecycle](./0002-plugin-lifecycle.md),
[state ownership](./0003-state-ownership.md), [shadow prediction](./0004-shadow-prediction.md).

## Context

The current [context builder](../../dokja_orch/internal/core/context_builder.go)
adds routing metadata to a generic map. The
[dispatcher](../../dokja_orch/internal/core/dispatcher.go) aggregates domain
results, while the hashtag workflow and chat handler compose evidence separately.
A shared task contract should coordinate macro capabilities while keeping
business decisions in their domains.

## Decision

Introduce an internal task workspace. The orchestrator is its sole writer.
Domains receive bounded typed input and return results; services and plugins
cannot mutate workspace state or dispatch other domains. Use copies of mutable
input/output at the boundary. Keep large documents, model vectors, and media
outside the workspace and refer to them through scoped evidence identifiers.

One workspace represents one execution attempt of a workflow. Its identity is
distinct from the public `event_id`: a retry may share an event identifier but
needs a new attempt identifier. Neither identifier proves deduplication.

| Part | Required meaning |
| --- | --- |
| Identity | Internal task ID, originating event ID, workflow ID/version, attempt ID |
| Scope | Source, actor ID, channel ID, and optional existing session reference; copied from the ingress/authorization path |
| Goal | Explicit task goal and allowed workflow/domain actions |
| Evidence | Reference, producing capability/plugin version, observation time, originating scope, and selection reason |
| Proposals | Action ID, domain decision, evidence references, preconditions, and structured reason |
| Prediction | Optional frozen snapshot described by ADR 0004 |
| Execution | Step results and side-effect receipts linked to an action attempt |
| Budget | Effective deadline, step count, evidence/proposal counts, and serialized workspace size |
| Terminal result | Status, stable reason code, and bounded diagnostic summary |

The existing envelope does not contain a tenant model. Do not invent a default
tenant or treat caller-supplied context flags as authorization. Distinct tasks do
not share workspace state even when source/actor/channel match. Cross-task reuse
must go through the existing authorized Knowledge, Memory, or session boundary.
An evidence reference does not grant access to its contents.

## Lifecycle and outcomes

Task lifecycle: `created -> running -> completed | stopped | failed | cancelled`.
Budget exhaustion ends as `stopped` with a reason such as `deadline_exceeded` or
`step_limit`. A terminal workspace rejects further mutation. Cancellation is
propagated to active work; it does not undo an already completed external action.

Step results have an explicit disposition:

- `continue`: make output available and proceed according to the workflow.
- `stop`: domain or workflow precondition forbids further execution.
- `abstain`: this domain has no supported action or adequate evidence; the
  registered workflow defines whether to finish or continue with a fallback.
- `fail`: execution failed; a required step terminates the task, while a declared
  optional dependency can produce a recorded degraded result and continue.

Optionality is declared by the registered workflow. Plugins cannot mark their
own failure as optional or override a moderation block. Eligibility and content
decisions remain domain policy. The orchestrator applies the returned outcome
without implementing the policy itself. A blocked moderation result must prevent
later generation/delivery steps; it is an intentional correction to test and
document when integrating the new outcome path.

## Initial limits

The prototype uses an explicit internal options value with positive limits:
at most 16 executed steps, 32 evidence references, 8 action proposals, and 64 KiB
of serialized workspace data. Every new prototype execution requires a deadline
supplied by its registered workflow or caller. Its effective deadline is the
earlier of the workflow and caller deadlines. Missing/invalid options fail before
invocation. Subcalls inherit cancellation and may have tighter local timeouts.

These are provisional experiment bounds, not new public defaults. Step `03`
measures whether representative inputs fit and records revisions before adoption.
No new environment variables or response fields are introduced by this decision.

## Persistence, compatibility, and rollback

Workspace state and receipts are volatile during the first prototype. Release
them at task termination after returning a bounded summary to the authorized
caller/test harness. There is no global task-history cache. Process termination
loses active attempts; the experiment makes no recovery or exactly-once claim.
Durable side-effect replay is outside this prototype and belongs to step `10`.

Add typed contracts behind existing handlers. Preserve public envelope and
compact/debug response fields until an explicit compatibility review. Do not
expose the complete workspace in public debug output. Non-migrated workflows
keep their existing execution path during the experiment. Rollback selects that
existing path at composition time; stored business data requires no rollback.

## Verification required

Contract tests must cover scope isolation, copied input/output, terminal
immutability, each outcome, required/optional failures, deadline/cancellation,
size/count limits, and blocking before side effects. Existing ingress response
fixtures must continue to pass. Failure traces identify the step and reason
without containing unbounded source content or credentials. These checks are
requirements for implementation; they have not been run for this record.
