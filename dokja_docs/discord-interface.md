# Discord Interface

This document describes the current Dokja Discord interface.

The Discord interface is implemented in:

- [dokja_interfaces/discord](../read_books/dokja_interfaces/discord)

Primary flow:

`Discord -> Interface -> Orchestrator -> Domain -> Service -> Orchestrator -> Interface -> Discord`

## What Discord Can Do

Current capabilities:

- plain text message forwarding
- slash commands
- buttons
- select menus
- modals
- internal outbound message delivery for orchestrator-driven sends

## Inbound Flows

### Plain Messages

The interface receives a Discord message, applies channel/DM filters, and forwards the content to the orchestrator as `message.created`.

Relevant files:

- [message_listener.ts](../read_books/dokja_interfaces/discord/message_listener.ts)
- [bridge.ts](../read_books/dokja_interfaces/discord/bridge.ts)

### Slash Commands

Current commands:

- `/chat`
- `/meme_fetch`
- `/meme_status`
- `/meme_refresh`
- `/ningo_panel`
- `/chat_modal`

Relevant files:

- [commands.ts](../read_books/dokja_interfaces/discord/commands.ts)
- [interactions.ts](../read_books/dokja_interfaces/discord/interactions.ts)

### Rich Interactions

Current interaction types:

- buttons
- select menus
- modals

Examples:

- `Ningo Status`
- `Fetch Meme`
- `Chat Prompt`
- meme count select
- chat modal submit

Relevant files:

- [interactions.ts](../read_books/dokja_interfaces/discord/interactions.ts)
- [carbon_runtime.ts](../read_books/dokja_interfaces/discord/carbon_runtime.ts)

## Outbound Delivery

The Discord interface also exposes an internal delivery endpoint for orchestrator-driven sends.

Endpoint:

- `POST /deliver`

Used for:

- scheduled meme delivery

Contract:

```json
{
  "channel_id": "123",
  "content": "Meme title",
  "attachment_url": "https://example.com/meme.jpg"
}
```

Rules:

- `channel_id` is required
- at least one of `content` or `attachment_url` is required
- when `attachment_url` exists, the interface downloads the file and uploads it to Discord

Response:

```json
{
  "status": "ok",
  "channel_id": "123",
  "message_id": "456",
  "has_attachment": true
}
```

Relevant files:

- [delivery_server.ts](../read_books/dokja_interfaces/discord/delivery_server.ts)
- [discord_interface_client.go](../read_books/dokja_orch/internal/clients/discord_interface_client.go)

## Runtime Boundaries

Dokja-specific app code uses local runtime abstractions instead of depending on Carbon types everywhere.

Main local boundaries:

- [types.ts](../read_books/dokja_interfaces/discord/types.ts)
- [carbon_runtime.ts](../read_books/dokja_interfaces/discord/carbon_runtime.ts)
- [app.ts](../read_books/dokja_interfaces/discord/app.ts)

This keeps Carbon as a transport/runtime dependency, not the owner of Dokja application structure.

## Important Constraints

- Discord does not call `dokja_services` directly
- Discord does not decide workflows
- Discord should emit orchestrator-friendly events and render orchestrator results
- scheduled outbound delivery is initiated by the orchestrator, not by the scheduler directly
