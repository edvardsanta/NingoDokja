# Step 09: generic knowledge consolidation and faculty migration

Date: 2026-10-19. Status: in progress. Tracker: [steps.json](../steps.json).
Design: [ADR 0008](./decisions/0008-consolidation-by-kind-of-knowledge.md).

ADR 0008 supersedes the earlier Book-specific facade as the first migration unit.
The target is a generic reading/classification/consolidation flow, followed by
Meme/Digest faculty workflows and conversation ownership. Existing Book runtime
contracts remain until retirement scope is resolved.

## 09.1: classification contract

Completed: the [Knowledge domain](../dokja_domain/dokja_knowledge/classification.go)
defines separate form and content-kind values with structured provenance for
each axis. The owner confirmed both axes and structured defining metadata
separate from free-form tags during this step.

| Responsibility | Behavior |
| --- | --- |
| Form | `book`, `article`, `note`, `conversation`, or `meme` |
| Content kind | `news`, `reference`, `research`, or `narrative` |
| Precedence per axis | Explicit user value; automatic proposals/defaults only absent uncertainty; otherwise ask the user |
| Unspecified value | Makes no claim; lower-priority sources may supply that axis |
| Missing or uncertain axis | `null`, with `user_required` provenance; only the user resolves uncertainty |
| Invalid user/default value | Refused when selected; never silently treated as a valid kind |
| Invalid provider value | That axis stays pending with a fixed reason code; the other valid axis can still be used |
| Provider provenance | Provider ID and version required before attributing a value to the classifier |
| File format | Kept outside form/content classification; PDF is not a content kind or form |

A research book remains `form=book`, `content_kind=research`. A research article
can use the same content method while retaining its different structure. Topic
labels such as physics are separate defining metadata, not content kinds.

The function resolves supplied proposals; it does not infer labels from text,
call an agent provider, cache classifications, select summary methods, or persist
metadata. This is an internal domain contract, not a new API payload.

### Compatibility review

- The event envelope, public actions/routes and HTTP/ZeroMQ contracts are unchanged.
- Existing `kind` strings, including `meme_hashtag`, are not reinterpreted.
- No database schema, setting, port, service, volume or compose file changes.
- Existing reading and ingestion paths do not call this resolver yet.
- Book service/domain/client/routes and CLI behavior remain as they are.

### Verification

[Classification tests](../dokja_domain/dokja_knowledge/classification_test.go)
cover independent axes, mixed provenance, user precedence, normalization, rejected
`general` labels, nullable cold start, partial proposals, invalid provider output/identity, invalid
user/source values, and subject-kind preservation across forms.

- Knowledge module: `go test -race ./...` and `go vet ./...` passed.
- Orchestrator: `go test ./... -count=1` passed.
- `git diff --check` passed.

The first sandboxed rerun could not write the Go build cache or bind local test
ports. The checks above are the successful rerun outside the sandbox. No live
provider, extraction, database migration or deployment was exercised.

## 09.2: persistence and reading integration

Implemented foundation in `.worktrees/knowledge-metadata`, branch
`feat/knowledge-metadata`. The Go Knowledge domain accepts explicit form/content,
topics, entities and a date anchor, resolves provenance, and adds reader identity
and version to Reading ingests. EPUB/MOBI default to book, HTML/RSS/Atom to article;
PDF has no form default. Content kind requires explicit input or a future
classifier; a feed alone does not imply news.

Knowledge service schema 2 stores a JSON metadata object in a separate
`document_metadata` table. Search/list project it alongside existing fields.
Existing rows expose `{}`; legacy `kind` and tag semantics remain intact. A
metadata-only write leaves chunk IDs, vectors and embedding-cache revision intact.
Unchanged content without metadata preserves previous metadata; changed content
without new metadata invalidates it. Reading supplies an explicit patch for
unchanged content, preserving earlier user assertions when a reread omits them.
No model or agent CLI is invoked.

