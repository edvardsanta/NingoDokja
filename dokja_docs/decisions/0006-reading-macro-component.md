# ADR 0006: reading is one macro-component; Book is not a service

> [ADR 0010](./0010-four-concepts.md) proposes treating reading as how Knowledge comes
> in (and an episode Memory keeps), not as a concept of its own.

Date: 2026-10-18. Status: accepted as the target direction. Migration: stage 2 done
(step `07`, [record](../step-07-reading.md)); stages 3 and 4 pending.
Supersedes the Book/source half of [ADR 0005](./0005-knowledge-experience-content.md).
Generalized by [ADR 0007](./0007-faculties-not-features.md): features are workflows over faculties.
Related: [decision index](./README.md), [plugin lifecycle](./0002-plugin-lifecycle.md),
[capability map](../capability-map.md).

## Owner clarification (2026-10-26)

Feeds is the external integration side of Knowledge, covering source and
perception adapters rather than a separate conceptual peer of internal
knowledge. [Cognitive realignment](../cognitive-realignment.md) records the
behavioral gate. Existing migration history remains scoped evidence; this
clarification introduces no direct cross-service calls or provider relocation.

## Context

The owner decided the direction: reading a book is a *reading*, the same
capability Feeds already provides for its sources. Knowledge keeps what was read,
and Experience/Memory records that the reading was a book and how it was used.
A dedicated Book service does not fit that model.

The current code shows the cost of the separate boundary:

- [Book extractors](../../dokja_services/dokja_book/extractors.py) duplicate
  formats that the [Knowledge extractor registry](../../dokja_services/dokja_knowledge/extractors/__init__.py)
  already reads (PDF, EPUB, HTML, text). The same file takes a different path
  depending on whether it arrives through `book.*` or `knowledge.ingest`.
- `classify` returns `resource_strategy`, `context_strategy` and `agent_route`
  from [fixed tables](../../dokja_services/dokja_book/book_summary_service.py). No
  orchestrator, domain or interface code reads them, and no `book-*-summarizer`
  agent exists. A zero-shot model is loaded even to guess the file format.
- Book summaries are extractive and never reach Knowledge, so nothing read
  through Book is retrievable later.
- [Feeds](../../dokja_services/dokja_feeds/feeds/service.go) reads sources on a
  schedule through subprocess plugins, Knowledge reads files and URLs through
  in-process Python plugins, and Book reads files through its own code: three
  reading paths with two plugin protocols.
- The Book service is wired in the dev and lite stacks only; the production
  compose file has no Book service.

## Decision

Model three separate responsibilities:

| Responsibility | Owner | Holds |
| --- | --- | --- |
| Reading | One reading host, grown from the Feeds plugin host | Source and format adapters (file formats, URLs, feeds), run on a schedule or on demand; output is content plus provenance |
| Knowledge | Knowledge domain and service | Stored content with provenance (source, format, structure such as chapters/pages), indexing and retrieval |
| Experience | Memory domain and service | The reading episode: what was read, for which goal, how it went, which procedure was used and its outcome |

"Book" is therefore not a capability. It is a value in provenance (what kind of
source was read) and in experience (the context in which it was read). How to
treat a book (for example, reading a long technical text chapter by chapter) is
a general method in Knowledge plus the applicable experience, as ADR 0005 frames
it, not code in a Book service.

"Summarize a book" becomes a product workflow: read the source → ingest into
Knowledge → apply a general summarization method → generate → deliver → record
the experience. The orchestrator sequences it; the domains own the decisions.

## Consequences

1. **Extraction sits between Reading and Knowledge.** The boundary is the
   document contract Knowledge already defines,
   [`Extracted`](../../dokja_services/dokja_knowledge/extractors/__init__.py)
   (`title`, `text`, `source_ref`, `key`):
   - Reading owns getting the bytes (fetching URLs, polling feeds, receiving
     uploads) and turning a format into text. Knowledge's `fetch.py`,
     `extractors/` (including `feedfmt`, which overlaps Feeds) and `plugins.py`
     move to the reading host as plugins that emit that contract.
   - Knowledge owns the contract itself and everything after it: chunking,
     embedding, storing, and retrieval. It keeps accepting already-extracted
     text, as it does today through `body`.

   A new format therefore changes only Reading; a change to what a document is
   goes through Knowledge.
2. **The reading host needs an on-demand mode.** Feeds today is schedule-only:
   its manifest has `command`, `interval`, `timeout` and `max_items`, and
   `feeds.items` returns cached results. Reading a file someone sends needs a
   request that carries input and returns a result. That is a new host contract
   and follows the versioned lifecycle of ADR 0002, with an adapter for current
   manifests.
3. **Feeds output can reach Knowledge.** Digest keeps item selection; storing
   selected feed items in Knowledge becomes an explicit, optional workflow step.
4. **Other perception is also reading, migrated later.** Meme scrapers, OCR
   and attachments and voice transcription fit the same responsibility. ADR 0007
   plans their migration; they keep their current services until then.

## Migration and compatibility

Removing Book touches boundaries that the project rules require reviewing:

- routes `book.resource.classify` and `book.summary.requested` in the
  [workflow engine](../../dokja_orch/internal/core/workflow_engine.go);
- the `book` command in the [CLI](../../dokja_interfaces/cli/cli/app.go);
- the Book HTTP client, handler and domain in the orchestrator, the `book` entry in
  [`KnownServices`](../../dokja_orch/internal/core/controls.go) and its health probe
  in `cmd/va/main.go`;
- `dokja-book` in the dev and lite compose files and `dokja_orch/service.yaml`;
- [workflows.md](../workflows.md) and the [production runbook](../production-runbook.md),
  whose status probe still lists `book` and reports a production stack as
  `degraded` until Book is switched off.

Stage it:

1. Record the current Book request/response contract and CLI behavior as a
   baseline (step `03`).
2. Prove on-demand reading with one extractor running as a reading-host plugin
   whose output goes through `knowledge.ingest` (step `07`). Done: `feeds.extract`
   and `knowledge.read`, with Knowledge's extractors served by the
   `knowledge-extractors` adapter plugin.
3. Keep `book.*` as a compatibility facade over reading → Knowledge →
   generation (step `09`).
4. Deprecate and remove the facade, the Book domain and the Book service only
   with the owner's approval.

No code is deleted by this record. Uncommitted local Book changes (a stub
classifier and a compose profile) are made moot by this direction and are not
part of it.

## Open questions

- Which Book outputs survive as product behavior: the summary shape, the
  book-type label, or neither?
