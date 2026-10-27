# Dokja Discord Interface

Chat replies may include up to three formula PNGs. The orchestrator supplies
validated inline image bytes through optional bridge `attachments`; the adapter
uploads them with messages, slash replies and chat modals. A long reply attaches
files only to its first chunk. Readable plain-text formulas are retained even
when rendering is unavailable.
Rendering failures are stated above the text using the interface locale, in
messages, slash replies and chat modals.

TypeScript Discord communication adapter for Dokja. It receives Discord messages and slash commands, maps them into Dokja orchestrator requests over HTTP, and sends the orchestrator reply back to Discord.

For Discord replies, the adapter fetches the referenced message in the same
channel and guild and forwards up to 8,000 characters as optional `replyToContent`
on the HTTP bridge. Unavailable references do not block the new message. The chat
handler treats this as quoted user context, including after compaction and restarts;
the current message remains the routing input. Deploy the updated orchestrator
and adapter for this additive feature. Only the direct reference is fetched;
the adapter does not walk the reply chain to the initial topic.

Natural server messages also forward an optional bounded `userName` for untrusted
speaker attribution. Account IDs remain the identity source; display names never
establish ownership. These discussions share context only within the same
guild/channel. DMs and slash-command conversations keep separate per-user scopes.
The orchestrator persists the latest summary and recent turns in its private
`dokja_chat_sessions.db`, using existing data mounts. It records handled exchanges,
not every channel message or pre-deployment history. It does not index chat in
pgvector. Only the verified owner can reset a shared channel snapshot.

Up to 20 mentioned Discord user IDs also travel to the authenticated bridge.
The orchestrator compares them with optional `DOKJA_OWNER_DISCORD_USER_ID` and
passes only a trusted owner mention fact to chat; the ID itself is not sent to
the model.

Long replies are split into messages of at most 2,000 characters. The active
discord.js runtime closes and reopens fenced code blocks with their language tag
at each boundary, preserving code indentation and blank lines. Exceptionally long
lines are split when necessary to fit the message limit.

Environment variables:

- `DISCORD_BOT_TOKEN`
- `DISCORD_APPLICATION_ID` optional override if automatic app-id probe fails
- `DOKJA_ORCH_HTTP_ENDPOINT`
- `DOKJA_GATEWAY_INGRESS_TOKEN` required gateway credential; set the same value on the orchestrator
- `DOKJA_ORCHESTRATOR_TIMEOUT_MS`
- `DISCORD_GUILD_ID` one server, or several separated by commas; the `/` commands are registered on each
  (globally when empty)
- `DISCORD_ALLOWED_CHANNELS`
- `DISCORD_DM_POLICY`
- `DISCORD_REQUIRE_MENTION` optional orchestrator policy setting; defaults to `false`

The orchestrator's Communications domain reads the guild, channel, DM and mention settings and decides which inbound messages or commands may run. The gateway sends normalized facts and still suppresses bot echoes. Deploy matching gateway and orchestrator revisions together; bridge requests without the new metadata are refused.
- `DOKJA_DISCORD_MODE` (`full` or `voice`)
- `DOKJA_DISCORD_DELIVERY_PORT`
- `DOKJA_DISCORD_API_BASE_URL`
- `DOKJA_DISCORD_WEBHOOKS` optional `channel_id=webhook_url` pairs, comma-separated. Deliveries to a listed `channel_id` go through that webhook instead of the bot (useful for servers the bot is not in). Webhook URLs are secrets, keep them out of git.

Voice-only mode:

- set `DOKJA_DISCORD_MODE=voice`
- only voice-oriented commands are deployed:
  - `/speak`
  - `/voice_chat_start`
  - `/voice_chat_stop`
  - `/play_radio`
  - `/stop_radio`
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
- an optional `attachment_data_b64` (with `attachment_content_type`) skips that download:
  the interface uploads those bytes instead, and only uses `attachment_url` to name the
  file. The orchestrator sends this when the meme service already fetched the bytes
  itself, for hosts this interface's own download cannot get past (Cloudflare and
  similar bot checks).

Response body:

```json
{
  "status": "ok",
  "channel_id": "123",
  "message_id": "456",
  "has_attachment": true
}
```
