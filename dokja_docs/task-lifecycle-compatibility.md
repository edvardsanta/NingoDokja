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

### Bind retry admission to the stated conversation

A retry request names both a conversation and a turn. Those identities must
match before mutation, not after reopening a turn. Previously the handler called
the turn-only retry operation and then compared the returned conversation ID.
A mismatched request returned not found but had already reopened another
conversation's turn and increased its attempt count/revision.

`Archive.RetryInConversation` now validates the full request and requires the
additive `ConversationRetryStore` capability. The SQLite adapter includes the
conversation identity in the retry update; mismatches return not found without
changing either conversation. Earlier turns of the matching conversation and
already-pending retries retain their conflict behavior.

The original `Store` interface, `Archive.Retry` and turn-only repository method
remain available for existing callers. A store lacking the atomic paired-ID
capability refuses the conversation-bound operation explicitly; it never falls
back to a mutation followed by a post-write identity check. The production
adapter implements the new capability. No public request shape or schema changes.

### Preserved contracts

- Preserve event envelope, routes/actions, public response shapes, interface
  commands, provider contracts, configuration and Compose files.
- Preserve SQLite schema/version, database location and existing archive records.
  Use the already-persisted attempt counter; no data conversion is required.
- Preserve valid current-attempt completion/failure, same-send replay and explicit
  retry behavior. The corrected behavior is rejection of stale settlements.
- Reject mismatched conversation/turn retries before mutation or generation.
  Preserve not-found/conflict results for unknown turns, foreign scopes, earlier
  matching turns and repeated retries. Unsupported custom stores report
  unavailable instead of attempting an unsafe fallback.
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

The paired-ID retry baseline also reproduced mutation after refusal for both a
failed and an interrupted turn in a different conversation. The corrected
application tests compare complete turn snapshots and both conversation records,
verify no generation on refusal and verify a subsequent valid retry. Repository
fixtures retain existing scope/conflict behavior. Domain fixtures verify input
validation and explicit refusal without fallback on an unsupported store. Full
race suites and static checks passed again for both affected modules.

## Ordered sends within a conversation (`10.6-b`, first batch)

The owner's desktop, CLI and TUI share one conversation scope, so two sends to the
same conversation could be answered at the same time. The later one then built
its context without the earlier reply, and the replies could settle out of order.
The owner chose to queue such sends in order rather than refuse them.

The orchestrator now keeps a volatile ordered queue per conversation
(`core.OrderedQueue`). A send is recorded as a pending turn and enrolled in the
queue in one admission, so queue order is transcript order. Its context is read
and the model is asked only after every earlier turn of the same conversation has
settled; the earlier reply is therefore part of its context. Independent
conversations are answered concurrently.

| Outcome | Behavior |
| --- | --- |
| Accepted and queued | Pending turn recorded at its transcript position; the request waits |
| Dispatched | The earlier turns settled; context is read and the model is asked |
| Stopped while queued | Turn fails with `the request stopped while waiting for the earlier reply`; the model was never asked |
| Refused | Eight sends already wait or run in the conversation; nothing is recorded |
| Replayed send | Answered from the archive, even when the queue is full; it neither joins nor waits |

A full conversation records nothing new: a repeated send ID is still looked up
read-only and answered as a replay (or conflicts when its text differs), while a
new send or a retry is refused.

Waiting counts against the request's own deadline: 120 s for a desktop chat
stream and the CLI's 2-minute default. With the runtime giving a provider up to
90 s, a second send behind a slow reply can run out of time and fail as stopped
while waiting; deeper queues reach that sooner than the bound of eight. A
desktop stream writes nothing while it waits, so a client that disconnected is
noticed only at its deadline and keeps its place until then.

This is an observable behavior change for the owner's interfaces: a second send
to a conversation now returns only after the earlier reply, and there is a new
refusal message and failure reason.

A retry shares its turn's position, so it does not wait behind a stale attempt of
the same turn; the attempt fence above still rejects that attempt's result. The
queue is in memory: a restart forgets it, and startup recovery marks every
pending turn, queued or running, interrupted. Nothing is resent. A turn that
fails while a later one is queued cannot be retried, because only the latest
turn may be retried. The request and response shapes, routes, schema and
configuration are unchanged; the queued state is not yet exposed as a separate
progress observation.

