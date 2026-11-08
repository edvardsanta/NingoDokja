# Current capability map

Verified: 2026-10-18 against the current working tree, based on commit `dd0efd4`.
Content plugin update: 2026-10-22; see [migration contract](feeds-content-plugins.md).
Tracking: [steps.json](../steps.json), step `01`.
Design background: [cognitive macro-components research](./cognitive-macrocomponents-research.md).

This document describes source-level behavior and candidate macro boundaries.
Runtime availability has not been tested. A macro-component can span a domain,
injected ports, service implementations, and provider plugins; this inventory
does not prescribe a deployment or folder reorganization.

## Reopened cognitive review

Owner direction (2026-10-26): [step 08 is reopened](cognitive-realignment.md) to
prove understanding and applying knowledge across documents. Feeds is
Knowledge's external integration wing; source/document views expose intake and
provenance. Existing storage, retrieval and feedback features are implementation
evidence, not proof of general consolidation or application learning. Broader
functional grouping remains a hypothesis.

## Layer responsibilities and extension path

External input follows `Interface -> Event/Request -> Orchestrator -> Domain -> Service`.
Business decisions belong to domains. Cross-domain sequencing belongs to the
orchestrator. Infrastructure/provider plugins execute under their service host.
Any future domain-policy extension must be hosted at the domain boundary.

For a new cross-layer operation:

1. Define domain behavior and the required injected port under `dokja_domain/`.
2. Register or extend the event route in [router.go](../dokja_orch/internal/core/router.go).
3. Define the domain action/step in [workflow_engine.go](../dokja_orch/internal/core/workflow_engine.go).
4. Adapt orchestration input/output under [handlers](../dokja_orch/internal/handlers).
5. Implement transport/provider access under [clients](../dokja_orch/internal/clients)
   and the relevant service, then wire dependencies in [main.go](../dokja_orch/cmd/va/main.go).
6. Verify public compatibility and affected module tests before migrating callers.

## Communication boundary classification

[ADR 0012](decisions/0012-communication-services.md) classifies Discord and
WhatsApp gateways as communication-service adapters, with inbound ingress and
outbound transport roles. Desktop, CLI and TUI are operator interfaces. The
gateways live under `dokja_services/dokja_discord/` and
`dokja_services/dokja_whatsapp/`. The [source audit](communication-boundaries.md) identifies
existing read, send, linking and session ownership without changing contracts.
Gateway ingress still submits the existing event/request to orchestration;
reclassification does not permit service-to-domain or operator-to-service bypasses.

## Capability ownership and consumers

The table distinguishes current implementation owners from target cognitive
responsibilities. [ADR 0005](./decisions/0005-knowledge-experience-content.md)
records that Knowledge defines general models/methods and Experience grounds
their concrete application, with Prediction in a feedback cycle the orchestrator
supervises;
[ADR 0006](./decisions/0006-reading-macro-component.md) decides that reading is
one macro-component and Book is not a service;
[ADR 0007](./decisions/0007-faculties-not-features.md) makes every row below a
faculty and treats Book, Meme and Digest as workflows over them. A current domain or deployed service is not automatically
a cognitive macro-component.

| Capability | Domain/policy owner | Mechanism or service | Existing consumer and operation | Candidate macro responsibility |
| --- | --- | --- | --- | --- |
| Reading | Digest owns item selection; Feeds owns collection mechanics | `dokja_services/dokja_feeds` | Digest handler calls `feeds.items` and `feeds.status`, and `feeds.invoke` for a post link | One reading host, scheduled and on demand, absorbing Book and Knowledge extraction (ADR 0006); keep product ranking in Digest |
| Semantic knowledge | `dokja_domain/dokja_knowledge` owns document and [hashtag-example lifecycle rules](hashtag-knowledge.md); consuming workflows decide when to use it | `dokja_services/dokja_knowledge` | Chat retrieves context; hashtag workflow sequences OCR with domain-owned example storage/retrieval; Knowledge commands manage documents | General facts, models and methods with provenance; current document retrieval is the starting mechanism |
| Experience | `dokja_domain/dokja_memory` interprets episodes/outcomes and owns [hashtag observations](hashtag-experience.md) and [confirmed media receipt policy](media-delivery-receipts.md) | Memory service for experiences; existing media cache for transitional delivery flags | Hashtag workflow sequences prediction/recording and feedback; scheduled/manual sends pass confirmed outcomes to Memory receipt policy | Contextual application of knowledge: concrete procedure, parameters, constraints and outcomes; receipt storage and durable attempt tracking still need migration |
| Prediction/evaluation | Memory domain's `Predict` and `Score` functions | Memory service supplies recalled experiences and resolved outcomes | `memory.predict`, `memory.stats`, and hashtag shadow recording | Explicit outcome estimation; currently part of Memory |
| Conversation/language | Go chat handler/session manager currently owns runtime conversation behavior; a separate Python chat domain also exists | `dokja_services/dokja_runtime/chat.go` | `message.*` conversation workflow | Chat domain policy plus replaceable generation providers |
| Media perception and meme pool | Meme facade owns intake composition; Moderation interprets media safety observations; Communications owns destination eligibility | Feeds `media.library.v1` / `media.perceive.v1` plugins; legacy Meme transport opt-in | Meme facade fetch/screen/list, hashtag OCR, scheduled delivery | [Media admissibility](media-admissibility.md) is domain-owned. ADR 0010 still proposes grouping intake under Knowledge, admissibility/relevance under Rules and receipts under Memory; Meme events remain a facade |
| Music in voice channels | Communications domain resolves tracks (`ResolveTrack`, `BestMatch`) and decides each server's queue (`MusicQueue`, `music.*` events) ([music plan](music-plan.md)) | One Feeds `audio.resolve.v1` plugin per source, the owner's in `feeds_plugins/`; `audio-pages` example; playback in the voice adapter; per-person listening as Memory experiences (`music.listen:<user id>`) | `music.*` events through the bridge, sent by the Discord commands and panel | A workflow across Knowledge intake, Rules, Action and Memory, not a component |
| Book processing | `dokja_domain/dokja_book` interprets actions; Python service also selects processing strategies | Feeds `document.process.v1` compatibility plugin; legacy HTTP transport opt-in | `book.resource.classify`, `book.summary.requested` | Retired (ADR 0006): extraction moves to Reading, content to Knowledge, book context to Experience; `book.*` kept as a facade during migration |
| Communications and delivery | Communications domain owns read and inbound eligibility, delivery planning and linking authorization | Communications HTTP read service; Discord and WhatsApp communication adapters | `communications.channels`, `.history`, `.send`, linking, scheduled sends | Read and transport adapters behind explicit eligibility and delivery policy |
| Voice/audio | Communications domain validates bounded synthesis/transcription requests; Chat domain formats multi-speaker room history | `dokja_services/dokja_voice` | Discord continuously captures human speakers with VAD, records incidental conversation, opens named follow-up exchanges, and finishes playback before the next turn; sentence synthesis is prefetched and replies still depend on CLI latency | Audio perception/output providers; [realtime conversation plan](realtime-conversation-plan.md) proposes scoped PostgreSQL history, background compaction and measured latency gates; implementation pending |
| Realtime observer | Conversation domain owns channel scope, evidence identity/revisions and ephemeral retention; orchestrator authorizes/routes | Existing per-speaker Discord capture and segment-final voice service | Silent start/stop channel observer commands and Desktop Realtime snapshot view; [contracts and limits](realtime-conversation/architecture.md) | Streaming partials, semantic thread projection and PostgreSQL adoption remain pending |
| Price history | `dokja_domain/dokja_price` owns the `price.observation.v1` contract, one point per history and day, and promotion verdicts ([ADR 0015](decisions/0015-price-history-domain.md)) | Owner collector plugins in the Feeds plugin directory, outside git; durable PostgreSQL adapter pending | None yet; orchestrator intake task and `assess-price` route pending | A Rules-and-Memory workflow over Knowledge intake; verdicts are codes translated by interfaces |
| Scheduling | Orchestrator controls decide whether a scheduled operation runs | `dokja_services/dokja_scheduler` emits events | Meme refresh and dispatch schedules | Trigger acquisition; workflow execution remains orchestrated |
| Working context/coordination | Orchestrator core; chat lifecycle is currently in handlers | Router, context builder, workflow engine, dispatcher | Every registered route; chat sessions are separate volatile state | Bounded task workspace with domain decisions and explicit transitions |
| Administrative configuration | Controls/System handler and shared profile store | `dokja_store`; host runtime validates explicit provider/model bindings | CLI/TUI profile administration; orchestrator lists/selects profiles | Configuration with deliberate local credential-management boundary |

