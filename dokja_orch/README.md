# Dokja Orchestrator

`dokja_orch` is the central coordination layer of Dokja.

It receives interface traffic, normalizes events, plans workflows, dispatches domain actions, and returns compact results. It is the place where cross-domain behavior lives; business rules still belong in `dokja_domain`, and infrastructure adapters still belong in `dokja_services` or `dokja_interfaces`.

## Role in the architecture

The intended flow is:

`Interface -> Orchestrator -> Domain -> Service`

In practice, this project currently handles:

- book classification and summary planning flows
- real-time chat requests
- meme request/reply flows
- scheduled meme dispatch workflows
- system/status aggregation
- domain sequencing such as moderation before chat

## Runtime interfaces

The orchestrator currently exposes three ingress paths:

- `zmq-pubsub`
  - event ingress
  - default endpoint: `tcp://127.0.0.1:5555`
  - topic: `events`
- `zmq-reqrep`
  - synchronous request/reply ingress
  - default endpoint: `tcp://*:5558`
- `http`
  - bridge ingress for Discord and other HTTP-facing adapters
  - default endpoint: `:8091`
  - route: `POST /orchestrator`

## Main workflows

### Conversation

Triggered by:

- `message.created`

Route:

- workflow: `conversation`
- domains: `moderation`, `chat`

Behavior:

- moderation screens the incoming message
- chat builds/continues the conversation session
- chat forwards full message history to `dokja-chat-ai`
- reply is normalized before returning to the interface

### Book

Triggered by:

- `book.summary.requested`
- `book.resource.classify`

Route:

- workflow: `book`
- domains: `book`

Behavior:

- delegates to the book domain
- book domain calls the specialized HTTP book service
- service classifies resource format and book type
- service selects context compaction and summarization route
- when extracted text is already available, service returns a structured initial summary

### Meme

Triggered by:

- `meme.fetch`
- `meme.pool.refresh`
- `meme.status`

Route:

- workflow: `meme`
- domains: `meme`

Behavior:

- delegates to the meme domain
- meme domain calls the ZeroMQ meme service

### Scheduled Meme Dispatch

Triggered by:

- `meme.dispatch.scheduled`

Route:

- workflow: `meme-dispatch`
- domains: `system`

Behavior:

- fetches memes from `dokja-meme`
- formats outbound content
- delivers to the Discord interface delivery endpoint
- `DISCORD_SCHEDULED_MEME_CHANNEL_ID` accepts a comma-separated list; every meme goes to every channel, and a failing channel is skipped for the rest of the run without blocking the others (the error is still returned)
- `DISCORD_SAFE_ONLY_CHANNEL_IDS` (comma-separated, subset of the meme channels) marks channels that only receive memes the NSFW screen approved; every other channel receives everything. A meme with no NSFW label counts as unsafe there. Skipped memes are reported as `skipped_unsafe`.

### System / Ningo Status

Triggered by:

- `ningo.status`

Route:

- workflow: `ningo`
- domains: `system`

Behavior:

- checks `dokja-chat-ai` health
- checks `dokja-meme` status
- returns an aggregated compact status reply

## Package layout

- `cmd/va`
  - application entrypoint and dependency wiring
- `internal/app`
  - lifecycle and coordinated startup/shutdown
- `internal/core`
  - event model, routing, context building, workflow planning, dispatch orchestration
- `internal/handlers`
  - orchestrator-facing adapters for system, chat, meme, moderation, and placeholder domains
- `internal/clients`
  - downstream service/interface clients
- `internal/ingress`
  - ZeroMQ and HTTP ingress adapters
- `internal/chat`
  - chat-specific orchestration helpers
- `internal/logger`
  - shared orchestrator logging

## Important environment variables

- `VA_ZMQ_ENDPOINT`
  - event ingress endpoint
- `VA_ZMQ_TOPIC`
  - event topic, usually `events`
- `VA_ZMQ_REQUEST_ENDPOINT`
  - request/reply ingress endpoint
- `VA_HTTP_ENDPOINT`
  - HTTP bridge bind address
- `VA_RESPONSE_MODE`
  - `compact` by default, `debug`/`verbose` for full envelopes
