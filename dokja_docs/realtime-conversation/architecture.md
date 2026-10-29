# Realtime conversation architecture and extension contracts

## Responsibility map

The mandatory dependency flow remains Interface -> Event/Request -> Orchestrator
-> Domain -> Service. Backend preferences in the external brief do not justify
replacing existing Go components with an additional Node backend.

| Component | Dokja location | Contract |
| --- | --- | --- |
| DiscordAudioAdapter / SpeakerResolver | Discord TypeScript voice adapter | Individual authorized streams, actual platform speaker identity, bounded buffers, transport timestamps, listener generation |
| AudioStreamProcessor / VoiceActivityDetector | Existing voice adapter and Communications voice integration | Decode/downmix/normalize, VAD/endpointing; no semantic topic decisions |
| TranscriptionService | Stateless voice service via orchestrator | Provider capability negotiation, per-stream hypotheses and finalized revisions |
| ConversationEventBus / LLMOrchestrator | Existing orchestrator routes/workflows | Independently admit observations; sequence projection jobs without capture depending on model latency |
| ConversationContextEngine | Conversation domain | Evidence identity, consent/scope, revision lifecycle, bounded journal; future thread/reply hypotheses and validated state transitions |
| TopicDetector / ThreadResolver / ContextMemoryManager | Future Conversation-domain policies with stateless inference adapters | Attributed evidence, unresolved alternatives, source references, correction-aware summaries; no replacement of transcript text by generated prose |
| RealtimeGateway | Existing orchestrator ingress and Desktop transport | Current bounded reads; future incremental subscription/replay with authorization and gap reporting |
| Frontend | Existing Dokja Desktop React workspace and Discord commands | Display/control consent, original chronology, independent semantic projection, lifecycle/latency status |

No additional Redis, public HTTP listener, frontend application, database container
or distributed broker is needed for the observer slice. Existing ZeroMQ requests
serve Desktop. Discord uses the authenticated HTTP bridge, with inbound channel
policy preceding the observer action. Capture/ASR admission is independent from
Chat's serialized response/playback loop.

## Additive operations

All requests retain the shared event envelope. No existing field changes type.
The new route `realtime.*` maps to Conversation with `observe-conversation` steps.
Discord bridge payloads add an optional `realtime` object; those fields are never
parsed as text commands. The bridge authenticates and establishes source, actor,
guild and origin text channel. The domain does not trust a payload speaker ID.

| Operation | Authorized source | Payload | Result |
| --- | --- | --- | --- |
| `realtime.start` | Authenticated Discord gateway after inbound policy | `session_id`, `voice_channel_id` | New silent observer; only actor consents |
| `realtime.join` | Same gateway, same guild/origin | `session_id` | Actor opts in; interface verifies current voice membership |
| `realtime.leave` | Same gateway, same guild/origin | `session_id` | Actor revoked; their retained messages removed |
| `realtime.observe` | Same gateway, active consenting speaker | Message fields below | Idempotent final evidence admission; no generated reply |
| `realtime.stop` | Same gateway, consenting actor | `session_id` | Delete ephemeral journal |
| `realtime.snapshot` | Local Desktop source only | None | Bounded session/transcript projection |

A final message has `message_id`, `session_id`, event-derived `speaker_id`,
`timestamp_start`, `timestamp_end`, `text`, `transcription_status: final`, and
`revision`. Timestamps are capture-envelope times, not word alignment. The journal
keeps input chronology and overlapping intervals. Stable identity plus revision
replaces a corrected transcript in place; a duplicate revision with different
content is a conflict. Interface-supplied semantic links are discarded.
`thread_ids` is initially empty, and absent `reply_to` means unresolved. No
confidence number is invented for an association that has not been inferred.

Future deltas require `utterance_id`, monotonic `revision`, `partial/final/revised`
status and provider item identity. Partials live in a replaceable immediate view;
they cannot confirm decisions or durable memory. Event IDs deduplicate delivery;
utterance/message IDs deduplicate transcription updates. Observation acknowledgments
must not be confused with durable commits when persistence is ephemeral.

## Provider verification and tradeoffs

Source checks on 2026-10-29:

- Discord's current Voice documentation describes bot voice handshakes, UDP,
  speaking events, resume and DAVE transitions. The installed `@discordjs/voice`
  0.19.1 README explicitly warns that audio receive is undocumented by Discord
  and stable support is not guaranteed. Use an authorized bot, not a personal
  account. Test reconnect/encryption transitions against the installed library;
  fail closed when speaker identity or receive compatibility is uncertain.
- Discord's Developer Policy requires respecting user data, disclosures and
  consent. Platform permissions alone do not constitute participant consent.
  Legal requirements depend on where recording/retention occurs; no jurisdiction
  or legal conclusion is assumed by this engineering plan. Long-term recording
  remains off pending operational review.
- Official OpenAI Realtime transcription documentation currently lists
  `gpt-live-transcribe` for transcription sessions. The server-side WebSocket uses
  transcription intent and `session.update` with transcription type/model. Audio
  is 24 kHz PCM in the documented example. This model uses client-side endpointing
  and manual commit, not server/semantic VAD. Deltas and completion are different
  events. The provider does not supply word timestamps, speaker labels or
  confidence; preserve Discord stream identity and application capture timestamps.
- A `TranscriptionProvider` should advertise `incremental`, supported PCM format,
  supported language hints, final/revision behavior, cancellation and bounded
  queue capabilities. The current Whisper adapter is segment-final. A streaming
  adapter must be implemented through orchestrated service integration; Discord
  must not call a cloud speech provider directly.
- A production `LLMProvider` must expose explicit model selection, bounded typed
  requests, deadlines, provenance, cancellation and capability flags. Current
  Codex CLI timings do not qualify it for realtime semantics. The observer invokes
  no CLI/model; the semantic worker should use a separately qualified API adapter.
  No unsupported model identifier or provisional CLI preview becomes audio.

Official documentation was checked via the providers' documentation and the
installed package README. External URLs are omitted from this repository document
under its publishing rules. This does not imply external integration was exercised.

## Semantic progression

The next domain projection is a graph of evidence, threads, questions, decisions
and hypotheses, not one message list used as the entire prompt. Each thread has
its own bounded memory. A topic change leaves other threads paused/active until
there is evidence to resolve or close them. Referring to another thread links it;
it does not merge it automatically. Model output is a proposed annotation with
scope, source message revisions, confidence and alternatives.

The JWT/Minecraft fixture must resolve the storage answer to the earlier token
question, even when intervening messages discuss a game. Similar wording or
recency alone cannot justify that relation. A semantic worker uses participant
turn-taking, open questions, explicit resumptions and candidate evidence. Below a
validated confidence threshold it retains alternatives and abstains. The full
original timeline remains accessible beside the semantic projection.

Projection jobs run after final admission in bounded queues. Model slowness does
not block audio capture or transcript display. A job applies only if its source
revision fingerprint still matches and the speaker/session has not been revoked
or deleted. Late results cannot recreate withdrawn evidence. Compaction and
embeddings use the same invalidation rules and separate resource budgets.

Persistent adoption uses the existing PostgreSQL server with a separately owned
Conversation schema. pgvector is retrieval support, not the source of truth for
thread lifecycle, participant access or decisions. Model-generated summaries keep
evidence links and never promote an inference into a confirmed user statement.
No database migration or embedding model is selected by this observer batch.

## Delivery sequence and acceptance

1. **Observer vertical slice (this batch):** consenting stream -> existing final
   ASR -> independent evidence journal -> Desktop view. Verify revocation, scope,
   duplicates, revisions, overlap and unavailable model independence.
2. **Incremental perception:** provider abstraction plus a real streaming adapter;
   partial/final revisions; bounded subscriptions instead of one-second polling;
   speaking state, replay cursors and explicit gaps. Benchmark p50/p95/p99.
3. **Thread engine:** API-backed background interpretation, ambiguity, questions,
   resumptions, correction fencing and per-thread compaction. Validate against
   labeled synthetic multi-topic fixtures and consented real conversations.
4. **Persistence and recall:** reviewed PostgreSQL migration/retention/deletion,
   scoped selective retrieval and optional pgvector after a recall/latency baseline.
5. **Contextual assistant:** explicit observer/on-demand/assistant/analysis modes;
   answer from the selected thread, cite evidence, acknowledge uncertainty, keep
   spoken playback uninterrupted. Observer remains the default new session mode.

Do not mark the later steps complete because the observer view exists. Release
claims require real provider tests, not just mocks or synthetic text fixtures.