### Source anchors

- Wiring and routing: [entry point](../dokja_orch/cmd/va/main.go),
  [router](../dokja_orch/internal/core/router.go),
  [workflow actions](../dokja_orch/internal/core/workflow_engine.go).
- Acquisition/selection: [Feeds service](../dokja_services/dokja_feeds/feeds/service.go),
  [Digest domain](../dokja_domain/dokja_digest/domain.go),
  [composition policy](../dokja_domain/dokja_digest/compose.go).
- Knowledge: [domain](../dokja_domain/dokja_knowledge/domain.go),
  [service](../dokja_services/dokja_knowledge/service.py),
  [store](../dokja_services/dokja_knowledge/store.py).
- Memory: [domain](../dokja_domain/dokja_memory/domain.go),
  [prediction and scoring](../dokja_domain/dokja_memory/predict.go),
  [service](../dokja_services/dokja_memory/memory/service.go),
  [store](../dokja_services/dokja_memory/memory/store.go).
- Conversation: [handler](../dokja_orch/internal/handlers/chat_domain_handler.go),
  [session state](../dokja_orch/internal/handlers/chat_session_manager.go),
  [retrieval integration](../dokja_orch/internal/handlers/chat_knowledge.go),
  [Python domain](../dokja_domain/dokja_chat/domain.py),
  [Private inference contract](../dokja_orch/runtime/chat.go).
- Book: [domain](../dokja_domain/dokja_book/domain.go),
  [HTTP contract](../dokja_services/dokja_book/server.py),
  [summary implementation](../dokja_services/dokja_book/book_summary_service.py).
- Delivery: [Communications domain](../dokja_domain/dokja_communications/domain.go),
  [read service](../dokja_services/dokja_communications/server.py),
  [System handler](../dokja_orch/internal/handlers/system_domain_handler.go).
- Audio and scheduling: [Voice contract/providers](../dokja_services/dokja_voice/server.py),
  [scheduler](../dokja_services/dokja_scheduler/scheduler/service.go).

## Contracts, state, and extension points

