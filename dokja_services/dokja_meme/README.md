# Meme Service Python

This service is the extraction boundary for the legacy Dokja Lab meme system.

It does not rewrite the meme stack. It wraps the existing legacy components so the orchestrator can talk to memes through ZeroMQ events instead of direct Flask or in-process calls.

## Legacy Components Reused

- `dokja_lab/handlers/meme_handler.py`
- `dokja_lab/workers/meme_worker.py`
- `dokja_lab/memes/*`
- `dokja_lab/infra/sqlite/storage.py`
- `dokja_lab/models/Meme.py`

## Responsibility

This service owns:

- meme pool refresh
- unsent meme retrieval
- sent-state updates
- meme service health/status

This service does not own interface rendering. The legacy Flask UI can continue to exist during migration, but the orchestrator should integrate through ZeroMQ only.

## Event Contract

The service accepts JSON requests over ZeroMQ `REP`.

Preferred orchestrator envelope:

```json
{
  "event_id": "uuid",
  "timestamp": "2026-03-10T12:00:00Z",
  "source": "orchestrator",
  "type": "meme.fetch",
  "user": {
    "id": "discord-user-id",
    "name": "Alice"
  },
  "channel": {
    "id": "discord-channel-id"
  },
  "payload": {
    "limit": 5
  },
  "context": {}
}
```

Supported event types:

- `meme.fetch`
  Returns unsent memes and marks them as sent.
- `meme.pool.refresh`
  Scrapes sources and stores new memes.
- `meme.status`
  Returns service health and current unsent count.

Response format:

```json
{
  "status": "ok",
  "result": {}
}
```

Error format:

```json
{
  "status": "error",
  "message": "description"
}
```

## Local Run

```bash
cd dokja_services/dokja_meme
python3 main.py
```

Environment variables:

- `MEME_SERVICE_ENDPOINT`
  Default: `tcp://*:5557`
- `MEME_SERVICE_DB_FILE`
  Default: `<repo>/ningo_memory.db`
- `MEME_SERVICE_SCRAPERS`
  Comma-separated list. Supported: `memedroid`, `ifunny`
