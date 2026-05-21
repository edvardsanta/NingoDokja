<!-- refreshed: 2025-02-13 -->
# Architecture

**Analysis Date:** 2025-02-13

## System Overview

```text
┌─────────────────────────────────────────────────────────────┐
│                      Interfaces                             │
│  `dokja_interfaces/discord`  `dokja_interfaces/cli`        │
└────────┬───────────────────────────────┬────────────────────┘
         │                               │
         ▼                               ▼
┌─────────────────────────────────────────────────────────────┐
│                    Orchestrator                             │
│         `dokja_orch/` (Go)                                  │
│         - Routing, Planning, Dispatching                    │
└────────┬───────────────────────────────┬────────────────────┘
         │                               │
         ▼                               ▼
┌─────────────────────────────────────────────────────────────┐
│                    Domains                                  │
│         `dokja_domain/` (Business Logic)                    │
└────────┬───────────────────────────────┬────────────────────┘
         │                               │
         ▼                               ▼
┌─────────────────────────────────────────────────────────────┐
│                    Services                                 │
│         `dokja_services/` (External Integrations)           │
└─────────────────────────────────────────────────────────────┘
```

## Component Responsibilities

| Component | Responsibility | File |
|-----------|----------------|------|
| Discord Interface | Bot gateway, message listening, voice transport | `dokja_interfaces/discord` |
| CLI Interface | Terminal-native request/reply | `dokja_interfaces/cli` |
| Orchestrator | Event routing, workflow orchestration, dispatching | `dokja_orch` |
| Book Domain | Book interpretation and workflow decisions | `dokja_domain/dokja_book` |
| Chat Domain | Conversation management and response logic | `dokja_domain/dokja_chat` |
| Meme Domain | Meme action mapping and pool logic | `dokja_domain/dokja_meme` |
| Moderation Domain | Content screening and safety rules | `dokja_domain/dokja_moderation` |
| AI Service | OpenAI/LLM integration and text generation | `dokja_services/dokja_chat_ai` |
| Book Service | PDF/Epub processing and summarization | `dokja_services/dokja_book` |

## Pattern Overview

**Overall:** Modular Orchestrator (Event-Driven Architecture)

**Key Characteristics:**
- **Decoupled Interfaces:** Interfaces only know how to emit events and receive replies.
- **Centralized Orchestration:** All complex workflows are coordinated by the Go-based Orchestrator.
- **Pure Domain Logic:** Business rules are isolated from infrastructure in the domain layer.
- **Polyglot Services:** Services are implemented in the most suitable language (mostly Python for AI/ML).

## Layers

**Interfaces:**
- Purpose: Convert external signals (Discord messages, CLI commands) into internal `Event` objects.
- Location: `dokja_interfaces/`
- Contains: Platform-specific SDKs and transport logic.
- Depends on: Orchestrator HTTP/ZMQ endpoints.
- Used by: End users.

**Orchestrator:**
- Purpose: The brain of the system. Maps events to workflows and executes them.
- Location: `dokja_orch/`
- Contains: Router, Workflow Engine, Dispatcher, and Service Clients.
- Depends on: Domain packages (Go) and Service endpoints (HTTP).
- Used by: Interfaces.

**Domains:**
- Purpose: Enforce business rules and decide which service operations to call.
- Location: `dokja_domain/`
- Contains: Decision logic, action mapping, and payload normalization.
- Depends on: Service Port interfaces.
- Used by: Orchestrator Handlers.

**Services:**
- Purpose: Infrastructure and external service integration.
- Location: `dokja_services/`
- Contains: Standalone HTTP servers for specialized tasks.
- Depends on: Third-party APIs, Databases, File System.
- Used by: Orchestrator (via Clients).

## Data Flow

### Primary Request Path (Discord Message)

1. **Interface**: `message_listener.ts` (`dokja_interfaces/discord/messages/message_listener.ts`) captures message and calls `dispatchToOrchestrator`.
2. **Orchestrator Ingress**: `HTTPBridgeIngress` (`dokja_orch/internal/ingress/http_bridge.go`) receives POST request.
3. **Routing**: `RuleBasedEventRouter` (`dokja_orch/internal/core/router.go`) identifies event as `message.created` and routes to `DomainChat`.
4. **Planning**: `DefaultWorkflowEngine` (`dokja_orch/internal/core/workflow_engine.go`) creates a workflow with `generate-response` action.
5. **Execution**: `ChatDomainHandler` (`dokja_orch/internal/handlers/chat_domain_handler.go`) manages session and calls AI Service.
6. **Service**: `OpenAIChatService` (`dokja_services/dokja_chat_ai/openai_chat_service.py`) calls OpenAI API.
7. **Response**: Result flows back through Handler -> Ingress -> Interface for delivery to user.

## Key Abstractions

**Event:**
- Purpose: Common data structure for all system inputs.
- Examples: `dokja_orch/internal/core/event.go`
- Pattern: Normalized Event Object.

**Domain Handler:**
- Purpose: Go interface that bridges the Orchestrator engine to specific domain logic.
- Examples: `dokja_orch/internal/handlers/book_domain_handler.go`
- Pattern: Adapter.

## Entry Points

**Orchestrator (VA):**
- Location: `dokja_orch/cmd/va/main.go`
- Triggers: System start.
- Responsibilities: Initializes all domain handlers, clients, and starts ZeroMQ/HTTP ingress listeners.

**Discord Interface:**
- Location: `dokja_interfaces/discord/main.ts`
- Triggers: Bot startup.
- Responsibilities: Connects to Discord Gateway, registers commands, and listens for events.

## Architectural Constraints

- **Single Orchestrator:** All cross-domain communication must pass through the orchestrator.
- **Stateless Services:** Services should avoid holding session state; state is managed by the orchestrator or domain repositories.
- **Port-Adapter Pattern:** Domains define Port interfaces that Orchestrator implements or injects via Clients.

## Anti-Patterns

### Logic Leakage

**What happens:** Business rules (like "if user is admin") being checked inside the Discord interface.
**Why it's wrong:** Forces duplication of logic across multiple interfaces (CLI would need the same check).
**Do this instead:** Check permissions in the `DomainModeration` or specific Domain handler.

## Error Handling

**Strategy:** Graceful degradation with user feedback.

**Patterns:**
- **Orchestrator Level:** Catches domain errors and returns a normalized error status to the interface.
- **Service Level:** Returns HTTP error codes with descriptive JSON payloads.

## Cross-Cutting Concerns

**Logging:** Centralized Go-based logger (`dokja_orch/internal/logger/`) with correlated event IDs.
**Validation:** Event normalization and validation in `dokja_orch/internal/core/event.go`.
**Authentication:** Managed at the interface level (e.g., Discord token) and passed through context.

---

*Architecture analysis: 2025-02-13*
