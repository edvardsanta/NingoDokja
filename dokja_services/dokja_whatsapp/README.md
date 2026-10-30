# Dokja WhatsApp Interface

TypeScript WhatsApp communication adapter for Dokja. It links to a WhatsApp account as a linked device (Baileys), forwards normalized messages to the orchestrator and sends eligible replies. The Communications domain decides which chats may be answered. Full description, setup and limits: [dokja_docs/whatsapp-interface.md](../../dokja_docs/whatsapp-interface.md).

The protocol is unofficial: the account can be restricted or banned. Prefer a dedicated number. Nothing is answered unless it is listed.

Environment variables:

- `DOKJA_WHATSAPP_AUTH_DIR` folder of the linked-device session (required). It is a secret: it holds the keys of the account.
- `DOKJA_WHATSAPP_SELF_CHAT` answer the account's own chat (`true` by default)
- `DOKJA_WHATSAPP_ALLOW_FROM` phone numbers whose direct messages are answered, comma-separated
- `DOKJA_WHATSAPP_ALLOWED_GROUPS` group ids (`<digits>@g.us`) where the bot answers when mentioned, comma-separated
- `DOKJA_ORCH_HTTP_ENDPOINT`
- `DOKJA_GATEWAY_INGRESS_TOKEN` required gateway credential; set the same value on the orchestrator
- `DOKJA_ORCHESTRATOR_TIMEOUT_MS`
- `DOKJA_WHATSAPP_STATUS_PORT` internal port that answers the interface's state (default `8094`, never published)

Run (a QR code appears in the log on the first start; scan it in WhatsApp > Linked devices):

```bash
pnpm install --no-frozen-lockfile
DOKJA_WHATSAPP_AUTH_DIR=<folder outside the repository> pnpm dev:watch
```

Test:

```bash
pnpm test
```

Structure:

- `main.ts`: process entrypoint
- `config.ts`: environment parsing and phone number matching
- `messages/`: echo, duplicate, age and empty-text filtering before orchestrator dispatch
- `transport/`: orchestrator bridge client
- `runtime/`: connection loop (`whatsapp_runtime.ts`), message conversion (`inbound.ts`), the only file that imports Baileys (`baileys_socket.ts`) and the QR renderer
- `tests/`: WhatsApp interface test suite

## Document summaries and publication links

In an eligible conversation, send a document to request a summary. Its optional
caption supplies the requested focus. Supported extensions are PDF, DOCX, EPUB,
TXT, Markdown, reStructuredText, HTML and XML/RSS/Atom, served by the enabled
`document.extract.v1` Feeds plugin. Scanned PDFs require a reader with OCR; this
adapter does not add OCR. The chat inference provider generates the summary.

The adapter first sends attachment metadata through the authenticated bridge.
Communications authorizes that request before Chat asks for file bytes. Denied,
stale, duplicate and bot-echo messages never download the attachment. Accepted
files are decrypted by Baileys, bounded to 10 MiB with a 30-second download deadline,
and sent as bytes through the same bridge. The HTTP bridge has a 15 MiB JSON body
limit to accommodate base64. No interface calls Feeds or a model directly.

Summaries use up to the first 80,000 Unicode characters of extracted text. Larger
readings receive an explicit localized excerpt notice. Reading does not silently
save the document in the Library or grant it authority over system instructions.
These are one-off summaries; they do not ingest the source into long-term memory.

Send a standalone HTTP(S) publication URL to request its original caption, source
link and image/video media. Digest selects an enabled `social.resolve.v1` reader,
then asks that same reader for each file using `action: "media"`. Reader URLs are
never fetched by the WhatsApp adapter. The reader must return bounded base64 bytes;
metadata-only readers produce a localized unavailable message. Limits are 10 files,
8 MiB per file and 16 MiB total, using JPEG, PNG, WebP or MP4. Long captions continue
in signed text messages. The adapter quotes the original incoming message and
remembers outgoing media IDs for echo suppression.

`DOKJA_WHATSAPP_LOCALE` defaults to `pt-BR`; `en` selects English. All new fixed
replies, errors and Ningo signatures come from matching locale catalogs. Summary
prompts request the configured language; post captions preserve their original
language. The existing direct/group/self-chat eligibility policy still applies.

Rebuild the orchestrator and WhatsApp adapter together, keep Feeds running, and
enable a document extractor and a media-capable post reader. Restart Feeds after
changing reader manifests or their output budgets. An optional generic
host-socket relay is provided under
`dokja_services/dokja_feeds/plugins.example/post-reader-relay`; private reader
settings and tokens remain outside Git. No real WhatsApp message is sent by tests.
