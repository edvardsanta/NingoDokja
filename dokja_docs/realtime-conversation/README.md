# Realtime Conversation in Dokja

Realtime Conversation is a Dokja capability shared by Discord and Dokja Desktop.
It follows the existing Go orchestrator/domain architecture; it is not a second
bot, a new web application or a replacement for the local owner's chat archive.
The root `AGENTS.md`, README, environment examples and architecture conventions
continue to govern this implementation.

## First vertical flow

1. `/conversation_start` joins the command author's current guild voice channel
   and starts transcribing its human speakers. The public reply announces capture.
   There are no join/leave commands or per-speaker opt-in steps.
2. Discord's individual participant streams pass through the existing bounded
   capture/VAD/ASR path. Only final recognized segments reach `realtime.observe`.
   Speaker identity comes from the authorized interface event, not transcript text.
3. The Conversation domain accepts bounded, revisioned evidence through the
   orchestrator. This route never calls Chat generation, TTS or an LLM.
4. Desktop polls the existing snapshot route and displays session-relative speaker
   attribution, local capture times and chronological final transcripts. UTC capture
   instants remain unchanged on the wire.
5. `/conversation_stop` closes capture and deletes the journal. A human member of
   the same voice channel can stop it from the original text channel. Replacing or
   disconnecting the listener also requests deletion. If deletion cannot reach the
   orchestrator, the journal expires after 15 minutes of inactivity.

No raw audio is written by the observer. Audio exists transiently in bounded
capture/recognition buffers. Journals are process-local, capped at 32 sessions,
16 tracked speakers, 512 messages and 256,000 transcript bytes per session.
Snapshots share a 256,000-byte transcript and 512-message budget across sessions; metadata adds
bounded overhead. Older messages outside the retained window are discarded in
this ephemeral MVP. Nothing is imported into Knowledge or durable Memory.
Stopping or restarting the orchestrator loses these observer journals. This is
an explicit privacy-first MVP boundary, not durable conversation storage.

The existing `/voice_chat_start` and music commands retain their existing behavior.
Use the observer start/stop commands for silent channel transcription.

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

A live acceptance check uses two test speakers: start once, speak overlapping known
sentences, verify both speakers appear without extra commands, inspect Desktop
local timestamps, then stop. Confirm no further evidence appears after stop and
the journal is deleted. Use known sentences to evaluate transcription accuracy.
Production rollout remains subject to the repository's deployment approval rule.

See [architecture and contracts](architecture.md) for provider capabilities and
semantic evolution, and [the implementation plan](../realtime-conversation-plan.md)
for latency budgets, compaction, PostgreSQL and release gates.
