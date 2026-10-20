# dokja_knowledge

A research knowledge base: it stores the documents a user feeds it and finds them again by
meaning and by keyword, with a citable source.

- Storage: its own SQLite file (`KNOWLEDGE_DB_FILE`), separate from `dokja.db`.
- Hybrid retrieval: cosine similarity over embeddings (bge-m3 through Ollama) plus an FTS5
  keyword index, merged by reciprocal rank fusion.
- Without Ollama (or right after changing the model, before a reindex) the service stays up:
  it still ingests and searches by keyword only (`degraded: true` plus `reason`).
  `knowledge.reindex` embeds whatever was missed.
- Sending the same content again does nothing (content hash); editing replaces the chunks.

## Events (ZeroMQ REQ/REP, port 5561)

| event | payload | result |
| --- | --- | --- |
| `knowledge.ingest` | one of `body` (with `title`), `content_b64` (with `filename`) or `source`; plus `title?`, `kind?`, `source_id?`, `source_ref?`, `tags?`, `metadata?` | `created`, `changed`, `chunks`, `embedded`, `degraded` (several entries: `documents[]`, `count`) |
| `knowledge.metadata.get` (internal) | `source_id` | stored `metadata`, title and revision |
| `knowledge.metadata.patch` (internal) | `source_id`, `revision`, validated `patch` | effective metadata, new revision, `metadata_changed`; content `changed: false` |
| `knowledge.search` | `query`, `k?` (1-20), `min_score?`, `kind?` | `hits[]`, `relevant_count`, `threshold`, `degraded` |
| `knowledge.list` | `limit?`, `offset?`, `kind?`, `include_text?` | `documents[]`, `total` |
| `knowledge.delete` | `source_id` | `deleted` |
| `knowledge.status` | none | counts, `embed_model`, `embedder_reachable`, `formats`, `fetchers` |
| `knowledge.reindex` | `limit?` | `embedded`, `remaining` |

## Structured metadata

Knowledge accepts optional defining `metadata`, separate from legacy `kind` and
free-form `tags`. Domain requests (`knowledge.ingest` and `knowledge.read`) accept:

```json
{
  "metadata": {
    "form": "book",
    "content_kind": "research",
    "topics": ["physics"],
    "entities": [{"name": "Example Lab", "type": "organization"}],
    "date_anchor": "2026-10-19"
  }
}
```

The Go Knowledge domain resolves classification and adds `form_choice` and
`kind_choice` provenance. Callers cannot supply provenance. `knowledge.read` also
records `reading` (`format`, `reader`, `reader_version`). EPUB/MOBI default to book;
HTML/RSS/Atom default to article; PDF supplies no form default. Formats never
imply a content kind. Missing or uncertain axes are `null`; `general` is not a
classification value. Only explicit user input resolves an uncertain axis. No model classifier is connected in this slice.

The storage service validates structure and persists the resolved object.
Unresolved axes use `user_required` provenance. Domain responses add
`classification: {status, needs_user_input, missing_axes, questions}` from the
effective metadata, with status `pending` or `classified`. These are questions
for the user, not instructions for an autonomous retry or another default.
The service's ingest response now includes its effective stored `metadata`, so
rereads do not ask about axes that were already answered. The public `knowledge.classify` orchestrator action now retrieves those questions
by `source_id` and applies explicit user `answers` with the snapshot `revision`.
The CLI presents the questions; other interfaces still need presentation wiring. Search
hits and listed documents add `metadata`; legacy documents return `{}`. Existing
`kind` filtering, including `meme_hashtag`, retains its meaning. Metadata is limited
to 64 KiB of JSON independently of the existing tag limit. Dates are calendar-valid
`YYYY-MM-DD` anchors; entities require `name` and may have `type`.

For direct ingestion, supplying metadata replaces that document's metadata. An
unchanged document with omitted metadata preserves its previous metadata. Changed
content with omitted metadata clears it to avoid stale assertions. A metadata-only
update returns `changed: false, metadata_changed: true`; `changed` continues to mean
the legacy content digest changed. Chunks, embedding bytes and the embedding matrix
revision remain intact. Existing digest inputs (including tags) are unchanged.

The Reading domain additionally sends the storage-only `metadata_if_unchanged`
patch. On the same digest, storage overlays defaults, previous metadata, then this
patch. The domain includes only explicit user assertions and current reader
provenance in the patch. Rereading without labels therefore preserves prior user
choices, topics and date anchors. Empty topic/entity arrays clear those fields.
For changed content the patch is ignored and the full newly resolved metadata is
stored. Ordinary domain ingest refuses this reserved field. Multi-document reads
retain their existing per-document commit and partial-result behavior.

