# Production Runbook

This document is the practical production checklist for Dokja.

Main files:

- [docker-compose.prod.yml](../docker-compose.prod.yml)
- [.env.prod](../.env.prod)

## Services

Production stack:

- `dokja-orchestrator`
- `dokja-meme`
- `dokja-knowledge` (research knowledge base) and `dokja-ollama` (its embedding server)
- `dokja-memory` (experience memory: what the bot did in a context and how it turned out)
- `dokja-feeds` (follows the sources the owner chooses, as plugins kept outside the repository)
- `dokja-discord`
- `dokja-scheduler`

## Orchestrator development startup

Development and lite orchestrator images download and verify Go dependencies
during the image build. Their module cache lives under `/go/pkg/mod`, outside the
`/workspace` source mount, so a recreated container can compile the mounted code
without downloading its pinned dependencies through bridge DNS. Rebuild
`dokja-orchestrator` after changing the orchestrator, local domain or store module
manifests. The production image already compiles its executable during the build.
This startup cache does not repair container DNS or restore network access to
other services; a DNS outage still needs a network-level fix.

## Chat context

Non-archived chat history is cached in memory and persisted in
`dokja_chat_sessions.db` beside `DOKJA_DB_FILE`. If the latter is unset, the default
is `.dokja/dokja_chat_sessions.db` relative to the orchestrator's working directory.
The new file uses schema version 1; existing database schemas are unchanged.
An unreadable, unwritable or unsupported session database fails explicitly; startup
does not silently fall back to volatile chat. Inactivity (default
three minutes) triggers real model-based compaction on the next message, rather
than a reset. The default raw-message threshold is 20; recent turns remain verbatim
alongside a bounded summary. Compaction adds an inference call using the configured
provider and can lose detail. A rejected or failed summary preserves the last
valid summary and all unsummarized raw turns, then continues generation with a
bounded recent-context projection. That projection includes at most eight prior
messages within 16 KiB, the current message intact, and the prior summary when
available. An untrusted omission notice marks withheld older turns; no successful
compaction notice is emitted. The complete unsummarized checkpoint remains in
the existing session store until valid compaction replaces its older portion.
Existing store/input byte limits still apply; this is not an unbounded archive.

After a summary failure, a one-minute per-session cooldown avoids repeating the
failed compaction on every turn. The cooldown is volatile: restart reloads pending
history and can attempt compaction again. Logs use `compaction_fallback` values
`invalid_summary`, `summary_timeout`, `provider_error` or `retry_cooldown`, never
summary text. Summary calls retain their existing 25-second deadline; a child
summary timeout can recover while caller cancellation remains terminal. The
inference client preserves context cancellation/deadline errors rather than
misreporting them as an unavailable Runtime. A failure of the actual reply provider
or durable session store still fails the turn explicitly. Restarting the
orchestrator reloads saved summaries and unsummarized turns. Server message discussions share only the same Discord guild/channel, with
untrusted display-name and participant attribution; direct messages and commands
retain separate per-user scopes. Explicit new conversations clear their scope, but
resetting a shared channel requires the configured verified owner. This change needs
an updated orchestrator and Discord adapter, with no new variables, ports or volumes.
It does not change archived owner conversations, index chat in pgvector, walk reply
chains, or import channel history from before deployment.

The stored summary is readable locally with a SQLite client in read-only mode:

```sql
SELECT scope_key, session_id, summary FROM chat_sessions;
```

Inspect the actual session file, not `dokja.db`. Keep it private: it contains
conversation data. The stored checkpoint is the latest summary and recent turns,
not a complete long-term transcript archive. Back it up using SQLite's online backup
mechanism or stop its writer before copying an active database and WAL.

## Required Secrets

Before starting production, replace placeholder values in [.env.prod](../.env.prod):

- `DISCORD_BOT_TOKEN`
- `DISCORD_APPLICATION_ID`
- `DOKJA_OWNER_DISCORD_USER_ID` (optional): the owner's Discord user ID. When
  configured, the orchestrator marks messages from this account so Ningo knows
  he is speaking with his owner. Keep the ID in the ignored environment file;
  never commit it. This identity label does not grant authorization to bypass
  policy or disclose secrets.

Do not deploy with `CHANGE_ME_*` values.

## Validate Configuration

Render the production compose config:

```bash
docker compose -f docker-compose.prod.yml --env-file .env.prod config
```

Build production images:

```bash
docker compose -f docker-compose.prod.yml --env-file .env.prod build
```

## Start Production

Start the full stack:

```bash
docker compose -f docker-compose.prod.yml --env-file .env.prod up -d
```

The orchestrator is the hub: the interfaces and the scheduler connect to it, and it does not depend on
the services it calls. It opens a connection per request and reports a service that is down as
unavailable, so it can run alone or with only the services you want, for example:

```bash
docker compose -f docker-compose.dev.yml up -d dokja-orchestrator dokja-meme
```

Check status:

```bash
docker compose -f docker-compose.prod.yml --env-file .env.prod ps
```

## Initial Smoke Checks

Watch logs:

```bash
docker compose -f docker-compose.prod.yml --env-file .env.prod logs -f dokja-orchestrator
docker compose -f docker-compose.prod.yml --env-file .env.prod logs -f dokja-discord
docker compose -f docker-compose.prod.yml --env-file .env.prod logs -f dokja-scheduler
docker compose -f docker-compose.prod.yml --env-file .env.prod logs -f dokja-meme
```

Things to confirm:

- orchestrator starts both ZeroMQ and HTTP ingress
- Discord bot logs in successfully
- the host runtime is running and the explicitly selected chat provider/model answers a manual conversation
- `dokja-cli status` (or the TUI panel) shows `ok` for every service you deployed
- meme service starts and opens SQLite
- scheduler emits bootstrap refresh and scheduled dispatch

## Scheduler Expectations

Current startup behavior:

1. emit `meme.pool.refresh`
2. wait `DOKJA_SCHEDULER_MEME_BOOTSTRAP_GRACE_PERIOD`
3. emit `meme.dispatch.scheduled`
4. continue normal cadence

Normal cadence:

- refresh every `45m`
- dispatch every `6h`

## Scheduled Meme Delivery Path

Expected production path:

`dokja-scheduler -> dokja-orchestrator -> dokja-meme -> dokja-discord -> Discord channel`

The scheduler must not talk to Discord directly.

The orchestrator owns the delivery workflow.

## Operator State and Shared Database

The orchestrator and scheduler share the `dokja-data` volume, mounted at `/data`:

- `/data/dokja_state.json` (`VA_STATE_FILE` in the orchestrator, `DOKJA_STATE_FILE` in the scheduler):
  which services and scheduler jobs an operator switched off and any job interval overrides. The
  orchestrator writes it; the scheduler only reads the intervals. A corrupt file starts every job paused.
- `/data/dokja.db` (`DOKJA_DB_FILE`): SQLite database with the chat provider profiles (base URL, model
  and token). Keep the volume private: the token is stored in plain text.
- `/data/dokja_chat_sessions.db`: durable non-archived session snapshots, created
  with mode `0600` and covered by the existing data volume. Include it in backups.
  An older build ignores this separate file; do not delete it during rollback.
- `/data/dokja_conversations.db`, beside `DOKJA_DB_FILE` and named from it, so there is no new
  variable: the owner's conversations with Ningo, as written by the desktop and the CLI.
  - Only the orchestrator opens it; it is created on the first start of a build that keeps
    conversations, readable by its owner only.
  - It is the owner's own text: keep the volume private and include it in backups.
  - A build older than the file refuses it rather than reading it wrongly, and the orchestrator then
    goes on without keeping conversations. Rolling back means the previous build and this file left
    alone.
  - At start, turns a stopped orchestrator left pending are marked interrupted; none is sent again.

Without a writable volume these files would be lost on every deploy.

Startup still emits every scheduled job once, but the orchestrator skips the ones an operator paused, so a
restart never posts something that was switched off. `dokja-cli services list` and `dokja-cli jobs list` show
the current state.

The status probe covers `chat`, `meme`, `book`, `knowledge` and `memory`. A stack that does not run one of
them (`docker-compose.prod.yml` has no book service) shows it as `error` and the platform as `degraded` until you switch it off (`dokja-cli services disable <name>` or
`space` in the TUI); a switched-off service is not probed.

Chat profiles are created and removed on the machine that holds the database (`dokja-cli chat profile add`
with `DOKJA_DB_FILE` pointing at it, or `p` in the TUI panel), never through the orchestrator. The
orchestrator can only list them (masked) and select one, these archived profiles no longer control inference. Use the explicit provider/model binding below.

