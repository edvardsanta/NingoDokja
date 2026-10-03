# Communications service

Read-only HTTP adapter for the desktop's Discord conversation view. The desktop requests
`communications.channels` or `communications.history` from the orchestrator; the communications
 domain validates the channel, and this service reads the external API. It does not run a model,
store messages or send them. Sending reuses the orchestrator's existing delivery workflow.

The service is optional. Enable the `communications` Compose profile and set the same
`DOKJA_COMMUNICATIONS_TOKEN` (at least 32 characters, randomly generated) in the desktop shell,
orchestrator and this service. Leave it unset to disable the new actions. Never commit its value.
The service's port is not published by Compose.

| Variable | Purpose |
| --- | --- |
| `DOKJA_COMMUNICATIONS_TOKEN` | Bearer credential required by `/dispatch` |
| `DISCORD_BOT_TOKEN` | Bot credential used only for the external API |
| `DOKJA_DISCORD_API_BASE_URL` | Base address of the Discord REST API, version path included: the setting the delivery interface already uses. It has no built-in value; without it every call answers `not_configured` and nothing is requested |
| `DISCORD_SCHEDULED_MEME_CHANNEL_ID` | Comma-separated allowed channel ids, shared with the delivery workflow |
| `DISCORD_GUILD_ID` | Optional guild id used to resolve channel names; otherwise the list shows ids |
| `COMMUNICATIONS_BIND` | Bind address, `127.0.0.1` on the host; container sets `0.0.0.0` |

`GET /health` reports process liveness. `POST /dispatch` accepts
`{"action":"channels|history","payload":{...}}` with `Authorization: Bearer <credential>`.
`channels` accepts only configured `channel_ids`. `history` accepts `channel_id` and optional
`before` (a message id); pages contain at most 30 messages in oldest-to-newest order. The response
returns an empty `before` when there is no further full page. The domain does not accept arbitrary
limits or upstream endpoints from the interface.

Only author display name, bot marker, text, timestamps, reply id and attachment filenames reach
the desktop. Attachment download addresses, tokens, member profiles and raw upstream errors do
not. Requests have a four-second upstream timeout, a one-MiB response cap and no redirects or
automatic retries. Rate-limit failures are reported to the caller; desktop automatic refresh pauses
on failure. A full last page can require one extra request to establish the end of history.

The bot needs access to the configured channels and their message history. Message text also
requires the platform's message content permission where applicable; the platform's documentation
of the message resource lists the permissions involved.

Run on the host with `python3 server.py`. Tests use only fake upstream replies:

```sh
python3 -m unittest -v
```
