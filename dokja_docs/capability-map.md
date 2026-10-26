# Current capability map

Verified: 2026-10-18 against the current working tree, based on commit `dd0efd4`.
Content plugin update: 2026-10-22; see [migration contract](feeds-content-plugins.md).
Tracking: [steps.json](../steps.json), step `01`.
Design background: [cognitive macro-components research](./cognitive-macrocomponents-research.md).

This document describes source-level behavior and candidate macro boundaries.
Runtime availability has not been tested. A macro-component can span a domain,
injected ports, service implementations, and provider plugins; this inventory
does not prescribe a deployment or folder reorganization.

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
| Reading | Digest owns item selection; Feeds owns collection mechanics | `dokja_services/dokja_feeds` | Digest handler calls `feeds.items` and `feeds.status` | One reading host, scheduled and on demand, absorbing Book and Knowledge extraction (ADR 0006); keep product ranking in Digest |
| Semantic knowledge | `dokja_domain/dokja_knowledge` owns document and [hashtag-example lifecycle rules](hashtag-knowledge.md); consuming workflows decide when to use it | `dokja_services/dokja_knowledge` | Chat retrieves context; hashtag workflow sequences OCR with domain-owned example storage/retrieval; Knowledge commands manage documents | General facts, models and methods with provenance; current document retrieval is the starting mechanism |
| Experience | `dokja_domain/dokja_memory` interprets episodes/outcomes and owns [hashtag observations](hashtag-experience.md) and [confirmed media receipt policy](media-delivery-receipts.md) | Memory service for experiences; existing media cache for transitional delivery flags | Hashtag workflow sequences prediction/recording and feedback; scheduled/manual sends pass confirmed outcomes to Memory receipt policy | Contextual application of knowledge: concrete procedure, parameters, constraints and outcomes; receipt storage and durable attempt tracking still need migration |
| Prediction/evaluation | Memory domain's `Predict` and `Score` functions | Memory service supplies recalled experiences and resolved outcomes | `memory.predict`, `memory.stats`, and hashtag shadow recording | Explicit outcome estimation; currently part of Memory |
| Conversation/language | Go chat handler/session manager currently owns runtime conversation behavior; a separate Python chat domain also exists | `dokja_services/dokja_runtime/chat.go` | `message.*` conversation workflow | Chat domain policy plus replaceable generation providers |
| Media perception and meme pool | Meme facade owns intake composition; Moderation interprets media safety observations; Communications owns destination eligibility | Feeds `media.library.v1` / `media.perceive.v1` plugins; legacy Meme transport opt-in | Meme facade fetch/screen/list, hashtag OCR, scheduled delivery | [Media admissibility](media-admissibility.md) is domain-owned. ADR 0010 still proposes grouping intake under Knowledge, admissibility/relevance under Rules and receipts under Memory; Meme events remain a facade |
| Book processing | `dokja_domain/dokja_book` interprets actions; Python service also selects processing strategies | Feeds `document.process.v1` compatibility plugin; legacy HTTP transport opt-in | `book.resource.classify`, `book.summary.requested` | Retired (ADR 0006): extraction moves to Reading, content to Knowledge, book context to Experience; `book.*` kept as a facade during migration |
| Communications and delivery | Communications domain owns read and inbound eligibility, delivery planning and linking authorization | Communications HTTP read service; Discord and WhatsApp communication adapters | `communications.channels`, `.history`, `.send`, linking, scheduled sends | Read and transport adapters behind explicit eligibility and delivery policy |
| Voice/audio | Provider selection currently in Voice service; no Voice domain binding found in inspected orchestrator entry point | `dokja_services/dokja_voice` | Service exposes synthesis, transcription, wakeword, and streaming APIs; full runtime consumer wiring is not established by this review | Audio perception/output providers, pending orchestration integration review |
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
| Feeds (reading host) | ZeroMQ `feeds.status` / `feeds.items` (cached), `feeds.extract` (on demand, step `07`), `feeds.classify` (on demand, step `09`), `feeds.refresh` and bounded `feeds.invoke` content providers; status/result response | Last successful result per collector and last result per extractor in process memory; readings keep nothing | Strict `plugin.json`, legacy or `plugin_api: 1`; collectors print items, `document.extract.v1` extractors read a JSON request on stdin and print Knowledge's `Extracted` contract; a `document.classify.v1` classifier reads documents and prints one proposal per document |
| Knowledge | ZeroMQ `knowledge.*`; typed-by-operation payload represented as dictionaries/maps | Explicit PostgreSQL+pgvector backend or preserved SQLite documents/metadata/chunks/embeddings; [verified migration and rollback](knowledge-postgres.md) | Python `register(registry)` adds extractors/fetchers; plugins execute inside the host |
| Memory | ZeroMQ service requests for record, resolve, recall, listing and status; prediction/stats handled by the domain | Own SQLite experiences, original probabilities, outcomes, and embeddings | Injected embedder; no general runtime predictor plugin protocol in inspected code |
| Chat inference | Private Unix socket `POST /chat`, `GET /health`; bootstrap `runtime.chat`, `runtime.chat.configure` | Explicit registered provider/model in host SQLite; bounded single inference; history supplied by caller | Host adapters, no implicit fallback or generation credential env |
| Book | `book.*` facade over Feeds `document.process.v1`; legacy HTTP classify/summarize/health endpoints remain opt-in | Per-request processing; no durable book repository established in inspected request path | Classifier/overview/extraction implementations; preserve request fields and response envelopes |
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

The chat handler opens/touches a session, retrieves optional Knowledge context,
and resolves article requests through the Digest domain's cached Feeds items. A
named source is selected from configured plugin or source names; at most eight
title, date, and summary excerpts enter chat inference. This path does not run a
collector or read a full article. The handler calls chat inference, records reply
history in memory, and composes the response. The
generic dispatcher does not currently interpret a successful moderation result
as a stop instruction. This is a workflow transition gap, not evidence that every
deployment currently has an active blocking policy.

Sources: [dispatcher](../dokja_orch/internal/core/dispatcher.go),
[moderation adapter](../dokja_orch/internal/handlers/moderation_domain_handler.go),
[chat handler](../dokja_orch/internal/handlers/chat_domain_handler.go).

### Digest

`digest.items -> Digest domain -> feeds.items -> cached plugin observations`

Digest applies freshness, ordering, duplicate removal, and source balancing before
paging. Each enabled Feeds plugin runs on its own collection schedule. Reading a
digest does not invoke the collectors synchronously.

Sources: [Digest handler](../dokja_orch/internal/handlers/digest_domain_handler.go),
[domain](../dokja_domain/dokja_digest/domain.go),
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
tagging resolves the corresponding experience.

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