## Research Knowledge Base

### PostgreSQL and pgvector adoption

Knowledge supports `KNOWLEDGE_STORAGE_BACKEND=postgres` with
`KNOWLEDGE_POSTGRES_DSN`; the default remains SQLite until an explicit verified
cutover. All three base stacks project these variables. The optional
`docker-compose.postgres.yml` overlay provisions `dokja-postgres` and the
independent `dokja-postgres-data` volume, with no published database port;
`DOKJA_POSTGRES_PASSWORD` is required for that overlay. Runtime does not manage
this infrastructure dependency automatically.

Follow [the import, cutover and rollback procedure](knowledge-postgres.md).
Stop writers, preserve the SQLite backup, import into an empty target, require
the full-digest verification result, and inspect `knowledge.status` for
`storage_backend: postgres` before reopening writes. No automatic import or
fallback occurs. After PostgreSQL has accepted writes, use the verified export
to a new SQLite file before any cutback; selecting the old snapshot alone does
not preserve those writes. Back up PostgreSQL independently after adoption.
Memory and receipt storage have not migrated in this batch. No deployment was
performed by this change.

### Knowledge metadata schema upgrade

The Knowledge build for step 09.2 upgrades its own database from schema 1 to 2 on
startup. Migration and `user_version` advancement are transactional. Version 2
adds `document_metadata`; existing documents, content hashes, FTS entries, chunk
IDs and embeddings are preserved. Old documents expose empty metadata until
explicitly updated or reread. No compose, environment, port or volume change is
needed. Deploy the storage service before the metadata-producing domain build.

Before upgrading, stop the Knowledge service and all writers and take a consistent
SQLite backup using the existing backup procedure. Preserve the backup with the
previous build identifier. Do not copy an active WAL database's main file alone.
Validate the upgrade on a backup copy before applying it to the production file.

An older build refuses schema 2. For an exact rollback, stop writers, restore the
pre-upgrade consistent backup, and restart the previous build; writes since that
backup will be lost. Alternatively, to retain documents and embeddings written
after the upgrade, back up the current database while stopped and perform this
**offline downgrade** against the chosen Knowledge file using SQLite:

```sql
BEGIN IMMEDIATE;
DROP TABLE document_metadata;
PRAGMA user_version = 1;
COMMIT;
```

This deliberately discards **all defining metadata**, retaining documents, tags,
legacy kinds, chunks, FTS and embeddings. Run it only on schema 2 from this build;
check `PRAGMA user_version` first and verify `PRAGMA integrity_check` afterward.
Restart the previous service build and roll back the metadata-producing domain
build together. A subsequent upgrade recreates empty metadata. Migration,
metadata-only updates and this downgrade are covered using disposable test
files; no owner database was migrated during development.

The reading-time classifier (step 09.2) adds a document-level `classifier`
field to that metadata, with no SQL migration. A Knowledge build without it
refuses the field in a metadata patch, so deploy the Knowledge service before
the orchestrator that sends it, and roll them back together. Until a
`document.classify.v1` plugin is enabled, the orchestrator never sends it.


`dokja-knowledge` stores the documents the operator feeds in and finds them by meaning and by keyword. Its
SQLite file is `/data/dokja_knowledge.db` on the `dokja-data` volume (separate from `dokja.db`). Embeddings
come from `bge-m3` on `dokja-ollama`, which keeps its models in the `dokja-ollama` volume. After the first
deploy, pull the model once:

```sh
docker compose -f docker-compose.prod.yml exec dokja-ollama ollama pull bge-m3
```

Without it, or while `dokja-ollama` is down, the service still ingests and searches by keyword only and
reports `degraded`; `knowledge.reindex` embeds what was missed. Changing `DOKJA_EMBED_MODEL` also needs a
reindex. `KNOWLEDGE_MIN_SCORE` (default `0.45`) decides which hits count as relevant; it was measured on a
tiny corpus, so retune it once the base has real content.

Documents can be added from `.txt`, `.md`, `.html`, `.docx`, `.epub`, `.pdf` and feed files, and from an
address (`dokja-cli knowledge add <address>`). A feed is only read once; following one over time is not built
in. Scanned PDFs are refused because OCR is not included. Because the orchestrator port has no
authentication, the address fetcher only reaches public hosts (see the service README for the exact rules);
set `KNOWLEDGE_FETCH=off` to disable it.

Anything specific to one site or system is a **plugin you own**: Python files in a directory outside the
repository, mounted read-only and named by `KNOWLEDGE_PLUGINS_DIR`, for example with a compose override:

```yaml
services:
  dokja-knowledge:
    environment:
      KNOWLEDGE_PLUGINS_DIR: /plugins
    volumes:
      - /path/to/your/plugins:/plugins:ro
```

Plugins run with the service's permissions, so only mount a directory you control.

Port `5561` is published on `127.0.0.1` only: the notes are private and the service has no authentication.
On every Discord message the orchestrator looks the question up and gives the chat model only the relevant
hits, then ends the reply with the sources it consulted. Switching `knowledge` off (`dokja-cli services`)
stops both that lookup and the `knowledge.*` events; chat keeps working either way.

### Ingest and reindex timeouts

Fetching, parsing and embedding may take longer than a health check. The
orchestrator gives `knowledge.ingest` and `knowledge.reindex` a dedicated
110-second ZeroMQ send/receive timeout; other Knowledge requests keep their
five-second timeout. The desktop already requests a two-minute ingestion wait.
A shorter caller deadline can still expire first, and the serial service may
queue requests behind a long write.

A timed-out write may already be stored. The orchestrator reports the fixed
`knowledge_write_outcome_unknown` reason for write socket timeouts or cancellation
after dispatch; the desktop treats that as a timeout and advises reloading the
document list before retrying. It does not resend automatically. This does not
add durable job acceptance, cancellation or a way to query an operation's outcome.
Very long imports still require checking stored documents after a timeout.

No service data, environment, port or compose changes are needed. Roll out the
orchestrator and rebuild/restart the desktop for the updated timeout guidance;
rolling back restores the earlier client timeout without a database migration.

## Experience Memory

`dokja-memory` stores experiences, "in this context the bot took this action, and this was the outcome",
and finds the closest earlier ones (see its [README](../dokja_services/dokja_memory/README.md)). Its SQLite
file is `/data/dokja_memory.db` on the `dokja-data` volume (separate from `dokja.db` and from the knowledge
base), created readable by its owner only: a context is the user's own text, so do not share or publish the
file. `memory.forget` deletes one experience.

It uses the same embedding server and model as the knowledge base (`DOKJA_EMBED_ENDPOINT`,
`DOKJA_EMBED_MODEL`), so provisioning the model once covers both (see "Research Knowledge Base"). Without the
model, or while `dokja-ollama` is down, it still records experiences and counts outcomes and reports
`degraded`; `memory.reindex` embeds what was missed, a batch at a time, and a changed `DOKJA_EMBED_MODEL`
needs it too (repeat until `remaining` is 0). The embedding call is short on purpose (`DOKJA_EMBED_TIMEOUT`,
5 seconds by default) because experiences are recorded on the path of an action.

An experience nobody resolved within `MEMORY_EXPIRE_AFTER` (default `720h`, `0` never) reads as `expired` and
is left out of every count and score; a verdict that arrives later still counts.

Port `5562` is published on `127.0.0.1` only because the service has no authentication. A listing or a recall
returns the first 160 characters of a context (a snippet) only when the request sets `include_context`; the
rest of the text never leaves the service. A hashtag suggestion is recorded with how close the earlier tagged
meme was and the start of its text; that text follows the same rule. The first start of the service after this
change adds two nullable columns to the database in place; nothing to run by hand. The previous build refuses
a database upgraded this way (it sees a newer schema version), so to roll back, restore a copy taken before
the upgrade.

The orchestrator routes the `memory.*` events to the service (`MEMORY_SERVICE_ENDPOINT`, set in the compose
files). `memory` is one of the services that can be switched off: while it is off, the orchestrator refuses
these events. A listing, a recall and the evidence of a prediction cross the orchestrator port, which has no
authentication, the same as `knowledge.search`: keep ports `5555` and `5558` off untrusted networks.

