# External Integrations

**Analysis Date:** 2025-02-23

## APIs & External Services

**Chat & AI:**
- OpenAI - Used for LLM-powered chat features.
  - SDK/Client: `openai` (Python)
  - Auth: `CHAT_AI_API_KEY`
- Hugging Face - Used for various ML models (Transformers, BLIP, GPT-2).
  - SDK/Client: `transformers`, `torch`
  - Auth: Local model loading or HF Hub.

**Social & Communication:**
- Discord - Primary user interface for the bot.
  - SDK/Client: `discord.js` (Node.js), `discordgo` (Go)
  - Auth: `DISCORD_BOT_TOKEN`, `DISCORD_APPLICATION_ID`

## Data Storage

**Databases:**
- SQLite
  - Connection: Local files (e.g., `ningo_memory.db`)
  - Client: Native `sqlite3` and `SQLAlchemy` (implied in `dokja_lab`)
- Redis
  - Connection: `REDIS_URL` or default localhost
  - Client: `redis` (Python), `go-redis/v8` (Go)

**File Storage:**
- Local filesystem only - Used for model caches and SQLite databases.
  - Locations: `/workspace/dokja_services/dokja_book/models`, `/workspace/dokja_services/dokja_voice/models`

**Caching:**
- Redis - Used as a distributed cache.

## Authentication & Identity

**Auth Provider:**
- Custom / API Key based
  - Implementation: Services use environment variables for internal authentication and external API keys for service access.

## Monitoring & Observability

**Error Tracking:**
- None detected - Basic logging to stdout/stderr.

**Logs:**
- Structured logging using `logging` (Python), `zap` (Go), and standard console output.

## CI/CD & Deployment

**Hosting:**
- Docker - Services are containerized and can be deployed to any Docker-compatible host.

**CI Pipeline:**
- GitHub Actions - Detected in `.github/workflows/`.

## Environment Configuration

**Required env vars:**
- `DISCORD_BOT_TOKEN`
- `CHAT_AI_API_KEY`
- `CHAT_AI_BASE_URL`
- `MEME_SERVICE_ENDPOINT`
- `VA_ZMQ_ENDPOINT`

**Secrets location:**
- `.env` files (local development) and CI/CD secret managers (production).

## Webhooks & Callbacks

**Incoming:**
- Discord Interactions - Handled by `dokja-discord` service.
- HTTP API Endpoints - Exposed by `dokja-orchestrator` (`8091`) and other services.

**Outgoing:**
- Discord Webhooks - Used for delivering messages and memes to specific channels.

---

*Integration audit: 2025-02-23*
