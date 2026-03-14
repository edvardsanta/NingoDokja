# Production Runbook

This document is the practical production checklist for Dokja.

Main files:

- [docker-compose.prod.yml](/home/vard/repos/my_repos/read_books/docker-compose.prod.yml)
- [.env.prod](/home/vard/repos/my_repos/read_books/.env.prod)

## Services

Production stack:

- `dokja-orchestrator`
- `dokja-meme`
- `dokja-chat-ai`
- `dokja-discord`
- `dokja-scheduler`

## Required Secrets

Before starting production, replace placeholder values in [.env.prod](/home/vard/repos/my_repos/read_books/.env.prod):

- `CHAT_AI_API_KEY`
- `CHAT_AI_BASE_URL`
- `DISCORD_BOT_TOKEN`
- `DISCORD_APPLICATION_ID`

Do not deploy with `CHANGE_ME_*` values.

## Validate Configuration

Render the production compose config:

```bash
docker compose -f docker-compose.prod.yml --env-file .env.prod config
```

Build production images:

```bash
docker compose -f docker-compose.prod.yml --env-file .env.prod build
```

## Start Production

Start the full stack:

```bash
docker compose -f docker-compose.prod.yml --env-file .env.prod up -d
```

Check status:

```bash
docker compose -f docker-compose.prod.yml --env-file .env.prod ps
```

## Initial Smoke Checks

Watch logs:

```bash
docker compose -f docker-compose.prod.yml --env-file .env.prod logs -f dokja-orchestrator
docker compose -f docker-compose.prod.yml --env-file .env.prod logs -f dokja-discord
docker compose -f docker-compose.prod.yml --env-file .env.prod logs -f dokja-chat-ai
docker compose -f docker-compose.prod.yml --env-file .env.prod logs -f dokja-scheduler
docker compose -f docker-compose.prod.yml --env-file .env.prod logs -f dokja-meme
```

Things to confirm:

- orchestrator starts both ZeroMQ and HTTP ingress
- Discord bot logs in successfully
- chat AI `/health` responds internally
- meme service starts and opens SQLite
- scheduler emits bootstrap refresh and scheduled dispatch

## Scheduler Expectations

Current startup behavior:

1. emit `meme.pool.refresh`
2. wait `DOKJA_SCHEDULER_MEME_BOOTSTRAP_GRACE_PERIOD`
3. emit `meme.dispatch.scheduled`
4. continue normal cadence

Normal cadence:

- refresh every `45m`
- dispatch every `6h`

## Scheduled Meme Delivery Path

Expected production path:

`dokja-scheduler -> dokja-orchestrator -> dokja-meme -> dokja-discord -> Discord channel`

The scheduler must not talk to Discord directly.

The orchestrator owns the delivery workflow.

## Common Failure Checks

### Discord issues

Check:

- `DISCORD_BOT_TOKEN`
- `DISCORD_APPLICATION_ID`
- `DISCORD_GUILD_ID`
- `DISCORD_ALLOWED_CHANNELS`
- `DISCORD_SCHEDULED_MEME_CHANNEL_ID`

### Chat AI issues

Check:

- `CHAT_AI_API_KEY`
- `CHAT_AI_BASE_URL`
- `CHAT_AI_MODEL`

### Scheduled delivery issues

Check:

- scheduler log for emitted `meme.dispatch.scheduled`
- orchestrator log for `deliver-scheduled-memes`
- Discord delivery log for `/deliver`

### Meme fetch issues

Check:

- meme service logs for scraper upstream failures
- SQLite file at `MEME_SERVICE_DB_FILE`

## Stop Production

```bash
docker compose -f docker-compose.prod.yml --env-file .env.prod down
```