- `CHAT_AI_SERVICE_ENDPOINT`
  - downstream chat AI service base URL
- `MEME_SERVICE_ENDPOINT`
  - downstream ZeroMQ meme service endpoint
- `BOOK_SERVICE_ENDPOINT`
  - downstream HTTP book service base URL
- `DISCORD_INTERFACE_ENDPOINT`
  - downstream Discord delivery endpoint
- `DISCORD_SCHEDULED_MEME_CHANNEL_ID`
  - scheduled meme delivery target
- `DOKJA_CHAT_SESSION_TIMEOUT`
  - conversation session timeout, default `3m`

## Running locally

From `dokja_orch`:

```bash
go test ./...
```

Or with the repo dev stack:

```bash
docker compose -f ../docker-compose.dev.yml up -d dokja-orchestrator
docker compose -f ../docker-compose.dev.yml logs -f dokja-orchestrator
```

## Response modes

The request ingress and HTTP bridge support:

- compact mode
  - default
  - returns `event_id`, `workflow`, optional `domain`, and `result`
- debug / verbose mode
  - controlled by `VA_RESPONSE_MODE`
  - returns the full orchestration envelope

## Current limitations

- chat session history is still held in-process inside the orchestrator
- `memory` and `automation` domains are still placeholder/logging paths
- book extraction by PDF / EPUB / MOBI is still a staged service concern; the first version focuses on classification and summary planning
- the HTTP bridge contains some Discord-specific command mapping that should stay narrow
- scheduled delivery currently targets Discord specifically through the interface delivery endpoint

### Admin events

- `meme.list` browses the pool without consuming it (`scope`, `limit`, `offset`).
- `meme.screen` runs the NSFW screen on an image (`url`, optional `caption`).
- `discord.send` posts to configured channels only. An image bound for a channel in
  `DISCORD_SAFE_ONLY_CHANNEL_IDS` is screened first and skipped if flagged or if the screen fails.
  `mark_sent: true` also flags the meme as sent so the scheduler does not repeat it.
- `ningo.status` also reports the delivery channels (`meme`, `safe_only`).

### Switches (services and jobs)

Operator switches live in `VA_STATE_FILE` (a JSON file; `/data/dokja_state.json` in prod, where the
orchestrator and scheduler share the `dokja-data` volume). They are read by all three ingresses, so access is
guarded, and they survive restarts. A corrupt file starts every job paused rather than resuming posting.

- `services.set` (`name`, `enabled`): `meme`, `chat_ai`, `book` or `scheduler` only. A disabled service makes the
  orchestrator answer its events with `{"skipped": true, "reason": ...}` (not an error), never runs a
  handler for them, and does not probe them in `ningo.status`. `discord.send` also stops using the meme
  screen, so images bound for a safe-only channel are refused.
- `scheduler.jobs.set` (`name`, `enabled` and/or `interval`): pauses a job by name, matched on the
  `schedule` stamp the scheduler puts in `Context`. Manual runs carry no stamp and are not blocked by a
  pause. Intervals are bounded to 1m..720h and picked up by the scheduler from the same state file.
- `scheduler.jobs.announce`: sent by the scheduler about once a minute with each job's interval and next run.
- `ningo.status` now returns `services` (with `enabled`), `jobs` (state, interval, last/next run) and `channels`.

The request port (`5558`) has no authentication and is published on every interface, so anything that can
reach it can flip these switches. Bind it to localhost (`127.0.0.1:5558:5558`) if the host is shared.

### Chat profiles

`DOKJA_DB_FILE` points at the shared SQLite database (`dokja_store`). The orchestrator opens it to list and
select chat provider profiles and can do nothing else with them:

- `chat.profiles.list`: profiles with base URL, model and a masked key hint (`…abcd`), plus the active one.
  `ningo.status` carries the same summary as `chat_profiles`.
- `chat.profile.use` (`name`): selects an existing profile.

There is no event that creates or edits a profile, and no query in this process selects the `api_key` column, so
a token cannot come out of the orchestrator (a test marshals every response and looks for it). Without
`DOKJA_DB_FILE` these events answer with a configuration error and the chat service keeps its environment settings.
