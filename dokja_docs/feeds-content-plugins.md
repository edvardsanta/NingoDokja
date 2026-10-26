# Content acquisition through Feeds plugins

Book and Meme are compatibility workflows over the Feeds plugin host. The
orchestrator uses this backend by default (`DOKJA_CONTENT_BACKEND=feeds`), and
local runtime controls list Feeds rather than separate Book and Meme containers.
The public `book.*` and `meme.*` event names and envelope stay unchanged.

## Responsibilities

The domain decides which observations belong in a workflow. The orchestrator
injects a Feeds client; it never connects an interface directly to a plugin.
Feeds validates manifests, selects providers, enforces process/output limits,
and reports their state. Plugins implement collection, persistence and perception
mechanisms; they neither route workflows nor send messages to chat platforms.

| Capability | Invocation | Result |
| --- | --- | --- |
| `source.collect.v1` | Existing scheduled collection, or `feeds.refresh` | Existing `items` list with optional `kind`, `media_url` and string-array `tags` |
| `document.extract.v1` | Existing `feeds.extract` | Extracted documents for `knowledge.read` |
| `document.classify.v1` | Existing `feeds.classify` | Classification proposals |
| `document.process.v1` | `feeds.invoke`, `classify` or `summarize` | Compatibility Book result |
| `media.library.v1` | `feeds.invoke`, `ingest`, `list`, `fetch`, `status`, `contains`, `mark-sent` | Media intake cache result |
| `media.perceive.v1` | `feeds.invoke`, `screen`, `read-text`, `attachment` | Screening, OCR or attachment bytes |
| `audio.resolve.v1` | `feeds.invoke`, `resolve` with exactly one of `url` or `query`, optional `limit` (1–5) | `tracks`: `id`, `source`, `url` (the page), `title`, `uploader`, `duration_s`, `live` |

A `feeds.invoke` payload is `{capability, action, input}`. An optional `plugin`
selects an explicitly named provider. The subprocess receives
`{operation: capability, action, payload: input}` on stdin and prints exactly one
`{result: {...}}` or `{error: "..."}` object on stdout. Logs belong on stderr.
Only the declared processing capabilities above can use this endpoint; it is not
an arbitrary command endpoint. Requests share the host's bounded on-demand slots.
Manifest execution time and output bounds still apply.

No provider is chosen by directory order. Missing, disabled and ambiguous
providers return an explicit error. Collection manifests remain compatible, and
the new fields are optional: ordinary text sources need no changes.

## Install a scraper

Copy `dokja_services/dokja_feeds/plugins.example/scraper` to the owner's external
plugins directory under a unique id, change the manifest's `id` to match that
directory, and enable it. Keep each source's configuration in its own
`source.json`; multiple sources can use the same adapter independently.

```json
{
  "factory": "my_package.scraper:Scraper",
  "module_path": "/plugins/my-source/code",
  "kind": "media",
  "max_items": 50,
  "options": {"endpoint": "https://example.com/feed"}
}
```

The factory receives `options` as keyword arguments and returns an object with
`scrape(max_items=...)`. That method returns dictionaries with `url`, `title`,
`source`, optional `tags`, `summary` and `published`. A scraper can also implement
the collection subprocess protocol directly, in any language supported by the
host image. Installing its runtime dependencies is an image build concern.

Use `kind: "media"` for attachments. A page's URL and its `media_url` may differ;
the library uses the media URL. A text source must not be tagged as media merely
because its page contains an image. The domain admits only explicit media
observations to the library and enforces the facade's per-source item bound.

The scheduled `meme.pool.refresh` flow now refreshes Feeds collectors, reads their
normalized observations, and ingests the selected media into the library.
Successful sources retain their results when another source fails. Reading the
pool never runs a scraper implicitly. Adding a scraper needs no route, domain
branch, dedicated service, port or Compose entry.

## Bundled cache and optional processors

`FEEDS_BUILTIN_PLUGINS_DIR` supplies bundled providers. All Compose stacks set it
to the shipped `plugins.builtin` directory, whose enabled `media-library` provider
uses Python's standard SQLite module. An external manifest with the same id
replaces the bundled one, including when it is disabled or invalid; this permits
explicit replacement without running duplicate providers.

`FEEDS_MEDIA_DB_FILE` must point to the existing media database. Development and
lite default to `/workspace/ningo_memory.db`; production defaults to
`/data/ningo_memory.db` in the existing `dokja-data` volume. Set the variable when
the old `MEME_SERVICE_DB_FILE` used another location. The plugin reuses the
existing `meme` table and columns, preserves sent flags on duplicate collection,
and serializes writes between plugin processes. There is no table migration or
automatic data copy. Back up the database before changing deployments.

