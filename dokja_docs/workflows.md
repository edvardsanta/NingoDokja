# Workflows

This document describes the real orchestrator workflows currently implemented in Dokja.

The orchestrator flow is:

`Interface -> Orchestrator -> Domain -> Service`

The workflow engine decides which domains run and which action each domain receives.

## Conversation Workflow

Used for:

- `message.created`

Route:

- workflow: `conversation`
- domains:
  - `moderation`
  - `chat`

Actions:

- moderation: `screen-event`
- chat: `generate-response`

Purpose:

- inspect an incoming real-time message
- generate an AI reply if the message continues through the workflow

Current files:

- [router.go](../dokja_orch/internal/core/router.go)
- [workflow_engine.go](../dokja_orch/internal/core/workflow_engine.go)
- [moderation_domain_handler.go](../dokja_orch/internal/handlers/moderation_domain_handler.go)
- [chat_domain_handler.go](../dokja_orch/internal/handlers/chat_domain_handler.go)

## Meme Workflow

Used for:

- `meme.fetch`
- `meme.pool.refresh`
- `meme.status`

Route:

- workflow: `meme`
- domains:
  - `meme`

Actions:

- `meme.fetch` -> `fetch-memes`
- `meme.pool.refresh` -> `refresh-meme-pool`
- `meme.status` -> `inspect-meme-service`

Purpose:

- fetch memes for interactive use
- refresh the meme pool in storage
- inspect meme service health/state

Current files:

- [meme_domain_handler.go](../dokja_orch/internal/handlers/meme_domain_handler.go)
- [domain.go](../dokja_domain/dokja_meme/domain.go)

## Book Workflow

Used for:

- `book.summary.requested`
- `book.resource.classify`

Route:

- workflow: `book`
- domains:
  - `book`

Actions:

- `book.summary.requested` -> `summarize-book`
- `book.resource.classify` -> `classify-book-resource`

Purpose:

- classify book resource format and book type
- select extraction and context compaction strategy
- generate an initial structured summary when extracted text is already available

Current files:

- [book_domain_handler.go](../dokja_orch/internal/handlers/book_domain_handler.go)
- [domain.go](../dokja_domain/dokja_book/domain.go)
- [book_service_client.go](../dokja_orch/internal/clients/book_service_client.go)

## Scheduled Meme Dispatch Workflow

Used for:

- `meme.dispatch.scheduled`

Route:

- workflow: `meme-dispatch`
- domains:
  - `system`

Action:

- system: `deliver-scheduled-memes`

Purpose:

- fetch memes through the meme service
- deliver them through the Discord interface
- keep scheduler interface-agnostic

Important rule:

- the scheduler only emits the event
- the orchestrator owns the delivery workflow

Current files:

- [service.go](../dokja_services/dokja_scheduler/scheduler/service.go)
- [system_domain_handler.go](../dokja_orch/internal/handlers/system_domain_handler.go)
- [discord_interface_client.go](../dokja_orch/internal/clients/discord_interface_client.go)

## System Status Workflow

Used for:

- `ningo.status`

Route:

- workflow: `ningo`
- domains:
  - `system`

Action:

- system: `inspect-ningo-platform`

Purpose:

- inspect orchestrated platform status in one request
- aggregate service-level checks like chat AI and meme state

Current files:

- [system_domain_handler.go](../dokja_orch/internal/handlers/system_domain_handler.go)
- [http_bridge_ingress.go](../dokja_orch/internal/ingress/http_bridge_ingress.go)

## Response Shape

By default, the orchestrator returns a compact response.

Example:

```json
{
  "event_id": "uuid",
  "workflow": "meme",
  "domain": "meme",
  "result": {}
}
```

Verbose response mode is enabled with:

```bash
VA_RESPONSE_MODE=debug
```

That returns the full orchestration envelope instead of the compact runtime result.
