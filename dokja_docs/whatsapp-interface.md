# WhatsApp Interface

This document describes the Dokja WhatsApp interface.

The WhatsApp interface is implemented in:

- [dokja_interfaces/whatsapp](../dokja_interfaces/whatsapp)

Inbound flow (talking to Ningo in WhatsApp):

`WhatsApp -> Interface -> Orchestrator -> Domain -> Service -> Orchestrator -> Interface -> WhatsApp`

There is no outbound delivery path yet: the interface only answers messages sent to it.

## How It Connects

The interface does not use the official WhatsApp Business platform. It links to a WhatsApp account as
a **linked device**, the same feature WhatsApp Web uses, through the Baileys library (a TypeScript
client of that protocol). The phone stays the main device; Ningo appears next to it in
Settings > Linked devices.

- the service opens one outbound connection to WhatsApp and one to the orchestrator; it listens on
  nothing and publishes no port
- linking is done once by scanning a QR code printed in the service log
- the keys of the linked device are saved in a session folder (a Compose volume) and the service
  reconnects from them on every start; the QR code is not needed again
- what it receives is every message the account receives: the interface drops what is not allowed
  before anything is logged, stored or sent to the orchestrator

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

An ignored message leaves no trace in the log, and a phone number never reaches it. The one
exception helps with setup: a group the bot is not allowed in is reported **once** with its id and
the reason `policy`, so the id can be copied into `DOKJA_WHATSAPP_ALLOWED_GROUPS`.

### Replies

The reply is plain text, cut at 8,000 characters. In a group it quotes the message that triggered it.
While the orchestrator works the chat shows "typing"; the interface never marks messages as read and
never sets the account online, so the phone keeps its own notifications.

A failing orchestrator is answered with a short apology.

## Configuration

| Variable | Where | Purpose |
| --- | --- | --- |
| `DOKJA_WHATSAPP_AUTH_DIR` | WhatsApp interface | Folder of the linked-device session. **Secret.** Required; `/auth/default` in Compose |
| `DOKJA_WHATSAPP_SELF_CHAT` | WhatsApp interface | Answer the account's own chat, default `true` |
| `DOKJA_WHATSAPP_ALLOW_FROM` | WhatsApp interface | Phone numbers whose direct messages are answered, comma-separated |
| `DOKJA_WHATSAPP_ALLOWED_GROUPS` | WhatsApp interface | Group ids (`<digits>@g.us`) where the bot answers when mentioned, comma-separated |
| `DOKJA_ORCH_HTTP_ENDPOINT` | WhatsApp interface | Orchestrator bridge, `http://dokja-orchestrator:8091/orchestrator` in Compose |
| `DOKJA_ORCHESTRATOR_TIMEOUT_MS` | WhatsApp interface | Wait for the orchestrator, default `180000` |

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
2. If the link is not completed in time the service stops with a message. Start it again for a new code.
3. After linking the log shows `"whatsapp":"connected"`. Write to your own "message yourself" chat to try it.

### Unlink and start over

- On the phone, remove the device from Linked devices.
- Then remove the session volume, so the next start asks for a new QR code:

  ```bash
  docker compose -f docker-compose.lite.yml --profile whatsapp down
  docker volume rm <compose project>_dokja-whatsapp-auth
  ```

## Reconnection

The service owns the connection. A lost connection is retried after 1 s, doubling up to 60 s, and the
wait starts over once a connection opens. Linking ends with WhatsApp asking for a new connection;
that one reconnects at once.

Three situations stop the service instead of retrying, with a message that says what to do:

- the session was **logged out** from the phone (or by WhatsApp): remove the session volume and link again
- the session is **in use by another connection**: stop the other one
- the account is **not linked yet** and either a link code was shown but not scanned in time, or
  WhatsApp closed the connection 5 times in a row before issuing any code

Every reconnect line and every one of those messages carries the status code WhatsApp gave.

The session folder is never deleted by the service.

## Troubleshooting

- **The service stops before any QR code appears, with a status code (for example 405).** WhatsApp
  refused the connection. If the network is fine, the Baileys version is usually too old for the
  current WhatsApp and needs an upgrade (it is pinned in `package.json`; run the tests and check a
  real account after upgrading).
- **The QR code expired.** Start the service again; each start issues a new one.
- **Nothing happens when a friend writes.** Their number must be in `DOKJA_WHATSAPP_ALLOW_FROM`; an
  ignored message is not logged on purpose. A group is logged once with its id.
- **A recipient sees "waiting for this message".** The bot keeps what it sent for about 10 minutes so
  WhatsApp can encrypt it again on request; after that a retry cannot be answered.

## Limits

- Text only; no cards, buttons, mentions of people, files or streaming.
- The Discord text commands (`meme fetch`, `meme status`, `meme refresh`) are not available from
  WhatsApp: their text goes to the chat as written. The orchestrator bridge enforces this for the
  `whatsapp` interface: its event type is fixed to `message.created`.
- A group message triggers an answer for **anyone** in the group who mentions the bot. There is no
  per-sender limit yet, so allow only groups you trust.
- On a personal number the bot is you: what the account owner writes in a group is the owner's own
  message and is ignored, so only other members can summon Ningo there. The owner talks to it in the
  "message yourself" chat.
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
- The orchestrator receives only allowed messages. For those, the chat session key and the
  orchestrator log carry the WhatsApp id, which for a one-to-one chat is the sender's phone number.
- Allowed numbers and group ids live in the ignored environment file, never in Git, tests or docs.

## Important Constraints

- WhatsApp does not call `dokja_services` directly
- WhatsApp does not decide workflows
- the text of a message can never pick how the orchestrator routes it: the event type is fixed to
  `message.created` by the interface and enforced again by the bridge
