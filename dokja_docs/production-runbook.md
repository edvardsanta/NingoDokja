# Production Runbook

This document is the practical production checklist for Dokja.

Main files:

- [docker-compose.prod.yml](../docker-compose.prod.yml)
- [.env.prod](../.env.prod)

## Services

Production stack:

- `dokja-orchestrator`
- `dokja-meme`
- `dokja-chat-ai`
- `dokja-discord`
- `dokja-scheduler`

## Required Secrets

Before starting production, replace placeholder values in [.env.prod](../.env.prod):

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

## Operator State and Shared Database

The orchestrator, scheduler and chat service share the `dokja-data` volume, mounted at `/data`:

- `/data/dokja_state.json` (`VA_STATE_FILE` in the orchestrator, `DOKJA_STATE_FILE` in the scheduler):
  which services and scheduler jobs an operator switched off and any job interval overrides. The
  orchestrator writes it; the scheduler only reads the intervals. A corrupt file starts every job paused.
- `/data/dokja.db` (`DOKJA_DB_FILE`): SQLite database with the chat provider profiles (base URL, model
  and token). Keep the volume private: the token is stored in plain text.

Without a writable volume both files would be lost on every deploy.

Startup still emits every scheduled job once, but the orchestrator skips the ones an operator paused, so a
restart never posts something that was switched off. `dokja-cli services list` and `dokja-cli jobs list` show
the current state.

Chat profiles are created and removed on the machine that holds the database (`dokja-cli chat profile add`
with `DOKJA_DB_FILE` pointing at it, or `p` in the TUI panel), never through the orchestrator. The
orchestrator can only list them (masked) and select one, and the chat service picks the selection up on its
next request.

## NSFW Screening for Memes

The meme service labels each meme with an NSFW verdict, and channels in `DISCORD_SAFE_ONLY_CHANNEL_IDS`
only receive memes that passed. It needs two read-only model folders, mounted from `DOKJA_MODELS_DIR`
(`nudenet/320n.onnx` and `rapidocr/*.onnx`). The compose default is a path on the development machine, so
set `DOKJA_MODELS_DIR` in [.env.prod](../.env.prod). If the models are missing the screen fails closed:
safe-only channels receive nothing, other channels are unaffected. `MEME_NSFW_EXTRA_WORDS` adds words to the
blacklist. `DOKJA_DISCORD_WEBHOOKS` (`channel_id=webhook_url`, comma-separated) makes the delivery server
post to those channels through a webhook instead of the bot; the URLs are credentials, keep them in the
env file only.

## Security Notes

The orchestrator request port (`5558`) has no authentication and is published on every interface, so anyone
who can reach it can flip the switches above and run manual deliveries. Bind it to localhost
(`127.0.0.1:5558:5558`) on shared hosts.

## Common Failure Checks

### Discord issues

Check:

- `DISCORD_BOT_TOKEN`
- `DISCORD_APPLICATION_ID`
- `DOKJA_ORCHESTRATOR_TIMEOUT_MS`
- `DISCORD_GUILD_ID`
- `DISCORD_ALLOWED_CHANNELS`
- `DISCORD_SCHEDULED_MEME_CHANNEL_ID`

### Chat AI issues

Check:

- `CHAT_AI_API_KEY`
- `CHAT_AI_BASE_URL`
- `CHAT_AI_MODEL`
- the selected chat provider profile (see "Operator state and shared database"); it wins over the
  `CHAT_AI_*` variables, and `GET /health` on the chat service reports which one is in use

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
