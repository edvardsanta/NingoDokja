# WhatsApp communication adapter

WhatsApp is classified as a communication-service adapter with inbound and outbound
roles ([ADR 0012](decisions/0012-communication-services.md)). Its source lives
under `dokja_services/dokja_whatsapp`; legacy interface identifiers remain compatible.

The WhatsApp adapter is currently implemented in:

- [dokja_services/dokja_whatsapp](../dokja_services/dokja_whatsapp)

Inbound flow (talking to Ningo in WhatsApp):

`WhatsApp -> gateway ingress -> Event/Request -> Orchestrator -> Domain -> Service -> reply -> gateway -> WhatsApp`

Outbound text delivery to configured named conversations is also available through
the orchestrator; see [Sending from the desktop](#sending-from-the-desktop).

## How It Connects

The interface does not use the official WhatsApp Business platform. It links to a WhatsApp account as
a **linked device**, the same feature WhatsApp Web uses, through the Baileys library (a TypeScript
client of that protocol). The phone stays the main device; Ningo appears next to it in
Settings > Linked devices.

- the service connects to WhatsApp and the orchestrator, and has an internal listener
  for status, linking, configured history and text delivery; Compose publishes no port
- linking is done once by scanning a QR code printed in the service log
- the keys of the linked device are saved in a session folder (a Compose volume) and the service
  reconnects from them on every start; the QR code is not needed again
- what it receives is every message the account receives: the interface suppresses echoes,
  duplicates, stale messages and empty text; the Communications domain in the orchestrator decides
  which normalized chats are eligible before chat processing

**Risk.** This protocol is unofficial and outside WhatsApp's terms of use, so the account can be
restricted or banned. Use a dedicated number when you can. Linking your own number works, and the
defaults below are built for it, but a ban would reach that number. The session folder is the whole
account: whoever can read it controls the account.

## What WhatsApp Can Do

- plain text chat in the account's own "message yourself" chat
- plain text chat in a one-to-one chat with an allowed number
- plain text chat in an allowed group, when the bot is mentioned

No images, voice notes, stickers, reactions or files: a message without text is ignored.

## Who Gets an Answer

Nothing is answered unless it is listed. There is deliberately no "open" mode for direct messages:
the linked account is usually somebody's own number.

| Rule | Behavior |
| --- | --- |
| Status updates, broadcast lists, channels | ignored |
| Echo of a reply the bot sent | ignored |
| Something the account owner typed in another chat | ignored |
| Message older than 5 minutes (bot was offline or reconnecting) | ignored |
| Own "message yourself" chat | answered when `DOKJA_WHATSAPP_SELF_CHAT` is `true` (default) |
| One-to-one chat | answered only when the sender is in `DOKJA_WHATSAPP_ALLOW_FROM` |
| Group | answered only when it is in `DOKJA_WHATSAPP_ALLOWED_GROUPS` **and** the bot is mentioned or one of its messages is replied to |
| Message has no text once the mention is removed | ignored |
| Same message delivered twice | answered once (chat id + message id, kept 10 minutes) |

Allowed numbers are matched on the phone number, written any usual way (`+55 (11) 91234-5678`).
A Brazilian mobile number matches with or without the ninth digit, because WhatsApp ids exist in
both forms. WhatsApp also identifies some senders only by an anonymous id: the number is then taken
from the one that arrives with the message or from the mapping Baileys keeps. When neither is known
the sender cannot be matched and is ignored.

An ineligible message receives no reply, and a phone number never reaches the log. The one
exception helps with setup: a group the bot is not allowed in is reported **once** with its id and
the reason `policy`, so the id can be copied into `DOKJA_WHATSAPP_ALLOWED_GROUPS`.

To choose groups without the log, open System › Settings in the desktop, edit **Groups** and press
**Find my groups**. While the account is linked and connected, it lists every group the account
takes part in, by name and number of members (`communications.link.groups`, served by the
interface's internal `GET /link/groups`). **Add** puts a group's id in the draft; nothing changes
until the setting is reviewed and saved as usual. Listing allows nothing by itself.

### Replies

The reply is plain text, cut at 8,000 characters. In a group it quotes the message that triggered it.
When the answer takes more than a second the chat shows "typing" until it arrives; a quick answer, or
no answer because the interface is switched off, shows nothing. The interface never marks messages as
read and never sets the account online, so the phone keeps its own notifications.

A failing orchestrator is answered with a short apology.

## Configuration

| Variable | Where | Purpose |
| --- | --- | --- |
| `DOKJA_WHATSAPP_AUTH_DIR` | WhatsApp interface | Folder of the linked-device session. **Secret.** Required; `/auth/default` in Compose |
| `DOKJA_WHATSAPP_SELF_CHAT` | Orchestrator and WhatsApp interface | Answer the account's own chat, default `true` |
| `DOKJA_WHATSAPP_ALLOW_FROM` | Orchestrator and WhatsApp interface | Phone numbers whose direct messages are answered, comma-separated |
| `DOKJA_WHATSAPP_ALLOWED_GROUPS` | Orchestrator and WhatsApp interface | Group ids (`<digits>@g.us`) where the bot answers when mentioned, comma-separated |
| `DOKJA_ORCH_HTTP_ENDPOINT` | WhatsApp interface | Orchestrator bridge, `http://dokja-orchestrator:8091/orchestrator` in Compose |
| `DOKJA_GATEWAY_INGRESS_TOKEN` | Orchestrator and WhatsApp interface | Dedicated bearer credential for the HTTP bridge. Required. |
| `DOKJA_ORCHESTRATOR_TIMEOUT_MS` | WhatsApp interface | Wait for the orchestrator, default `180000` |
| `DOKJA_WHATSAPP_CONVERSATIONS` | WhatsApp interface | `alias=<phone number or group id>` pairs, comma-separated: the only conversations that are recorded and shown in the desktop. Empty (the default) records nothing. Managed as the `whatsapp.conversations` setting (System › Settings, or `dokja-cli settings`): the database decides it, and `.env` only seeds it once |
| `DOKJA_WHATSAPP_HISTORY_DIR` | WhatsApp interface | Folder of the recorded text. Required when a conversation is named; `/history` (its own volume) in Compose |
| `DOKJA_WHATSAPP_HISTORY_DAYS` | WhatsApp interface | How long the text is kept, 1 to 365 days, default `30` |
| `DOKJA_WHATSAPP_STATUS_PORT` | WhatsApp interface | Internal port that answers the interface's state, default `8094`. Never published |
| `WHATSAPP_INTERFACE_STATUS_ENDPOINT` | Orchestrator | Defaults to `http://dokja-whatsapp:8094/status` in the lite and dev stacks. While nothing answers there, WhatsApp shows as not running, with no alarm. An empty value means the desktop shows WhatsApp as not configured |

The service refuses to start, naming the variable and the position of the entry (never its value),
when an entry is not a phone number or a group id, and when nothing at all would be answered (self
chat off and both lists empty).

## Setup

Set the lists in the ignored environment file, then start the service. It is optional and starts only
with the `whatsapp` Compose profile:

```bash
docker compose -f docker-compose.lite.yml --profile whatsapp up -d dokja-whatsapp
docker compose -f docker-compose.lite.yml logs -f dokja-whatsapp
```

1. The log shows a QR code. On the phone open WhatsApp > Settings > Linked devices > Link a device
   and scan it. The code lasts about a minute.
2. If nobody scans it in time, a new code follows after 5 s, then 10 s, 20 s and so on up to 5 minutes,
   and the state stays `waiting_for_link`.
3. After linking the log shows `"whatsapp":"connected"`. Write to your own "message yourself" chat to try it.

To see and switch it from the desktop, set `WHATSAPP_INTERFACE_STATUS_ENDPOINT` in the ignored
environment file (see "State and the desktop") and recreate the orchestrator.

### Unlink and start over

- On the phone, remove the device from Linked devices.
- Then remove the session volume, so the next start asks for a new QR code:

  ```bash
  docker compose -f docker-compose.lite.yml --profile whatsapp down
  docker volume rm <compose project>_dokja-whatsapp-auth
  ```

## State and the Desktop

The service never ends the process on its own. It runs an internal listener that answers
`GET /status` with a level, one state from the list below and a short sentence, and the orchestrator
shows it in `ningo.status`, so the desktop can list WhatsApp next to Discord and switch it.
The reply never carries a phone number, a chat id or a link code, and the port is not published.

| State | Meaning | What to do |
| --- | --- | --- |
| `connected` | Linked and receiving | nothing |
| `connecting` | Opening the connection, or retrying after a lost one | wait |
| `waiting_for_link` | Not linked yet; a link code is in the log | scan it, or wait for the next one |
| `logged_out` | The account was logged out from the phone or by WhatsApp | remove the session volume and link again |
| `replaced` | Another connection is using the same session | stop the other one |
| `refused` | WhatsApp closed the connection 5 times in a row before issuing any code | check the network, then the Baileys version |
| `off` | The service is shutting down | |

Setting `WHATSAPP_INTERFACE_STATUS_ENDPOINT` on the orchestrator is what makes the desktop list the
interface; without it WhatsApp is simply not shown, never shown as a failure.

**The switch.** Switching WhatsApp off in the desktop makes the interface answer nothing: the
orchestrator skips the message and the bot says nothing in the chat, not even "switched off". The
linked session and the connection are left alone, so switching it back on takes effect at once.

## Linking from the Desktop

The health tab of the desktop has a **Link** button on the WhatsApp row. It opens a panel with the
code to scan (read again every couple of seconds while it is open), a **New code** button and
**Unlink and start over**.

The internal listener has three routes for it, next to `/status`:

| Route | What it does |
| --- | --- |
| `GET /link` | The state and, only while it is `waiting_for_link`, the code as rows of `0` and `1` |
| `POST /link/start` | Asks for a new link attempt now |
| `POST /link/reset` | Unlinks the device on WhatsApp's side when connected, erases the saved session and asks for a new code |

The orchestrator relays them as `communications.link.status`, `.start` and `.reset`. They sit under
the `communications.` prefix, so they need the communications credential (`DOKJA_COMMUNICATIONS_TOKEN`)
like the Discord history and send actions: set it for the orchestrator **and** for the desktop
process, or the panel says the access is not configured. The listener itself has no authentication,
like the other internal ones, so its port must stay unpublished.

How the code is handled:

- the code string never leaves the WhatsApp process; only the rows of its picture do, and the
  orchestrator accepts nothing but a square grid of `0` and `1` within QR sizes
- the desktop's main process draws the picture itself; the page receives the picture and never the
  code as text, and nothing logs it
- reading the state is a read; asking for a new code is a change that needs a real click or key
  press; starting over also asks in a window the shell opens, because it unlinks the device and
  erases the session
- starting over from `logged_out` erases the dead session and asks for a new code; from `connected`
  it removes the device from the phone's list as well (if WhatsApp cannot be reached the device may
  stay listed there, and can be removed on the phone)

The code is the way to attach a device to the account, so show it only on the screen of the person
who is linking.

## Conversations in the Desktop

WhatsApp has no history to read from a server, so what the desktop shows is what this interface
recorded itself. It records **only the conversations you name**:

```text
DOKJA_WHATSAPP_CONVERSATIONS=family=<phone number>,project=<group id>
```

The alias is the only name that ever leaves the interface for a conversation: the desktop lists it as
"WhatsApp · family", and the number or the group id is never sent, listed or asked for.

What is recorded, for a named conversation only:

- the text, who wrote it as a label (`owner`, `bot`, `contact` or `member`), and when
- messages that arrive whether or not the bot answers them, what the bot sends, and what you type in
  that chat on the phone
- never media, a phone number or a name; a message is cut at 4000 characters, a conversation keeps at
  most 2000 messages, and what is older than `DOKJA_WHATSAPP_HISTORY_DAYS` is forgotten at start-up
  and once a day

In a named **group** every member's text is recorded, not only the messages that mention the bot, so
name a group only when its members would expect that. The folder is readable by its owner only and
lives in its own volume (`dokja-whatsapp-history`); to erase it now, remove the volume or the
`<alias>.jsonl` file. Removing an alias stops recording and listing it; the file is dropped when its
text passes the retention.

The internal listener answers two more read routes, `GET /conversations` (aliases and the
retention) and `GET /history?alias=..&before=..` (a page of 30, shaped like a page of Discord
messages). The orchestrator relays them under the existing `communications.channels` and
`communications.history` actions, so they sit behind the communications credential and the desktop
shows them in the communications tab. An alias that is not named, and a cursor that is not a number,
are refused before anything is read.

### Sending from the desktop

The composer of a named conversation sends text **as you**, through the linked account. It is the
one place where this interface writes to a chat on its own, so it is the strictest:

- **Text only, one conversation at a time**, by alias, up to 2000 characters. The chat is found by the
  alias in the interface's own configuration; a number or a group id in the request is never used.
- **A fixed budget**: 6 messages a minute and 40 an hour, for all conversations together. It is not
  a setting. A burst of automated messages is what gets a number restricted, so a message past the
  budget is refused ("too many messages") and a refused message costs nothing from it.
- **Confirmed in a window the shell opens**, naming the conversation by its alias and showing the
  exact text, and only just after a real click or key press. The page cannot ask on its own.
- **Only through `communications.send`**, behind the communications credential: not `discord.send`,
  not the scheduler, and never "all". The WhatsApp switch in the health tab turns it off too.
- Nothing is sent while the account is not `connected`.

What was sent is recorded in the conversation as yours, with the id it went out with. The route
behind it is `POST /deliver` on the internal listener; its answers are fixed sentences and never echo
the request.

## Reconnection

The service owns the connection. A lost connection is retried after 1 s, doubling up to 60 s, and the
wait starts over once a connection opens. Linking ends with WhatsApp asking for a new connection;
that one reconnects at once. The states that need the operator (`logged_out`, `replaced`, `refused`)
are kept, and logged once, until the service is restarted.

Every reconnect line, and `refused`, carries the status code WhatsApp gave. The session folder is
never deleted by the service.

## Troubleshooting

- **The state is `refused`, with a status code (for example 405).** WhatsApp refused the connection
  before any link code. If the network is fine, the Baileys version is usually too old for the
  current WhatsApp and needs an upgrade (it is pinned in `package.json`; run the tests and check a
  real account after upgrading).
- **The QR code expired.** A new one follows by itself; the wait between codes grows up to 5 minutes,
  so restart the service if you want one now.
- **Nothing happens when a friend writes.** Their number must be in `DOKJA_WHATSAPP_ALLOW_FROM`; an
  ignored message is not logged on purpose. A group is logged once with its id.
- **A recipient sees "waiting for this message".** The bot keeps what it sent for about 10 minutes so
  WhatsApp can encrypt it again on request; after that a retry cannot be answered.

## Limits

- Text only; no cards, buttons, mentions of people, files or streaming.
- Every reply Ningo writes, the failure notice included, ends with `Assinado _Ningo Dokja_` (shown
  in italics), so on a personal number the chat tells Ningo's words from the owner's. A message the
  owner sends from the desktop is the owner's own and is not signed. Long replies are cut so the
  signature always fits.
- The Discord text commands (`meme fetch`, `meme status`, `meme refresh`) are not available from
  WhatsApp: their text goes to the chat as written. The orchestrator bridge enforces this for the
  `whatsapp` interface: its event type is fixed to `message.created`.
- A group message triggers an answer for **anyone** in the group who mentions the bot. There is no
  per-sender limit yet, so allow only groups you trust.
- On a personal number the bot is you: what the account owner writes in a group is the owner's own
  message and is ignored, so other members summon Ningo there by mentioning it. The owner cannot
  mention their own account, so in an **allowed** group a message of the owner's that starts with
  `Ningo,` or `/ningo` calls Ningo instead: the word is removed and the message counts as a mention.
  Everything else the owner writes in a group stays ignored, and the reply still comes from the
  owner's account, signed. Group turns are not kept in the owner's conversation archive, and the
  owner is not recognized by name there. The owner's private conversation is the "message yourself"
  chat.
- The "message yourself" chat is the owner's conversation with Ningo: it is kept in the conversation
  archive and continues the conversation the desktop shows, so you can start on the phone and go on
  at the desktop, or the other way round. The phone continues the latest conversation; in the
  self-chat, `/ajuda` (or `/help`) lists the commands to start, list, open, rename and delete
  conversations. Deleting asks for `/sim` first. Contacts and groups have no commands. Other chats and groups are never kept there. See
  [conversation history](conversation-history-plan.md#phone-the-whatsapp-self-chat).
- A reply to a bot message counts as a mention only for about 10 minutes after the bot sent it.
- Baileys 7 is a release candidate that follows WhatsApp's own changes: the version is pinned and a
  test checks the parts of its API that the interface uses, but an upgrade needs a real-account check.
- Everything here is covered by automated tests against fakes. It has not been connected to a real
  WhatsApp account.

## Security and Privacy Notes

- The session folder and the QR code give control of the account. The session lives in a named
  Compose volume (never in the repository) with mode `0700`, and the interface never logs or returns it.
  The QR code goes to the service log only.
- Baileys logs ids, keys and message details; the interface replaces its logger with one that keeps
  only warnings and errors as short text.
- The orchestrator receives normalized candidates and checks eligibility before chat processing.
  The bridge requires `DOKJA_GATEWAY_INGRESS_TOKEN`; metadata and the configured allowlists stay
  inside the stack. The chat session key and orchestrator log may carry a WhatsApp id, which for a
  one-to-one chat is the sender's phone number. Sender-number candidates are not logged.
- Real allowed numbers and group ids live in the ignored environment file, never in Git, tests or docs.

## Important Constraints

- WhatsApp does not call `dokja_services` directly
- WhatsApp does not decide workflows
- the text of a message can never pick how the orchestrator routes it: the event type is fixed to
  `message.created` by the interface and enforced again by the bridge
