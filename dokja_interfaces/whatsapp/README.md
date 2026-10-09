# Dokja WhatsApp Interface

TypeScript WhatsApp interface for Dokja. It links to a WhatsApp account as a linked device (Baileys), forwards the messages it is allowed to answer to the orchestrator and sends the reply. Full description, setup and limits: [dokja_docs/whatsapp-interface.md](../../dokja_docs/whatsapp-interface.md).

The protocol is unofficial: the account can be restricted or banned. Prefer a dedicated number. Nothing is answered unless it is listed.

Environment variables:

- `DOKJA_WHATSAPP_AUTH_DIR` folder of the linked-device session (required). It is a secret: it holds the keys of the account.
- `DOKJA_WHATSAPP_SELF_CHAT` answer the account's own chat (`true` by default)
- `DOKJA_WHATSAPP_ALLOW_FROM` phone numbers whose direct messages are answered, comma-separated
- `DOKJA_WHATSAPP_ALLOWED_GROUPS` group ids (`<digits>@g.us`) where the bot answers when mentioned, comma-separated
- `DOKJA_ORCH_HTTP_ENDPOINT`
- `DOKJA_ORCHESTRATOR_TIMEOUT_MS`

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
- `messages/`: who gets an answer, orchestrator dispatch and duplicate protection
- `transport/`: orchestrator bridge client
- `runtime/`: connection loop (`whatsapp_runtime.ts`), message conversion (`inbound.ts`), the only file that imports Baileys (`baileys_socket.ts`) and the QR renderer
- `tests/`: WhatsApp interface test suite
