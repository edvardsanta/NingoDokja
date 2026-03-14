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

- [router.go](/home/vard/repos/my_repos/read_books/dokja_orch/internal/core/router.go)
- [workflow_engine.go](/home/vard/repos/my_repos/read_books/dokja_orch/internal/core/workflow_engine.go)
- [moderation_domain_handler.go](/home/vard/repos/my_repos/read_books/dokja_orch/internal/handlers/moderation_domain_handler.go)
- [chat_domain_handler.go](/home/vard/repos/my_repos/read_books/dokja_orch/internal/handlers/chat_domain_handler.go)

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

- [meme_domain_handler.go](/home/vard/repos/my_repos/read_books/dokja_orch/internal/handlers/meme_domain_handler.go)
- [domain.go](/home/vard/repos/my_repos/read_books/dokja_domain/dokja_meme/domain.go)

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

- [service.go](/home/vard/repos/my_repos/read_books/dokja_services/dokja_scheduler/scheduler/service.go)
- [system_domain_handler.go](/home/vard/repos/my_repos/read_books/dokja_orch/internal/handlers/system_domain_handler.go)
- [discord_interface_client.go](/home/vard/repos/my_repos/read_books/dokja_orch/internal/clients/discord_interface_client.go)

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

- [system_domain_handler.go](/home/vard/repos/my_repos/read_books/dokja_orch/internal/handlers/system_domain_handler.go)
- [http_bridge_ingress.go](/home/vard/repos/my_repos/read_books/dokja_orch/internal/ingress/http_bridge_ingress.go)

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
