# Events vs Requests

This project uses the same orchestration model for both `events` and `requests`, but the transport semantics are different.

## Core Difference

- `event`: asynchronous input to the orchestrator
- `request`: synchronous input to the orchestrator that expects an immediate response

In both cases, the payload can still be represented as an orchestrator event envelope:

```json
{
  "event_id": "uuid",
  "timestamp": "ISO8601",
  "source": "cli | discord | web | api",
  "type": "message.created",
  "user": {
    "id": "string",
    "name": "string"
  },
  "channel": {
    "id": "string"
  },
  "payload": {},
  "context": {}
}
```

The difference is how the caller interacts with the orchestrator.

## Event Flow

Event flow is fire-and-forget.

Pattern:

`Interface -> publish -> Orchestrator -> Domain -> Service`

Properties:

- caller does not wait for a result
- useful for background or decoupled workflows
- good for scheduled work, notifications, and side effects

Current transport:

- ZeroMQ pub/sub ingress
- orchestrator subscriber: [dokja_orch/internal/ingress/zmq_ingress.go](../dokja_orch/internal/ingress/zmq_ingress.go)

Current example in the repo:

- generic CLI emit command: [dokja_interfaces/cli/cli/app.go](../dokja_interfaces/cli/cli/app.go)

Typical future uses:

- scheduled meme refresh
- automation triggers
- moderation side effects
- memory synchronization

## Request Flow

Request flow is synchronous.

Pattern:

`Interface -> request -> Orchestrator -> Domain -> Service -> response`

Properties:

- caller waits for an immediate result
- useful for interactive interfaces
- best for chat, fetch-now actions, and status reads

Current transport:

- ZeroMQ request/reply ingress
- orchestrator request ingress: [dokja_orch/internal/ingress/zmq_request_ingress.go](../dokja_orch/internal/ingress/zmq_request_ingress.go)

Current examples in the repo:

- CLI chat
- CLI meme fetch
- CLI meme status
- CLI meme refresh

Relevant files:

- [dokja_interfaces/cli/cli/requester.go](../dokja_interfaces/cli/cli/requester.go)
- [dokja_interfaces/cli/cli/app.go](../dokja_interfaces/cli/cli/app.go)

## Practical Rule

Use `request` when the interface cannot continue without the answer.

Use `event` when eventual processing is acceptable.

Examples:

- `message.created` from a real-time CLI chat session: request
- `meme.fetch` from a user command: request
- `meme.pool.refresh` from a scheduler: event
- `memory.sync` after a conversation: event

## Why Both Exist

The orchestrator is the same decision layer in both cases:

- route event types
- build context
- plan workflows
- dispatch to domains

What changes is the interaction contract:

- `event` means "process this"
- `request` means "process this and return the result now"

That distinction is important for interface design:

- CLI chat should use requests
- background jobs should prefer events
- interfaces must not call services directly