| Boundary | Current contract | Current state | Extension point and compatibility concern |
| --- | --- | --- | --- |
| Orchestrator ingress | Envelope: `event_id`, `timestamp`, `source`, `type`, `user`, `channel`, `payload`, `context`; HTTP bridge or ZeroMQ request/reply; PUB/SUB events | Controls state; handler-owned sessions; per-request context maps | Preserve envelope and compact/debug responses; normalize typed internal contracts behind ingress |
| Feeds (reading host) | ZeroMQ `feeds.status` / `feeds.items` (cached), `feeds.extract` (on demand, step `07`), `feeds.classify` (on demand, step `09`), `feeds.refresh` and bounded `feeds.invoke` content providers; status/result response | Last successful result per collector and last result per extractor in process memory; readings keep nothing | Strict `plugin.json`, legacy or `plugin_api: 1`; collectors print items (optionally including full plain-text `body`, capped at 100,000 UTF-8 bytes, for Library ingestion and a plain-text preview without refetching a client-rendered page; Digest retains `media_url` for gated image/video previews and applies optional `source`, `query`, `from`, `to` filters before pagination), `document.extract.v1` extractors read a JSON request on stdin and print Knowledge's `Extracted` contract; a `document.classify.v1` classifier reads documents and prints one proposal per document |
| Knowledge | ZeroMQ `knowledge.*`; typed-by-operation payload represented as dictionaries/maps | Explicit PostgreSQL+pgvector backend or preserved SQLite documents/metadata/chunks/embeddings; [verified migration and rollback](knowledge-postgres.md) | Python `register(registry)` adds extractors/fetchers; plugins execute inside the host |
| Memory | ZeroMQ service requests for record, resolve, recall, listing and status; prediction/stats handled by the domain | Own SQLite experiences, original probabilities, outcomes, and embeddings | Injected embedder; no general runtime predictor plugin protocol in inspected code |
| Chat inference | Private Unix socket `POST /chat`, `GET /health`; bootstrap `runtime.chat`, `runtime.chat.configure` | Explicit registered provider/model in host SQLite; bounded single inference; history supplied by caller | Host adapters, no implicit fallback or generation credential env |
| Book | `book.*` facade over Feeds `document.process.v1`; legacy HTTP classify/summarize/health endpoints remain opt-in | Per-request processing; no durable book repository established in inspected request path | Classifier/overview/extraction implementations; preserve request fields and response envelopes |
| Price | Domain actions `record-price-observations` (feed items whose `body` is `price.observation.v1`) and `assess-price` (verdict, signals, medians in cents and basis points); not yet routed | `Repository` port; in-memory reference repository only, so nothing survives a restart until the PostgreSQL adapter lands | Strict contract with unknown fields refused; histories keyed by product, merchant, series (`merchant`, `market_min`) and price kind (`pix`, `card`, `unknown`) are never compared across keys; Digest must exclude price sources before wiring |
| Meme | `meme.*` facade over Feeds collection/library/perception plugins; legacy ZeroMQ transport remains opt-in | Legacy pool/sent state in SQLite | Existing Lab scraper/provider mechanisms; extraction boundary is still coupled to Lab |
| Communications | Authenticated HTTP dispatch for reads; interface clients for delivery/linking/history | Read service is an adapter; platform/interface state varies | Platform-specific adapters; preserve configured-channel and conversation-alias eligibility |
| Voice | HTTP `/tts`, `/stt`, `/wakeword`, `/health`; WebSocket `/tts/stream` | Loaded providers/models and per-stream buffers | Environment-selected provider classes and stubs; no shared plugin manifest |
| Scheduler | Existing envelope, asynchronous scheduled events and job announcements | Timers; reads orchestrator-owned interval configuration | Configured schedules; publish success is not durable workflow acceptance |
| Shared profiles | Local CLI/TUI and `dokja_store` API; Python service reads shared schema | SQLite profiles/settings containing local credentials | Preserve local credential-management restrictions; schema and ownership changes require migration |

Relevant implementation contracts:
[event envelope](../dokja_orch/internal/core/event.go),
[context builder](../dokja_orch/internal/core/context_builder.go),
[Feeds manifest](../dokja_services/dokja_feeds/feeds/manifest.go),
[Feeds runner](../dokja_services/dokja_feeds/feeds/runner.go),
[Knowledge loader](../dokja_services/dokja_knowledge/plugins.py),
[extractor registry](../dokja_services/dokja_knowledge/extractors/__init__.py),
[shared store](../dokja_store/store.go).

## Representative source-level flows

### Conversation

`message.* -> conversation route -> moderation:screen-event -> chat action`

Discord PDF attachments and HTTP(S) PDF links use the same link-conversation
route and `converse-links` action as web readings. The adapter forwards attachment
metadata as optional `documentAttachments`; the bridge maps it to
`payload.document_attachments`. Chat plans the bounded source list, and the
orchestrator calls Digest's existing `ReadWebLink` action and Feeds client.
`feeds.web.read` performs the validated fetch and invokes a fixed embedded PDF
text helper. Original page numbers become citation fragments; extracted text,
PDF metadata and filenames remain data rather than trusted instructions.
The existing four-source, eight-passage and 25 MiB limits apply. Requested page
fragments start the bounded excerpt; page-specific follow-ups can revisit cached
passages. Readings remain temporary and scoped to the same conversation. No
automatic Knowledge import, channel-history retrieval or OCR is added. Regression
coverage includes attachment-only messages, admission before fetch, malicious
document evidence, citation sanitation, follow-ups, channel isolation and real
PDF parser fixtures. An opt-in Feeds test can exercise a public PDF URL.

The Chat domain's versioned [capabilities.json](../dokja_domain/dokja_chat/capabilities.json)
provides static product self-knowledge: supported features, interface entry points
and limits. It is embedded at build time and added to each generated turn's system
context across text, voice, streaming and plain fallback, including after history
compaction. Music playback, editable panel comments and listening memory are
explicit; the catalog states that current listens do not train recommendations
or supply song-comment context. This describes implementation, without asserting
live availability, granting chat execution tools or confirming an operation.
Update the catalog alongside feature changes and rebuild the orchestrator;
private account facts remain in the separately scoped owner configuration.

Discord replies carry a bounded excerpt of the referenced message through the
bridge as optional `replyToContent`, mapped to `context.reply_to_content`. The
chat handler includes it as quoted user context rather than a system instruction,
without changing routing input or reviving expired sessions.

