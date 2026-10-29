# Realtime Conversation in Dokja

Realtime Conversation is a Dokja capability shared by Discord and Dokja Desktop.
It follows the existing Go orchestrator/domain architecture; it is not a second
bot, a new web application or a replacement for the local owner's chat archive.
The root `AGENTS.md`, README, environment examples and architecture conventions
continue to govern this implementation.

## First vertical flow

Implemented in the observer worktree:

1. `/conversation_start` joins the command author's current guild voice channel.
   The public command response announces temporary transcription. Starting grants
   consent only for that author, not everybody in the room.
2. Other speakers use `/conversation_join` from the original text channel while
   connected to the same voice channel. Unconsenting streams are not subscribed.
3. Discord's individual participant streams pass through the existing bounded
   capture/VAD/ASR path. Only final recognized segments reach `realtime.observe`.
   Speaker identity comes from the authorized interface event, not transcript text.
4. An independently routed Conversation-domain observer journal accepts bounded,
   revisioned evidence. This route never calls Chat generation, TTS or an LLM.
5. Dokja Desktop's Realtime workspace polls a bounded snapshot through the existing
   shell allowlist and orchestrator transport. It shows session selection,
   participant attribution, capture timestamps and chronological final transcripts.
6. `/conversation_leave` withdraws that speaker's consent and deletes their
   ephemeral evidence. Consent cannot be regranted within the same session; start
   a fresh session to avoid admitting audio captured before withdrawal.
7. `/conversation_stop` closes capture and deletes the journal. Any consenting
   participant may stop. Replacing/disconnecting the listener requests deletion
   and invalidates the local consent set. If that request cannot reach the
   orchestrator, the journal expires after 15 minutes of inactivity.

No raw audio is written by the observer. Audio exists transiently in bounded
capture/recognition buffers. Journals are process-local, capped at 32 sessions,
16 consenting participants, 512 messages and 256,000 transcript bytes per session.
Snapshots share a 256,000-byte transcript and 512-message budget across sessions; metadata adds
bounded overhead. Older messages outside the retained window are discarded in
this ephemeral MVP. Nothing is imported into Knowledge or durable Memory.
Stopping or restarting the orchestrator loses these observer journals. This is
an explicit privacy-first MVP boundary, not durable conversation storage.

The existing `/voice_chat_start` and music commands retain their existing behavior.
Use the new observer commands for the consent-gated silent workflow. This batch
does not claim to retrofit all legacy capture paths with the new consent contract.

## Current limits

- Local Whisper emits segment-final text. It does not provide native incremental
  deltas. Desktop polls once per second after each request completes; this is not
  the target sub-500-ms partial transcription experience.
- Thread/topic/reply associations remain unresolved. No lexical heuristic or
  proximity rule is presented as semantic intelligence. The chronological journal
  provides evidence for the next stage without pretending that the latest utterance
  is a reply to the previous one.
- Participant labels in Desktop are session-relative; original platform identity
  remains attached to the evidence. Speaking indicators and partial hypotheses
  need the future perception-event stream.
- A connected Desktop status means the snapshot request succeeded, not a guarantee
  that Discord is currently capturing. A failed adapter may leave a temporary
  snapshot until deletion succeeds or its inactivity limit expires.
- There is no PostgreSQL migration, vector indexing or long-term retention in this
  batch. Existing PostgreSQL/pgvector infrastructure will be reused when the
  persistent thread journal is introduced with its retention and access contract.
- The configured ASR model and language remain operator settings. Portuguese and
  English provider capability does not imply accuracy on overlapping or noisy audio.
- The new commands must be deployed by the Discord adapter. No bot actions or
  channel transcription are started by opening Desktop.

## Run and verify

Use the repository's existing compose stacks and development commands. There are
no new environment variables, services, ports or volumes for the observer MVP.
Use real configured speech providers; the stub provider is not a valid live test.
Do not invoke the opt-in observer in a channel until its participants have been
informed and explicitly opted in. The one command author does not authorize
other participants' speech or retention on their behalf.

From the relevant modules:

```sh
# dokja_domain/dokja_conversation and dokja_orch
 go test ./...
# dokja_domain/dokja_conversation concurrency paths
 go test -race ./...
# dokja_services/dokja_discord
 pnpm test
# dokja_interfaces/desktop
 pnpm typecheck
 pnpm test
 pnpm build
```

A live acceptance check uses two consenting test participants and one person who
has not opted in: start, opt in the second participant, speak overlapping known
sentences, inspect Desktop attribution, withdraw the second person's consent,
then stop. Confirm no further evidence appears after withdrawal/stop. Test with
exact known sentences rather than asking the model whether it understood.
Production rollout remains subject to the repository's deployment approval rule.

See [architecture and contracts](architecture.md) for provider capabilities and
semantic evolution, and [the implementation plan](../realtime-conversation-plan.md)
for latency budgets, compaction, PostgreSQL and release gates.
