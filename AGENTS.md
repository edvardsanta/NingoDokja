This file provides instructions for AI coding agents (Codex, Cursor, etc.) working in this repository.

The goal of this project is to migrate a monolithic Discord bot into a modular multi-interface AI platform using an orchestrator architecture.

Agents must follow the architectural rules defined here.

---

# Project Goal

Refactor the existing Discord bot (written in Go) into a distributed architecture with:

Interfaces → Orchestrator → Domains → Services

The system must support multiple interfaces:

- Discord
- WhatsApp
- CLI
- Web
- API

Interfaces must be decoupled from business logic.

---

# Target Architecture

```
Interfaces
├─ Discord
├─ WhatsApp
├─ CLI
├─ Web
└─ API
    │
    ▼
Orchestrator
├─ Event Router
├─ Context Builder
├─ Workflow Engine
└─ Domain Dispatcher
    │
    ▼
Domains
├─ Chat Domain
├─ Memory Domain
├─ Automation Domain
├─ Moderation Domain
    │
    ▼
Service
├─ LLM
├─ Vector DB
├─ SQL DB
├─ Plugins
└─ External APIs
```

# Architectural Rules

Agents must follow these rules:

1. Interfaces MUST NOT call services directly.
2. Interfaces only emit events.
3. The orchestrator decides routing and workflows.
4. Domains contain business logic.
5. Services are stateless integrations.

Correct flow:
Interface → Event → Orchestrator → Domain → Service

# Communication Protocol

All components communicate via ZeroMQ.

Event format:

```json
{
  "event_id": "uuid",
  "timestamp": "ISO8601",
  "source": "discord | whatsapp | web",
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


# Interfaces

Interfaces are responsible only for:

- receiving external messages
- converting them into events
- sending events to the orchestrator

# Orchestrator

The orchestrator is the central brain of the system.

- receive events
- build context
- route to domains
- coordinate workflows

# Domains

Domains encapsulate business logic.

- Domains are independent services.
- Domains communicate only with the orchestrator.
- Domains may use different programming languages.

# Services

Services provide integrations and infrastructure.
Domains call services when needed.

# Folder Structure

```
ningo-dokja/
│
├── dokja_interfaces/
│
├── dokja_orch/
│
├── dokja_domain/
│
├── dokja_services/
│
├── shared/
│
├── deployments/
│
├── scripts/
│
├── dokja_docs/
```

