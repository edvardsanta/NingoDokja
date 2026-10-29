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
| `audio.resolve.v1` | `feeds.invoke`, `read {url}` or `search {query, limit}` | `handled`, and `refused` or `tracks` (see Music sources) |
| `social.resolve.v1` | `feeds.invoke`, `read {url}` | `handled`, and `refused` or `post` (see Post readers) |

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

### Music sources

Each music source is a plugin providing `audio.resolve.v1`, with two actions:
`read {url}` (the song a link names) and `search {query, limit}` (songs for a few
words). A source answers `{handled: false}` for what it does not serve, so the
caller asks the next one without the source being marked failed;
`{handled: true, refused}` for a request it cannot do; and `{handled: true,
tracks}` otherwise. Each track has `id`, `source`, `url`, `title`, `uploader`,
`duration_s`, `live`, `playable` and, optionally, `thumbnail` (an http(s) picture: a
catalogue's album cover or a video's frame; a catalogue cover wins); a track that is not playable names a song to
be found on a playable source. Sources return pages, never stream addresses, and
step aside before loading heavy libraries. The Communications domain asks enabled
sources in id order and decides which result is the song
([music plan](music-plan.md)).

`plugins.example/audio-pages` reads any page yt-dlp understands and searches with
the yt-dlp search prefix set in a `settings.json` beside the installed `run.py`
(`{"search_prefix": "…", "hosts": ["…"]}`, both optional). Other sources are the
owner's, in `feeds_plugins/`.

Libraries for the owner's plugins come from a requirements file of the owner's,
named by the `FEEDS_PLUGIN_REQUIREMENTS` build argument as a path inside the build
context (for example `feeds_plugins/requirements.txt`). The default,
`dokja_services/dokja_feeds/no-plugin-requirements.txt`, lists nothing. The
example needs `yt-dlp` there. Rebuild Feeds after changing the list.

### Post readers

A post reader is a plugin providing `social.resolve.v1`. It opens a public post
link without signing in, so the owner can look at it on the desktop. Its one
action is `read {url}`. Like a music source, it answers `{handled: false}` for a
link it does not serve, `{handled: true, refused}` for one it cannot open, and
`{handled: true, post}` otherwise. `refused` is one of `login_required`,
`unavailable` (private, removed or not found) and `unsupported`.

A `post` has `url`, the canonical address of the post. It may also carry
`author`, `caption`, `published` (RFC 3339) and `media`. `media` is a list of
`{kind, url}` objects, where `kind` is `image` or `video`, and each may carry a
`thumbnail`. A media `url` must be downloadable with a plain GET: no cookies and
no sign-in. A reader that combines tools does so itself; for example, it may take
the media from one tool and the caption from another.

The Digest domain asks enabled readers in id order and takes the first that
handles the link. The repository ships no reader: readers reach third-party
sites, so they live with the owner's plugins in `feeds_plugins/`, together with
whatever they need to run.

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
