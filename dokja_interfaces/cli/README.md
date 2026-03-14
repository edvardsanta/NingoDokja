# Dokja CLI Interface

This CLI is an interface adapter. It does not call domains or services directly.

It uses two orchestrator channels:

- `DOKJA_ORCH_ENDPOINT`
  Default: `tcp://127.0.0.1:5555`
- `DOKJA_ORCH_REQUEST_ENDPOINT`
  Default: `tcp://127.0.0.1:5558`
- `DOKJA_ORCH_TOPIC`
  Default: `events`

Behavior:

- `meme ...` commands use synchronous request/reply with the orchestrator and print the returned JSON
- `emit ...` uses asynchronous event publishing

Examples:

```bash
cd dokja_interfaces/cli
go run ./cmd/dokja-cli meme status
go run ./cmd/dokja-cli meme fetch --limit 3
go run ./cmd/dokja-cli meme refresh --max-items 10
go run ./cmd/dokja-cli emit message.created --payload content=hello --payload channel=general
```
