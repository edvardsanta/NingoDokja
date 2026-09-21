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
- `book ...` commands upload a file payload to the orchestrator and print the returned JSON
- `discord ...` commands are administrative and also use request/reply
- `emit ...` uses asynchronous event publishing
- `--timeout` (default `2m`) bounds how long a request waits for the orchestrator

Examples:

```bash
cd dokja_interfaces/cli
go run ./cmd/dokja-cli meme status
go run ./cmd/dokja-cli meme fetch --limit 3
go run ./cmd/dokja-cli meme refresh --max-items 10
go run ./cmd/dokja-cli meme pool
go run ./cmd/dokja-cli meme dispatch --limit 2
go run ./cmd/dokja-cli meme screen https://example.com/meme.jpg
go run ./cmd/dokja-cli services disable book
go run ./cmd/dokja-cli jobs list
go run ./cmd/dokja-cli jobs disable meme.dispatch
go run ./cmd/dokja-cli jobs interval meme.refresh 30m
go run ./cmd/dokja-cli jobs run meme.refresh
go run ./cmd/dokja-cli discord send --all --text "teste kkk"
go run ./cmd/dokja-cli discord send --channel 123456789012345678 --text oi --image https://example.com/a.png
go run ./cmd/dokja-cli book classify ./test/sample.pdf
go run ./cmd/dokja-cli book summarize ./test/sample.epub --title "Clean Architecture"
go run ./cmd/dokja-cli emit message.created --payload content=hello --payload channel=general
```

Administrative commands:

- `meme pool` (alias of `meme status`) shows unsent and sent counts.
- `meme list [--scope unsent|sent] [--limit N] [--offset N]` browses the pool without consuming it.
- `status` shows service health and the delivery channels (`ningo.status`).
- `meme dispatch --limit N` runs the scheduled delivery now, to every channel in
  `DISCORD_SCHEDULED_MEME_CHANNEL_ID`; channels in `DISCORD_SAFE_ONLY_CHANNEL_IDS` skip memes
  the NSFW screen flagged. `--limit` defaults to `1` and must be at least `1`,
  because an absent limit means "the whole pool" to the orchestrator.
- `meme screen <url>` downloads an image and prints the NSFW verdict, every detection
  with its score, and the active thresholds. Nothing is sent or marked.
- `discord send` posts text and/or an image (`--text`, `--image`) to `--channel <id>`
  (repeatable) or `--all`. The channel picked decides the transport: a channel with a
  webhook in `DOKJA_DISCORD_WEBHOOKS` goes through the webhook, any other through the
  bot. Only channels the orchestrator already delivers to (the meme delivery channels) are
  accepted; anything else is rejected before anything is sent. `--mark-sent` (with `--image`) also
  flags the meme as sent so the scheduler does not repeat it. Text-only sends are not screened.

Switches and jobs:

- `services list|enable|disable <meme|chat_ai|book|scheduler>`: a logical switch in the orchestrator. While a
  service is off its events are refused (scheduled jobs included). The container is not touched.
- `jobs list`: state, interval, last and next run of every scheduler job.
- `jobs enable|disable <job>`: pauses or resumes the job's scheduled runs; persisted across restarts.
- `jobs interval <job> <45m|6h|default>`: between 1 minute and 720 hours; the scheduler adopts it within
  seconds and restarts that job's countdown.
- `jobs run <job> [--limit N]`: runs the job now. A paused job does not block a manual run; a disabled
  service does. `meme.dispatch` needs `--limit` of at least 1 (default 1).

Chat provider profiles:

- `chat profile add <name> --base-url URL --model M [--token-env VAR | --token-file PATH] [--use]`: creates or
  replaces a profile by writing the local database directly. With no token source it asks on the terminal
  with the input hidden (or reads one line when piped). There is deliberately no `--token` flag, since it would
  end up in shell history. The token is never printed.
- `chat profile remove <name> [--force]`: local write; the active profile needs `--force`.
- `chat profile list`: through the orchestrator, keys masked (`…abcd`), active one marked.
- `chat profile use <name>`: through the orchestrator (it only selects an existing profile); the chat service
  uses it from its next request, no restart.

The database is `--db`, else `DOKJA_DB_FILE`, else the repository's `.dokja/dokja.db`.
