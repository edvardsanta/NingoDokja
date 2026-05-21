# Codebase Structure

**Analysis Date:** 2025-02-13

## Directory Layout

```
[project-root]/
├── dokja_orch/         # Go-based central orchestrator (the system brain)
├── dokja_domain/       # Business logic and domain rules (Go & Python)
├── dokja_services/     # Microservices for external integrations (Python)
├── dokja_interfaces/   # Interface adapters (Discord, CLI)
├── dokja_lab/          # Experimental laboratory and legacy Python components
├── dokja_legacy/       # Deprecated Go implementation
├── dokja_docs/         # Architectural documentation and runbooks
├── scripts/            # Development and deployment utility scripts
├── test/               # Integration tests and test data
└── Dockerfile          # Root deployment configuration
```

## Directory Purposes

**dokja_orch/:**
- Purpose: The central routing and workflow execution engine.
- Contains: Go source code for event routing, workflow planning, and service orchestration.
- Key files: `cmd/va/main.go`, `internal/core/router.go`, `internal/core/workflow_engine.go`.

**dokja_domain/:**
- Purpose: Pure business logic separated by domain concerns.
- Contains: Subdirectories for each domain (book, chat, meme, moderation) with their respective rules.
- Key files: `dokja_book/domain.go`, `dokja_chat/domain.py`, `dokja_meme/domain.go`.

**dokja_services/:**
- Purpose: Stateless microservices that handle IO-heavy or specialized tasks.
- Contains: Standalone Python services for AI, Voice, Book processing, etc.
- Key files: `dokja_chat_ai/server.py`, `dokja_book/server.py`, `dokja_voice/server.py`.

**dokja_interfaces/:**
- Purpose: Entry points for users to interact with the system.
- Contains: Discord bot (TypeScript/Node.js) and CLI tool (Go).
- Key files: `discord/main.ts`, `cli/cmd/dokja-cli/main.go`.

**dokja_lab/:**
- Purpose: Development sandbox and legacy Python dispatcher.
- Contains: Various experimental modules, ETL processes, and old route handlers.
- Key files: `main.py`, `scheduler.py`, `handlers/`.

## Key File Locations

**Entry Points:**
- `dokja_orch/cmd/va/main.go`: Main orchestrator entry.
- `dokja_interfaces/discord/main.ts`: Discord bot entry.
- `dokja_interfaces/cli/cmd/dokja-cli/main.go`: CLI entry.

**Configuration:**
- `docker-compose.dev.yml`: Development stack configuration.
- `dokja_orch/service.yaml`: Orchestrator service definition.
- `dokja_services/*/service.yaml`: Microservice definitions.

**Core Logic:**
- `dokja_orch/internal/core/`: Orchestration engine logic.
- `dokja_domain/`: Shared domain logic.

**Testing:**
- `test/`: Global integration tests.
- `dokja_orch/internal/**/*_test.go`: Go unit/integration tests.
- `dokja_interfaces/discord/tests/`: TypeScript tests.

## Naming Conventions

**Files:**
- Go: `snake_case.go` (e.g., `event_router.go`)
- Python: `snake_case.py` (e.g., `openai_chat_service.py`)
- TypeScript: `snake_case.ts` or `kebab-case.ts` (mostly `snake_case.ts` observed)

**Directories:**
- Sub-modules: `snake_case` (e.g., `dokja_chat_ai`)

## Where to Add New Code

**New Feature (Cross-domain):**
1. Define actions in `dokja_domain/` (Go or Python).
2. Register event route in `dokja_orch/internal/core/router.go`.
3. Add workflow step in `dokja_orch/internal/core/workflow_engine.go`.
4. Implement handler in `dokja_orch/internal/handlers/`.

**New Service Integration:**
1. Create new directory in `dokja_services/`.
2. Implement HTTP server (usually Flask/FastAPI in Python).
3. Create client in `dokja_orch/internal/clients/`.

**New User Interface:**
1. Create new directory in `dokja_interfaces/`.
2. Implement bridge to Orchestrator (HTTP POST to orchestrator endpoint).

## Special Directories

**dokja_lab/:**
- Purpose: Sandbox for new features. Code here is often moved to `dokja_domain` or `dokja_services` once stable.
- Generated: No
- Committed: Yes

**dokja_legacy/:**
- Purpose: Contains the old monolithic Go implementation. Referenced for logic migration but not for new features.

---

*Structure analysis: 2025-02-13*
