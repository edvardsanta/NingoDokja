# Dokja Domain

This folder is the explicit domain layer for the Dokja architecture.

Flow:

`Interfaces -> Orchestrator -> dokja_domain -> dokja_services`

Domains are where business rules live.

What belongs here:

- workflow/business decisions
- domain-specific action handling
- normalization of service results into domain semantics
- rules that should not live in interfaces or service adapters

What does not belong here:

- Discord, CLI, HTTP, or other interface code
- transport details such as ZeroMQ sockets or FastAPI routing
- low-level third-party API integration logic

Current domains:

- `dokja_chat`
  - conversation/session logic
  - chat history coordination for AI requests
- `dokja_meme`
  - meme business actions such as fetch, refresh, and status
- `dokja_moderation`
  - moderation decisions for inbound message flows

The orchestrator should call these domains.
The domains should call services.
