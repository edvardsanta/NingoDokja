# Conversation history and resumable Ningo conversations

Status: H1 (domain contract and storage) and H2 (orchestrator operations) implemented; H3 under
way (Talk keeps, restores, lists and reopens archived conversations; rename/delete and a
queued-waiting state remain); H4 and H5 planned. Tracks step 09.5.

Attempt-settlement correction (2026-10-29):
[identity/lifecycle compatibility](task-lifecycle-compatibility.md) records the
atomic attempt check that prevents an old completion/failure from settling a
retried turn, and binds the conversation/turn pair before retry mutation.
Existing IDs, schema and public actions are preserved; the broader
task lifecycle and history interface rollout remain separate work.

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
  The one exception is the owner's decision of 2026-10-30: the linked WhatsApp
  account's own "message yourself" chat speaks for the owner and continues the
  owner's conversation (see "Phone: the WhatsApp self-chat" below). No other
  WhatsApp chat, group or Discord channel is archived.

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

**Storage decision (owner, 2026-10-23).** The archive is its own SQLite file,
`dokja_conversations.db`, beside the shared `dokja.db` (path derived from
`DOKJA_DB_FILE`, so no new variable), opened by the orchestrator alone. The shared
database refuses a schema newer than the build, and four processes open it (the
orchestrator, the CLI, the TUI and the desktop's runtime controller); a migration
there would make every older one refuse the file. A separate file keeps them
untouched, and rollback is the previous build. CLI, TUI and desktop reach the archive
through orchestrator actions (H2), never by opening the file.

**As built.**
- `dokja_domain/dokja_conversation` holds the contract and rules:
  - one owner scope for the desktop, the CLI and the TUI, and none for any other source;
  - opaque IDs;
  - turn states, with the selected provider and model kept apart from the observed ones;
  - bounds;
  - a send ID that makes a retried request return its turn (other text under the same ID is a
    conflict);
  - an explicit retry, on the owner's request only, that reopens the same turn instead of adding
    one. Only the latest turn, and only a failed or interrupted one, may be retried; `attempts`
    counts the sends;
  - the bounded selection of completed turns for context.
- `dokja_orch/internal/infrastructure/conversations` is the SQLite store:
  - every query is scoped, and keyset pagination is used;
  - turn order is assigned in a transaction;
  - a turn that already ended keeps its outcome;
  - turns left pending by a stop become interrupted, and none is sent again;
  - an archive from a newer build is refused.

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

**As built.** All of it is additive: the envelope, every existing route and every turn without a send
id behave as before.
- **Requests.** `conversation.start` (`title?`, `reading_id?` for a reading discussion),
  `conversation.list` (`cursor?`, `limit?`), `conversation.read` (`conversation_id`, `before_seq?`,
  `limit?`), `conversation.rename` and `conversation.delete` (`conversation_id`, `revision`).
  - The workflow is `conversation-archive`, in the new `conversation` domain.
  - The scope comes from the event's source, never the payload: `desktop` and `cli`, the source the
    TUI also speaks as. Any other source is refused.
  - Errors name what to do ("not found", "changed; read it again"); storage details are not passed on.
- **Continuing a conversation.** A `message.created` from an owner's interface that carries the
  archived `conversation_id` and a `send_id` is a turn of that conversation:
  - it is recorded pending before the model is asked, and ends completed, with the provider and model
    the runtime reported, or failed, with the reason;
  - the context is the archive's completed turns, newest first within 20 turns and 24,000
    characters, so it survives restarts and the 3-minute session no longer bounds it;
  - a reading discussion is grounded by the reading its conversation was started for, whatever the
    payload says, and a general one by none (a turn's `reading_id` is dropped); a question about
    recent readings is grounded by the cached feeds, as in any other turn;
  - the transcript keeps the reply as shown, citations included, so the context Ningo sees carries
    its own citation markers (for H5 to weigh);
  - the same `send_id` again is answered from the archive, never by the model a second time: the
    stored reply, "still on its way", or "got no reply; retry it";
  - `retry_turn_id` sends the latest failed or interrupted turn again, as the same turn.
- **Unchanged.** A channel's message (Discord, WhatsApp) is never archived, even if it carries those
  fields. Without a `send_id`, the desktop's and the CLI's turns use the short-lived session as
  before.
- **Start.** The archive opens beside the shared database. Pending turns are marked interrupted. If the
  file cannot be opened, the orchestrator goes on without it and says so.

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

H3 first batch (2026-10-29): Talk is an archive client. The conversation is
started in the archive with the first message (a reading discussion is started
with its reading), so opening Talk records nothing. Each message carries its own
send id. A failed or interrupted turn is retried with `retry_turn_id`, never sent
as a new message; a send whose outcome could not be read is sent again under the
same send id, which the archive answers once; a turn still pending is only read
again. After any failure the screen reads the archive back instead of inferring
the outcome. If the archive cannot start a conversation, Talk says so and gives
the message back unsent; there is no unarchived fallback. The session-notice
("started a fresh conversation") no longer appears, because the desktop no longer
uses the process-local session path.

H3 second batch (2026-10-30): when the desktop starts, Talk continues the latest
general conversation that holds turns, read into the already mounted composer so
focus and a draft survive. A Conversations drawer lists the kept conversations a
page at a time and reopens one as its own panel; a reply on its way to another is
dropped. A reopened send still pending is read again every 3 s for up to 2 minutes
and never resent; an interrupted turn shows a retry. New conversations are titled
from their first message. Talk also reads the conversation again after each
settled send and when the window regains focus, so turns added from another
interface (the phone) appear. The archive's own failure reasons are shown in the
screen's language.

H3 third batch (2026-10-30): the drawer renames a conversation in place and
deletes one after the shell's own confirmation window, which names it by its id;
both name the revision listed, so a conversation changed elsewhere is left alone
and the list is read again. Deleting the conversation on screen opens a new one.
Talk also offers the chat model under the message (the same declared models as
System, saved at once) and a readable conversation layout. A queued-waiting state
remains.

### Phone: the WhatsApp self-chat

The owner talks to Ningo from the phone in WhatsApp's "message yourself" chat of
the linked account. Each such message is recorded in the archive as a turn of the
owner's latest general conversation that holds turns, the same rule the desktop
uses when it starts; with none, a general conversation is started and titled
from the message. So a conversation begun on the desktop continues on the phone
and back. Choosing the conversation and recording the turn happen under one
lock, so two first messages land in one conversation, and the turn waits in order
behind any earlier turn of that conversation.

- Only sending is allowed from WhatsApp (`SendScopeFor`); listing, reading,
  renaming and deleting stay with the desktop, CLI and TUI (`ScopeFor`).
- The send id is `wa-` plus a hash of the chat and WhatsApp message id, so a
  redelivered message is answered from the archive instead of being asked again.
  A redelivery whose turn is still being answered gets no second reply; one whose
  turn completed gets the stored reply again. Deduplication is per conversation:
  if the desktop starts a new conversation between a delivery and its redelivery,
  the redelivery is a new turn (the interface's own 10-minute duplicate filter
  still covers ordinary repeats).
- The self-chat manages its conversation with commands, answered in the language
  of the name used (catalogs in `dokja_orch/internal/handlers/locales/`):
  `/nova` (`/new`) starts an empty conversation the next message goes to, named by
  that message; `/conversas` (`/conversations`) lists the latest eight general
  conversations, numbered, marking the current one; `/abrir N` (`/open N`) makes
  conversation N the current one by marking it just used (`Archive.Touch`), so the
  desktop agrees; `/renomear título` (`/rename`) renames the current one; `/apagar N`
  (`/delete N`) asks first and only `/sim` (`/yes`) within 2 minutes deletes it;
  `/ajuda` (`/help`) lists them. Numbers come from the last list for 10 minutes and
  every change names the revision listed, so a conversation changed meanwhile is
  left alone. Commands are not turns, contacts and groups have none, and an
  unknown slash word is an ordinary message. The list and a pending deletion are
  kept in memory; the current conversation is kept in the archive.
- There is no retry on the phone: a failed turn is retried from the desktop, or
  sent again. The interface's payload cannot pick a conversation, send id or retry.
- A self-chat turn is bounded to 170 s, waiting included, below the interface's
  default 180 s wait, so the archive records a failure before the phone shows one.
  Setting `DOKJA_ORCHESTRATOR_TIMEOUT_MS` below about 170 s brings back the case
  where the phone shows a failure and the archive later completes the turn.
- The bridge refuses every request when `DOKJA_GATEWAY_INGRESS_TOKEN` is unset, so
  the self-chat flag only arrives from the authenticated interface.
- On a dedicated bot number, the owner's personal number is a contact: those
  messages are not archived. If the archive could not be opened, the self-chat
  answers as before from the process's own session.

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
