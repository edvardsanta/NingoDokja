# Discord communication adapter

Discord is classified as a communication-service adapter with inbound and outbound
roles ([ADR 0012](decisions/0012-communication-services.md)). The implementation
lives under `dokja_services/dokja_discord`; Communications owns inbound application eligibility.
This guide describes its current contracts; legacy interface identifiers remain
compatible.

The Discord adapter is currently implemented in:

- [dokja_services/dokja_discord](../dokja_services/dokja_discord)

Primary flow:

`Discord -> gateway ingress -> Event/Request -> Orchestrator -> Domain -> Service -> reply -> gateway -> Discord`

## What Discord Can Do

Current capabilities:

- plain text message forwarding
- slash commands
- buttons
- select menus
- modals
- internal outbound message delivery for orchestrator-driven sends
- music in a voice channel (below)

## Music

`/play song:<words or a link>`, `/skip`, `/pause`, `/resume`, `/stop`, `/queue`, `/history` and
`/volume percent:<0-100>` send `music.*` events through the bridge, from the channel they were
typed in, so the server and allowed-channel checks apply. The orchestrator keeps the queue; every
music reply carries it (`music` in the bridge response, with an `epoch` and a `version`), and the
adapter plays what it says, ignoring a reply older than the last one it applied.

- A track plays from its page: `yt-dlp` (pinned in the image, using the image's node for pages
  that run JavaScript) pipes its audio into ffmpeg, which hands Discord raw 48 kHz stereo PCM, so
  the volume can change while it plays. Both processes end on skip, stop or shutdown. A track
  takes a few seconds to start (about 7 to 9 in tests, yt-dlp reading the page; no other client
  was faster): the reply comes at once, the sound after. The next track in the queue is made
  ready while one plays, so the songs after the first start at once.
- ffmpeg brings every track to about the same loudness (`loudnorm`, -16 LUFS), and Discord
  encodes music at the voice channel's bitrate, within 32 to 128 kbps.
- When a track ends, or cannot start, the adapter sends `music.ended` with its entry and plays
  what the reply says next. If the voice connection is gone, it sends `music.stop`.
- `/play` needs the person in a voice channel; while music plays, the controls need them in
  Ningo's voice channel. `/queue` works from anywhere allowed.
- Ningo's speech pauses the music and resumes it after, unless the queue itself was paused. The
  radio is refused while a queue plays, and `/play` stops a radio that is playing.
- Only Discord plays the queue: a change made elsewhere (desktop, CLI) is heard at the next
  Discord command or track end.
- **The panel.** When a song starts, Ningo posts one message in the channel of the last music
  command: a card with the song, who asked for it, the volume, how many wait and the song's cover
  beside it (when its source gives one), with Pause/Resume, Skip,
  Stop and volume −10/+10 buttons. It is edited as the queue changes, never posted again for the
  same queue, and loses its buttons when the queue ends. Buttons follow the commands' rule and
  answer only the person who pressed.
- **`/history`** shows a person the last songs they asked for and how each ended, only to them.
  Each song is kept in Memory for the person who asked for it, with no time limit.
- **A remark on the song.** On about one song in three, from the first, the adapter's
  `music.comment` asks for one line in Ningo's voice, written in `DOKJA_DISCORD_LOCALE`. It joins
  the panel a moment later, if that song is still playing. Nothing waits for it: a slow or failed
  model means no line, and the music goes on.
- **Alone in the channel.** When nobody but bots is left in Ningo's voice channel, the music
  pauses (apart from the queue's own pause and the pause while Ningo speaks). If someone comes
  back within three minutes it resumes; otherwise Ningo leaves and the queue is reported stopped.
- **Language.** Replies, refusals and the panel are said in `DOKJA_DISCORD_LOCALE` (`pt-BR` by
  default, `en` too). The orchestrator sends the queue and a refusal `code`; a source's own
  refusal keeps its words.

## State

The delivery listener also answers `GET /status` with a level, a gateway state (`connected`,
`connecting` or `disconnected`) and an empty sentence. The state follows the discord.js gateway, so a
dropped shard is reported as `disconnected` even while the process is up. The orchestrator reads it
through `DISCORD_INTERFACE_STATUS_ENDPOINT` (set in the Compose stacks) and lists Discord in
`ningo.status`, so the desktop shows it and can switch it. Switching it off makes the bot answer
nothing and leaves Discord channels out of scheduled and manual deliveries (reported as skipped); the
gateway connection stays up. The reply never carries a channel, a guild or a token.

While Discord is off, a plain message gets no reply at all. A slash command is answered
`No response.` to the person who ran it only (an ephemeral reply), so nothing is posted in the channel.

## Inbound Flows

### Plain Messages

The adapter receives a Discord message, normalizes its channel, DM and mention
facts, and forwards the content to the orchestrator as `message.created`.
Communications decides application eligibility before the chat workflow runs.

Relevant files:

- [message_listener.ts](../dokja_services/dokja_discord/messages/message_listener.ts)
- [bridge.ts](../dokja_services/dokja_discord/transport/bridge.ts)

### Slash Commands

Current commands:

- `/chat`
- `/meme_fetch`
- `/meme_status`
- `/meme_refresh`
- `/ningo_panel`
- `/chat_modal`

Relevant files:

- [commands.ts](../dokja_services/dokja_discord/commands/commands.ts)
- [interactions.ts](../dokja_services/dokja_discord/commands/interactions.ts)

### Rich Interactions

Current interaction types:

- buttons
- select menus
- modals

Examples:

- `Ningo Status`
- `Fetch Meme`
- `Chat Prompt`
- meme count select
- chat modal submit

Relevant files:

- [interactions.ts](../dokja_services/dokja_discord/commands/interactions.ts)
- [carbon_runtime.ts](../dokja_services/dokja_discord/runtime/carbon_runtime.ts)

## Outbound Delivery

The Discord interface also exposes an internal delivery endpoint for orchestrator-driven sends.

Endpoint:

- `POST /deliver`

Used for:

- scheduled meme delivery

Contract:

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
- when `attachment_url` exists, the interface downloads the file and uploads it to Discord
- an optional `attachment_data_b64` (with `attachment_content_type`) skips that download:
  the interface uploads those bytes instead, and only uses `attachment_url` to name the
  file. The orchestrator sends this when the meme service already fetched the bytes
  itself, for hosts this interface's own download cannot get past (Cloudflare and
  similar bot checks).

Response:

```json
{
  "status": "ok",
  "channel_id": "123",
  "message_id": "456",
  "has_attachment": true
}
```

Relevant files:

- [delivery_server.ts](../dokja_services/dokja_discord/delivery/delivery_server.ts)
- [discord_interface_client.go](../dokja_orch/internal/clients/discord_interface_client.go)

## Runtime Boundaries

Dokja-specific app code uses local runtime abstractions instead of depending on Carbon types everywhere.

Main local boundaries:

- [types.ts](../dokja_services/dokja_discord/types.ts)
- [carbon_runtime.ts](../dokja_services/dokja_discord/runtime/carbon_runtime.ts)
- [app.ts](../dokja_services/dokja_discord/app.ts)

This keeps Carbon as a transport/runtime dependency, not the owner of Dokja application structure.

## Important Constraints

- Discord does not call `dokja_services` directly
- Discord does not decide workflows
- Discord should emit orchestrator-friendly events and render orchestrator results
- scheduled outbound delivery is initiated by the orchestrator, not by the scheduler directly