Verification: queue unit tests cover position order, independent lanes, same
position retries, the bound refusing before recording, cancellation and lane
cleanup. Handler regressions use channel-controlled generations: a second send
is recorded but not asked until the first reply settles, and then sees it; other
conversations proceed together; a queued request that stops is failed without a
generation; a send past the bound is refused unrecorded, while a repeated settled
or pending send ID is still answered from the archive. Making `Wait` a no-op
fails the ordering and cancellation regressions. `go vet ./...` and
`go test -race ./...` passed in `dokja_orch`.

## Queued and dispatched observation (`10.6-b`, second batch)

The orchestrator now reports where an accepted send stands, in a vocabulary that
belongs to it and not to any client or provider (`core.SendState`): `queued` when
the send waits behind earlier turns of its conversation, then `dispatched` when it
goes on to be answered. A send with nothing ahead reports neither. Neither state
proves a reply or an external effect.

An ingress that can deliver the report asks for it (`core.WithSendProgress`). The
desktop chat stream writes `{"progress":"queued"}` and `{"progress":"dispatched"}`
lines before the previews, and the desktop only renders them. It ignores a state it
does not know, but an older desktop refuses the new line, so desktop and orchestrator
are updated together. `queued` repeats every 5 seconds: a write is the only way the
stream can notice a requester that went away, and then the send fails as stopped
while waiting. That detection is best effort, because one write can succeed after
the other side has gone. Other request paths, including WhatsApp, report nothing.

## Stopping a send (`10.6-b`, third batch)

The owner can stop a send of an archived conversation from another request:
`conversation.stop` takes `conversation_id` and `send_id`, the identities the
interface already holds, so a send can be stopped before it has a turn on screen.
The action is additive and goes to the conversation handler like the other
`conversation.*` actions; the event envelope, schema and configuration do not change.

The orchestrator keeps the running sends, queued ones included, in `core.RunningSends`,
shared by the chat and conversation handlers. A stop only cancels the send's context.
The send's own request still settles the turn, so there is one writer and one outcome,
and the attempt fence above needs no change.

| Situation | Outcome |
| --- | --- |
| Queued | Turn fails with `the owner stopped this reply`; the model was never asked |
| Being answered | Inference is cancelled, which kills the provider's process group; the turn fails with the same reason |
| Reply already arrived | The reply is recorded: a stop is a request, and the turn says how the send ended |
| Nothing running under that send, restart included | Conflict; nothing changes |
| Unknown conversation | Not found; nothing changes |
| A source that keeps no conversations | Refused |
| Same-turn steering (`conversation.steer`) | Refused as unsupported; a separate capability that nothing offers |

A stopped turn retries like any failed turn, under the existing rule that only the
latest turn can be retried, so stopping a turn while a later one is queued leaves it
unretryable once that later turn exists. Stopping an earlier turn does not stop later
ones: they are answered next.

The queue admits a send just before it can be stopped, so a stop in that moment
conflicts. A conflict says nothing about the turn: Talk says the reply could not be
stopped only while it is still on its way.
A send from the owner's phone goes through the same path, so Talk can stop it too. The
WhatsApp adapter treats the error like any failed dispatch and posts its generic failure
message to the owner's own chat; it does not say the owner stopped the reply.

Talk offers Stop, and an older orchestrator answers it as an unsupported action, so
desktop and orchestrator are updated together. CLI and TUI do not offer it yet.

Verification: unit tests cover the cause, naming, finished sends and a retry replacing
an older attempt. Handler regressions use channel-controlled generations for a running
send, a retry, a queued send, nothing running, refused requests leaving a running send
untouched, and a stop through `core.Service` while the send's request is open. Making
`Stop` a no-op fails the running, retry and queued regressions. No live provider process
or real Electron run was exercised; the process kill is the existing cancellation.

## Remaining architecture work

`10.6-a` remains open for the general logical-task mapping, state-revision and
attempt relationships, authority and aggregate-budget contract, and compatibility
at an actual workflow consumer. `10.6-b` still owns a general capability report and
ordering beyond archived chat. `10.6-c` owns broader result/report settlement and justified
durable recovery. Scope/unknown-effect checks and the existing cognitive evaluation
gate remain required; this archive fix does not satisfy them by itself.
