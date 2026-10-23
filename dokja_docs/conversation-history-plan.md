# Conversation history and resumable Ningo conversations

Status: planned, not implemented. Tracks step 09.5.

## Current evidence

- `AssistantConversation` keeps a transcript in React state and creates a new
  conversation ID on mount. Closing/reopening the interface or unmounting the
  component does not restore that transcript.
- `ChatDomainHandler` accepts a bounded desktop `conversation_id`, isolates reading
  discussions and passes recent messages to the generator. Its session manager is
  process-local and expires sessions after inactivity; it is not a durable archive.
- PR 93 adds explicit registered chat provider/model selection and a private host
  inference socket. Provider sessions and model context are not the conversation
  history of record.
- `dokja_store` already supplies shared SQLite persistence, but its archived chat
  provider profiles are configuration records, not a message history schema.
- Communications history for Discord/WhatsApp is a separate source-owned feature;
  it must not silently become part of the assistant's local conversation archive.

## Intended behavior and boundaries

The owner can start, list, reopen, continue, rename and explicitly delete local
Ningo conversations. Desktop is the first complete experience; CLI and TUI must
use the same orchestrator/domain API and persistence. Web can reuse it later.

Preserve `Interface -> Event/Request -> Orchestrator -> Domain -> Service`.
The interface renders and requests operations. Orchestration sequences persistence,
retrieval and inference. Domain contracts govern scope, conversation lifecycle,
turn status and eligibility for context. Infrastructure implements persistence and
model calls without owning the transcript or deciding which memories to trust.

Use the existing database if the schema review supports it; no new database,
container, chat microservice or provider-native session dependency is planned.

Keep three records distinct:

1. **Transcript:** durable messages and visible outcomes, including failed or
   interrupted sends. A displayed send is not proof that inference completed.
2. **Inference context:** a bounded, ordered selection of eligible completed turns
   and authorized source references. Reopening a long transcript must not send the
   entire archive to the model or silently restore expired context.
3. **Experience/Knowledge:** separately recorded evidence and observed outcomes.
   Archiving a conversation must not turn every reply into trusted knowledge or a
   resolved experience. Learning requires its own provenance and feedback policy.

## Incremental implementation batches

### H1: Domain contract and storage

Review schema compatibility before implementation. Define opaque conversation and
turn IDs, canonical owner/scope, timestamps, deterministic ordering, revisions,
optional reading references and send states (pending/completed/failed/interrupted).
Record provider/model provenance when reported; distinguish selected from observed
models. Do not archive credentials, raw provider envelopes or private reasoning.

Add bounded paginated list/read operations, append/update lifecycle operations and
explicit rename/delete. Define retry idempotency from a stable send identity,
concurrent append ordering and crash recovery for incomplete turns. Do not reuse a
new event ID to duplicate a previous send. Exercise schema upgrade/rollback limits
and restart persistence against disposable databases.

### H2: Orchestrator operations and compatibility

Propose additive list/read/rename/delete actions and a way to continue an existing
conversation without changing the shared event envelope. Review route, HTTP,
SQLite and interface compatibility before implementing them. Validate scope on
every operation; a supplied conversation ID alone is not authorization.

Resolve transcript access and context eligibility before inference. Persist a
failed/interrupted turn distinctly, surface storage failures, and bound messages,
response bytes and result counts. Define cancellation semantics without automatic
retry, duplicate inference or automatic resending after restart.

### H3: Desktop history

Provide a conversation list, explicit new/reopen/continue flows, rename and a
reviewed delete action. Restore transcript and selection after restart through the
shared API. Keep general conversations separate from reading discussions; preserve
source references without silently replacing an unavailable reading.

Show loading, empty, unavailable, pending and failed states. Late replies must not
appear in the wrong conversation after navigation. Retrying a failed send must not
duplicate the displayed user message or overwrite a completed result.

### H4: CLI and TUI parity

Expose the same scoped list/read/continue and lifecycle operations. Reopening in a
different interface must preserve IDs, ordering and the same visible outcomes.
Do not introduce an alternative CLI-only database or direct provider session reuse.

### H5: Context, retention and evidence validation

Separate archive retention from inactivity/context expiry. Specify whether context
is restored, reset or summarized, with an explicit notice and bounded budgets.
Treat summaries as derived, versioned artifacts with source turn references, not
observed facts. Define delete/retention behavior for transcripts and derived
artifacts; deletion does not silently retract independently recorded experiences.

Document the local identity/trust boundary and test foreign-scope reads/writes,
concurrent interfaces, restart during generation, provider changes, missing reading
sources, retention/deletion and corrupt/unavailable persistence. Evaluate memory
reuse separately with observed outcomes and counterfactual controls.

## Acceptance and verification

- A conversation survives both interface and orchestrator restart and can be
  continued with stable IDs; failed sends remain visibly failed.
- Two conversations and two scopes never leak transcripts or inferred context.
- The same send retried after disconnect does not create duplicate completed turns.
- Concurrent writers and late replies preserve ordering and conversation identity.
- Pagination and context budgets hold for large histories; no automatic full
  transcript replay or background provider inference occurs on listing/reopening.
- Desktop, CLI and TUI see the same stored history through the orchestrator.
- Explicit deletion and unavailable sources have documented, tested behavior.
- Recording a transcript neither resolves pending experiences nor makes model
  output trusted Knowledge.

Record module tests, integration fixtures and a desktop reopen/restart scenario.
Live provider inference remains a separate verification item. This plan adds no
schema, routes, environment variables, runtime behavior or production migration.
