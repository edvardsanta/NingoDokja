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
| `knowledge.ingest` | one of `body` (with `title`), `content_b64` (with `filename`) or `source`; plus `title?`, `kind?`, `source_id?`, `source_ref?`, `tags?` | `created`, `changed`, `chunks`, `embedded`, `degraded` (several entries: `documents[]`, `count`) |
| `knowledge.search` | `query`, `k?` (1-20), `min_score?` | `hits[]`, `relevant_count`, `threshold`, `degraded` |
| `knowledge.list` | `limit?`, `offset?` | `documents[]`, `total` |
| `knowledge.delete` | `source_id` | `deleted` |
| `knowledge.status` | none | counts, `embed_model`, `embedder_reachable`, `formats`, `fetchers` |
| `knowledge.reindex` | `limit?` | `embedded`, `remaining` |

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
