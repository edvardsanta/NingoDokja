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
- `memory ...` commands inspect the experience memory (what the bot did in a context and how it turned out) with request/reply
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
go run ./cmd/dokja-cli meme hashtag tag https://example.com/pave.jpg TioDoPave
go run ./cmd/dokja-cli meme hashtag suggest https://example.com/new.jpg
go run ./cmd/dokja-cli meme hashtag list
go run ./cmd/dokja-cli meme hashtag untag https://example.com/pave.jpg
go run ./cmd/dokja-cli services disable book
go run ./cmd/dokja-cli memory status
go run ./cmd/dokja-cli memory predict --action hashtag.suggest some words from a meme
go run ./cmd/dokja-cli memory recall --action hashtag.suggest some words from a meme
go run ./cmd/dokja-cli memory score --action hashtag.suggest
go run ./cmd/dokja-cli memory list --state pending --limit 10
go run ./cmd/dokja-cli jobs list
go run ./cmd/dokja-cli jobs disable meme.dispatch
go run ./cmd/dokja-cli jobs interval meme.refresh 30m
go run ./cmd/dokja-cli jobs run meme.refresh
go run ./cmd/dokja-cli discord send --all --text "teste kkk"
go run ./cmd/dokja-cli discord send --channel 123456789012345678 --text oi --image https://example.com/a.png
go run ./cmd/dokja-cli book classify ./test/sample.pdf
go run ./cmd/dokja-cli book summarize ./test/sample.epub --title "Clean Architecture"
go run ./cmd/dokja-cli emit message.created --payload content=hello --payload channel=general
go run ./cmd/dokja-cli conversations list
go run ./cmd/dokja-cli conversations read 1a2b3c4d
go run ./cmd/dokja-cli conversations rename 1a2b3c4d Justice and power
go run ./cmd/dokja-cli conversations delete 1a2b3c4d
go run ./cmd/dokja-cli chat --continue And from the terminal?
go run ./cmd/dokja-cli chat --conversation 1a2b3c4d
go run ./cmd/dokja-cli chat --new A new topic
```

Conversations are the archive the desktop and the phone share: `--continue` takes the latest
general conversation, as the phone does, and without a message opens the REPL there. A plain
`chat` is a session of its own and is not archived.

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
- `meme hashtag tag <url> <hashtag> [--text]` stores explicit hashtag knowledge: this meme's
  text (read from the image itself, or `--text` to give it directly) means this hashtag.
  `meme hashtag suggest [url] [--text] [--min-score]` finds the closest tagged example
  and reports its hashtag; check the `relevant` field, a hit below the threshold is
  still shown but is not a match. `meme hashtag list` and `meme hashtag untag <url>`
  browse and remove tagged examples. Knowledge retrieves candidates; experience memory
  predicts and records whether used suggestions are accepted. See
  `dokja_services/dokja_meme/README.md`.
- `memory ...` works with the experience memory, where an experience is "in this context the bot
  took this action, and this was the outcome". `memory predict --action <name> <context...>`
  estimates how likely the action is to be accepted from the similar earlier experiences, against
  a baseline, and says when there is too little evidence. `memory recall` lists the closest
  experiences with their outcomes, `memory score` scores the stored predictions against what
  happened (and says when there are too few to judge), `memory record`, `memory resolve`
  (`--outcome`, or `--observed` to compare with what the bot did), `memory show` and
  `memory forget` manage single experiences, `memory list [--state pending|resolved] [--action]
  [--limit] [--offset]` pages through them newest first with a snippet of each context (it is how you
  recognize one, since the ref is a hash) and, for a hashtag suggestion, how close the earlier tagged meme it
  rested on was and the start of its text, and `memory status` and `memory reindex` look after the
  service. See `dokja_docs/experience-memory-plan.md`.
- `discord send` posts text and/or an image (`--text`, `--image`) to `--channel <id>`
  (repeatable) or `--all`. The channel picked decides the transport: a channel with a
  webhook in `DOKJA_DISCORD_WEBHOOKS` goes through the webhook, any other through the
  bot. Only channels the orchestrator already delivers to (the meme delivery channels) are
  accepted; anything else is rejected before anything is sent. `--mark-sent` (with `--image`) also
  flags the meme as sent so the scheduler does not repeat it. Text-only sends are not screened.

Switches and jobs:

- `interfaces link whatsapp [--new] [--wait 3m]`: draws the code to scan in the terminal and waits until the
  account is linked; `--new` asks for a fresh code first. `interfaces unlink whatsapp [--yes]` unlinks it and
  erases the saved session, after asking. Both need `DOKJA_COMMUNICATIONS_TOKEN`, the credential the
  orchestrator and the desktop share; it is sent only with `communications.*` requests and never printed.
- `settings list` / `settings set <id>=<value>... [--yes]`: the stack's non-secret settings (Discord
  channels and DMs, WhatsApp people and groups, models), saved in the shared local database. Setting saves
  say which modules are recreated and ask first. Delivery channels show whether the bot or a webhook
  delivers; webhooks and tokens are edited in `.env` by hand. The embedding
  profile is fixed and cannot be edited. Settings are shared with desktop and TUI
  through the controller; its first read imports allowed legacy options once.
  Manual Compose commands still use `.env`; see the production runbook.
- `modules list|start|stop <module> [--wait] [--runtime-root DIR]`: reports or changes local container
  lifecycle through the runtime controller the desktop and TUI also use. This includes the Discord and
  WhatsApp communication adapters. Start brings up dependencies; stop keeps data. It never changes
  application routing or scheduled jobs. Starting `discord` or `scheduler` first prints what will happen
  with the saved routing and jobs, then asks; `--yes` skips the question. Run it on the host; inside a
  container it is refused.
- `services list|enable|disable <meme|chat|book|knowledge|memory|feeds|scheduler|discord|whatsapp>`:
  reports or changes application routing in the orchestrator. For communication adapters this
  controls whether inbound events and outbound delivery are accepted; it does not start or stop
  their containers.
  Use `modules` for container lifecycle and `services` for application routing; the desktop and TUI join
  these states so each communication service appears once.
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

## Answering Knowledge classification questions

A document may have a known form and an unresolved content kind. The CLI prints
`knowledge classify <source-id>` for pending documents in ingest/list output.
This is separate from the legacy `--kind` storage label.

```sh
dokja-cli knowledge list
dokja-cli knowledge classify file:study.pdf
# Or supply explicit answers without interactive prompts:
dokja-cli knowledge classify file:study.pdf --form book --content-kind research
```

The command gets the current questions through the orchestrator and asks only
for unresolved axes. Enter leaves an axis pending; EOF cancels submission.
No default is selected. With flags it submits only the named axes and never
prompts for the others. The Knowledge domain validates and attributes every
answer to the user. The command can also explicitly correct an existing label.

Answers patch the stored document's metadata: no file upload, source fetch,
extraction, chunk replacement or embedding call. A revision from the question
snapshot guards the update. If content or metadata changes while the user is
answering, the command reports `classification_conflict`; run it again to read
the new questions and decide. It never retries an answer against a newer
revision automatically. No persistence, transport or credential setting changes
are needed. Desktop and chat presentation of this interaction remain separate
interface work.

To ask the configured provider to revise an automatic classification on stored
text, use `dokja-cli --timeout 7m knowledge reclassify <source-id>`. This sends a
bounded reconstruction of stored chunks to the enabled reading-host classifier;
it does not need the original file. Each explicit request runs inference again.
User answers, source defaults and axes already awaiting user clarification stay
unchanged. An uncertain new proposal becomes a question for `knowledge classify`.
Conflicts are reported without automatic retries. No chunks or embeddings change.

`dokja-cli providers` inspects host Codex/Claude Code executables and lists models
in the selected running lite stack’s Ollama. It starts no service and runs no
inference. See [provider inventory](../../dokja_docs/provider-inventory.md).

`providers configure <provider> --enabled=false|true --models=<names> [--yes]`
saves discovery eligibility and unverified declarations in the shared database.
Supported provider IDs are `codex`, `claude_code` and `ollama`.
Either option may be omitted to preserve that field; `--models=` clears the list.
The command refuses stale revisions rather than overwriting another interface's
edit. It does not change legacy inference consumers or the embedding profile.

Chat requires an explicit enabled provider and declared model. Select it in Desktop
System → Inference providers → Chat provider and model, or run
`dokja-cli providers chat <provider> <model>`. Missing/disabled configuration
shows a setup error. Archived profile editing no longer selects live inference.
