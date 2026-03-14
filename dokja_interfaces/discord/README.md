# Dokja Discord Interface

TypeScript Discord interface for Dokja. It receives Discord messages and slash commands, maps them into Dokja orchestrator requests over HTTP, and sends the orchestrator reply back to Discord.

Environment variables:

- `DISCORD_BOT_TOKEN`
- `DISCORD_APPLICATION_ID` optional override if automatic app-id probe fails
- `DOKJA_ORCH_HTTP_ENDPOINT`
- `DISCORD_GUILD_ID`
- `DISCORD_ALLOWED_CHANNELS`
- `DISCORD_DM_POLICY`
- `DOKJA_DISCORD_MODE` (`full` or `voice`)
- `DOKJA_DISCORD_DELIVERY_PORT`
- `DOKJA_DISCORD_API_BASE_URL`

Voice-only mode:

- set `DOKJA_DISCORD_MODE=voice`
- only `/play_radio` and `/stop_radio` are deployed
- orchestrator chat/message forwarding and outbound delivery server are skipped

Run:

```bash
pnpm install --no-frozen-lockfile
pnpm dev:watch
```

Structure:

- `main.ts`: process entrypoint
- `app.ts`: Discord client bootstrap and lifecycle
- `config.ts`: environment/config parsing
- `types.ts`: local Discord interface contracts
- `commands/`: slash commands and richer interaction definitions
- `messages/`: text-message forwarding rules
- `transport/`: orchestrator bridge client
- `delivery/`: internal outbound delivery endpoint used by the orchestrator
- `runtime/`: Discord runtime adapters (`discord.js` active, Carbon legacy/reference)
- `voice/`: voice controller and radio stream pipeline
- `tests/`: Discord interface test suite

Outbound delivery contract:

`POST /deliver`

Request body:

```json
{
  "channel_id": "123",
  "content": "Meme title",
  "attachment_url": "https://example.com/meme.jpg"
}
```

Rules:
- `channel_id` is required
- at least one of `content` or `attachment_url` is required
- when `attachment_url` is present, the interface downloads the file and uploads it to Discord

Response body:

```json
{
  "status": "ok",
  "channel_id": "123",
  "message_id": "456",
  "has_attachment": true
}
```
