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
