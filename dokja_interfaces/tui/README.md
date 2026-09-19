# Dokja TUI

Terminal UI for administering Dokja. It is an interface adapter like the CLI: it only talks to the
orchestrator (ZeroMQ request/reply) and reuses the CLI's `Requester`.

Run it (from the repo root; the container gets a TTY):

```bash
docker compose -f docker-compose.dev.yml run --rm --no-deps dokja-tui
```

Or directly, with `libzmq` installed:

```bash
cd dokja_interfaces/tui && go run ./cmd/dokja-tui
```

Flags: `--request-endpoint` (default `DOKJA_ORCH_REQUEST_ENDPOINT` or `tcp://127.0.0.1:5558`),
`--refresh` (panel poll, default `5s`), `--timeout` (one orchestrator answer, default `2m`).

## Language

The interface is available in English (the default) and Portuguese. It is chosen with
`--lang en|pt`, else `DOKJA_LANG`, else the system locale (`LC_ALL`, `LC_MESSAGES`, `LANG`;
any `pt*` value selects Portuguese), else English. `--tab` accepts `panel`, `memes`, `discord`
and `history` (the Portuguese names `painel` and `historico` also work).

The text in the code is English; `tui/i18n_pt.go` maps each message to Portuguese and a message
without an entry is shown in English. To add a language, add a catalog and a case in
`SetLanguage`. The tests fail when a message has no Portuguese entry, when an entry is stale,
when a translation changes the format verbs, and when any screen shows Portuguese words in
English mode. Text that comes from the orchestrator (for example a service's `detail`) is
English in both modes.

## Screens

- **Painel** (`1`): services, scheduler jobs, pool counts and the delivery channels. The channel that only
  accepts NSFW-screened memes is marked. Refreshes on its own while this tab is open; `r` forces it.
  Rows are selectable (`↑↓`): `space` switches a service off/on or pauses/resumes a job, `x` runs the
  selected job now (asks first), `i` changes its interval (`45m`, `6h`, or `default`).
- **Memes** (`2`): browse the pool without consuming it. `s` runs the NSFW screen on the selected meme
  (verdict, text read by OCR, detections). `enter` sends *that* meme to the channels you tick.
  `d` dispatches N memes from the top of the queue. `t` switches queue/already-sent, `n`/`p` page,
  `R` refreshes the pool.
- **Discord** (`3`): send text and/or an image to chosen channels (`ctrl+s`).
- **Histórico** (`4`): what you triggered in this session and how each action ended.

Anything that posts to Discord asks for confirmation first (`y`/`n`). Keys: `1`-`4`, `tab`, `F1`-`F4`
switch tabs (digits are typed, not treated as shortcuts, while a text field is focused).

## Previews (images and videos)

The Memes tab draws the selected meme next to the list (below it on narrow terminals). Videos play as a
short loop (up to 5 s at 8 fps, extracted with `ffmpeg`).

- `--images auto` (default, or `DOKJA_TUI_IMAGES`) picks the best mode: **kitty** in Kitty and Ghostty
  (real pixels, using the kitty graphics protocol with unicode placeholders), **blocks** (colored half-block
  characters, any truecolor terminal) inside tmux or elsewhere, **off** without truecolor.
  Force one with `--images kitty|blocks|off`.
- Images are downloaded by the TUI itself (many image hosts need a browser User-Agent and a Referer from their own site, which it sends).
- A preview is only uploaded to the terminal when its meme is selected, and old ones are deleted, so
  scrolling past videos costs nothing. A video is roughly 5 MB of terminal traffic when first shown.
- Inside the container, `docker-compose.*.yml` passes `KITTY_WINDOW_ID`, `TERM_PROGRAM` and `COLORTERM`
  so detection works there, and the image ships `ffmpeg` (rebuild with `--build` once).
- `--tab memes` opens straight on the Memes tab.

## Switches and jobs

- A service switch is **logical**: while a service is off the orchestrator refuses its events and says so
  (the Memes tab shows "recusado pelo orquestrador"); the container keeps running. It also refuses to use
  the meme service's NSFW filter, so an image can never reach a safe-only channel unchecked.
- Pausing a job makes the orchestrator skip its *scheduled* runs; the scheduler keeps ticking. A **manual**
  run (`x`) ignores the pause but not a switched-off service, and the confirmation says which case it is.
- Everything is saved (`VA_STATE_FILE`) and survives restarts, so a job paused now stays paused when the
  scheduler boots, including its first run on startup.
- "Next run" and the interval come from the scheduler, which announces them about once a minute. The panel
  warns when it has been silent for a while (probably not running).

## Behaviour worth knowing

- One request at a time. The orchestrator answers requests serially and a dispatch that screens images
  can take a while, so the panel poll is skipped and other actions wait while one is running.
- The safe-only rule is enforced by the orchestrator, not the TUI: an image bound for a safe-only channel
  is screened server-side and skipped if flagged. Sending a picked meme from the queue also marks it as
  sent so the scheduler does not repeat it.
- `chat_ai` shows `error` when that service is not running; the meme features do not depend on it.

## Chat profiles

`p` on the Painel opens the chat provider profiles (the active one is also shown next to `chat_ai`). `enter`
selects one through the orchestrator (name only). `n` creates one and `d` deletes one, both written to the local
database directly: the token is typed masked, is cleared from the form once saved, and is never shown, logged
in the history or sent to the orchestrator. Without local access to the database (`--db` / `DOKJA_DB_FILE`) you
can still list and select, just not create or delete.