The first use is the hashtag suggestion: each learned hashtag the orchestrator appends is recorded with the
chance it would be kept, and when the operator tags the same meme it is resolved as accepted or replaced (the
meme's address, hashed, links the two). This runs in shadow mode, so it changes nothing that is sent. The
thresholds that decide what counts as similar, as enough evidence and as enough scored predictions are
provisional and live in `dokja_domain/dokja_memory`: calibrate them against real data before trusting a score.

Calls made by an action are best effort with a five second budget and run in the background: an unreachable
`dokja-memory` never delays or fails a delivery or a tag. Switching `memory` off stops these notes too.

`dokja-cli memory` works with it: `status` and `reindex` look after the service, `list`, `recall` and `show`
inspect experiences, `predict` estimates the chance an action is kept for a context, `score --action
hashtag.suggest` compares the stored predictions with a baseline and says when there are too few to judge,
`record` and `resolve` add and settle one by hand, and `forget` deletes one. `dokja-cli services disable
memory` switches the service off.

The TUI shows the same on `m` from the Painel (read on demand, never as part of the panel refresh), lets you
browse the experiences (`l`) and forget one, predict for a typed text (`p`), and shows the chance a suggestion
is kept next to the hashtag `g` suggests on the Memes tab. The `memory` row in its service list switches the
service off like any other.

To back up or move the data, copy `/data/dokja_memory.db` while the service is stopped, or use SQLite's
online backup. The schema is versioned in the file; a build refuses a database from a newer version.

## Feeds

`dokja-feeds` follows the sources the owner chooses and keeps their latest items in memory, so the digest
(`digest.status` and `digest.items`) can show them (see its [README](../dokja_services/dokja_feeds/README.md)).
The repository ships the mechanism only: a source is a plugin, a directory with a `plugin.json` manifest and
a program, kept outside the repository. Compose mounts `${FEEDS_PLUGINS_HOST_DIR:-./feeds_plugins}` read-only
at `/plugins` (that default directory is git-ignored). With no plugin the service still answers and the
digest is empty.

Development and lite run Feeds from the source bind mount with `go run`. Their
development image downloads and verifies the pinned `go.mod`/`go.sum`
dependencies at build time, retaining the module cache under `/go/pkg/mod`,
outside `/workspace`. Startup can compile the mounted sources without fetching
Go modules through the container bridge. Rebuild the image after changing either
Go dependency manifest:

```sh
docker compose -f docker-compose.dev.yml build dokja-feeds
docker compose -f docker-compose.dev.yml up -d --no-deps dokja-feeds
```

Use `docker-compose.lite.yml` for the lite stack. This retains the existing data
and plugin mounts. The production image already builds its executable before
startup. A bridge DNS outage may still affect plugins that fetch external
sources; the cached Go modules address the startup dependency failure.

A plugin runs only when its manifest says `"enabled": true`, and changes to the directory are read when the
service starts. Its program runs inside the `dokja-feeds` container, so it must exist in the image: the image
has a POSIX shell, the usual command-line tools, and `python3` with `pypdf` (Debian packages). For anything
else, extend the image or put a static binary in the plugin directory. A manifest whose program is missing
shows as `invalid` with the program's name in the status.

Feeds is also the reading host: `feeds.extract` reads one file on demand through the enabled
`document.extract.v1` plugin that serves its extension, and the orchestrator's `knowledge.read` stores the
result in Knowledge. The `knowledge-extractors` example serves Knowledge's own formats (PDF, EPUB, DOCX, HTML,
text, feeds): copy `dokja_services/dokja_feeds/plugins.example/knowledge-extractors` into the plugins directory
and set `"enabled": true`. It finds Knowledge's extractors through `KNOWLEDGE_EXTRACTORS_DIR`, set in every
compose file (the production image copies them to `/opt/dokja_knowledge`). Set `KNOWLEDGE_PLUGINS_DIR` on
`dokja-feeds` and mount that directory too only if your own Knowledge format plugins should be read here as
well. A reading runs with the service's permissions and no sandbox: the plugin gets only its declared
variables, at most 2 readings run at once, and each stops at its manifest timeout or the 2-minute request
limit. With no enabled extractor, `knowledge.read` fails with `unavailable` and `knowledge.ingest` with a
file keeps working as before.

An enabled `document.classify.v1` plugin (at most one) classifies what `knowledge.read` stored. The
`agent-kinds` example (disabled) asks Claude Code, then Codex, then Ollama; it needs Feeds running where
those CLIs are logged in, which is not the image. See its variables in the Feeds README.
It sees a 16,000-character sample of each new document, shares the 2 reading slots and stops at its
manifest timeout or 3 minutes. A classifier that calls a remote model sends that text to its provider, so
enable one only for documents you are willing to send. Without one, or when it fails, documents are stored
with their form and content kind pending for the user.

A token a plugin needs belongs to the service's environment, not to the manifest: the manifest lists only the
variable names, and only those variables reach the plugin. A failed run keeps the previous items and the
status says why (`exit status 2`, `timed out after 30s`); what the plugin wrote to stderr goes to the service
log and nowhere else, so read that log to debug a plugin.

Port `5563` is published on `127.0.0.1` only because the service has no authentication. The orchestrator
routes the `digest.*` events to it (`FEEDS_SERVICE_ENDPOINT`, set in the compose files). `feeds` is one of the
services that can be switched off: while it is off, the orchestrator answers these events as skipped. The
items cross the orchestrator port, which has no authentication, the same as `knowledge.search`: keep ports
`5555` and `5558` off untrusted networks. `ningo.status` lists `feeds` with a note when no plugin is enabled
or some of them are failing.

## NSFW Screening for Memes

The meme service labels each meme with an NSFW verdict, and channels in `DISCORD_SAFE_ONLY_CHANNEL_IDS`
only receive memes that passed. It needs two read-only model folders, mounted from `DOKJA_MODELS_DIR`
(`nudenet/320n.onnx` and `rapidocr/*.onnx`). The compose default is a path on the development machine, so
set `DOKJA_MODELS_DIR` in [.env.prod](../.env.prod). If the models are missing the screen fails closed:
safe-only channels receive nothing, other channels are unaffected. `MEME_NSFW_EXTRA_WORDS` adds words to the
blacklist. `DOKJA_OCR_REC_MODEL` (empty by default) names another text recognizer inside the `rapidocr` folder;
a Latin-alphabet one keeps Portuguese accents that the default drops (see the meme service README, "OCR
recognizer"). `DOKJA_DISCORD_WEBHOOKS` (`channel_id=webhook_url`, comma-separated) makes the delivery server
post to those channels through a webhook instead of the bot; the URLs are credentials, keep them in the
env file only.

## Hashtag Suggestion for Memes

The operator tags a meme's own text (read by OCR, not the scraper's title or tags) with a hashtag; a new
meme's text is retrieved against labelled examples in the shared Knowledge service and, if the closest one is
similar enough, its hashtag is suggested. `dokja-cli meme hashtag tag|suggest|list|untag`; see
`dokja_services/dokja_meme/README.md`.

The orchestrator coordinates the flow: the meme service supplies OCR, Knowledge stores and retrieves
`kind=meme_hashtag` documents, and Prediction/Experience records whether suggestions that were actually used
were accepted or replaced. The meme service no longer owns a classifier or a hashtag database.

Embeddings and keyword fallback therefore use the Knowledge configuration. If the model is missing, explicit
labels are still stored and retrieval reports its degraded keyword-only state. `MEME_HASHTAG_MIN_SCORE`
(default `0.6`) is now read by the orchestrator and passed to Knowledge for hashtag searches; it remains
provisional and should be calibrated against representative labelled memes.

The orchestrator appends relevant suggestions to manual and scheduled deliveries, and notes each one in the
experience memory (see "Experience Memory" above).
Missing OCR text, unsupported video input, low similarity or a classifier error leaves
the original message intact. Disabling the meme service also disables automatic suggestions
for manual delivery. Channel validation and safe-only screening remain in effect.

The TUI supports learning (`h`), suggestion (`g`), tagged examples (`l`) and removal (`u`). After `g` the
selected meme's panel shows why: the closeness of the closest tagged meme, the text read from this meme and
the text of that one. `dokja-cli meme hashtag suggest` prints the same fields (`score`, `query_text`,
`matched_text`).
Corrections replace the Knowledge document for the same URL; automatic suggestions are not saved as labels.
Back up `/data/dokja_knowledge.db` with the other persistent databases. Existing
`ningo_hashtags.db` files are retained for rollback but are no longer read at runtime. With the orchestrator
running, import one before retiring it:

```bash
python3 scripts/migrate_meme_hashtags.py ningo_hashtags.db --apply
```

