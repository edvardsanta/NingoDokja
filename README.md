# Ningo Dokja

Ningo Dokja is being refactored from a monolithic Discord bot into a modular multi-interface AI platform.

The active architecture is:

`Interfaces -> Orchestrator -> Domains -> Services`

## Current Architecture

### Interfaces

- [dokja_interfaces/discord](./dokja_interfaces/discord)
  - Discord messages, slash commands, richer interactions, outbound delivery, and voice/radio
- [dokja_interfaces/cli](./dokja_interfaces/cli)
  - request/reply CLI and chat REPL

### Orchestrator

- [dokja_orch](./dokja_orch)
  - central routing, workflow planning, and domain dispatch
  - ingress paths:
    - `zmq-pubsub`
    - `zmq-reqrep`
    - `http`

### Domains

- [dokja_domain/dokja_chat](./dokja_domain/dokja_chat)
- [dokja_domain/dokja_meme](./dokja_domain/dokja_meme)
- [dokja_domain/dokja_moderation](./dokja_domain/dokja_moderation)

### Services

- [dokja_services/dokja_chat_ai](./dokja_services/dokja_chat_ai)
  - FastAPI chat AI service
- [dokja_services/dokja_meme](./dokja_services/dokja_meme)
  - ZeroMQ meme service wrapping legacy meme logic
- [dokja_services/dokja_scheduler](./dokja_services/dokja_scheduler)
  - scheduled event emitter for meme refresh and scheduled dispatch

## Current Workflows

Implemented workflows include:

- conversation
  - moderation -> chat -> chat AI
- meme
  - fetch, refresh, status
- scheduled meme dispatch
  - scheduler -> orchestrator -> Discord delivery
- system status
  - aggregate chat AI + meme health/status

## Communication

The repo currently uses a pragmatic mix of:

- `zmq-pubsub`
- `zmq-reqrep`
- `http`

When writing `service.yaml`, prefer accurate protocol names such as:

- `http`
- `zmq-pubsub`
- `zmq-reqrep`

## Current State

What is already working:

- Discord interface through the orchestrator
- CLI request/reply and REPL chat
- compact orchestrator responses with optional debug mode
- scheduled meme refresh and scheduled dispatch workflow
- chat sessions with timeout-based revocation
- long Discord replies split safely across multiple messages
- voice/radio commands in the Discord interface

Known rough edges:

- chat history is still in-process, not yet behind a dedicated memory domain/store
- moderation exists but is still a thin domain
- some legacy logic is still reused behind new service boundaries
- `dokja_legacy` remains as reference only

## Important Docs

- [AGENTS.md](./AGENTS.md)
  - architectural rules for coding agents
- [AGENTS.INFO.md](./AGENTS.INFO.md)
  - current project state summary
- [dokja_docs/README.md](./dokja_docs/README.md)
  - architecture and behavior docs

Key project docs:

- [events-vs-requests.md](./dokja_docs/events-vs-requests.md)
- [workflows.md](./dokja_docs/workflows.md)
- [discord-interface.md](./dokja_docs/discord-interface.md)
- [production-runbook.md](./dokja_docs/production-runbook.md)
- [domain-responsibilities.md](./dokja_docs/domain-responsibilities.md)

## Development

### Run Tests

Central test runner:

```bash
./scripts/test_all.sh
```

Reports are written to:

- [test_reports/latest/summary.md](./test_reports/latest/summary.md)
- [test_reports/latest/summary.json](./test_reports/latest/summary.json)

### Dev Stack

Use:

```bash
docker compose -f docker-compose.dev.yml up -d
```

Main stack files:

- [docker-compose.dev.yml](./docker-compose.dev.yml)
- [docker-compose.prod.yml](./docker-compose.prod.yml)
- [.env](./.env)
- [.env.prod](./.env.prod)

## Legacy Areas

- [dokja_lab](./dokja_lab)
  - still exists as a legacy runtime/reference area
- [dokja_legacy](./dokja_legacy)
  - reference only unless explicitly needed

New work should prefer the modular architecture instead of extending those legacy paths directly.
