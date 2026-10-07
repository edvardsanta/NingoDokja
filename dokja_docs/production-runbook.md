# Production Runbook

This document is the practical production checklist for Dokja.

Main files:

- [docker-compose.prod.yml](../docker-compose.prod.yml)
- [.env.prod](../.env.prod)

## Services

Production stack:

- `dokja-orchestrator`
- `dokja-meme`
- `dokja-chat-ai`
- `dokja-knowledge` (research knowledge base) and `dokja-ollama` (its embedding server)
- `dokja-memory` (experience memory: what the bot did in a context and how it turned out)
- `dokja-feeds` (follows the sources the owner chooses, as plugins kept outside the repository)
- `dokja-discord`
- `dokja-scheduler`

## Required Secrets

Before starting production, replace placeholder values in [.env.prod](../.env.prod):

- `CHAT_AI_API_KEY`
- `CHAT_AI_BASE_URL`
- `DISCORD_BOT_TOKEN`
- `DISCORD_APPLICATION_ID`

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
docker compose -f docker-compose.prod.yml --env-file .env.prod logs -f dokja-chat-ai
docker compose -f docker-compose.prod.yml --env-file .env.prod logs -f dokja-scheduler
docker compose -f docker-compose.prod.yml --env-file .env.prod logs -f dokja-meme
```

Things to confirm:

- orchestrator starts both ZeroMQ and HTTP ingress
- Discord bot logs in successfully
- chat AI `/health` responds internally
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

The orchestrator, scheduler and chat service share the `dokja-data` volume, mounted at `/data`:

- `/data/dokja_state.json` (`VA_STATE_FILE` in the orchestrator, `DOKJA_STATE_FILE` in the scheduler):
  which services and scheduler jobs an operator switched off and any job interval overrides. The
  orchestrator writes it; the scheduler only reads the intervals. A corrupt file starts every job paused.
- `/data/dokja.db` (`DOKJA_DB_FILE`): SQLite database with the chat provider profiles (base URL, model
  and token). Keep the volume private: the token is stored in plain text.

Without a writable volume both files would be lost on every deploy.

Startup still emits every scheduled job once, but the orchestrator skips the ones an operator paused, so a
restart never posts something that was switched off. `dokja-cli services list` and `dokja-cli jobs list` show
the current state.

The status probe covers `chat_ai`, `meme`, `book`, `knowledge` and `memory`. A stack that does not run one of
them (`docker-compose.prod.yml` has no book service) shows it as `error` and the platform as `degraded` until you switch it off (`dokja-cli services disable <name>` or
`space` in the TUI); a switched-off service is not probed.

Chat profiles are created and removed on the machine that holds the database (`dokja-cli chat profile add`
with `DOKJA_DB_FILE` pointing at it, or `p` in the TUI panel), never through the orchestrator. The
orchestrator can only list them (masked) and select one, and the chat service picks the selection up on its
next request.

## Research Knowledge Base

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

A plugin runs only when its manifest says `"enabled": true`, and changes to the directory are read when the
service starts. Its program runs inside the `dokja-feeds` container, so it must exist in the image: the image
has a POSIX shell and the usual command-line tools, and no Python. For anything else, extend the image or put
a static binary in the plugin directory. A manifest whose program is missing shows as `invalid` with the
program's name in the status.

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
meme's text is compared to every tagged example and, if the closest one is similar enough, its hashtag is
suggested. `dokja-cli meme hashtag tag|suggest|list|untag`; see `dokja_services/dokja_meme/README.md`.

Embeddings use the same configured server and model as the knowledge service. If the model is
missing, provision it once (see "Research Knowledge Base" above): the meme service works without it too, it just
stores tagged text without a vector and `meme.hashtag.suggest` reports `degraded` instead of guessing.
`MEME_HASHTAG_MIN_SCORE` (default `0.6`) decides which suggestion counts as relevant; like the knowledge
base's threshold, it needs validation on representative examples, so retune it once there is a real set of tagged
memes. Its SQLite file is `MEME_HASHTAG_DB_FILE`, separate from `MEME_SERVICE_DB_FILE`.

The orchestrator appends relevant suggestions to manual and scheduled deliveries, and notes each one in the
experience memory (see "Experience Memory" above).
Missing OCR text, unsupported video input, low similarity or a classifier error leaves
the original message intact. Disabling the meme service also disables automatic suggestions
for manual delivery. Channel validation and safe-only screening remain in effect.

The TUI supports learning (`h`), suggestion (`g`), tagged examples (`l`) and removal (`u`). After `g` the
selected meme's panel shows why: the closeness of the closest tagged meme, the text read from this meme and
the text of that one. `dokja-cli meme hashtag suggest` prints the same fields (`score`, `query_text`,
`matched_text`).
Each suggestion recovers up to 32 examples without embeddings or from another model.
After the embedding model becomes available, subsequent suggestions progressively
recover the backlog. Corrections replace the example for the same URL; automatic
suggestions are not saved as training examples.
Back up the hashtag database alongside the meme pool. Rebuild the meme service and
orchestrator and update the operator interfaces to enable the feature; no new ports.

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
- `DISCORD_GUILD_ID`
- `DISCORD_ALLOWED_CHANNELS`
- `DISCORD_SCHEDULED_MEME_CHANNEL_ID`

### Chat AI issues

Check:

- `CHAT_AI_API_KEY`
- `CHAT_AI_BASE_URL`
- `CHAT_AI_MODEL`
- the selected chat provider profile (see "Operator state and shared database"); it wins over the
  `CHAT_AI_*` variables, and `GET /health` on the chat service reports which one is in use

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

- Set `DOKJA_COMMUNICATIONS_TOKEN` to a randomly generated value of at least 32 characters in the
  desktop shell, orchestrator and service. Empty, short and `CHANGE_ME` credentials disable the
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
