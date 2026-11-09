# Dokja Discord Interface

Share a searchable PDF link or attach a PDF to discuss it in the same chat, with
page citations and follow-up questions. File-only messages are accepted. PDFs
share the four-source limit with links and must fit within 25 MiB per file. The
adapter forwards only platform metadata through optional `documentAttachments`;
the orchestrator authorizes the turn before Feeds downloads anything. The reader
does not run PDF instructions or links, and scanned/protected documents may be
unreadable. Readings expire after inactivity or restart and are not saved to Knowledge.

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
The orchestrator compares them with `owner.accounts.discord.user_id` from its
private `.dokja/owner.json` configuration and
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

## Price search

`/preco produto:<words> marca:<brand> loja:<store>` asks the orchestrator for
`price.search`, best buys first (real discounts, deepest first; then ordinary
prices by how far under their median; then products still gathering history;
warnings last). `loja` suggests the stores already seen as you type
(`price.merchants`, answered within Discord's three seconds; a failure suggests
nothing). Each card's title links to its cheapest offer and each source has an
"open at the store" link; only http(s) addresses become links. When the owner's
search starts a new watched search, the reply says so.
Both options are optional and match whole words of a watched product's title or
key, without accents or case (`produto: hd`, `produto: rtx 5070`, `marca: amd`);
a word of three letters or more also matches the start of a longer one. The
reply is private to the person who asked: one card per product, at most 10,
coloured by the leading verdict (a warning outranks a discount), with one field
per price source and the verdicts translated from the Price domain's codes in
`commands/price_command.ts` (`pt-BR` and `en`, from `DOKJA_DISCORD_LOCALE`).
Store names come from the owner's collector data; the adapter names none.


## Questionnaires

`/teste_politico link:<url>` asks the orchestrator for `quiz.take`: Ningo takes
the online questionnaire himself, through an owner's Feeds quiz runner, and
answers from his persona and private worldview. Discord sees the command
acknowledged at once; the reply follows within 14 minutes and is public. It
states how many answers he gave and the site's label, adds his short comment
and ends with the site's own result link. A refusal (`busy`, `no_runner`,
`unsupported`, `unavailable`, `unanswered`, `invalid_link` or `failed`) is
worded from the command's catalog in `commands/quiz_command.ts`; a site's own
text never reaches the channel, and no link is invented when a run fails.
## Live group voice

Join a server voice channel and run `/voice_chat_start` once. Ningo listens to
all human participants, retaining their attribution and recent room context.
Starting the command opens your exchange immediately, so you can say `teste`.
Call `Ningo` or `Dokja` to reopen it after a pause; follow-ups need no name for
30 seconds after estimated playback. Other participants can join with a clear
question or continuation during a short handoff window. Incidental conversation
and acknowledgments are stored without model replies.

Ningo finishes speaking while continuing to listen. Speech heard during an answer
is combined into the next turn; it never cuts off the current answer. Spoken
replies default to one short sentence. Ask for detail to get a longer explanation,
which plays sentence by sentence with the next sentence prepared ahead.
`/voice_chat_stop` cancels playback and outstanding work.

What participants say is also shown live in the owner's Desktop **Realtime**
view. `/voice_chat_start` and `/wake_chat` open the same ephemeral journal that
`/conversation_start` keeps, marked `voice_chat`, and add each transcribed
segment to it. The journal is deleted when the voice chat stops. If the
orchestrator refuses the journal, the voice chat still works without it.

WebRTC VAD checks short activity probes and gates Whisper transcription; acoustic
guards reject quiet packets and isolated impulses. Bots are ignored. Rebuild the
Voice image and restart Voice, the orchestrator and Discord together for this
version. Speech requests go through the authenticated orchestrator bridge.

The current CLI path supplies a validated final reply, rather than trusted
partial speech segments. Model latency remains part of response time. See the
production runbook for configuration, limits and live verification.
