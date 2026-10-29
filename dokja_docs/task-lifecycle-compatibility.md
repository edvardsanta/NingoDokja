# Task identity and lifecycle compatibility baseline

Date: 2026-10-29. First implementation batch of `10.6-a`; see the
[architecture review](session-task-architecture-review.md). This establishes
existing identities and closes one stale-attempt settlement gap. It does not
implement a general task registry, input queue or durable workflow engine.

## Identity map

| Identity | Current owner and lifetime | Compatibility requirement |
| --- | --- | --- |
| Conversation ID | Conversation domain/archive; durable logical transcript | Remains stable across reads, retries and provider changes; does not grant access |
| Send ID | Caller supplies it within a scoped conversation; archive deduplicates it | Same ID/content returns the existing turn; different content conflicts; duplicate transport submission is not a new inference request |
| Turn ID | Archive's durable message/result identity | Explicit retry retains turn ID, send ID and transcript position |
| Turn `Attempts` | Archive increments it when an eligible failed/interrupted turn is explicitly retried | Completion/failure carries the original attempt snapshot; a result for another attempt conflicts without changing current state |
| Public event ID | Existing ingress envelope | Transport correlation only; must not be treated as a general deduplication key |
| Current workspace `TaskID` and `AttemptID` | `RunTask` creates both for each invocation | Both remain fresh per invocation, including repeated events; do not silently repurpose `TaskID` as a durable logical-task key |
| State revision | Existing inference/application contracts | Correlates a proposal with the state it examined; neither an event ID nor an attempt counter replaces it |
| Provider binding/native execution | Runtime adapter; provider-specific | Selected configuration, reported answering provenance and native runtime identity are distinct; none becomes the transcript identity |

Evidence: [conversation contract](../dokja_domain/dokja_conversation/conversation.go),
[archive storage](../dokja_orch/internal/infrastructure/conversations/store.go),
[task contract](../dokja_orch/internal/core/task.go),
[runner](../dokja_orch/internal/core/task_runner.go), and
[inference contract](../dokja_domain/dokja_inference/contract.go).

The future application-level task key still needs an explicit mapping to these
records and a reviewed integration consumer. It must not be manufactured by
overwriting current workspace IDs, hashing caller scope into authority, or
treating a provider thread as the record of work.

## Lifecycle and settlement

An archived send begins pending. The current attempt may finish completed or
failed. Recovery may mark a pending turn interrupted; it does not resend it.
An explicit eligible retry reopens the same turn and increments `Attempts`.

Settling a result now requires all existing scope/turn checks, pending state and
an equal stored attempt counter. The repository enforces this condition in the
same SQL update that changes the outcome. A late completion/failure cannot pass
a stale read/check followed by an unconditional write.

The domain's `Store.FinishTurn` contract states the rule. `Archive.Complete` and
`Archive.Fail` preserve the supplied attempt snapshot. Executors must not reread
the latest attempt number merely to relabel an old result as current. Conflict
does not authorize retry or replacement; it means the proposed settlement did
not apply.

The original gap was observable without new interfaces: attempt 1 could fail or
be interrupted, attempt 2 could become pending, and a late result from attempt 1
would update attempt 2 because storage checked only turn ID/pending state/scope.
The existing `attempts` column supplies the missing atomic comparison, so no
schema migration or new identifier is needed for this correction.

The chat handler already refuses a final response when archive completion
conflicts. The new application regression verifies that behavior with controlled
overlapping generations. Stream previews remain provisional; this correction
does not introduce a new preview-retraction or routing protocol.

## Compatibility and rollback

- Preserve event envelope, routes/actions, public response shapes, interface
  commands, provider contracts, configuration and Compose files.
- Preserve SQLite schema/version, database location and existing archive records.
  Use the already-persisted attempt counter; no data conversion is required.
- Preserve valid current-attempt completion/failure, same-send replay and explicit
  retry behavior. The corrected behavior is rejection of stale settlements.
- Keep the current task runner's fresh-ID behavior and volatile state semantics.
  A stronger compatibility assertion guards against changing them accidentally.
- Reverting this batch requires no migration, but restores the stale-attempt
  overwrite defect. An older concurrently running writer would still lack this
  guard; the application continues to assume orchestrator-owned archive access.

## Verification design

The new repository regressions use disposable SQLite databases and two independent
connections: one holds the old executor snapshot, the other processes retry or
recovery. They cover old completion and failure, preservation of the pending
retry, successful current completion, stale failure after completion and
same-send replay of the accepted answer/provenance.

The handler regression uses two channel-controlled generations without sleeps.
The old attempt returns while the retry remains pending; it must produce no
final response or transcript mutation. The current reply then completes and a
duplicate send returns that stored reply without a third generation.

These fixtures verify archive recovery operations and persisted counter behavior,
not a killed-process/provider integration or exactly-once external execution.
No live provider, platform message or owner database is used.

Verification results: all four repository regression cases failed on the old
update and passed after the attempt comparison. Targeted repository/handler/core
race checks passed. `go test -race ./...` and `go vet ./...` passed in both
`dokja_orch` and `dokja_domain/dokja_conversation`; formatting checks passed.

## Remaining architecture work

`10.6-a` remains open for the general logical-task mapping, state-revision and
attempt relationships, authority and aggregate-budget contract, and compatibility
at an actual workflow consumer. `10.6-b` owns input admission/ordering and explicit
capability outcomes. `10.6-c` owns broader result/report settlement and justified
durable recovery. Scope/unknown-effect checks and the existing cognitive evaluation
gate remain required; this archive fix does not satisfy them by itself.