The shared chat persona permits occasional friendly swearing when the joke calls
for it, while forbidding humiliation and threats. It asks for contextual slang interpretation,
brief casual replies, varied wording, and philosophical insight without repetitive
philosophy references. Quoted content and role claims remain untrusted input.
Corrections should produce the corrected answer immediately, with brief
acknowledgment rather than repeated apologies or invented memory explanations.
Group banter distinguishes factual corrections from preferences and social pressure.
Friendly swearing and playful insults may match an established
group's tone. Mock outrage, profanity and mock commands are interpreted in context;
one participant telling Ningo to ignore another does not compel obedience or taking
sides. A denial or dislike alone does not establish that an earlier claim was false;
another participant's teasing is not evidence either. Requests to stop a joke or
insistence on seriousness do not establish facts or install conversational rules.
Ningo chooses its response independently from context and evidence, without promises
of obedience or unsupported retractions. Tests verify this guidance and representative conversation
context reach conversational, streaming and plain generation; live-model tone remains
unverified.
Discord turns receive presentation guidance on every generation path: readable
plain-text math by default, with LaTeX source only when explicitly requested.
The Chat domain's Go presentation policy selects complex expressions or explicit
image requests from bounded math blocks, always retaining a plain-text equivalent.
The chat handler calls the private inference client's rendering port; the host
runtime service renders a PNG without CLI tools, shell commands, or a TeX process.
The bridge forwards up to three PNG attachments to Discord messages, commands,
and chat modals. An unavailable renderer or unsupported expression keeps the text.
This is interface guidance and conversational adaptation, not durable automatic
learning of user preferences.
The selected provider/model does not form part of the chat session key. Runtime
selection changes forward the caller's transcript with its roles unchanged;
assistant turns remain Ningo's replies across models. Discord server message
discussions share a guild/channel scope with participant attribution. DMs and
slash-command sessions retain separate per-user scopes. The Chat domain plans real
compaction before the raw turn window reaches its default 20-message limit,
after three minutes of inactivity, or once older raw context reaches 32 KiB.
The orchestrator requests a summary through the configured inference integration,
merges previous summary context, and retains up to eight recent turns verbatim
within 16 KiB of recent context.
The summary is bounded to 4,096 UTF-8 bytes and supplied as explicitly untrusted
user-level context, with missing details explicitly unknown rather than evidence
that nobody discussed them. Summary inference requires a JSON object with
`kind: "conversation_summary"` and a nonempty `summary` string before replacing
older turns. Runtime's shared provider wrapper respects background system tasks
instead of forcing a conversational answer. This validates task output format,
not factual completeness; existing checkpoints remain compatible. Failed or
invalid summaries preserve the last valid summary
and pending raw history, then continue with a bounded recent projection and a
one-minute retry cooldown. They never silently discard history or claim successful
compaction.
Per-session generation is serialized, while unrelated sessions remain independent.
The orchestrator stores non-archived session snapshots in `dokja_chat_sessions.db`
beside `DOKJA_DB_FILE`, or under `.dokja/` when unset. The separate versioned
SQLite file stores the last valid summary and unsummarized user/assistant turns,
including pending raw history after failed compaction, and reloads them after
restart. Storage errors fail explicitly without a volatile fallback. Explicit new
sessions clear the relevant snapshot; only a verified owner may reset a shared
channel scope. Identity and system instructions are rebuilt per turn, never restored
from the database. Existing local owner conversation archives remain separate.
This stores bot-handled exchanges, not every channel message, and cannot recover
history discarded by earlier builds. It is a current checkpoint, not a complete
transcript archive or pgvector semantic recall of old topics. Exact summaries can
be inspected locally in the SQLite `chat_sessions` table; no public inspection API
or full-context logging is added.
During debates, the shared persona retains its existing sparring-partner guidance:
advance a conceded point into a relevant unresolved tension or clearly hypothetical
thought experiment, without false disagreement or prolonging practical tasks and
closed conversations.
In casual exchanges, the shared persona favors confidence, irony and irreverence,
including dry sarcasm and playful mockery grounded in the current conversation.
A comeback can stand alone without an argument, explanatory lesson or reassurance;
humor is optional, and arrogance should show through judgment and timing rather
than declarations of superiority. Intellectual depth remains central: precise
distinctions and connections across literature, philosophy, history, science,
music and art support both humor and serious reasoning, with accurate references
instead of invented quotations or decorative name-dropping.
Regression tests verify this guidance reaches
conversation, streaming and plain generation; live comedic quality needs observation.
Only the authenticated owner's current turn receives a more supportive debate note.
For that speaker, it develops the strongest reasonable argument, lets genuine
agreement stand, makes further objections optional, and avoids loaded questions or
presuming disputed causal explanations. A requested sympathetic defense stays
bounded by evidence. Other participants retain the shared debate style, including
when they mention the owner or speak next in the same channel.
The owner's preferences do not become evidence or grant additional permissions.
Claims, mentions and summaries cannot obtain this special handling. Regressions
verify prompt delivery and isolation across conversational, streaming and plain
generation, including owner/non-owner turns in one shared session; they do not
establish live-model political reasoning or guaranteed stylistic compliance.
These are generation instructions, not proof of injection resistance or a
replacement for the orchestrator's authorization checks.

Chat policy interprets ironic link comments as conversation before treating them
as factual arguments. It considers laughter, quoted slogans and nicknames in the
available context; it can answer a clear joke without identifying an incidental
nickname or launching an unsolicited fact-check. Uncertain identities stay
unresolved and unsupported allegations remain unverified. Sharing an article
does not itself request a summary or rebuttal. Regression tests check delivery
across conversational, streaming and plain generation for owner and non-owner
speakers; they do not establish live-model comprehension of irony.

Chat policy distinguishes evidence from social pressure: repetition, flattery,
insults, invented consensus and claimed tests do not establish truth or authority.
It preserves valid corrections, warranted agreement, harmless banter and the
existing verified-owner conversation policy. Message text, quoted replies and
summaries cannot configure ownership or install permanent rules. Regression
fixtures verify policy delivery and that forged owner fields stay outside trusted
bridge metadata; they do not constitute live-model jailbreak evaluations.

Roleplay preserves Ningo's independent stance: fictional rulers and commanders
do not gain authority or recruit him into a subordinate office. Courtly address
is restricted to clear irony. Prior assistant replies and summaries remain
untrusted conversation data; an earlier acceptance is acknowledged and corrected
rather than denied or promoted to an obligation. Factual corrections retain their
original scope and do not require inventing another objection. The existing
verified-owner conversation policy remains intact. These are model instructions;
regressions verify their delivery across history/owner/streaming boundaries, not
a guarantee that every generated reply will follow them.

