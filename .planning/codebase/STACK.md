# Technology Stack

**Analysis Date:** 2025-02-23

## Languages

**Primary:**
- Python 3.13 - Used in `dokja_services/` (book, chat_ai, meme, news, voice) and `dokja_lab/`.
- Go 1.25.0 - Used in `dokja_orch/` (orchestrator), `dokja_legacy/`, and `dokja_scheduler/`.

**Secondary:**
- TypeScript/Node.js - Used in `dokja_interfaces/discord/` for the Discord bot interface.
- SQL (SQLite) - Used for local data persistence in `dokja_services/dokja_meme/` and `dokja_services/dokja_news/`.

## Runtime

**Environment:**
- Docker - Containerized services defined in `docker-compose.dev.yml` and `docker-compose.prod.yml`.
- Linux (Ubuntu-based Docker images) - Primary deployment target.

**Package Manager:**
- pip - Python dependencies managed via `requirements.txt` and `pyproject.toml`.
- pnpm - Node.js dependencies managed via `package.json` in `dokja_interfaces/discord/`.
- go mod - Go dependencies managed via `go.mod` in `dokja_orch/`, `dokja_legacy/`, etc.
- Lockfile: present (`go.sum` for Go, `package.json` for Node.js - though `pnpm-lock.yaml` wasn't explicitly read, it's implied by pnpm usage).

## Frameworks

**Core:**
- FastAPI - Python web framework used for chat AI and voice services.
- Flask - Python web framework used in `dokja_lab/`.
- Fiber (Go) - Web framework used for the orchestrator API in `dokja_orch/`.
- Discord.js - Node.js library for interacting with the Discord API.

**Testing:**
- Pytest - Python testing framework (`dokja_lab/requirements.txt`).
- Testify - Go testing toolkit (`dokja_orch/go.mod`).
- Node:test - Native Node.js test runner used in `dokja_interfaces/discord/`.

**Build/Dev:**
- Docker Compose - Orchestrates multi-container development and production environments.
- Air - Live reload for Go applications during development (`docker-compose.dev.yml`).
- tsx - TypeScript execution and watch mode for Node.js.

## Key Dependencies

**Critical:**
- ZeroMQ (pyzmq / pebbe/zmq4) - Primary inter-service communication protocol (PubSub and Req/Rep).
- Discordgo / Discord.js - Integration with Discord for user interaction.
- OpenAI SDK - Interface for large language models.
- Transformers (HuggingFace) - Used for NLP tasks, image captioning, and voice STT/TTS.

**Infrastructure:**
- Redis - Used for caching and potentially state management.
- SQLite - Local database for news and memes.
- APScheduler / robfig/cron - Scheduling tasks in Python and Go respectively.

## Configuration

**Environment:**
- Environment variables - Managed via `.env` files and Docker Compose environment blocks.
- Viper (Go) - Configuration management for Go services.

**Build:**
- Dockerfiles - Located in service subdirectories (e.g., `dokja_orch/Dockerfile.dev`, `dokja_services/dokja_book/Dockerfile.dev`).
- `docker-compose.dev.yml` and `docker-compose.prod.yml` - Root-level orchestration configs.

## Platform Requirements

**Development:**
- Docker and Docker Compose
- Python 3.13, Go 1.25, Node.js
- ZeroMQ development libraries (libzmq)

**Production:**
- Docker Swarm or Kubernetes (implied by containerization)
- Access to external APIs (OpenAI, Discord)

---

*Stack analysis: 2025-02-23*
