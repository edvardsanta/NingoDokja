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

## Knowledge Reading Workflow

Used for:

- `knowledge.read`

Route:

- workflow: `knowledge`
- domains:
  - `knowledge`

Actions:

- `knowledge.read` -> `read-knowledge`

Purpose:

- read a file on demand through the reading host (`feeds.extract`, the enabled
  `document.extract.v1` plugin for its extension)
- store each document it yields through `knowledge.ingest`, with `file:<name>` as the
  source and `format:<ext>` and `reader:<plugin>` tags as provenance
- run as a typed task (`read`, `ingest`, then `classify`); the file and its text stay
  outside the workspace, which records a hashed reference and the reader's version
- classify the documents read whose form or content kind nobody decided
  through the enabled `document.classify.v1` plugin (`feeds.classify`), whatever their
  form, until a classifier has seen their current content; a failed or timed-out
  classification is retried on the next reread

The payload is `filename` and `content_b64`, plus the optional `title`, `kind`, `tags`,
`source_id` and `source_ref` that `knowledge.ingest` takes. A failure names its step
reason: `reading_unavailable`, `reading_ambiguous`, `reading_timeout`, `reading_failed`,
`reading_unreadable`, `reading_invalid`, `reading_unsupported`,
`reading_host_unavailable` or `knowledge_ingest_failed`. It is refused while `feeds` is
switched off. `knowledge.ingest` with a file still extracts inside Knowledge, unchanged.
Classification never fails the reading: its outcome is the reply's `classification`
(`candidates`, `classified`, `classifier`, and a `reason` such as
`classifier_not_configured`, `classifier_unavailable`, `classifier_timeout`,
`classifier_failed`, `classifier_invalid_result` or `knowledge_classify_failed`), and
undecided axes stay questions for the user.

Current files:

- [knowledge_reading.go](../dokja_orch/internal/handlers/knowledge_reading.go)
- [reading.go](../dokja_domain/dokja_knowledge/reading.go)
- [feeds_service_client.go](../dokja_orch/internal/clients/feeds_service_client.go)
- [extract.go](../dokja_services/dokja_feeds/feeds/extract.go)
- [reading_classification.go](../dokja_domain/dokja_knowledge/reading_classification.go)
- [classify.go](../dokja_services/dokja_feeds/feeds/classify.go)

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