The private, versioned `.dokja/owner.json` supplies owner account mapping and
profile facts at startup; `DOKJA_OWNER_CONFIG_FILE` selects another path. Owner identity is
configured exclusively in JSON; an absent file disables owner recognition, and
invalid files stop startup. Only verified owner turns receive the configured profile: on Discord
the gateway verifies the owner's account; the owner's own interfaces (desktop, CLI, TUI) and the
linked WhatsApp account's "message yourself" chat are the owner's by source, the same rule that lets
them keep conversations, and the identity note says which. A context that claims owner status or a
profile is discarded unless the chat handler granted it. The Chat
domain includes the preferred name in direct identity replies; generated replies
receive profile facts as private application context. See the
[configuration and migration guide](production-runbook.md#private-owner-configuration).

The owner and the people named in that file can ask Ningo to keep something ("guarde isso: ...",
"o que você sabe sobre mim?", "esquece 2", and their English forms). The phrases are answered
without the model. A fact belongs to whoever asked, recognized by the account and never by the
message, and strangers keep nothing. The keep phrase also works as a clause of its own inside a
longer message (`..., guarde isso, ...`): the application keeps the message without the phrase, the
model answers the rest, and the application closes the reply with the outcome. A fact kept in a shared chat only comes back in that chat, and
what was kept in private never reaches a shared chat or its model turn, even when its person asks
there. Facts live beside the conversation archive, are not turns, and the owner sees and deletes
everyone's on the desktop (`conversation.facts`, `conversation.fact_delete`).

Direct owner-identity questions such as `Quem sou eu?` and `Who am I?` have a
Chat-domain reply derived from the Discord gateway's verified-owner metadata.
The ordinary chat handler records that reply without asking the model to infer
identity from history or a display name. Mentions, text claims, unknown request
fields and matching account strings on another interface do not activate this
reply. Other questions retain normal generation and trusted owner guidance.
The existing session-compaction step can still require inference before a turn.
Context diagnostics expose an `owner_verified` boolean, never the configured ID.
For ordinary model-generated conversation, a system note also carries explicit
current-turn ownership and mention booleans. It is rebuilt from the gateway event
on every turn, including after compaction, rather than inferred from history or
names. Non-owner speakers receive an explicit negative verification note. The
provider adapter distinguishes application system messages from identity claims
inside user text, quotes and summaries, with unchanged no-tools controls. These
delivery regressions do not guarantee every model follows the supplied context.

Failed context compaction is recoverable: the chat domain selects a bounded
recent projection while the orchestrator retains pending raw history and the last
valid summary. A one-minute volatile cooldown prevents per-turn summary retries;
caller cancellation and durable storage failures still stop the turn. See the
[operational policy](production-runbook.md#chat-context).

The chat handler opens/touches a session, retrieves optional Knowledge context,
and resolves article requests through the Digest domain's cached Feeds items. A
named source is selected from configured plugin or source names; at most eight
title, date, and summary excerpts enter chat inference. This path does not run a
collector or read a full article. The handler calls chat inference, records reply
history in memory, and composes the response. The
generic dispatcher does not currently interpret a successful moderation result
as a stop instruction. This is a workflow transition gap, not evidence that every
deployment currently has an active blocking policy.

Chat turns and archived conversations read documents the same way. A document a reply cites becomes
the conversation's focus: follow-ups search inside it twice, on their own words and with the start
of Ningo's previous reply, or the question that brought it up when there is none yet, and take hits
from both in turn, until three replies pass without it. The reply comes from the session, so it survives a restart, and it
only guides the search: it is never evidence. The focus lives in the orchestrator's memory. When it
is gone, a short follow-up or a request for the text that finds nothing searches everything with the
previous reply, and a reply that cites the document brings the focus back. A
turn about the text itself (quotes, passages) brings more passages and a text-first brief. Quotes
of four words or more are checked against the document, all of them in such a turn and those
followed by a citation in any other; one it does not hold is replaced by `[…]` before the session
keeps the reply. A cited quotation must occur in its referenced passage; text elsewhere
in the same document cannot validate that link. Each supplied passage has a separate
citation number, including multiple passages of one book. PDF pages and EPUB chapter
files may carry versioned original locations through extraction, Knowledge and the
conversation archive. See [exact passage references](passage-references.md) for the
storage, reader, compatibility and verification contract.

Sources: [dispatcher](../dokja_orch/internal/core/dispatcher.go),
[moderation adapter](../dokja_orch/internal/handlers/moderation_domain_handler.go),
[chat handler](../dokja_orch/internal/handlers/chat_domain_handler.go).

### Discord web conversations

Eligible Discord text messages containing one or more HTTP(S) URLs use
`link-conversation` / Chat's `converse-links` action. Authorization still precedes
all reading. The orchestrator asks Digest's `read-web-link` action to dispatch
`feeds.web.read`; the built-in Feeds reader fetches and parses text without
executing plugins or page scripts. Signature-based refusals may retry through the
bundled HTTP-only `curl_cffi` helper; validated IP pinning, redirect checks, byte
limits and the original deadline apply to both transports. Main article selection
excludes related-story cards and sidebars. Existing social-link delivery remains separate.
Up to four distinct links per message replace the conversation's previous web
reading. URLs in page content or generated replies never schedule reads.

Chat balances up to eight passages across the pages and ranks additional passages
against the current question. Original section anchors, legal article/paragraph
labels, page-supplied publication dates, retrieval times and partial-reading flags
travel with the evidence. Citations identify each supplied passage independently
and append the validated source URL. The existing Library citations continue to
work, with distinct numbers when combined with web evidence.

Web evidence, including every metadata field, enters inference as JSON in a
user-role message following fixed application instructions. It never enters a
system message, enables model tools, or becomes permanent Library knowledge.
The Runtime's existing inference-only event validation remains in force. Exact
web quotations are checked against their cited passages before session storage
and delivery, unknown citation numbers are removed, and arbitrary generated web
links are suppressed. Citation labels remove Markdown controls, mentions and
embedded URLs. These controls restrict effects and attribution; they do not
prove semantic immunity to prompt injection or correctness of every paraphrase.

The orchestrator retains readings only in memory, for at most 64 conversation
scopes and 30 minutes of inactivity. Guild/channel scopes are shared by members;
DMs and commands retain their existing independent scopes. A session reset, new
link message, expiry, capacity eviction or restart discards that reading. Follow-ups
reuse the captured text without refetching; failed and excess links receive a
localized application notice. When every supplied link fails, the application
records one honest failure reply and skips reply inference, preventing speculation
from unread titles or URL slugs. Mixed-success turns retain grounded discussion
and a single application notice for unread links. Web turns require a
conversation-message generator and disable streaming previews so output checks precede delivery. See the
[reader contract](../dokja_services/dokja_feeds/README.md#restricted-web-text-reader).

### WhatsApp content messages

An eligible WhatsApp document is normalized as `message.created` with optional
attachment metadata. Communications authorizes the metadata before a structured
`document_required` reply lets the adapter download bounded bytes. The
`incoming-content` workflow uses Chat's document policy, the Feeds document
extractor and the configured inference provider to generate a localized summary.
Large readings explicitly identify their 80,000-character excerpt limit; no source
is automatically ingested into the Library.

A standalone publication URL uses the same workflow to ask Digest for
`read-link-delivery`: select a `social.resolve.v1` reader, have it write each file
to Feeds through its `media_file` action (or return bytes through `media`), and
return the original caption, source link and files. A file named by token is streamed
through `GET /orchestrator/media/<token>`, up to 100 MB. WhatsApp renders these with locale-catalog signatures and
errors; it never calls a reading service or model directly. See
[WhatsApp content behavior](../dokja_services/dokja_whatsapp/README.md#document-summaries-and-publication-links).

### Digest

`digest.items -> Digest domain -> feeds.items -> cached plugin observations`

Digest applies freshness, ordering, duplicate removal, and source balancing before
paging. Each enabled Feeds plugin runs on its own collection schedule. Reading a
digest does not invoke the collectors synchronously.

`digest.link.read {url} -> Digest domain -> feeds.invoke social.resolve.v1 -> post reader`

A post link is read on demand. Digest asks the enabled post readers in id order,
takes the first that handles the link, bounds the post's fields and keeps only
`image`/`video` media with web addresses. It also gives the post a stable
`document_id` for the Library. A reader's refusal becomes one fixed reason:
`unsupported`, `login_required`, `unavailable`, `no_reader` or `failed`. The
reader's own wording never reaches an interface. The desktop downloads the media
to a temporary folder, and saving goes through `knowledge.ingest`
([post readers](feeds-content-plugins.md#post-readers)).

Sources: [Digest handler](../dokja_orch/internal/handlers/digest_domain_handler.go),
[domain](../dokja_domain/dokja_digest/domain.go),
[post links](../dokja_domain/dokja_digest/link.go),
[policy](../dokja_domain/dokja_digest/compose.go),
[Feeds service](../dokja_services/dokja_feeds/feeds/service.go).

### Book

`book.resource.classify / book.summary.requested -> Book domain -> Feeds client -> document.process.v1 plugin`

The default uses the versioned plugin host; `DOKJA_CONTENT_BACKEND=legacy`
restores the HTTP path. The compatibility adapter preserves the earlier result
shape. [Content plugins](feeds-content-plugins.md) describes collection, media
cache and perception hosting, dependency setup and rollback.

The domain selects the service operation and merges event context/payload. Python
implements classification, processing strategy, and summary preparation. The
policy/mechanism split must be reviewed before reusing extraction or generation
as broader capabilities. ADR 0006 retires this boundary: format/structure becomes
reading provenance, a general method plus applicable experience determines how
the knowledge is used, and the `book.*` routes stay as a compatibility facade
until the owner approves their removal. Current local classifier/server edits belong to another
track and were only read during this inventory.

Sources: [handler](../dokja_orch/internal/handlers/book_domain_handler.go),
[domain](../dokja_domain/dokja_book/domain.go),
[client](../dokja_orch/internal/clients/book_service_client.go),
[service](../dokja_services/dokja_book/book_summary_service.py).

### Cross-document application preparation

`knowledge.application.prepare` is an opt-in, read-only task through the existing
Knowledge route. The orchestrator bounds selected-document reads and workspace
lifetime; Knowledge checks source identity, text fingerprints and content limits.
The result contains versioned, unassessed source observations for an explicit
goal. `knowledge.application.assess` re-reads pinned observations and validates an
explicit caller proposal of cited claims and method prerequisites. It exposes
missing/conflicting/contradicted support and conditional applicability, with
semantic meaning unverified and execution disabled.
`knowledge.application.propose` now obtains a task-bound model proposal through
Runtime, anchors unique quotations and runs that assessor. An explicit trusted
host binding can now journal native pinned source-read receipts through the
preparation route; see [source receipts](knowledge-source-receipts.md). This
proves source validation only. The Desktop Apply view, its native source supervisor
and operator routes were removed on 2026-11-08 because no flow consumed them; the
[Apply review](knowledge-apply-review.md) moves preparation into the Talk workflow.
The PostgreSQL journal stays, configured by its DSN alone, and startup recovery still
interrupts reads an earlier process left unsettled. Preparation inside Talk, actual
method outcomes and learned episode reuse remain pending in `08.2`.
See [preparation](knowledge-application.md), [assessment/evaluation inputs](knowledge-applicability.md)
[provider proposal bounds](knowledge-provider-proposals.md), and
[measured inference](inference-token-budgets.md). The separate
`knowledge.application.propose.measured` action applies post-generation token
acceptance limits with retained usage; it does not enforce provider billing caps.
`knowledge.application.review` is a separate measured action that asks for one model
opinion on a candidate and derives a rule-based outcome. It verifies nothing and
disables execution; see [semantic review](knowledge-semantic-review.md).
`knowledge.application.propose.adaptive` finds its own evidence from the goal and
retrieves once more on a gap; see [adaptive retrieval](knowledge-adaptive-retrieval.md).
`cmd/knowledge-comparison` runs the frozen cases through this path for scoring; see
[the comparison harness](knowledge-comparison.md).

### Automatic intake

`scheduler (knowledge.intake) -> knowledge.intake.run -> intake task in the background: collect (feeds.items) -> Knowledge domain selects -> ingest (knowledge.ingest per document)`

Off until the `library.intake_sources` setting lists Feeds plugins. The Knowledge domain decides what is
taken (`dokja_domain/dokja_knowledge/intake.go`); Feeds never calls Knowledge. Storing is not
assimilation: this path adds documents, not concepts, claims or methods.

### On-demand reading

`knowledge.read -> reading task: read (feeds.extract -> document.extract.v1 plugin) -> ingest (Knowledge domain -> knowledge.ingest per document) -> classify (feeds.classify -> document.classify.v1 plugin -> Knowledge domain -> knowledge.metadata.get/patch per new document)`

The orchestrator sequences reading and storage; Feeds never calls Knowledge. The
Knowledge domain owns the provenance it stores (`file:<name>` source, format and
reader tags). Step 09.2 adds structured form/content classification with per-axis
provenance, topics, entities, date anchors and reader version. The domain resolves
explicit assertions and source defaults, then proposals from an enabled
`document.classify.v1` plugin for new content whose axes nobody decided (none
ships). Classifier failures leave the questions pending; they never fail the
reading. See the [reading-time classifier](./step-09-consolidation.md#reading-time-classifier).
Knowledge persists metadata separately from chunks and projects it in search/list.
Metadata-only updates preserve embeddings; unchanged rereads preserve unspecified
assertions through a domain-defined patch. Legacy `kind` filtering is unchanged.
See the [storage contract](../dokja_services/dokja_knowledge/README.md#structured-metadata)
and [migration/rollback](./production-runbook.md#knowledge-metadata-schema-upgrade).
The `knowledge-extractors` example plugin serves Knowledge's own
extractors through the host, so the same formats are read on both paths while
`knowledge.ingest` with a file stays unchanged. Results and costs are in the
[step 07 record](./step-07-reading.md).

Sources: [reading task](../dokja_orch/internal/handlers/knowledge_reading.go),
[domain](../dokja_domain/dokja_knowledge/reading.go),
[host](../dokja_services/dokja_feeds/feeds/extract.go),
[classification](../dokja_domain/dokja_knowledge/reading_classification.go),
[classifier host](../dokja_services/dokja_feeds/feeds/classify.go),
[adapter](../dokja_services/dokja_feeds/plugins.example/knowledge-extractors/run.py).

### User classification answers

`CLI knowledge classify <source-id> -> knowledge.classify -> Knowledge domain ->
knowledge.metadata.get / knowledge.metadata.patch`

The existing Knowledge route prefix and generic service client are reused; the
workflow maps `knowledge.classify` to `classify-knowledge`. The domain owns the
questions, allowed taxonomy values and user provenance. Storage applies a
revision-checked metadata patch, never extraction or re-embedding. The CLI asks
only unresolved questions or accepts explicit flags, leaves blank answers
pending and reports conflicts without automatic retry. Other interfaces can use
the same action after adding their own explicit presentation contract.

### Hashtag, action, and feedback

`meme.hashtag.suggest -> orchestrated OCR -> Knowledge search restricted to explicit labels -> candidate`

Manual tagging writes an explicit label to Knowledge. Scheduled/manual delivery
can append a relevant learned suggestion. Experience integration starts background
work to obtain an acceptance prediction and record the suggestion; later explicit
tagging resolves the corresponding experience. Desktop tag saves also evaluate
a shadow candidate before storing the new Knowledge label, then record and
resolve it only after the save succeeds. Missing candidates create no scored
example; historical predictions are preserved.

Memory owns the observation identity, action name, bounded context/evidence,
copied prediction probabilities and feedback request preparation. Orchestration
retains the background work and time budget. The typed task records its frozen
prediction only after successful delivery and never predicts again while
recording; a missing snapshot stays unscored. See
[hashtag experience ownership](hashtag-experience.md).

Media fetch now leaves delivery state untouched. Scheduled delivery records a
media-level receipt after at least one transport success; manual delivery keeps
the explicit `mark_sent` option, including known partial success. Memory owns
receipt eligibility, while the current media cache retains physical flags. See
[receipt semantics and rollout](media-delivery-receipts.md).

The background callback predicts before recording its experience, but is not a
durable prediction-before-delivery gate. The experience reference is derived from
the media address, rather than a unique execution attempt. These semantics must be
accounted for before comparing repeated action attempts in a new workspace.

Sources: [hashtag composition](../dokja_orch/internal/handlers/meme_hashtag_workflow.go),
[Memory hashtag rules](../dokja_domain/dokja_memory/hashtag.go),
[experience integration](../dokja_orch/internal/handlers/hashtag_experience.go),
[delivery](../dokja_orch/internal/handlers/system_domain_handler.go).

## Gaps and compatibility review queue

| Finding | Consequence | Planned review |
| --- | --- | --- |
| Domain/service boundaries frequently use generic maps | Internal contract mismatches appear at runtime | Typed contracts behind existing public adapters |
| Step execution aggregates results without transition decisions | Stop/abstain/degrade behavior is not a general workflow primitive | Internal step outcomes and task state |
| Chat policy resides in Go orchestration handlers alongside a separate Python domain | Two places can appear authoritative | Caller audit and one runtime policy owner |
| System handler mixes administration, delivery, status, screening and platform adapters | Macro-component boundaries are implicit | Focused domain ports and orchestrated workflows |
| Feeds and Knowledge have different plugin lifecycle/isolation models | Compatibility, health, and invocation limits vary | Versioned lifecycle with legacy adapters; keep capability-specific APIs |
| Services already own persistent stores and caches despite the stateless-service wording | Intent and implementation terminology conflict | Architecture decision; retain current responsibilities until explicitly resolved |
| Memory estimate is acceptance probability; retrieval has similarity/relevance | A generic confidence field would conflate different claims | Named prediction target, method, support, baseline and outcome |
| Voice providers exist but are absent from inspected orchestrator bindings | Service presence does not prove end-to-end integration | Trace actual consumers before adding Voice domain routes |
| `dokja_prediction` is an untracked Rust scaffold with overlapping storage/recall and a heuristic confidence placeholder | It could duplicate Knowledge and Memory if treated as production architecture | Keep the experiment untouched; reconcile scope before adoption |
| Background experience callbacks and scheduled publications are not durable job records | Lost or repeated work can invalidate traces and evaluation | Explicit acceptance/receipt design for workflows requiring durability |

Compatibility checks before any implementation: public envelope; routes/actions;
service HTTP/ZeroMQ payloads; interface commands; plugin manifest and registry APIs;
SQLite schemas; environment variables; compose service/port/volume configuration;
compact/debug replies; local credential-management and configured-channel rules.

Existing test entry points are module-local Go suites and Python tests, with
[scripts/test_all.sh](../scripts/test_all.sh) providing partial aggregate coverage.
Its list must be inspected for the affected module; it is not proof that every
service/domain is covered. No runtime suite was executed for this documentation
inventory.

## Prototype decisions and next step

The [prototype architecture decisions](./decisions/README.md) record workspace
ownership/lifecycle, plugin compatibility, domain versus provider responsibilities,
the first prediction target, and the existing state terminology conflict.
These are implementation decisions, not claims that the prototype is delivered.
Step `03` captures behavior and measurement baselines before the internal contract
implementation. The experiments remain hashtag composition plus one document
extractor adapter.

### Explicit Knowledge reclassification

`CLI knowledge reclassify <source-id> -> knowledge.reclassify -> typed
snapshot/classify/keep task -> Knowledge domain -> knowledge.classification.sample
-> feeds.classify -> Knowledge domain -> knowledge.metadata.patch`

The domain owns eligible axes and provenance; orchestration owns the provider
call and task lifecycle. The service provides a bounded chunk reconstruction and
a revision from one read transaction. The patch uses that pre-inference revision.
User answers and questions remain authoritative; chunks and embeddings are kept.
See [reclassification domain](../dokja_domain/dokja_knowledge/reclassification.go)
and [task](../dokja_orch/internal/handlers/knowledge_reclassification.go).


### Explicit experience feedback entry points

Desktop is the primary feedback interface: inspect a record, accept its result,
enter the actual result or explicitly mark it unused, then confirm. The TUI
experience list provides `a`/`c`/`i` with confirmation. Coding agents can mediate
an explicit user statement through the existing request adapter. Each uses
`memory.resolve` through the orchestrator; the Memory domain owns observed/detail
comparison and the service retains the first verdict. Concurrent receipts report
the stored outcome. Feedback does not alter Knowledge hashtag examples. See
[experience feedback](experience-feedback.md) for semantics, receipts and the
existing media-reference/attempt limitation.
### Local module lifecycle bootstrap

Desktop's local container controls use a host bootstrap orchestrator in
`dokja_orch/runtime` because the main orchestrator may itself be stopped. The flow
remains Interface → Request → Orchestrator → Domain → Service:
`runtime.modules`/`runtime.set` are routed locally to
`dokja_domain/dokja_runtime` and the stateless `dokja_services/dokja_runtime`
adapter. Conversational/domain events remain on the existing application router.
The local control envelope has the existing fields and a separate versioned,
project-bound response; no existing route or envelope is changed.

The domain owns the fixed lite-stack dependency graph, ownership and lifecycle
state. The adapter provides bounded Compose start/stop/inspect operations and
application readiness probes. The Electron main process alone owns private
socket access and detached daemon bootstrap; the renderer can request only known
module actions, with a real input gesture for writes. No arbitrary event dispatch,
command execution, database access or container socket reaches the renderer.

Opening desktop never starts the stack. Explicit On starts a module and its
required dependencies; Off preserves data, shared/borrowed dependencies and
recognized external consumers. Runtime lifecycle and `services.set` routing
controls remain separate. The first backend is the local lite stack on Unix;
production, remote hosts and Windows are outside this initial implementation.
See [runtime controller](../dokja_orch/runtime/README.md).

Runtime operational settings are shared by Desktop, CLI and TUI through the
bootstrap controller. The Runtime domain owns validation and apply policy; the
runtime infrastructure adapter stores a revisioned snapshot in the shared
`dokja_store` SQLite settings table and projects it into Compose child processes.
Legacy `.env` values are imported once; credentials stay outside this API and the
embedding profile is fixed. Provider inventory and registrations are available;
model/capability bindings remain pending. See [ADR 0013](decisions/0013-database-runtime-settings.md).

The bootstrap controller also routes read-only `runtime.providers` through the
Runtime domain and provider-discovery infrastructure. Operator interfaces share
the same inventory; discovery cannot infer, select a model, start a module or
write memory. See [provider inventory](provider-inventory.md).

Supported provider registrations now persist independently under
`runtime.providers.v1`. `runtime.providers.configure` validates known adapter IDs,
discovery eligibility, model declarations and the exact read revision before
writing through the Runtime domain/infrastructure port. Desktop, CLI and TUI
confirm edits and preserve stale revisions rather than merging them silently.
Declared names remain unverified; inference bindings and the fixed embedding
profile are unaffected. See [provider registrations](provider-registrations.md).

## Communication policy update

Step 09.4-b adds domain-owned [delivery ports and policy](../dokja_domain/dokja_communications/delivery.go).
System handlers continue to sequence I/O but delegate target validation,
disabled-platform selection, explicit bypass permission, safe-only decisions and
named-conversation restrictions. [Link authorization](../dokja_domain/dokja_communications/link.go)
and [inbound eligibility](../dokja_domain/dokja_communications/inbound.go)
now belong to Communications. The orchestrator runs inbound authorization before
the existing chat or command workflow; gateways keep protocol filtering. The
[migration record](communication-boundaries.md) covers the coordinated bridge
contract change and remaining relocation work.