Schema version 2 adds `document_metadata` with a cascading document foreign key.
It does not backfill labels, replace chunks or re-embed existing documents. See
[upgrade and rollback](../../dokja_docs/production-runbook.md#knowledge-metadata-schema-upgrade)
before starting this build with an existing database.

## Formats and sources

The repository ships **generic mechanisms only**, with no site or brand built in:

| input | how it is read |
| --- | --- |
| `.txt`, `.md`, `.rst` | UTF-8 text; a leading `# title` becomes the title |
| `.html` | text with headings (`#`) and lists; menus, footers and scripts are dropped |
| `.docx` | paragraphs, headings by style, tables and the title from the document properties |
| `.epub` | chapters in reading order |
| `.pdf` | the text of each page; a scanned PDF (no text layer) is refused, OCR is not built in |
| `.xml`, `.rss`, `.atom` | a feed you hand over: **each entry becomes a document** |
| `source` = `http(s)://...` | fetches the page you named and picks the extractor from its type |

Reading a feed only reads what you handed over, once. **Following** a feed over time (a
stored address polled on a schedule) is deliberately not built in: that is your own
integration.

Sending the same file or address again does nothing, and every feed entry has a stable id.

The orchestrator also stores explicit, feature-owned facts here under distinct `kind`
values. Meme hashtag labels use `kind=meme_hashtag`; filtered search prevents ordinary
research documents from becoming hashtag candidates.

### Address fetching (`KNOWLEDGE_FETCH`, default `on`)

The orchestrator port has no authentication, so the fetcher refuses anything that could
reach your network: only `http`/`https` on their default ports, no credentials in the URL;
**every** address the host resolves to must be public (loopback, private, link-local,
shared-address, multicast and reserved ranges are refused); the connection uses the address
that was validated (no DNS rebinding); redirects are followed by hand (at most 3) and each
one is checked again; the response is capped at 20 MB and 30 s, is never decompressed and
must be a known type. `KNOWLEDGE_FETCH=off` disables it entirely.

### User plugins (`KNOWLEDGE_PLUGINS_DIR`)

For a specific site, internal system or format, write a plugin **outside the repository**:
a `*.py` file in a directory of your own (mounted read-only and kept out of git) that
defines `register(registry)`:

```python
def register(registry):
    registry.add_extractor(".ext", lambda data, name: [...])   # -> list[Extracted]
    registry.add_fetcher("myscheme", lambda ref: (bytes, "file.md"))
```

Then `knowledge add myscheme:anything`. An example that uses no network is in
`plugins.example/`. Plugins are ordinary Python running with the service's permissions, so
only load directories you control. A plugin that fails to load is logged and skipped.

## Relevance

`search` always returns the best candidates; **whoever injects context into a prompt must
use only the hits with `relevant: true`**. `relevant` is an absolute signal: the cosine
score is at or above the threshold, or the passage contains at least 2 of the query's terms
and at least 75% of them. A high rank alone does not prove relevance.

### Threshold (`KNOWLEDGE_MIN_SCORE`, default 0.45), provisional

Measured with bge-m3 on only 3 documents (Kant, a cake recipe, interest rates):

| kind of question | highest cosine |
| --- | --- |
| paraphrase sharing no words with the text | 0.46 - 0.61 |
| question using the text's own words | 0.63 - 0.73 |
| neighbouring topic ("a lasagna recipe", "what is a tax") | 0.43 - 0.45 |
| unrelated (football, nginx, a joke, the capital of France) | 0.27 - 0.41 |

The groups almost touch (the weakest paraphrase scored 0.456, the strongest neighbour
0.446). Treat 0.45 as a starting point, not a calibration: a larger base changes the
distribution. That is why a consumer should always cite the source of each passage it uses,
so a wrong retrieval is visible. The live test (`tests/test_live_embedder.py`, skipped
without Ollama) includes the neighbouring cases and prints the distribution. It uses a
Portuguese corpus on purpose, to check that retrieval works across languages.

## Variables

`KNOWLEDGE_SERVICE_ENDPOINT`, `KNOWLEDGE_DB_FILE`, `DOKJA_EMBED_ENDPOINT`
(default `http://127.0.0.1:11434`), `DOKJA_EMBED_MODEL` (default `bge-m3`),
`DOKJA_EMBED=off` (keyword search only), `DOKJA_EMBED_TIMEOUT`, `KNOWLEDGE_MIN_SCORE`,
`KNOWLEDGE_FETCH`, `KNOWLEDGE_PLUGINS_DIR`.

Changing `DOKJA_EMBED_MODEL` invalidates the old vectors (they are tagged by model); run
`knowledge.reindex`.

## Tests

```sh
pip install numpy pyzmq pypdf pytest
python -m pytest dokja_services/dokja_knowledge/tests
```

## Classification continuation contract

`knowledge.classify` is an additive orchestrator action, not a service-side
classifier. With `{source_id}` it returns metadata and domain-owned questions.
With `{source_id, revision, answers: {form?, content_kind?}}` the domain validates
nonempty user answers and constructs a patch containing only the selected axes
and user provenance. Answers cannot supply producer provenance or arbitrary
metadata. Missing axes remain untouched. Existing labels may be corrected only
by this explicit user input. No autonomous inference runs on this path.

The generic service client carries `knowledge.metadata.get` and
`knowledge.metadata.patch` through its existing request/reply port. The service
owns structural validation and transaction mechanics. The revision fingerprints
the source ID, legacy content identity and canonical metadata snapshot; it is a
concurrency token, not an authorization grant. A patch acquires a SQLite write
lock, compares the revision, merges and validates metadata, and commits atomically.
A stale revision returns `classification_conflict`; a missing document returns
not-found. Neither case changes existing state. Successful patches preserve
chunks, vector bytes and the embedding matrix revision. An identical patch with
a current revision is a no-op. A timed-out write may have committed; retrieve the
current questions before deciding whether another answer is needed.

This adds no schema version or durable workflow ledger. Restarting the CLI can
retrieve pending state from the existing metadata store. It resumes the
classification decision, not an interrupted summary or external side effect.
