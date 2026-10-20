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

The substep remains in progress: a provider-backed reading-time classifier,
content/model-version cache and explicit reclassification are not connected.
The existing resolver can validate classifier proposals, but the integration in
this slice uses explicit user assertions and unambiguous source defaults. Missing
axes stay pending for the user; no general fallback is used.
The stable reread tests establish preservation behavior, not model quality.

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
its provenance without replacing embeddings. This does not add a model provider
or an interactive conversation-resumption mechanism: questions are returned in
the existing API result for the interface/caller to present. Provider and
interface continuation work remains in 09.2.

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
