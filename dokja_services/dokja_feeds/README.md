# dokja_feeds

Follows the sources the owner chooses and keeps their latest items in memory, so the digest can
show them. The repository ships **the mechanism only**: no source, site or provider is built in.
What to follow is a plugin, kept outside the repository.

- A plugin is a directory with a `plugin.json` manifest and a program. The service reads the
  manifest without running anything, so a plugin that is disabled, broken or unknown is still
  listed with the reason.
- Each enabled plugin runs on its own schedule. A request only reads the last good result from
  memory, so a slow or hung plugin never delays an answer.
- A failed run keeps the previous items and says why it failed; one failing plugin does not hide
  the others.
- Nothing starts by itself: a plugin runs only if its manifest says `"enabled": true`.

## Events (ZeroMQ, REQ clients, port 5563)

The envelope is the one the memory and knowledge services use: `{"type", "payload"}` in,
`{"status", "result" | "message"}` out. `feeds.status` and `feeds.items` read memory and take no
payload. Requests are answered concurrently, so a reading that waits on its plugin never delays
them; at most 16 requests are in flight, past that a request is refused as busy.

| event | result |
| --- | --- |
| `feeds.status` | `configured`, `directory_error`, `plugins[]`, `extractors[]`, and the counts `enabled`, `ok`, `failed`, `pending`, `disabled`, `invalid`, `items` |
| `feeds.items` | `items[]` (at most 1000), `total`, `updated` |
| `feeds.extract` | payload `filename`, `content_b64` (at most 20 MB); result `documents[]` (`title`, `text`, `source_ref`, `key`), `count`, `format`, `plugin`, `version` |
| `feeds.classify` | payload `documents[]` (`title`, `text`, `key`) and an optional `format`; result `classifications[]` (one per document), `count`, `plugin`, `version` |

A `plugins[]` row has `id`, `name`, `state` (`ok`, `failed`, `pending`, `disabled` or `invalid`),
`running`, `enabled`, `interval_seconds`, `items`, `skipped`, `last_run`, `last_ok` and
`last_error`. An `items[]` entry has `id`, `plugin`, `title`, `summary`, `url`, `published` (RFC 3339
UTC, or empty), `source`, optional `body` (full plain text) and `prompts` (a list, possibly empty). `last_error` is a short fixed phrase such as `exit status 2` or
`timed out after 30s`; what a plugin writes to stderr never appears in a reply. A manifest this
service cannot run shows as `invalid`, and one that asks for a version, runtime or capability it
does not support has a `last_error` starting with `incompatible:`.

`extractors[]` lists versioned plugins that do not collect, such as those that only provide
`document.extract.v1` or `document.classify.v1` (see below):
`id`, `name`, `enabled`, `plugin_api`, `version`, `provides`, `formats`, and the last reading's
`last_run`, `last_ok` and `last_error`. They are not feed sources, never run on a schedule and do
not change the counts.

## Writing a plugin

One directory per plugin under `FEEDS_PLUGINS_DIR`, named like its `id`:

```
plugins/
  my-source/
    plugin.json
    run.sh
```

`plugin.json` (unknown fields are refused, so a typo is reported instead of ignored):

| field | meaning | default |
| --- | --- | --- |
| `id` | lowercase letters, digits, `-` or `_`, at most 40; must equal the directory name | required |
| `category` | `"feed"` | required |
| `command` | the program and its arguments, run without a shell; the program is either on `PATH` or a file inside the plugin directory | required |
| `enabled` | `true` to run it | `false` |
| `name` | what to call it when an item names no source | the `id` |
| `interval` | time between runs, `1m` to `24h` | `15m` |
| `timeout` | how long one run may take, `1s` to `5m` | `30s` |
| `env` | names of the environment variables the plugin receives, taken from the service's own environment | none |
| `max_items` | most items kept from one run, 1 to 200 | `50` |

The program prints one JSON object on **stdout** and exits with status 0. Anything else it wants
to say goes to **stderr**, which only reaches the service log:

```json
{"items": [
  {"id": "1", "title": "An example title", "summary": "Some text", "url": "https://example.com/1",
   "published": "2026-10-01T08:00:00Z", "source": "Example"}
]}
```

A collector may also provide `body`: the complete plain text it already obtained from its
source. This is separate from the bounded `summary`. The Library saves this text with the
reading's stable identity and source address, without fetching the page again. This supports
pages whose visible content comes from a client application or an API. Supply a stable `id`
for each edition, such as `daily:2026-10-27`, even when editions share a homepage. Omit `body`
when only an excerpt is available; saving then fetches the original page as before.

Only `title` is required. Every field is a string, except `prompts`: an optional list of up to 3
questions the plugin suggests for discussing the item, written from its own text (for a liturgy,
"O que Lc 13,10-17 ensina?"). The desktop offers them when a conversation about the item opens,
instead of its generic ones. Without an `id` the service derives a stable
one from the address and the title. A `published` that is not RFC 3339, or lies more than an hour
in the future, is treated as unknown.