The [service contract](../dokja_services/dokja_knowledge/README.md#structured-metadata)
details replacement/patch behavior and partial multi-document writes. The
[runbook](./production-runbook.md#knowledge-metadata-schema-upgrade) documents
transactional migration, consistent backup and two rollback options: restore the
pre-upgrade backup, or discard metadata offline while retaining current content.
Older binaries refuse schema 2. Roll out the storage service before the domain;
roll both back together. These compatibility changes are additive on the wire;
there are no envelope, route, command, configuration or compose changes.

### Remaining 09.2 work

The substep remains in progress. The reading-time classifier capability is now
connected (see [Reading-time classifier](#reading-time-classifier) below), but no
classifier plugin ships, explicit reclassification is not implemented and the
desktop and chat interfaces do not present the questions yet. Missing axes stay
pending for the user; no general fallback is used. The stable reread tests
establish preservation behavior, not model quality.

The remainder of step `09` also includes generic summary/consolidation methods,
the unresolved Book runtime retirement scope, and the faculty migrations in
ADRs 0006 and 0007.

### Verification for the persistence foundation

- Knowledge service: `python -m pytest -q dokja_services/dokja_knowledge/tests`:
  **143 passed, 8 skipped**. Skips require live Ollama; no live model was tested.
- Knowledge domain: `go test -race ./...` and `go vet ./...` passed.
- Orchestrator: `go test ./... -count=1` passed.
- Disposable SQLite tests cover v1 upgrade, failed-migration rollback, old-build
  refusal, metadata downgrade and re-upgrade, retained vectors/FTS and deletion.
- `git diff --check` passed.
- Credential and project-policy scans passed using the repository-pinned gitleaks
  version installed in a temporary tools directory during PR preparation.

Sandboxed Go cache writes and local sockets were denied; the successful checks
ran with those permissions. The first full Python run had five failures because
`pypdf` was absent; installing the declared dependency in a temporary test
directory enabled the successful full rerun. No deployment or owner database
migration was performed.

### Storage scope

SQLite is the current incremental persistence mechanism under ADR 0003. This
slice does not establish it as the long-term store for the cognitive graph.
Evaluate graph traversals, provenance queries, temporal history, vector retrieval
and operational requirements before choosing the target storage architecture.

The follow-up [storage research](./cognitive-storage-research.md) and
[ADR 0009 proposal](./decisions/0009-cognitive-storage-evaluation.md) now map the
cognitive graph's target queries and compare database candidates. They recommend
an isolated PostgreSQL+pgvector versus Neo4j spike with SQLite as baseline, not
an immediate migration. Step 09.2 provider/classification work remains open.

## Owner correction: uncertainty belongs to the user

The owner rejected `general` and confirmed that only the user answers uncertain
classification. The form/content vocabularies no longer contain that label.
Unresolved axes serialize as `null` with `user_required` provenance. The domain
returns a `classification` result with `status: pending`, `needs_user_input`,
`missing_axes` and questions for the missing axes. Fully specified metadata yields
`status: classified` and no questions. These status fields are derived from the
effective stored metadata, not persisted as independently mutable labels.

A provider can mark an axis uncertain or the provider unavailable; an existing
user-required flag also prevents automatic resolution. Explicit user input wins.
Invalid proposals do not fall through to source defaults. Unchanged rereads keep
pending nulls and earlier valid axes; an explicit answer patches that axis and
its provenance without replacing embeddings. This does not add a model provider or chat conversation resumption. The API
returns questions for the caller to present; the CLI continuation implemented
below is the first interface integration. Other interfaces remain pending.

Compatibility: this intentionally revises the new draft metadata contract on
PR #53, as directed by the owner; legacy public `kind` values are unchanged. No
new route, SQL migration, environment variable or compose change is introduced.
Older experimental `general` metadata must be replaced by explicit user labels
or null axes through reingestion; no owner database is rewritten here.

Verification for the owner correction: Knowledge service **148 passed, 8 skipped**
(live Ollama); Knowledge domain `go test -race ./...` and `go vet ./...` passed;
orchestrator `go test ./... -count=1` passed. Coverage includes rejection of
`general`, null serialization, invalid/uncertain/unavailable provider output,
user-only resolution, per-document questions, preserved resolved axes and
unchanged embeddings after a user answer.

## Classification continuation: API and CLI

The additive `knowledge.classify` action completes the first user-facing answer
path. Query with a source ID to get current questions and a revision; submit
explicit `answers` and that revision to patch only selected axes. Existing
metadata supplies the other axes. CLI `knowledge classify <source-id>` asks the
questions; `--form` and `--content-kind` submit only explicit flags. Ingest/list
output points pending documents to the command. Empty answers remain pending;
EOF cancels submission and conflicts are not retried automatically.

The service's internal metadata get/patch operations run through the existing
Knowledge client. A compare-and-patch transaction rejects stale content or
metadata revisions, including competing answers from two writers. Successful
answers preserve chunks, embedding bytes and the embedding cache revision. No
source file is needed and no provider/extractor is invoked.

Compatibility: one additive public event/action and CLI command, two internal
service operations, no removed commands or changed envelope, SQLite schema,
environment, ports or compose. The existing handler and generic client port are
reused; policy stays in the Knowledge domain. This resumes classification only;
it does not claim summary/workflow recovery or conversational resumption in every
interface. Desktop integration has its own acceptance criteria below.

Validation: Knowledge service **154 passed, 8 skipped** (live Ollama). Knowledge
`go test -race ./...`, orchestrator `go test ./... -count=1`, and CLI
`go test ./... -count=1` passed. Tests cover interactive input, explicit flags,
blank answers, EOF, invalid values, no automatic conflict retry, route-to-domain
composition, content/metadata/deletion conflicts, and concurrent writers. No
live provider or owner database was used.

## Desktop implications and acceptance criteria

The owner reported empty Document/Conversation tab proposals and a context menu
left with only Delete. The cognitive changes supply domain meaning and an answer
contract, not a desktop detail UI.

- The current Knowledge service already accepts `knowledge.list` with
  `include_text: true`, and the domain forwards it. Desktop's main-process action
  currently forwards only limit/offset and projects summary fields, discarding
  text, structured metadata and classification questions. Its shared type and
  renderer likewise describe rows. Existing text reconstruction joins stored
  chunks and may repeat overlap/headings; it is not the original source body.
- A useful Document view needs a bounded, per-document detail contract with
  source identity, a declared content representation, provenance, metadata,
  classification questions and the revision needed to answer them. Fetch detail
  on demand; do not load every document's full text in a listing or present a
  chunk reconstruction as an untouched original. This contract is still pending.
- A document discussion references the same knowledge ID/version and the existing
  conversation model. A Conversation tab needs a navigation/use case beyond the
  Messages pane; these changes do not justify duplicating that pane or its state.
- Open and classification actions should appear when the desktop implements those
  capabilities. A Delete-only context menu adds no required behavior beyond the
  existing row button. Tabs and menus follow useful operations, not faculty names.

No desktop runtime or tab behavior was changed in this slice. Extend its
allowlisted input, typed main-process projections, renderer and interface tests
together when implementing these criteria. The new classify action alone does
not make those capabilities available to desktop automatically.

## Reading-time classifier

Branch `feat/knowledge-classifier`. The reading host gains `document.classify.v1`
(ADR 0008): `feeds.classify` hands a 16,000-character sample of each document to
the single enabled classifier plugin and checks its output, one bounded proposal
per document (form, content kind, uncertainty flags, topics, entities, date
anchor). The host checks shape; Knowledge decides which labels exist.

`knowledge.read` (reading task version 2) runs `read → ingest → classify`.
Classification happens after storage and only for documents whose content was
stored just now and whose form or content kind nobody decided. That is the
cache: an unchanged reread never asks again, and a new classifier version does
not reclassify stored documents by itself. The domain resolves each proposal
and applies a revision-checked metadata patch, so an answer the user gives
meanwhile wins. Undecided axes get `classifier` provenance; an uncertain axis or
unknown label becomes a user question (`user_clarification_required` or
`classifier_value_invalid`). Topics, entities and the date anchor are filled
only when the request gave none. A document-level `classifier: {id, version}`
records who saw the current content. Content and embeddings are untouched.

Privacy: every form may be sent, notes and conversations included. The owner
decided (2026-10-20, ADR 0008) that enabling a classifier is the permission to
send it what was read, with no per-form setting.

Failures never fail the reading: no classifier, timeout, broken plugin, host
down, wrong proposal count or a conflicting answer all leave the documents
stored with their questions pending, and the reply's `classification.reason`
says why (`classifier_unavailable`, `classifier_timeout`, `classifier_failed`,
`classifier_host_unavailable`, `classifier_invalid_result`,
`knowledge_classify_failed`). The task deadline is now 4 minutes: a 2-minute
reading, a 1-minute classification and the Knowledge calls (raised to 6 minutes
and a 3-minute classification for the agent classifier, below). No interface sends
`knowledge.read` today (desktop and the CLI use `knowledge.ingest`); an API
caller with a shorter timeout stops waiting, but the task still completes.

Compatibility: one additive service event (`feeds.classify`), one additive
capability, one additive metadata field (`classifier`) in the existing JSON
column with no SQL migration, and an extra `classify` step and `classification`
field in the `knowledge.read` reply. No envelope, route, command, environment
variable or compose change. Older Knowledge builds refuse the `classifier` field
in a patch; roll out the Knowledge service before the orchestrator, as for
schema 2.

### Verification

- Feeds: `go vet ./...` and `go test -race ./...` passed.
- Knowledge domain: `go vet ./...` and `go test -race ./...` passed.
- Orchestrator: `go vet ./...` and `go test ./... -count=1` passed.
- Knowledge service: `python -m pytest -q tests`: **158 passed, 8 skipped**
  (live Ollama). `pypdf` was installed into a temporary directory for the run.

No classifier plugin, live provider or owner database was exercised.

## Agent classifier

Branch `feat/agent-classifier`, stacked on `feat/knowledge-classifier`. The owner
chose an agent CLI over deterministic rules and decided (ADR 0008, 2026-10-20):
Claude Code first, then Codex, then Ollama; every form may be sent, notes and
conversations included; Feeds runs on the owner's machine, where the CLIs are
logged in, and the orchestrator does not call a CLI itself.

- The privacy gate that skipped known notes and conversations is removed.
- A classifier output may name its `provider`; the host returns
  `<manifest version>+<provider>`, such as `0.1.0+claude.haiku-5-5`, so the
  cache and provenance keep models apart. Classification may take 3 minutes,
  and the reading task deadline is 6 minutes. Classification is not a reading,
  so it sits outside the step 08 cap of 2 minutes for on-demand reading, and it
  holds one of the 2 reading slots while it runs.
- `plugins.example/agent-kinds` (disabled) tries the providers in order and moves
  on only when one fails; an unsure answer is a result. Labels outside the
  vocabulary and missing values become user questions; the date anchor is kept
  for news only.

Isolation was checked with canary files before writing the plugin:

- Codex in its read-only sandbox **read** a file it was asked to read. With
  `shell_tool` and `unified_exec` off (and apps, browser and computer use) it
  could not, and said so.
- Claude Code with `--tools ""` could not read the file either.
- The owner's ai-memory hooks live in the user settings; Claude Code runs with
  `--setting-sources ""` and `disableAllHooks`, and a canary sent through it did
  not appear in the hook spool or session files.

A real run on a made-up two-document request answered correctly with Claude
Code (Haiku, 6 s) and Codex (9 s). Codex anchored the news on the weekday
mentioned in the text rather than the dateline, a quality point for the live
check.

### Verification

- Feeds: `go vet ./...` and `go test -race ./...` passed, including the plugin
  run against fake `claude` and `codex` programs and a fake Ollama: isolation
  flags, fallback on each failure, no fallback on an unsure answer, provider
  naming and the error when nothing answers.
- Knowledge domain: `go test ./...` passed. Orchestrator: `go test ./... -count=1`
  passed.

Still pending: running Feeds on the host for the stack (bind address and an
overridable `FEEDS_SERVICE_ENDPOINT` for the orchestrator), the live check on
the owner's documents, an Ollama chat model, and explicit reclassification.


## Explicit reclassification

Implemented 2026-10-21 on `feat/knowledge-reclassification` in
`.worktrees/knowledge-reclassification`. `knowledge.reclassify` accepts only
`source_id`; CLI `dokja-cli --timeout 7m knowledge reclassify <source-id>` calls
it. Each explicit request runs the configured classifier again, allowing an
operator to apply a changed provider/version. Ordinary rereads retain their
existing behavior and never trigger this action automatically.

A typed snapshot/classify/keep task reuses `document.classify.v1`. Knowledge
requests at most 16,000 Unicode characters reconstructed from stored chunks,
with no claimed original format, and a metadata/content revision from the same
read transaction. No source upload or extraction is needed. The sample stays
outside the task workspace, response and trace. The provider ID/version is kept
through the existing classifier provenance contract.

The domain makes classifier-origin axes and undecided missing axes eligible.
User answers, source defaults, axes already awaiting user clarification and
existing defining metadata remain unchanged. If no axis is eligible, no provider
is called. Uncertain proposals become user questions. Failed inference or invalid
output leaves storage untouched. The metadata patch uses the revision captured
**before** inference: a content edit, deletion or competing answer rejects the
stale proposal without retrying. Chunks, embeddings and the embedding cache
revision are unchanged.

Compatibility: one additive public action and CLI command, one internal read
operation (`knowledge.classification.sample`). The existing router prefix,
client, classifier capability, metadata patch and schema are reused. No envelope,
SQL migration, configuration, compose or deployment change. Roll out Knowledge
service before the orchestrator; older services refuse the new internal read
operation. Rollback restores the previous binaries; metadata remains schema-2
compatible. CLI callers should allow seven minutes for the six-minute task;
shorter client timeouts can stop waiting before the server finishes.

Validation: Knowledge domain `go test -race ./...` and `go vet ./...` passed;
orchestrator `go test -race ./...` passed; CLI `go test ./...` passed. Knowledge
service: **159 passed, 8 skipped**, using a disposable Python environment; skipped
tests require live Ollama. Sandbox socket restrictions required unsandboxed CLI
and Python reruns. Regression tests cover the public route, user protection,
provider failures, malformed responses, uncertainty, stale revisions, bounded
sampling, retained vectors and CLI no-retry behavior. No live inference, owner
file or owner database was used. Step 09.2 remains in progress for its other
listed interface and operational items. Staged credential and project-policy
scans passed with repository-pinned gitleaks v8.28.0.