The migration reads the legacy file without modifying it and sends each label through `meme.hashtag.tag`.
Rebuild the meme, knowledge and orchestrator images for this change; no new ports are introduced.

For manual TUI sends only, the channel picker exposes `--force`. It bypasses screening
for selected safe-only channels and requires a highlighted confirmation. The orchestrator
rejects the field from CLI, API and scheduled events, records the bypass in its log and
returns `nsfw_bypassed: true`. The request bridge is an operator interface rather than an
authentication boundary, so keep its existing network-access restrictions in place.

### Local lite stack

`docker-compose.lite.yml` connects the meme service to the internal embedding server.
Set `MEME_SERVICE_SCRAPERS` to select the locally configured scraper names; when
empty, the meme service uses all scrapers from its local configuration.
The `dokja-embedding-init` one-shot service downloads `${DOKJA_EMBED_MODEL:-bge-m3}`
after the server is healthy; the meme service starts only if that download succeeds.
The default uses CPU, requires no GPU passthrough and publishes no embedding port.
Models persist in `${OLLAMA_MODELS_DIR:-${HOME}/.ollama}`. Set `OLLAMA_MODELS_DIR`
to another writable directory before startup if needed; this changes the model cache,
not the container engine's image storage. Both locations need sufficient free space.

```bash
docker compose -f docker-compose.lite.yml up -d dokja-meme
docker compose -f docker-compose.lite.yml logs dokja-embedding-init
docker compose -f docker-compose.lite.yml exec -T dokja-ollama ollama list
```

The first start requires network access to download the image and model. If the
download fails, fix the reported storage/network issue and repeat the startup command.
After changing `DOKJA_EMBED_MODEL`, repeat it to provision the new model. Later server
outages preserve tags as pending examples; they do not erase the learning database.

## Attachment Fetch for Memes

Some hosts put media downloads behind a client-fingerprint check (Cloudflare and
similar) that the Discord interface's own plain HTTP download fails, even with correct
`Referer`/`User-Agent` headers. `meme.attachment.fetch` (`MEME_ATTACHMENT_FETCH`,
default `on`) has the meme service download the bytes itself and hand them to the
orchestrator, which then sends them to the Discord interface instead of a URL to fetch.
Scoped to URLs already in the meme service's own pool.

A failed or disabled fetch falls back to the previous behaviour (sending the plain
URL), so this never blocks delivery for hosts that do not need it; no new ports, and
rebuilding the meme service and orchestrator is enough to pick it up.

## Discord voice conversations

The lite and dev voice commands use `main.py --reload`: Uvicorn watches only the
speech service's Python source directory and creates providers in the reload
worker. Model-cache changes do not restart it. Application timing logs remain
configured after each reload. Discord uses its existing `tsx watch` process and
the orchestrator uses Air, watching `dokja_orch`, `dokja_domain`, and `dokja_store`
so changes in replaced Go modules also trigger a rebuild. Its process working
directory remains `dokja_orch`. Production continues without source reload.
During development, a Python reload briefly reloads speech models; changes to
Discord code restart its listener, so run `/voice_chat_start` again afterward.

Voice chat defaults to the saved Runtime text provider/model
(`DOKJA_VOICE_CHAT_PROVIDER=runtime`). The local voice path is experimental and
requires explicitly setting `DOKJA_VOICE_CHAT_PROVIDER=ollama` on the orchestrator.
That path uses the dedicated local Ollama model configured by
`DOKJA_VOICE_CHAT_MODEL` (default `qwen3.5:2b` in dev and lite). Install it in
the stack's Ollama service before testing, for example with
`docker compose -f docker-compose.lite.yml exec dokja-ollama ollama pull qwen3.5:2b`.
This experimental binding is independent of the Runtime text chat provider.
The first local voice request loads the model; afterward it stays loaded until
Ollama stops or the model is unloaded (`DOKJA_VOICE_CHAT_KEEP_ALIVE` defaults to
`-1s`), trading a few GB of RAM
for low follow-up latency.
The local model streams complete sentences to Discord as soon as they are usable;
the listener keeps collecting other speakers while playback finishes.

Discord `/voice_chat_start` sends recognized speech to the orchestrator and plays
its reply in the caller's voice channel. Replies use the saved Runtime chat
provider/model by default; the dedicated local voice model is optional.
Speech providers do not select the text LLM.
Use `/voice_chat_stop` to stop listening, or `/speak` to test synthesis alone.

The lite and dev stacks read `DOKJA_VOICE_PROVIDER` and
`DOKJA_VOICE_STT_PROVIDER` from the environment. Their defaults are `stub`:
placeholder tones and transcripts do not establish working speech. For real
speech, configure the following in the ignored `.env` file:

```dotenv
DOKJA_VOICE_PROVIDER=piper
DOKJA_VOICE_PIPER_MODEL=/workspace/.dokja/models/voice/pt_BR-faber-medium.onnx
DOKJA_VOICE_STT_PROVIDER=transformers
DOKJA_VOICE_STT_MODEL=openai/whisper-tiny
DOKJA_VOICE_MODEL_CACHE_DIR=/workspace/.dokja/models/voice/whisper
```

Download the Piper ONNX model and its matching `.onnx.json` into the configured
directory before restarting. Whisper downloads into its persistent cache on first
load, which requires network access. Models under `.dokja/` stay out of Git.
Recreate only the voice service with
`docker compose -f docker-compose.lite.yml up -d --no-deps --force-recreate dokja-voice`.
Query its `/health` endpoint and verify `tts_provider=piper` and
`stt_provider=transformers`, then test synthesis and transcription. Finally, join
a Discord voice channel and use `/voice_chat_start` to verify actual playback
and microphone reception. Ordinary text replies do not automatically play aloud.

Group conversations now capture every human in the connected voice channel,
with independent audio streams and stable participant aliases in shared room
history. The channel where `/voice_chat_start` was invoked remains the inbound
policy channel; the physical voice room is the conversation scope. Bot audio is
ignored. A speaker change does not reset history, and only the last speaker's
account can supply current-turn owner verification.

Capture uses 600 ms of Discord silence and an 80 ms room settling period.
WebRTC VAD checks 320 ms probes before allowing microphone activity to hold the
floor. Speech is also checked before Whisper transcription. Quiet packets,
isolated impulses and stationary noise are rejected by acoustic guards and VAD;
VAD quality still needs verification with the actual room and microphones.
A VAD silence boundary can end an utterance even when Discord continues sending
background audio. Long speech is transcribed in eight-second segments. At most
16 human streams and two transcription requests run at once; queued segments
and pending transcripts are bounded to 32 each. Capacity errors are logged.

The Chat domain records incidental speech without generating replies or running
model summaries. Starting `/voice_chat_start` opens the exchange for the person who invoked it,
so an immediate `teste` works without a second wake phrase. Calling `Ningo` or
`Dokja` opens or reopens a conversational exchange.
The addressed speaker can follow up without repeating the name for 30 seconds;
that window includes estimated playback duration. Another participant can join
with a clear question or continuation during a 12-second handoff window.
Acknowledgments such as `uhum`, `ok` and `sim` remain context and do not invoke
inference or extend engagement. The engagement window is transient; after a
restart, address Ningo again. This eligibility grants no account authority.

Ningo finishes each spoken reply while continuing to capture human speech.
Speech heard during generation or playback is combined into the next room turn.
It does not pause playback, cancel inference, or start overlapping replies.
`/voice_chat_stop`, explicit session shutdown and transport loss still cancel
outstanding work. The default spoken answer is one sentence, capped at 45 words;
explicit requests for detail allow a longer explanation. A long explanation is
synthesized sentence by sentence with one sentence prepared ahead, so the first
can play before synthesis of the whole answer finishes.

Only accepted reply text becomes audio. The current CLI adapters expose previews
that are not validated speech segments, so they still wait for the final reply.
No token-level speech streaming or subsecond end-to-end guarantee is established.
The authenticated `/orchestrator/voice/chat/stream` endpoint sends keepalives and
an optional `accepted` eligibility frame, then the final reply. The gateway
sends a bounded listener ID and starter identity; repeated metadata for the same
listener cannot refresh an expired window. Starter identity affects eligibility
only, never current-speaker verification or permissions. A disconnected
requester cancels its inference context and releases the room session lock.