### What the service checks

Plugin output comes from the outside, so it is bounded and cleaned before it is kept:

- at most 1 MiB of output per run, and `max_items` items; an item that cannot be used is skipped
  and counted in `skipped`, the rest stays;
- text is made one line of valid UTF-8 without control characters or the characters that reorder
  what is displayed, and cut to 300 characters (title), 1000 (summary), 80 (source) and 120 (each
  prompt; only the first 3 non-empty prompts are kept);
- full `body` text preserves paragraphs and is limited to 100,000 UTF-8 bytes; oversized
  bodies are omitted rather than truncated;
- `url` is kept only if it is an absolute `http` or `https` address with a host and no credentials.

### What the service does to a plugin

A plugin is code you wrote, but it may still be slow, wrong or hung, so each run is a child
process that:

- starts in its own directory with no input (an on-demand reading gets its request) and only
  `PATH` plus the variables named in `env`
  (a secret reaches a plugin only by being listed there, by name; keep tokens in the service
  environment, never in a manifest);
- is stopped when `timeout` ends or the output passes the limit, together with everything it
  started;
- is never run while another run of the same plugin is in progress.

### Where plugins run

The plugin runs inside the service's container, so **its program must exist in that image**. The
image is `debian:bookworm-slim` plus the libraries the service needs: it has a POSIX shell (`sh`),
the usual command-line tools, and `python3` with `pypdf` for the reading example. For anything else, extend
the image, or ship a static binary in the plugin directory. A manifest whose program is not
installed shows as `invalid` with the program's name.

Mount the plugins directory read-only and keep it out of git. Changes to it are read when the
service starts.

`plugins.example/` holds three plugins. `local-file` prints the items of a local JSON file and
`knowledge-extractors` reads documents on demand; neither uses the network. `agent-kinds` classifies
documents with an agent CLI, which sends them to that CLI's provider (see below). All three ship
disabled.

### Versioned manifests (`plugin_api: 1`)

This service is growing into the reading host of
[ADR 0006](../../dokja_docs/decisions/0006-reading-macro-component.md). A manifest without
`plugin_api` is the shape above and keeps working unchanged. A manifest with `"plugin_api": 1`
follows the lifecycle of [ADR 0002](../../dokja_docs/decisions/0002-plugin-lifecycle.md); any
other value is reported as incompatible, never read as the old shape. Unknown fields are refused
here too.

| field | meaning |
| --- | --- |
| `plugin_api` | `1` |
| `id`, `name`, `enabled`, `command`, `env` | as above; `enabled` still defaults to `false` |
| `version` | the plugin's own version, for diagnostics only; it never decides which plugin is used |
| `runtime` | `"subprocess"` |
| `provides` | `source.collect.v1` (a scheduled feed source), `document.extract.v1` and/or `document.classify.v1` |
| `interval`, `max_items` | only with `source.collect.v1`, as above |
| `formats` | only with `document.extract.v1`: the extensions it reads, such as `[".pdf"]` |
| `limits.timeout_ms`, `limits.output_bytes` | required: one run's time (1 s to 5 min) and output (at most 1 MiB) |

A plugin that only extracts may print up to 16 MiB; one that also collects keeps 1 MiB.

When more than one enabled plugin extracts the same format, none is chosen until one is bound
explicitly; the order of directories never decides.

### Reading on demand (`feeds.extract`)

`feeds.extract` reads one file now and keeps nothing: the orchestrator's `knowledge.read` hands
the result to Knowledge. The enabled plugin that provides `document.extract.v1` for the file's
extension (case-insensitive) runs once and reads one JSON object on **stdin**:

```json
{"operation": "document.extract.v1", "filename": "notes.pdf", "format": ".pdf", "content_b64": "..."}
```

It prints one JSON object on stdout, Knowledge's `Extracted` contract, or the reason the file
cannot be read:

```json
{"documents": [{"title": "Chapter 1", "text": "...", "source_ref": "chapter-1", "key": "c1"}]}
{"error": "the PDF is password protected"}
```

Unknown fields are refused. Text keeps its line breaks but must be valid UTF-8; titles, refs and
keys are cleaned like item text; documents without text are dropped. A run ends at the manifest's
`timeout_ms` or the request's 2-minute limit, whichever comes first, and at most 2 readings run at
once. A failure answers an error whose message starts with its reason:

| reason | meaning |
| --- | --- |
| `invalid` | no filename, no or bad `content_b64`, an empty or too large file |
| `unsupported` | the file name has no extension a plugin could serve |
| `unavailable` | no enabled, compatible plugin serves the format; plugins not loaded because of their version are named |
| `ambiguous` | several enabled plugins serve it and none is bound |
| `timeout` | the plugin did not finish in time, or no reading slot freed up |
| `failed` | the plugin crashed, printed too much or printed something that is not the contract |
| `unreadable` | the plugin answered `error`, or every document was empty |

