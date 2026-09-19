# Dokja Chat AI

Standalone FastAPI service for chat completion requests.

Endpoints:

- `GET /health`
- `POST /chat`

Environment variables:

- `CHAT_AI_SERVICE_HOST`
- `CHAT_AI_SERVICE_PORT`
- `CHAT_AI_API_KEY`
- `CHAT_AI_BASE_URL`
- `CHAT_AI_MODEL`

## Provider profiles (no restart needed)

Besides the `CHAT_AI_*` variables, the service reads provider **profiles** (base URL, model, token) from the
shared SQLite file `DOKJA_DB_FILE` (`.dokja/dokja.db` in dev, `/data/dokja.db` in prod). The selected profile
is re-read on every request and wins over the environment; the OpenAI client is rebuilt only when the
selection or the profile changed. With no profile selected the environment settings still work, and with
neither the service answers 503 "no chat profile is selected".

- `GET /health` reports the active profile, model and base URL and whether a key exists. It never calls the
  provider (the panel polls it) and never returns the key.
- Profiles are created and removed locally (`dokja-cli chat profile add|remove`, or `p` in the TUI panel); the
  orchestrator can only list them (masked) and select one, so a token never crosses its unauthenticated port.
- The token is stored in plain text in the database file (mode 600, git-ignored). Keep the file private.