The room retains up to 128 recent messages within a bounded checkpoint, with a
128 KiB bound on older retained messages. Inference uses the existing configured
recent-context projection (20 messages by default), plus any previous summary;
older omitted context is identified explicitly. Live voice does not wait for a
model-generated summary. This keeps incidental listening and context maintenance
off the reply-generation path while keeping recent participant context.

Whisper normalizes PCM to mono 16 kHz, rejects segments shorter than 250 ms and
near-digital silence, and requires WebRTC speech activity before decoding. It
bounds deterministic decoding to 12 tokens per second of audio (16–128 tokens).
STT runs outside the HTTP event loop and serializes the shared model. Inspect
`dokja_voice.stt` logs for audio duration, elapsed time, text length and token
bounds; speech text is not logged. The 15-second transcription deadline is
unchanged. Activity probes have a separate 1.5-second adapter deadline.

Speech requests follow Interface → Event → Orchestrator → Communications domain
→ Voice service. The authenticated bridge accepts optional `voice` data for
`voice.synthesize`, `.transcribe`, `.wakeword` and `.activity`; the Voice service
adds `/activity` for bounded speech probes. Rebuild the Voice image to install
`requirements-vad.txt`, then restart Voice, the orchestrator and Discord together.
Both Voice Dockerfiles install the pinned VAD dependency. The orchestrator uses
`DOKJA_VOICE_HTTP_ENDPOINT`, defaulting to the internal Voice service on port 8081.
The production stack needs a reachable Voice service before enabling voice
commands. Roll back the three components together. Existing envelopes, routes,
commands, schemas, ports and volumes retain their contracts.

Verify with two human accounts: start once, have an incidental exchange, call
Ningo, follow up without repeating his name, and say `uhum` while he answers.
He should finish, retain speech heard meanwhile and address the next turn only
afterward. Try keyboard noise while he speaks; it must not cancel playback or
create a model reply. Finally stop while transcription or playback is running;
no later audio should play. Repeat in another room to verify isolated context.
These microphone and playback checks require a live Discord call; synthetic
transport tests and generated speech do not establish them.

Wake-word mode is separate: it requires an installed detector and a matching
trained model. `/voice_chat_start` does not use the wake-word provider. To restore
placeholder speech for development, set both speech providers to `stub` and
recreate the voice service again. Existing ports, volumes and HTTP contracts are
unchanged.

## Music in voice channels

The Discord image now carries `yt-dlp` (pinned, in `/opt/yt-dlp`, on the PATH) for music.
Rebuild `dokja-discord` to get it (`up -d --build dokja-discord`); the dev and lite stacks
mount the code, but the image itself must be rebuilt once. The queue lives in the orchestrator
and is lost on restart; finding tracks needs the owner's music sources enabled in Feeds (see
[the music plan](music-plan.md)). No port, volume or environment variable is added.

## Security Notes

The orchestrator request port (`5558`) has no authentication and is published on every interface, so anyone
who can reach it can flip the switches above and run manual deliveries. Bind it to localhost
(`127.0.0.1:5558:5558`) on shared hosts.

## Common Failure Checks

### Discord issues

Check:

- `DISCORD_BOT_TOKEN`
- `DISCORD_APPLICATION_ID`
- `DOKJA_ORCHESTRATOR_TIMEOUT_MS`
- `DISCORD_GUILD_ID` (one server, or several separated by commas)
- `DISCORD_ALLOWED_CHANNELS`
- `DISCORD_SCHEDULED_MEME_CHANNEL_ID`

### Chat inference issues

Check that the host runtime controller is running, the selected registration is enabled and
its model remains declared. Use System → Inference providers → Chat provider and model,
or `dokja-cli providers chat <provider> <model>`. A missing selection is a setup error;
there is no environment credential fallback or automatic provider fallback.
The chat health row checks configuration, not authentication, quota or live inference.

### Scheduled delivery issues

Check:

- scheduler log for emitted `meme.dispatch.scheduled`
- orchestrator log for `deliver-scheduled-memes`
- Discord delivery log for `/deliver`

### Meme fetch issues

Check:

- meme service logs for scraper upstream failures
- SQLite file at `MEME_SERVICE_DB_FILE`

## Stop Production

```bash
docker compose -f docker-compose.prod.yml --env-file .env.prod down
```


## Optional desktop communications

The desktop communications tab adds authenticated actions without changing the existing ingress
addresses or the event envelope. `communications.channels` and `communications.history` route
through `dokja_domain/dokja_communications` to the read-only `dokja-communications` HTTP service.
`communications.send` reuses the existing Discord delivery workflow and safe-only channel rules.

- Set `DOKJA_COMMUNICATIONS_TOKEN` to a randomly generated value of at least 32 characters for the
  orchestrator and service. Desktop reads the checkout's `.env` when the variable is absent from
  its process environment. If Compose uses `.env.prod`, launch Desktop from its package directory
  with `pnpm start --env-file=../../.env.prod`. An exported value, including a blank one, overrides
  the file. Restart Desktop after updating configuration. Empty, short and `CHANGE_ME` credentials disable the
  new routes. Keep the value in the ignored environment configuration, never in Git. The
  orchestrator removes the credential before dispatch and debug responses.
- `COMMUNICATIONS_SERVICE_ENDPOINT` is `http://dokja-communications:8084` in all three Compose
  stacks. The service has no host port mapping and starts only with the `communications` profile.
- The read service receives `DISCORD_BOT_TOKEN`, optional `DISCORD_GUILD_ID` for channel labels,
  `DOKJA_DISCORD_API_BASE_URL` (the same operator input the delivery interface uses; the service
  has no built-in address and answers `not_configured` without it) and the existing comma-separated
  `DISCORD_SCHEDULED_MEME_CHANNEL_ID`. Both domain and service
  enforce this channel list. A webhook alone cannot read message history.
- The desktop uses loopback/IPC for these requests. Remote use requires a secure tunnel ending at
  loopback; this shared credential does not encrypt ZeroMQ. Existing unauthenticated actions and
  port publishing remain as documented above.

After configuring the ignored environment file, the production command is:

```sh
docker compose --env-file .env.prod -f docker-compose.prod.yml --profile communications up -d --build dokja-orchestrator dokja-communications
```

This is a production deployment and still requires the owner's approval. Development uses
`--env-file .env -f docker-compose.dev.yml`; the lite stack uses `docker-compose.lite.yml`.
The existing Discord delivery interface must also be running for sends.

The bot needs channel visibility, message history and the applicable message content permission.
The desktop shows names of attachments; it does not download arbitrary conversation attachments
or open their links. It can attach a meme from the existing queue to a new message after native
confirmation. Polling pauses when hidden, on older pages and after errors (including rate limits).
No send is retried automatically. On timeout verify the channel before resending.

Compatibility: new routes, new optional environment settings and one internal service; no change
to CLI/Discord commands, SQLite schemas, existing port mappings or existing HTTP contracts.

## Optional WhatsApp communication service

People can talk to Ningo in WhatsApp through a linked device (Baileys, an unofficial protocol: the
account can be restricted or banned, so prefer a dedicated number). See
[whatsapp-interface.md](./whatsapp-interface.md) for the policy, setup and limits.

- The service starts only with the `whatsapp` Compose profile, has no published port and listens on
  nothing. Set `DOKJA_WHATSAPP_ALLOW_FROM` and/or `DOKJA_WHATSAPP_ALLOWED_GROUPS` in the ignored
  environment file; self chat is on by default. Nothing is answered unless it is listed.
- First start: read the QR code from the `dokja-whatsapp` log and scan it in Linked devices within
  about a minute. The session lives in the `dokja-whatsapp-auth` volume and is the whole account:
  back it up like a secret and never copy it into the repository.
- The service keeps running, in a state that says what it needs, when the session was logged out
  (`logged_out`), is used by another connection (`replaced`) or was refused (`refused`). Remove the
  volume and link again, as described in the interface doc.
- Check that it works in the `dokja-whatsapp` log (`"whatsapp":"connected"`, then
  `"message":"replied"`). Ignored messages are not logged on purpose; a group that is not allowed
  is logged once with its id and `"reason":"policy"`.
- The orchestrator bridge accepts the `whatsapp` interface next to `discord`, limited to
  `message.created`; requests without the field are Discord requests, as before.

Compatibility: new optional environment settings, one optional service and one named volume; an
additive value for the optional `interface` field of the HTTP bridge request (the `whatsapp` event
source already existed); no change to the event envelope, existing ports, CLI/Discord commands or
HTTP contracts.

### Inbound eligibility migration (breaking)