As of step 09.4-h, `fetch` is a read and must not set sent flags. The
orchestrator records `mark-sent` only after at least one destination acknowledges
successful delivery. Custom `media.library.v1` replacements must follow this
revised behavior. Update the reader and orchestrator together; see
[confirmed media delivery receipts](media-delivery-receipts.md) for the breaking
behavior change and receipt-storage limitations.

### Audio tracks

`plugins.example/audio-resolve` turns a link or a search into tracks with yt-dlp,
reading metadata only: it downloads nothing and returns no stream address, because
those expire and are bound to the network that asked for them. Whoever plays a
track resolves it again from its page. It is the intake step of the music workflow
([plan](music-plan.md)); it needs `FEEDS_PLUGIN_EXTRAS=media` or `content`, which
pin yt-dlp, and no settings. Errors from yt-dlp itself are reported as a generic
failure, with the detail only in the Feeds log.

Browsing and caching work without OCR or model dependencies. To use the shipped
`document-processor` and `media-perception` compatibility adapters:

1. Build Feeds with `FEEDS_PLUGIN_EXTRAS=content` for both adapters, or with
   `FEEDS_PLUGIN_EXTRAS=media` for `media-perception` alone. The default `none`
   keeps the image small.
   - `content` includes the document processor's model and browser libraries
     (`torch` and `transformers`, several gigabytes).
   - `media` installs only screening, OCR, the attachment transport and the
     meme scrapers' HTTP clients (`media-plugin-requirements.txt`), so it fits
     where `content` runs out of disk. Memes are Feeds media: a meme source is a
     `source.collect.v1` plugin with `kind: "media"`.
   - A custom provider can use its own dependency image.
2. Copy the desired adapter from `plugins.example` to the external directory and
   set `enabled: true`. Examples always ship disabled. Keep only one enabled
   provider per processing capability, or bind one explicitly in internal calls.
3. Configure model paths, providers and cache directories through the existing
   `DOKJA_BOOK_*`, `DOKJA_NSFW_*`, `DOKJA_OCR_*` and `MEME_*` settings, now passed
   to Feeds by every Compose stack. `DOKJA_REPO_ROOT` and `DOKJA_BOOK_DIR` locate
   the installed compatibility mechanisms. No request installs packages or models.
4. Restart Feeds to rediscover manifests. Inspect processing plugins alongside
   source plugins in the desktop's Knowledge → Sources view.

The media adapter reuses existing screening and OCR, without importing the legacy
scraper config. Missing perception never yields a safe verdict. Attachment reads
first require membership in the media cache. The document adapter preserves the
old Book result shape; `knowledge.read` remains the route that extracts and
stores documents. Retiring legacy strategy fields and moving delivery receipts
from the intake table into Memory are subsequent domain changes, not claims of
this migration.

## Deployment and rollback

`dokja-book` and `dokja-meme` now have the `legacy-content` Compose profile; they
are not started by the normal stack. Stop an existing Meme server before allowing
the plugin to read its queue, so two dispatchers do not compete for the same
items. Runtime inventory no longer manages these old containers.

To restore the earlier transport, set `DOKJA_CONTENT_BACKEND=legacy` and explicitly
start the `legacy-content` profile in the dev/lite stack (production previously
shipped only Meme). Restore the existing endpoints and restart the orchestrator.
The same database remains readable by the old service. No rollback schema work
is required. The desktop keeps its Feeds grouping; legacy containers are managed
through Compose during rollback.

The Feeds switch gates both compatibility workflows in plugin mode. Previously
saved Book/Meme off switches remain honored; they are not silently resumed by
migration and can still be changed through the existing `services.set` contract.
Scheduler event names and their configured intervals remain unchanged.

## Verification record

Go suites passed for Feeds (including `-race`), Book, Meme, runtime domain,
runtime integration service, bootstrap orchestrator and main orchestrator.
The real Python adapters were exercised for custom scraper collection and cache
persistence across host recreation, including retained sent flags. Desktop
TypeScript checks and its Node/Vitest suites passed. Compose configuration
validated for dev and lite; production validation used a temporary model-path
value, without starting containers or reading production credentials.

Content dependency images and real OCR/Book models were not built or exercised
in this change. The optional processors start a subprocess for each invocation,
so model startup contributes to their declared timeout. Gitleaks was unavailable
locally; its required PR check remains outstanding.
