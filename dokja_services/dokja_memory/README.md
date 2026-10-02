# Dokja Memory

Experience memory: it stores what the bot did in a context and how it turned out, and finds
the closest earlier experiences by meaning.

An experience is `(ref, action, context, outcome)`. For example, the bot suggested a label
(`action`) for a piece of text (`context`) and the operator later accepted or replaced it
(`outcome`). What a prediction is and how it is scored are business rules and live in
[`dokja_domain/dokja_memory`](../../dokja_domain/dokja_memory); this service only stores
and compares.

- Storage: its own SQLite file (`MEMORY_DB_FILE`), created readable by its owner only,
  because a context is the user's own text.
- Similarity: cosine over embeddings (bge-m3 through Ollama), by brute force. That is fine
  into the tens of thousands of experiences; add an index when it is not.
- Without Ollama (or with `DOKJA_EMBED=off`) the service stays up: it still records, resolves
  and counts outcomes, and reports `degraded: true` with a `reason`. `memory.reindex` embeds
  whatever was missed.
- Experiences are immutable: recording a `ref` that exists changes nothing, and the first
  outcome wins. The prediction and baseline stored with an experience are never recomputed,
  so a score only reflects what was known when the decision was made.
- Calls are meant to be best effort: the embedder timeout is short (5 s by default), `reindex`
  works in bounded batches and nothing here ingests documents, so one slow call cannot hold
  every other action for long.
- A context is never logged: requests are logged by type and number of payload fields only.
- A context is returned in one case only: `memory.list` and `memory.recall` hand back its first 160
  characters, as `context_snippet`, when the request sets `include_context`. The cut is made here, so the
  rest never leaves the service. It is how an operator recognizes an experience whose `ref` is a hash. The
  orchestrator port has no authentication, the same as for `knowledge.search`: keep it off untrusted
  networks. `memory.get` and `memory.resolved` never return a context.
- The text of the earlier example a choice rested on (`matched_context`) follows the same rule: `memory.list` hands back its first
  160 characters as `matched_snippet`, only with `include_context`, and `memory.get` never returns it. `matched_score` is a number,
  not text, and comes back whenever one was recorded. An experience recorded without them reads as before; a database created by an
  earlier version upgrades in place when the service starts (two nullable columns).

## Events (ZeroMQ REQ/REP, port 5562)

Same envelope as the knowledge service: `{"type": ..., "payload": {...}}` in,
`{"status": "ok", "result": {...}}` or `{"status": "error", "message": ...}` out.

| event | payload | result |
| --- | --- | --- |
| `memory.record` | `ref`, `action`, `context`, `detail?` (what exactly was done, up to 200 characters), `predicted_p?` and `baseline_p?` (together, 0 to 1), `matched_score?` (0 to 1) and `matched_context?` (up to 200 characters): what the choice rested on, how close the earlier example was and the start of its text | `created`, `embedded`, `degraded`, `reason?` |
| `memory.resolve` | `ref`, `outcome` (a short lowercase word) | `found`, `resolved`, `outcome` |
| `memory.get` | `ref` | `found`, `action`, `detail`, `outcome`, `predicted_p`, `baseline_p`, `matched_score`, `created_at`, `resolved_at` (never the context, nor the text of the example it matched) |
| `memory.recall` | `context`, `action?`, `k?` (1-50), `include_context?` | `neighbors[]` (`ref`, `action`, `detail`, `outcome`, `similarity`, `created_at`, `context_snippet?`), `outcomes` (count per outcome), `degraded`, `reason?` |
| `memory.list` | `action?`, `state?` (`all`, `pending` or `resolved`), `limit?` (1-100, default 20), `offset?`, `include_context?` | `experiences[]` (`ref`, `action`, `detail`, `outcome`, `predicted_p`, `baseline_p`, `matched_score`, `created_at`, `resolved_at`, `context_snippet?`, `matched_snippet?`), newest first, plus `total`, `limit`, `offset` |
| `memory.resolved` | `action?`, `limit?` | `experiences[]` (`ref`, `action`, `outcome`, `predicted_p`, `baseline_p`, `resolved_at`), latest first taken, oldest first returned |
| `memory.forget` | `ref` | `deleted` |
| `memory.status` | none | counts, `embed_model`, `embeddings`, `embedder_reachable`, `needs_reindex`, `degraded` |
| `memory.reindex` | `limit?` (1-500, default 32) | `embedded`, `remaining` |

Only experiences that have an outcome are neighbours or counted: a pending one has taught
nothing yet (`memory.list` shows them all). `pending` and `resolved` in a listing follow the same
reading of expiry as `memory.status`, so an expired experience lists as resolved. An unresolved experience older than `MEMORY_EXPIRE_AFTER` reads as `expired`.
That is a view, not a write, so a verdict that finally arrives still replaces it.

## Variables

| variable | default |
| --- | --- |
| `MEMORY_SERVICE_ENDPOINT` | `tcp://*:5562` |
| `MEMORY_DB_FILE` | `dokja_memory.db` |
| `MEMORY_EXPIRE_AFTER` | `720h` (`0` never expires) |
| `DOKJA_EMBED` | `on` (`off` disables similarity) |
| `DOKJA_EMBED_ENDPOINT` | `http://127.0.0.1:11434` |
| `DOKJA_EMBED_MODEL` | `bge-m3` |
| `DOKJA_EMBED_TIMEOUT` | `5` (seconds) |

Changing `DOKJA_EMBED_MODEL` invalidates the old vectors (they are tagged by model); run
`memory.reindex` until `remaining` is 0.

## Tests

```sh
cd dokja_services/dokja_memory
go test ./...
```

The `server` package opens a real ZeroMQ socket, so it needs `libzmq` (`libzmq3-dev`). The
embedding client is tested against a fake server that speaks Ollama's API; a live check
against a real Ollama is not part of the suite.