Discord and WhatsApp gateways now send normalized entry metadata to the HTTP
bridge. The Communications domain in the orchestrator decides guild/channel/DM,
named phone/self-chat and group mention eligibility before any chat or command
step runs. Gateways still suppress bot echoes, duplicate messages, stale WhatsApp
messages and empty text. The existing event envelope and reply format are intact,
but older gateway bridge requests without eligibility metadata are silently
refused. Recreate the orchestrator and both gateways from the same revision;
rollback must restore all of them together.

Set the same nonempty `DOKJA_GATEWAY_INGRESS_TOKEN` in the ignored environment
file for the orchestrator and gateways. The HTTP bridge refuses requests without
the bearer credential and will not start without a configured token. Keep this
credential separate from `DOKJA_COMMUNICATIONS_TOKEN`, which controls operator
communications actions. The Compose files project the gateway credential to
each affected process.

Generate a private value once and save it as `DOKJA_GATEWAY_INGRESS_TOKEN` in
the stack's ignored `.env` or `.env.prod`. Keep the same value for the
orchestrator and every enabled gateway in that stack:

```sh
printf '\nDOKJA_GATEWAY_INGRESS_TOKEN=%s\n' "$(openssl rand -hex 32)" >> .env
```

For production, replace `.env` with `.env.prod`. Do not commit or paste the
generated value into logs or chat. If the orchestrator says this variable is
required, Compose did not pass it to the container; correct the ignored env file
and recreate the orchestrator and enabled gateways together.

The bridge also answers `POST /orchestrator/chat/stream` with the same bearer credential. It takes
a desktop `message.created` envelope, exactly what the request socket takes, and answers
newline-delimited JSON: `{"preview": ...}` lines with the reply written so far, then the response
the request socket would give. Anything but a desktop chat turn is refused. Previews reach only the
caller and are never stored; the reply is the record. The route is additive and nothing changes
until an interface uses it.

The desktop uses that route for Talk when its environment sets `DOKJA_BRIDGE_ENDPOINT` to the bridge
on the same machine (for example `http://localhost:8091`; any other host is ignored) and
`DOKJA_GATEWAY_INGRESS_TOKEN` to the bridge's credential. Without both, Talk goes over the request
socket and shows the reply whole, as before. Giving the desktop this credential widens who holds it:
it is the gateways' credential, and with it a process can also post to `/orchestrator` as the Discord
or WhatsApp gateway. Give it only to a desktop on a machine you would trust with the gateways. If the
bridge is not listening, or is too old to stream, the desktop sends the turn over the request socket.

The orchestrator now reads `DISCORD_GUILD_ID`, `DISCORD_ALLOWED_CHANNELS`,
`DISCORD_DM_POLICY`, `DOKJA_WHATSAPP_SELF_CHAT`,
`DOKJA_WHATSAPP_ALLOW_FROM` and `DOKJA_WHATSAPP_ALLOWED_GROUPS` from the same
ignored environment as the gateways. `DISCORD_GUILD_ID` may name several servers,
separated by commas: Ningo answers in each (still only in `DISCORD_ALLOWED_CHANNELS`)
and the Discord gateway registers its commands on each. A single value works as
before. Recreate the orchestrator, `dokja-discord` and, when it runs,
`dokja-communications` after changing it. The desktop's Messages lists every text channel of
those servers, grouped by server, and the operator may read and write to any of them by hand
(`communications.send` with explicit channels only); scheduled memes and "send to all" still use
`DISCORD_SCHEDULED_MEME_CHANNEL_ID` alone. `dokja-communications` runs from a built image: after
updating it, `docker compose --profile communications up -d --build dokja-communications`. `DISCORD_REQUIRE_MENTION` is optional and
defaults to false, matching the previous running Discord listener. Set it to
true only when ordinary guild messages should require a bot mention; slash
commands remain available in allowed channels. Test with synthetic gateway
requests before reconnecting live accounts. No credential or phone number is
written to the repository.

### Communication gateway package relocation

The Discord and WhatsApp build contexts now use
`dokja_services/dokja_discord/` and `dokja_services/dokja_whatsapp/`.
Build and recreate the orchestrator and enabled gateways from the same revision
after the inbound bridge migration above. Keep the existing Compose service
names and named volumes; the WhatsApp profile remains opt-in. A rollback must
restore the matching orchestrator and gateway revision together and retain the
WhatsApp auth and history volumes. Runtime changes to inbound allowlists now
recreate both the orchestrator and any running affected gateway. Check status,
linking and synthetic inbound/outbound flows before reconnecting live sessions.

## Interfaces in the desktop

The desktop's System view lists Discord and WhatsApp under Communication services, with
one row per adapter, its state and routing control. See the adapter docs for state details.

- Each interface runs an internal listener that answers `GET /status` (Discord `8092`, WhatsApp
  `8094`). Neither port is published; the orchestrator reads them over the Compose network. A reply carries a level, a state from a closed list and a short sentence, never an id.
- The orchestrator always lists known interfaces, including before setup. A missing
  status endpoint is `unchecked` / `not_configured` with `configured:false`; it
  does not degrade overall platform health. `DISCORD_INTERFACE_STATUS_ENDPOINT`
  is set in the stacks.
- `WHATSAPP_INTERFACE_STATUS_ENDPOINT` defaults to
  `http://dokja-whatsapp:8094/status` in the lite and dev stacks.
  - While the WhatsApp container is not running, nothing answers there. The
    interface is then reported `disabled` / `not_running`: it is not a problem
    and does not degrade health, and the desktop asks to start the module
    instead of offering Link. A crashed container shows in its module's own
    state.
  - Set the variable empty to show WhatsApp as not configured instead.
  - Missing setup never removes the WhatsApp row or falsely reports a connected
    account.
- **Switching an interface off** makes it answer nothing at all in its chats (no "switched off"
  reply) and leaves its channels out of scheduled and manual deliveries, which are reported as
  `skipped_interfaces` and are not failures. Its connection and credentials are left alone, and the
  desktop and the CLI are never gated, so it can always be switched back on.
- A configured interface that reports a connection failure makes `ningo.status`
  report `degraded`. Unconfigured optional interfaces and interfaces switched off
  do not. The desktop keeps them visible and explains missing setup; link and
  routing controls wait for configuration.
- `ningo.status` is not authenticated and the request port is published, so it carries only these
  states. Anything that would carry a link code, history or a send travels under the
  `communications.` event types, which need the communications credential.
- **WhatsApp conversations in the communications tab.** Name them in `DOKJA_WHATSAPP_CONVERSATIONS`
  (`alias=<number or group id>`); only those are recorded, as text, in the `dokja-whatsapp-history`
  volume for `DOKJA_WHATSAPP_HISTORY_DAYS` days (30 by default). The orchestrator reads them from the
  interface's status address and lists them as `whatsapp:<alias>` next to the Discord channels,
  behind the communications credential. A named group records every member's text. To erase the
  history now, remove that volume.
- **Sending from the desktop to a WhatsApp conversation** goes out as the owner's own account, through
  `communications.send` only, one named conversation at a time, as text, under a fixed budget of 6 a
  minute and 40 an hour. The desktop asks in a window of its own first. Turning WhatsApp off in the
  health tab stops it as well.
- **Linking WhatsApp from the desktop, CLI or TUI** uses `communications.link.status`, `.start` and
  `.reset`. They need `DOKJA_COMMUNICATIONS_TOKEN` on the orchestrator and in the environment of the
  desktop process, `dokja-cli` or `dokja-tui`; each draws the code itself (the CLI and TUI with
  terminal blocks) and never shows it as text. `dokja-cli interfaces link whatsapp [--new]` waits
  for the scan; `interfaces unlink whatsapp` asks first. In the TUI, `l` on the whatsapp row opens
  the link window. The orchestrator derives the link routes from
  `WHATSAPP_INTERFACE_STATUS_ENDPOINT`, so that is the only address to set. Starting over unlinks the
  device and erases the saved session; the session volume itself is not removed.

Compatibility: additive. New optional environment variables on the orchestrator, one status route on
each interface, new names in the switchable set (listed apart from the services) and extra fields
(`kind`, `state`) on interface entries of `ningo.status`. Existing `VA_STATE_FILE` files stay valid.

## Desktop local runtime controller

The desktop can bootstrap the **local lite stack** with a host daemon, independent
of the application orchestrator. This does not manage the production stack.
Opening desktop starts only the daemon and queries container status; all optional
application containers may remain stopped. In System → Modules, Start
runs one selected module and its required dependencies, and Stop preserves
persistent data and dependencies still in use.