`plugins.example/knowledge-extractors` serves Knowledge's own extractors this way (PDF, EPUB, DOCX,
HTML, text and feed files). It needs `python3`, which the image has, and
`KNOWLEDGE_EXTRACTORS_DIR`, set in the compose files. Replacing it with another reader for a
format means disabling one manifest and enabling another; no code changes.

### Classifying what was read (`feeds.classify`)

`feeds.classify` proposes the form and content kind of documents already read, with defining
metadata (ADR 0008). It keeps nothing and decides nothing: Knowledge resolves each proposal against
the user's answers, and an axis the plugin is unsure of stays the user's to answer. The single
enabled plugin that provides `document.classify.v1` runs once (it takes no `formats`; two enabled
classifiers are `ambiguous`). It reads one JSON object on **stdin**, with at most the first 16,000
characters of each document's text:

```json
{"operation": "document.classify.v1", "format": ".pdf", "documents": [{"title": "Chapter 1", "text": "...", "key": "c1"}]}
```

It prints exactly one proposal per document, in order, or why it cannot classify them:

```json
{"classifications": [{"form": "article", "content_kind": "news", "topics": ["elections"],
  "entities": [{"name": "City Council", "type": "organization"}], "date_anchor": "2026-10-19"}]}
{"classifications": [{"form": "book", "uncertain_content_kind": true}]}
{"error": "the classifier is not configured"}
```

Every field is optional. `form` and `content_kind` must look like labels (lowercase letters and
`_`); which labels exist is Knowledge's rule, not the host's. At most 20 topics and 50 entities,
and `date_anchor` is `YYYY-MM-DD`. Output outside this contract fails the whole run with `failed`;
`error` answers `unreadable`. A run ends at the manifest's `timeout_ms` or the request's 3-minute
limit, and shares the 2 reading slots. The reasons are those of `feeds.extract`.

A plugin that chains several models may add `"provider"`, a short lowercase name such as
`claude.haiku-5-5` (letters, digits, `.`, `_`, `-`; at most 24). The result's `version` becomes
`<manifest version>+<provider>`, so Knowledge keeps classifications by different models apart.

A classifier that sends text to a remote model sends the owner's documents with it; enable one only
for documents you are willing to send.

#### The `agent-kinds` example

`plugins.example/agent-kinds` asks Claude Code, then Codex, then Ollama, moving on only when one
fails (missing, not logged in, an error, too slow, or the wrong number of answers). An answer that
is unsure of an axis is kept, not retried elsewhere. Its `provider` names the model that answered,
such as `claude.haiku-5-5` or `codex.gpt-6.1-sol`. It needs `python3` and the CLIs logged in for the
user that runs Feeds, so it is meant for Feeds running on the owner's machine, not in the image.

| variable | meaning |
| --- | --- |
| `HOME` | where the CLIs keep their login |
| `AGENT_KINDS_PROVIDERS` | order to try, default `claude,codex,ollama` |
| `AGENT_KINDS_TIMEOUT` | seconds per provider, default 50; no provider starts after 160 s |
| `AGENT_KINDS_CLAUDE_MODEL` | Claude Code model, default `haiku` |
| `AGENT_KINDS_CODEX_MODEL` | Codex model; empty uses Codex's default |
| `AGENT_KINDS_OLLAMA_URL`, `AGENT_KINDS_OLLAMA_MODEL` | an Ollama `/api/chat` endpoint and a chat model; Ollama is skipped unless both are set |

The model sees up to 48,000 characters per call, shared among the documents (at least 500 each);
past 96 documents the rest are left to the user. Both CLIs run without tools, from an empty
directory. Claude Code runs with `--tools ""`, without user settings or hooks
(`--setting-sources ""`, `disableAllHooks`) or MCP servers, and keeps no session. Codex runs with
`--sandbox read-only --ignore-user-config --ephemeral` and its shell, apps, browser and computer use
turned off. Its read-only sandbox alone still reads files. Canary files confirmed that, with these
flags, neither CLI reads a file it is asked to read and the documents do not reach the user's
hooks.

## Variables

`FEEDS_SERVICE_ENDPOINT` (default `tcp://*:5563`), `FEEDS_PLUGINS_DIR` (empty means no plugins).
Variables a plugin lists in `env` come from the same environment, such as
`KNOWLEDGE_EXTRACTORS_DIR` for the `knowledge-extractors` example.

## Tests

The service links libzmq through cgo (`libzmq3-dev` and `pkg-config`), like the other Go services:

```sh
cd dokja_services/dokja_feeds && go test ./...
```

## Content and scraper plugins

Feeds also hosts Book/Meme compatibility mechanisms through `feeds.invoke`, and
collectors can be refreshed through `feeds.refresh`. See the
[content plugin contract and migration](../../dokja_docs/feeds-content-plugins.md)
for scraper extension, media observations, bundled cache, optional dependencies,
provider status and rollback.