Build prerequisites now include Go 1.25, native ZeroMQ development headers and
pkg-config for the generated host binary. Runtime requires a running Docker or
compatible Podman engine with `docker compose` available to the desktop user's
PATH. The lite Compose project must be used from its local checkout; the shell
accepts a trusted `--runtime-root` for launches outside the checkout. There is no
new application environment variable, TCP port, Compose service or mounted engine socket.
A local Unix `DOCKER_HOST` is bridged into `CONTAINER_HOST` for Podman compatibility
when no Podman override is present. Known remote/custom engine targets are refused.
Windows and remote/custom application endpoints do not expose these controls.

The host bootstrap orchestrator routes a restricted `runtime.modules` or
`runtime.set` envelope to the Runtime domain, which owns module policy, then to a
stateless Compose adapter. Normal feature requests still use the main
orchestrator. Existing `services.set` controls only routing and remains unchanged;
container startup does not silently enable a capability the owner disabled.

The inventory covers the core, Memory, Knowledge, Meme, Book, Feeds, Voice,
Communications, WhatsApp, Discord and the scheduler. Communications only reads channels and history; it never sends. Embeddings/model initialization start only when required by Memory or
Knowledge. Only an explicit WhatsApp selection starts its gateway. No module implicitly
starts Discord or scheduled delivery: Discord (with Voice) and the scheduler start only when
selected, and every interface first says what they will do with the switches and jobs already
saved (which jobs post and when, whether the bot answers) and asks; nothing is changed on the
way. `dokja-cli modules start` takes `--yes` to skip that question.
Running external consumers are recognized when deciding whether dependencies may
stop. Existing containers are reused rather than recreated; image deployment is
separate from runtime Start/Stop.

**Stack settings.** Desktop (System → Settings), `dokja-cli settings list|set` and the TUI
(`c`) use the same controller actions (`runtime.settings`, `runtime.settings.set`). Operational
settings now live in the shared SQLite database, default `<checkout>/.dokja/dokja.db`.
The daemon's process environment may select `DOKJA_DB_FILE`; relative paths resolve against the
checkout. It does not load this bootstrap path from `.env`. Prefer an absolute custom path and
ensure profile clients/container mounts address the same database.

The first settings read or controller Compose command imports only the allowed communication
and model options from `.env`, overlaid by managed variables exported to the daemon. Import
happens once; future database edits win over both sources. Credentials and webhook addresses
are excluded, and `.env` content and permissions are unchanged. An invalid legacy value refuses
import until corrected. A custom embedding model also refuses import: this version supports the
fixed `bge-m3` profile, shown read-only in all three interfaces. Do not change the vector space
without a separate evaluated reindex/migration plan.

The catalog covers Discord meme delivery, safe-only and answer channels and DM policy; WhatsApp
people, groups and own chat; and existing book, speech-to-text and OCR model settings. Tokens and
`DOKJA_DISCORD_WEBHOOKS` remain edited by hand. Only webhook channel IDs are displayed. Provider
inventory/discovery and capability bindings are a later lot, not delivered by this migration.

Before each controller Compose command, database settings are supplied in the child process's
environment using the existing variable names. Running readers are recreated after saving
(`up -d --no-deps --force-recreate`, data kept); stopped readers wait until their next Start.
Failed recreation retains stale markers for explicit retry. Recreating the orchestrator ends
active chat sessions. A database failure refuses the operation rather than silently using old
values. Legacy-file read errors also block projection because webhook channel labels are still
read there. Services themselves do not query this configuration snapshot.

**Upgrade/rollback:** back up the database and `.env`, rebuild the controller/interfaces, stop
the existing daemon once with SIGTERM, then reopen a local interface. Application containers
stay running. The SQLite schema version and profile rows are unchanged. Older daemons and manual
Compose commands still use legacy environment configuration: they do not see subsequent database
edits. Reconcile desired values explicitly before recreating containers with those paths. No
export command is provided yet. Keep session/history volumes intact.

The settings commit and stale-reader journal are not one transaction: a daemon crash between
them can leave a reader on old values. Durable apply/recovery remains pending. This behavior is
limited to the local lite controller; production configuration and deployment are unchanged.
See [ADR 0013](decisions/0013-database-runtime-settings.md) for the boundary and verification.

The daemon listens on a mode-0600 Unix socket in a mode-0700 per-checkout directory
under `$XDG_STATE_HOME/dokja/` (default `~/.local/state/dokja/`), shared by the
desktop, the CLI (`dokja-cli modules`) and the TUI panel. Host-run CLI/TUI launch
it from the optional `DOKJA_RUNTIME_BIN` (an absolute path), `dokja-runtime` on
`PATH` or the desktop build; the `dokja-cli`/`dokja-tui` containers report module
control as unavailable. After upgrading from a build that kept this state in the
desktop data directory, open the new desktop first (only it adopts the old
ownership journal into the new location), then stop the old daemon once with SIGTERM.
A module that already shows failed from an earlier stop clears after its next Start and Stop. A stop the
controller asked for shows as stopped even when the process exits non-zero. An OS lock rejects duplicate instances
and permits safe stale-socket recovery. Private ownership state records which
services the controller created and which modules were explicitly selected. Do
not delete live socket/ownership files. When replacing the controller build or
changing the checkout's Compose project configuration, stop the old daemon with
normal SIGTERM before reopening desktop. This leaves application containers
running. No automatic OS boot/login service is installed.

Operations continue after closing desktop. The UI polls actual status and reports
starting, ready, running without a readiness check, stopping, stopped or failed.
The application must acknowledge a ZeroMQ status or HTTP health probe before
`ready`; Ollama must answer and model initialization must complete successfully.
Ordinary readiness waits at most two minutes, initialization up to eight minutes,
within a ten-minute operation limit. Errors do not trigger automatic retries.
After a timeout, inspect current state before retrying. Start failure never
rolls back by stopping unrelated containers. Stop never removes containers,
volumes or data, and it refuses dependencies required by running consumers.

The three new Go modules are tested explicitly in CI and `scripts/test_all.sh`;
the nested bootstrap module is not traversed by the existing orchestrator test
command. Rollback is the previous desktop build; stop the host daemon separately
if no longer wanted. There are no application schema or deployment migrations.

## Feeds content plugin migration

Book/Meme now default to the Feeds backend (`DOKJA_CONTENT_BACKEND=feeds`). Their
old containers are opt-in through the `legacy-content` profile. Before rollout,
point `FEEDS_MEDIA_DB_FILE` at the existing database, stop the old queue consumer,
and configure source and processing plugins. The optional
`FEEDS_PLUGIN_EXTRAS=content` build argument installs the shipped compatibility
processors' dependencies; `media` installs only the media-perception adapter's
(screening, OCR and attachment download, without `torch`); `none` is the small
default. `FEEDS_PLUGIN_REQUIREMENTS` names a requirements file of the owner's, by a
path inside the build context (for example `feeds_plugins/requirements.txt`, which
git ignores), whose libraries are installed for the owner's own plugins; the
default is an empty list in the repository. Both are build arguments of the
`dokja-feeds` image in every stack: rebuild Feeds after changing either.

All stacks configure `FEEDS_BUILTIN_PLUGINS_DIR`, the existing `dokja-data` volume
for plugin state/model cache, and an optional models mount. Processing settings
are passed to Feeds instead of requiring Book/Meme containers. Source manifests
are still supplied by `FEEDS_PLUGINS_HOST_DIR`. No port changes are introduced.
See [the migration and rollback contract](feeds-content-plugins.md) for the exact
paths, plugin setup and preserved legacy controls. Existing status examples
listing Book/Meme refer to `DOKJA_CONTENT_BACKEND=legacy`; plugin mode reports
providers under Feeds and does not probe standalone Book/Meme endpoints.

Step 09.4-h changes media fetch from consuming to read-only. Deploy the
orchestrator and selected reader together (Feeds bundled/custom media library,
or the legacy Meme service). A successful scheduled delivery now writes its
receipt explicitly; manual delivery retains the explicit `mark_sent` option.
One confirmed destination is sufficient even when another destination fails.
Blocked, failed and unknown sends do not set a receipt. Existing flags and
schema are preserved; old flags cannot establish historical delivery evidence.
Rolling back the reader restores consume-on-fetch behavior, so rollback also
requires the matching orchestrator. No deployment was performed by this change.
See [receipt semantics and failure limits](media-delivery-receipts.md).

## Local inference-provider inspection

The local lite controller exposes read-only `runtime.providers`: desktop System
→ Inference providers → Inspect providers; `dokja-cli providers`; TUI settings
(`c`) → providers (`p`). CLI executables are discovered from the daemon’s PATH.
Ollama models are listed inside the selected running lite-stack container; no
host port, credential, environment variable or Compose change is introduced.
Discovery does not start services, load/pull models or perform inference. Rebuild
and restart the old controller once to enable the new action. Authentication,
quota and capability/inference validation remain untested by this operation.
See [provider inventory](provider-inventory.md) for bounds and pending bindings.

The same controller can start the Ollama engine on its own and download and remove its models
(`runtime.models.engine.start`, `.pull`, `.cancel`, `.remove`, `.status`), always inside the stack's running `dokja-ollama` container, through
`docker compose exec`. Models land in the existing Ollama volume (`OLLAMA_MODELS_DIR`). There is no new
port, environment variable or Compose change. A download can take gigabytes of disk and network, and
starts only when an operator interface asks for it. The embedding model (`DOKJA_EMBED_MODEL`, default
`bge-m3`) cannot be removed while memory or knowledge run. Rebuild and restart the controller once to
enable the actions.

## Persistent provider registrations

The same local controller now stores discovery eligibility and up to 16 declared
model names for each supported adapter in `runtime.providers.v1`, an independent
revisioned key in the existing SQLite settings table. There is no schema-version,
environment-variable, port, service or Compose change. Credentials and operational
settings remain separate. Back up the shared database before rollout, rebuild the
controller/interfaces and restart the old daemon once.

Desktop: inspect providers, then Edit → Review → Save registration. CLI:
`dokja-cli providers configure <provider> --enabled=false|true --models=<names>`
(optionally `--yes`). TUI: settings (`c`) → providers (`p`), select and press `e`;
Ctrl+T toggles discovery, Enter reviews, and confirmation saves. Explicit empty
models clear declarations; omitted CLI flags retain saved values. An older
controller without a configuration revision remains read-only to new clients.

Disabling skips discovery for that adapter; it does not stop containers or change
legacy inference consumers. Declared models remain unverified, and the fixed
embedding profile stays unchanged. Concurrent edits refuse stale revisions; reload
and explicitly retry. Database/corruption errors block editing and inventory
rather than restoring enabled defaults. Older builds ignore the new key on
rollback and resume their previous discovery behavior. See
[provider registrations](provider-registrations.md) for contracts and remaining
capability-binding work. No live operator configuration was changed in testing.

## Chat provider migration

The dedicated language-generation container and its environment-based configuration are removed.
The conversational orchestrator sends bounded history to the host runtime's private inference
socket; the Runtime domain validates the saved provider/model against enabled registrations,
and infrastructure invokes only that adapter. No model is chosen from inventory automatically.

1. Back up the shared settings database. Rebuild the host runtime controller and interfaces
   using their existing build commands, and restart the controller as described above.
2. Register an enabled adapter and declare a model in System → Inference providers, or
   `dokja-cli providers configure <provider> --models=<model>`.
3. Select the pair under Chat provider and model, use TUI settings (`c`) →
   providers (`p`) → chat (`c`) and confirm the selected pair, or run
   `dokja-cli providers chat <provider> <model>`. Selection is stored in `runtime.chat.v1`
   with a separate compare-and-swap revision. No database table/schema migration is required.
4. Recreate the orchestrator from the updated Compose definition. Every stack mounts only
   `./.dokja/inference` read-only at `/run/dokja-inference` and sets
   `DOKJA_INFERENCE_SOCKET=/run/dokja-inference/chat.sock`. The controller creates the
   directory/socket privately; the container user must have permission to connect.
   The lifecycle control socket is never mounted. Native orchestrators may set the socket path
   directly; provider/model selection always comes from the host database.
5. Send a manual chat message and verify a reply. Disable the registration and verify that
   chat refuses inference; re-enable and retry. Authentication/quota errors stay explicit.
   History, reading reference and Knowledge citations continue through the same chat handler.

The private inference socket also provides `POST /math/render` for bounded formula
rendering. It accepts `{"expression":"..."}` and returns a PNG as `data_b64` with
`content_type: "image/png"`. This is a pure Go math parser; inference CLIs keep
their tools disabled. The orchestrator applies Chat domain presentation policy
and sends at most three images per reply, each limited to 256 KiB, with readable
text retained on rendering failures. Unsupported LaTeX commands use the text
fallback; this is a math subset, not a full TeX document compiler.

Rebuild/restart the host runtime controller, orchestrator and Discord adapter
together to enable formula images. An older runtime returns no image and the
orchestrator keeps the text fallback. No new port, volume or environment variable
is required. The bridge response gains optional `attachments` entries with
`filename`, `content_type` and `data_b64`; older clients continue using `reply`.

When one or more selected formulas cannot be rendered, the chat result reports
`chat.math_render_failed` as its notice. Discord localizes this notice above the
readable text, including for slash replies and modals; it takes priority over a
session lifecycle notice. The orchestrator logs the incomplete rendering count
without logging formula contents. A 404 from `/math/render` means the running
host controller does not provide this endpoint; updating containers alone does
not update that host process.

The same socket also answers `POST /chat/stream`: newline-delimited JSON with
`{"preview": ...}` lines (the text written so far, at most every 100 ms) and a last line that is
the reply `/chat` would give, or `{"error": ...}`. Ollama and Codex give previews; Claude Code gives
only the last line for now. A preview is never the reply and is not stored.

Bounded adapter regressions and opt-in synthetic live checks are documented in
[chat inference validation](chat-inference-validation.md). A healthy configuration
only establishes registration validity; it does not prove authenticated inference.
Provider errors return fixed messages for recognized session/usage limits,
authentication, unavailable models and incompatible CLIs; unknown failures stay
generic. Check the selected provider's allowance or host authentication before an
explicit retry. The runtime never changes the selected provider automatically or
returns raw provider diagnostics. Rebuild/restart it to adopt diagnostic fixes.

CLI adapters use installed host executables and their existing authentication. Each call runs
from an empty temporary directory with tools disabled, bounded input/output and a 90-second
budget; cancellation kills the process group. The local-model adapter uses the selected lite
stack and requires an already installed model. It does not start the model server or pull models.
The socket exposes only generation and configuration health, never lifecycle or settings writes.

Compatibility: the envelope and `message.*` actions are unchanged. The logical service switch
is now `chat`; saved disabled state from the retired switch is retained on load. Old clients
must be rebuilt to use the new status key. The obsolete standalone runtime module is removed.
Archived profile rows/commands are preserved for operator-data compatibility but cannot choose
live inference. Existing generation environment settings are ignored. Recreate containers to
apply service removal and the socket mount; stop/remove any obsolete container during rollout.
This development change does not perform a production deployment or live provider invocation.

Rollback: restore the prior application/controller builds and Compose files. The separate chat
binding key is ignored by older builds. Preserve the existing database and auth stores; do not
restore a database backup over newer operator changes without reviewing them.


### Discord package-manager startup failures

The Discord images pin pnpm through `packageManager` and prepare it during the
build. The dev/lite image also caches locked dependencies in `/opt/pnpm-store`.
Its startup script installs from that store with `--offline --frozen-lockfile`
before launching the watcher. The production image starts Node directly with its
installed TypeScript loader. Corepack network access is disabled in both images
at runtime; changing manifests or the lockfile requires rebuilding the image.
The dev startup still writes its dependency links under the existing workspace
bind mount, as the previous startup install did. No new volume is introduced.

If an older image fails before gateway login with `EAI_AGAIN` during pnpm release
discovery, rebuild the Discord image from this revision and recreate only that
service using the selected stack. Build-time networking is required. Do not
remove runtime databases or volumes to repair package-manager discovery. An
uncached dependency or changed lockfile now fails locally rather than attempting
a runtime download. This removes the registry bootstrap dependency; Discord
login, media sources, and voice transport still need working network access.


To verify dependency bootstrap without logging in to Discord, build an isolated
review image and run the network-disabled regression:

```sh
docker build -f dokja_services/dokja_discord/Dockerfile.dev -t dokja-discord-startup-test .
bash scripts/test_discord_startup.sh dokja-discord-startup-test
```

The check uses a fresh disposable project, loads TypeScript and voice dependencies,
checks lockfile preservation, and rejects a changed manifest before gateway startup.
It does not recreate application services or test live Discord connectivity.
