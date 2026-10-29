# Dokja Desktop

Desktop interface for Ningo. It is an interface adapter like the CLI, the TUI and Discord: it only
talks to the orchestrator over ZeroMQ request/reply and holds no business logic. The plan, the
decisions and the Phase 0 results are in [`dokja_docs/desktop-app-plan.md`](../../dokja_docs/desktop-app-plan.md).

Status: v1 without voice, laid out as a workspace (Phase A of the workspace replan in the plan; the
redesign under way is in [`dokja_docs/desktop-ux/`](../../dokja_docs/desktop-ux/)). A sidebar on the
left lists five workspaces, Talk, Library, Notebook, Messages and, at the foot, System; the open one
fills the rest of the window as one panel: a legend that says where the owner is (workspace › view),
a header with the views on the left and one refresh plus the workspace's actions on the right, and a
body that is the only thing that scrolls. Modules, services, interfaces and jobs are named in words
(Chat, Scheduler, Post memes); the id the orchestrator reports stays in the tooltip. A module is
started and stopped (a verb, a button); a switch is on or off (a state). A sidebar entry says its state
in a word only when something behind it is not well. Each workspace asks its service only when it is first
opened, and then keeps what it showed. The screen opens on Talk, the conversation with Ningo. The status
(`ningo.status`) is read once, at the top of the screen, when it starts (and again when System asks
for it): the name's pulse, the dot next to a workspace (the worst state among the services behind
it) and the number at the foot (the services with a problem) all show that one answer. When the
orchestrator does not answer, the foot says `offline` and System opens instead of Talk, unless the
owner has already moved. The sidebar asks nothing itself.

| Workspace | Asks | Shows |
|---|---|---|
| system › settings | `runtime.settings`, `runtime.settings.set`, when the view opens | Discord channels (each delivery channel marked bot or webhook) and DMs, WhatsApp people and groups, and models; editing one shows which modules are recreated and asks |
| system › status | `ningo.status`, `services.set`, `scheduler.jobs.set` | what needs attention first (every part with a problem or not checked, its state and why), then how every service is doing, with a switch for each one the orchestrator can switch, the chat interfaces (Discord, WhatsApp) under their own heading with their state and a switch, and the scheduled jobs with their own switch and interval; the name's ghost follows it |
| talk | `message.created` when something is sent | the conversation with Ningo: his opening line, beginnings of a question that fill the message (they never send by themselves), the transcript, and the message docked at the foot (Enter sends, Shift+Enter breaks the line); "Start a new conversation" gives it a new id. With the orchestrator down it says so and offers to open System |
| notebook (memory) | `memory.status`, `memory.stats` | the experience memory and how well its predictions score |
| library (knowledge) | `knowledge.status`, `knowledge.search`, `knowledge.ingest`, `knowledge.list`, `knowledge.reindex`, `knowledge.delete`; `digest.status`, `digest.items` in its readings view | two views, as tabs (the arrows move between them). **Documents**: a search over the Library first, then its documents, listed as the view opens; **Add to the Library** beside the search opens the form, in a drawer over the right of the window, to add a note, a web address or a file (Ctrl+Enter sends a note; a file can be dropped anywhere on the workspace, which lights up while it is dragged over and opens the drawer; Esc or × closes it). A document's title opens it; its menu (a right click on the row, the Menu key or Shift+F10 on it, or its ⋯ button) holds **Open**, **Classify…**, which opens it with its classification shown, and **Delete…**. **Readings**: source and inclusive date-range filters (UTC), plus text search, are applied by the Digest domain before pagination; explicit dates search all collected cached items, while the unfiltered digest keeps its freshness window. Each entry has an expandable preview of collected full text or a labeled excerpt, and pictures/videos load through the shell only when expanded. **Readings**: what the feeds service reads and the first entries it gave, newest first; reading is how knowledge comes in, so it is a view of this workspace, asked only when opened; **Save to the Library** on a reading adds its page to the Library (`knowledge.ingest`): the screen names the reading by id and the shell keeps any full source text and address when it projects the page, then sends the full text with a stable per-reading document id and the address as provenance; when full text is absent it sends the address for fetching; the screen receives bounded plain text for its expandable preview and media references only through the shell preview gate; **Discuss with Ningo** opens Talk on a new conversation about that reading, named above the transcript with **Close discussion**, and every turn carries the reading's id (the list itself has no composer) |
| messages (communications) | `communications.channels`, `communications.history`, `communications.send` | the text channels of the servers in `DISCORD_GUILD_ID` (grouped under each server's name, with a note when a server cannot be read), the configured Discord channels and the WhatsApp conversations the owner named; any listed channel can be written to by hand, while scheduled memes still go only to their channels; paginated history, text and meme delivery through Ningo to Discord; the conversation fills the workspace, its history scrolling above a one-row composer; the image button beside the field opens the waiting memes in a tray over the history (previews fetched by the shell; **Fetch new memes** at its top asks the sources now, through `meme.pool.refresh`, says how many came and shows the first page again), a click on a picture attaches it (a second click takes it off), and the attached meme stays in a chip with its own × and the force-send option. After a send the tray stays open and reads its page again, so the meme just sent leaves it. Without `DOKJA_COMMUNICATIONS_TOKEN` the workspace says messages are not set up and what to set |

Ctrl+1 to Ctrl+5 open the workspaces in the sidebar's order (System is Ctrl+5), from anywhere, a field
included; the up and down arrows (with Home and End) move through the sidebar when it has the focus.
Alt+1 to Alt+3 open the views of the workspace on screen (Library, System), and Ctrl+K opens Talk
with the focus in its message, from any workspace. A bare digit is always typed, never taken as a
shortcut. Memes have no workspace of their own: Ningo
sends them, so they are picked in Messages. Changes to the research base require a real click or key press in the window (see
"How it is built"). The Library lists its documents as it opens, can open one in a tab of its own (its text, rebuilt
from the stored passages and said to be so, asked for alone with `include_text`; a tab closes with its
×; a search hit opens its document the same way, read by its id), page through them, index one
batch of up to 32 pending passages per click, and delete a selected document after a native confirmation
naming its id. Cancelling leaves the document alone. A timeout asks the person to refresh and check;
changes are never retried automatically. Adding, indexing and deleting refresh the counts and any open
document list. The messages workspace sends to Discord through its own confirmed action. Nothing opens an external link in a browser.

System is also where things are switched on and off. Each service the orchestrator can switch
(`feeds`, `chat`, `knowledge`, `memory`, `scheduler`; legacy mode also lists `meme` and `book`) has a switch, and each scheduled job has a
switch and an interval (minutes or hours, between one minute and thirty days; "Back to default" drops a
custom one). Switching `scheduler` off pauses every job. A switch needs a real click or key press just
before it, like any change, and asks for no window, because the operator can switch it back at once. The
screen never trusts its own guess: after every change it reads the status again and shows what the
orchestrator holds, and a refused or timed-out change says so.

## Run

```bash
cd dokja_interfaces/desktop
pnpm install
pnpm start     # builds, then opens the window
```

The Desktop reads `DOKJA_COMMUNICATIONS_TOKEN` from the checkout's ignored `.env`
when it is absent from the process environment. An exported value takes precedence,
including an explicitly blank value. Only this credential is read; other entries
are not imported and shell expressions are never executed. The value stays in the
main process. Restart Desktop after changing the file.

If your Compose stack uses `.env.prod`, select the same file from this directory:

```sh
pnpm start --env-file=../../.env.prod
```

An explicit file path is relative to the launch directory. Without a discoverable
checkout, use `--runtime-root` or an explicit `--env-file`; the Desktop does not
guess a production file or generate another token.

It needs a running orchestrator. By default it asks `tcp://127.0.0.1:5558`, the port the compose
stacks publish. Without one the card says so after about three seconds. Each workspace needs the service
behind it: a service that is stopped or switched off is said so on its workspace, and the others keep working.

### The sources view of knowledge

The sources view needs the feeds service (`dokja_services/dokja_feeds`) and, in the orchestrator, the
`digest.*` route. The repository ships no source: a source is a plugin you keep outside it (see the
service's README). With no plugin the workspace says why there is nothing to read (no directory, no source,
none enabled) instead of showing an empty list. A source that fails is listed with its reason, and the
list opens by itself when one does.

Flags: `--request-endpoint` (default `DOKJA_ORCH_REQUEST_ENDPOINT` or `tcp://127.0.0.1:5558`),
`--env-file` (communications credential file), `--lang en|pt`, and `--timeout` (the longest a request may wait, default `2m`; the health card asks for
15 s). For example: `pnpm start --lang=pt`.

## Post links

The shell can open a link to a public post without signing in. It asks the
orchestrator (`digest.link.read`), which asks the post readers the owner installed
as Feeds plugins (see `dokja_docs/feeds-content-plugins.md`, "Post readers").

- The shell downloads the post's pictures and videos straight away into a folder
  in the system's temporary directory. It erases that folder when the app quits,
  and erases one left by a crash on the next start. The screen sees those files
  only as `ningo-post://` addresses that the shell serves itself. Nothing is
  fetched from the page.
- Saving a post copies its files to the posts folder, one folder per post. It then
  adds the caption, author, date, link and the saved file names to the Library
  (`knowledge.ingest`) under the post's own id, so saving it again updates the same
  document.
- After a save, the screen can open that post's folder in the system's file
  manager. It only works on a real press, and only for a post saved in this run.
- `--posts-dir` chooses the posts folder. The default is `saved-posts` in the
  app's data folder.
- A reader that hands its files out from a server on this machine needs that one
  origin named with `--post-media-origin` or `DOKJA_POST_MEDIA_ORIGIN` (for example
  `http://127.0.0.1:9000`). The shell never fetches from any other address on this
  machine or the local network.

## Replies as Ningo writes them

Talk can show a reply while it is being written. Set `DOKJA_BRIDGE_ENDPOINT` to the orchestrator's
HTTP bridge on this machine (for example `http://localhost:8091`; any other host is ignored) and
`DOKJA_GATEWAY_INGRESS_TOKEN` to the bridge's credential, the one the orchestrator has. The shell then
sends each Talk turn to `/orchestrator/chat/stream` and hands the screen the text so far, for that
conversation only; the reply that lands replaces it. Without both, a turn goes over the request socket
and the screen waits for the whole reply, as before. Only Ollama and Codex give previews; a Claude
reply arrives whole.

## WhatsApp conversations

The messages workspace also lists the WhatsApp conversations the owner named on the interface
(`DOKJA_WHATSAPP_CONVERSATIONS`), as "WhatsApp · alias". Their text is what the interface recorded
itself, shown with who wrote in words and a note on how long it stays on the machine. The composer of
one sends text only, as the owner, from the linked account: the shell asks in a window of its own,
naming the conversation by its alias, and the interface keeps a fixed budget (6 a minute, 40 an
hour). The chat itself is never named: only its alias reaches the desktop.

## Linking WhatsApp

On System, known interfaces stay visible even before setup. An unconfigured
WhatsApp shows **not configured** and setup guidance, without querying a link
code or offering an unusable routing toggle. Once configured, the WhatsApp row
has a **Link** button. It shows the code to scan while the
account waits for a link, and **New code** and **Unlink and start over**. It needs the
communications credential (`DOKJA_COMMUNICATIONS_TOKEN`, loaded from the environment or selected
env file, with the same value the orchestrator has) and `WHATSAPP_INTERFACE_STATUS_ENDPOINT` set on the orchestrator. The shell
draws the code from rows of modules it validated, so the page gets a picture and never the code as
text. Starting over is asked in a window the shell owns, because it unlinks the device and erases the
session. See [`dokja_docs/whatsapp-interface.md`](../../dokja_docs/whatsapp-interface.md).

## Communications

The messages workspace centralizes the configured Discord channels. It lists 30 messages at a time,
including messages written before the desktop started, with author, bot marker, time, edits,
reply ids and attachment filenames. Older pages are read on demand. The latest page refreshes
every 15 seconds only while the workspace and window document are visible, and with the panel's
refresh. An error pauses polling until a manual retry succeeds.

Each named server heading expands or collapses its channel list with a click,
Enter, or Space. Groups open independently; the current conversation and draft
stay in place, and a Messages refresh preserves collapsed groups. The same
control is available when there is only one server.

The composer sends text (up to 2000 characters) and optionally a meme picked from the waiting
queue's previews. The main process confirms the exact channel id, text and attachment address in a native
window. The renderer cannot request broadcast, force a safety bypass, mark a meme as sent, choose
an unlisted attachment, or retry a delivery automatically. On cancellation or failure the draft
stays in the form. On timeout, check the latest messages before retrying because delivery may
have succeeded. Changing channels clears that channel's current draft; switching workspaces preserves it.

Setup:

1. Run an orchestrator built with the `communications.*` routes and the new
   [`dokja_communications` service](../../dokja_services/dokja_communications/README.md).
2. Configure `DOKJA_COMMUNICATIONS_TOKEN` with the same random value of at least 32 characters
   for the orchestrator and service. The Desktop reads it from the checkout's `.env`, or the
   file selected by `--env-file`; an exported value overrides the file. It is separate from the
   bot token and never enters the renderer. Missing credentials leave the other cards usable.
3. Use a loopback or IPC request endpoint. For a remote orchestrator use a secure local tunnel;
   the desktop refuses to send this credential directly to a remote ZeroMQ address.
4. Keep `DISCORD_SCHEDULED_MEME_CHANNEL_ID` consistent between orchestrator and service. These
   are the existing permitted delivery channels. Set the bot token in the service, and optionally
   `DISCORD_GUILD_ID` for friendly channel names. The service also needs `DOKJA_DISCORD_API_BASE_URL`,
   the same operator input the delivery interface uses; it has no built-in value.

No channel history is stored locally. The ordinary bot and configured delivery webhooks retain
 their current behavior. This workspace does not log in as the operator's personal Discord account.

## Language

English by default, Portuguese on request. The language is `--lang`, else `DOKJA_LANG`, `LC_ALL`,
`LC_MESSAGES`, `LANG`, else the system locale; any value starting with `pt` selects Portuguese. Screen
text is English in the code and lives in two catalogs, `src/renderer/i18n/locales/en.json` and
`pt.json`, addressed by stable message ids. Add or change a message in both. The tests fail on a
missing, unused or untranslated id, on a changed placeholder and on Portuguese leaking into English.
Text that comes from the orchestrator (a service's `detail`) is shown as it arrives.

## Install notes

- Electron has no install script: it downloads its binary the first time it runs. To use one your
  system already has, point `ELECTRON_OVERRIDE_DIST_PATH` at its directory (on Arch,
  `/usr/lib/electron43` for the `electron43` package). The version is pinned to the one the Phase 0
  spike validated.
- Use one pnpm major per checkout. If pnpm complains that `node_modules` was linked from another
  store, run `pnpm install` again with the pnpm you use.

## Development

```bash
pnpm dev:web     # the screen in a browser, with made-up data and no shell
pnpm typecheck
pnpm test        # Node test runner for the logic, Vitest for the screen
pnpm test:e2e    # builds, then runs the built app against a fake orchestrator
```

`test:e2e` starts the real Electron shell with its own profile and drives its page over the Chromium
debugging protocol. It checks at run time what no unit test can: the page has no Node and only three
functions to reach the shell, the content security policy is strict, it cannot navigate or open a window,
every permission is denied, it makes no request of its own to the network, an action outside the
allow-list never reaches the orchestrator, a change is refused unless a real click or key press came
just before it, a reply is cut down to what a card reads, and a picture is fetched only at an address
the orchestrator listed. The app is shown in a compositor with no screen (weston's headless backend) when weston is installed, so the test never takes the focus of a person's screen; without weston it uses the display, and someone typing meanwhile can make it fail. Without any display it is skipped.

`dev:web` is the fast loop for the look. To run the real shell against it, start the dev server and
launch Electron with `DOKJA_DESKTOP_DEV_URL=http://localhost:5173` (only a local address is accepted).

## How it is built

```
screen (React)  ->  preload (four functions)  ->  main process  ->  orchestrator
src/renderer        src/preload                   src/main          ZeroMQ REQ, or the HTTP
                                                                    bridge for a streamed Talk turn
```

- **The screen** only knows a `Transport` (`src/shared/transport.ts`): `request(type, payload)`
  resolves with data or with an error code, never throws. In Electron the preload implements it
  over IPC; in a browser a fake does. Nothing in the screen depends on the shell.
- **The main process** owns the socket. It sends up to eight requests at once (the orchestrator
  answers that many in parallel, so a chat turn does not hold the Library), and counts any wait for
  a turn against each request's deadline. Each request uses its own socket, because a REQ socket that timed
  out can never send again and a late reply must not be taken for the next answer. `immediate` mode
  makes it fail fast, as "unavailable", when nobody is listening.
- **The allow-list** (`src/shared/actions.ts`) names the only actions the screen may ask for. Anything
  else is refused before the socket is touched: the orchestrator has no authentication and also
  exposes administrative actions (`discord.send`, `scheduler.jobs.announce`, `chat.profile.use`). The
  switches (`services.set`, `scheduler.jobs.set`) are on the list as changes, and the scheduler's own
  heartbeat is not: a page could otherwise fake a running scheduler. A job change carries only what the
  screen supplied, so switching a job never clears its interval.
- **A change needs the person** (`src/main/presence.ts`): the actions in `WRITE_ACTION_TYPES` are refused
  unless the browser reported a real mouse press or key press in the window in the last 3 seconds. A
  script in the page can call the shell but cannot fake that input, so content that tricks the page into
  running code cannot write on its own, and it cannot wait for an unrelated click either. The payload of
  `knowledge.ingest` (a note, an address or a file) is built by the shell: the person chooses its kind,
  where it came from and an id, within the service's own rules (a kind is one lowercase word, an id uses
  letters, digits and `. _ : / # @ -`), but never the service's `source` field: an address must be `http`
  or `https` (anything else would be handed to one of the owner's own plugins). Without an id of its own,
  a note or a file gets one made from what it holds, so adding the same thing twice changes nothing and
  two notes with one title do not replace each other; an id the person chose replaces the document that
  has it.
- **Payloads and projections** (`src/main/actions.ts`): each action builds its own payload, so the
  screen cannot add fields or change the digest's rules, and hands the screen only the fields a card
  reads, so channel IDs, provider profiles, plugin names and item addresses never reach it. Long texts
  are cut.
- **Pictures and videos:** the screen never loads a remote file. `preview(url)` asks the shell, which
  fetches only an address the orchestrator itself listed in a meme page, over http or https on the default
  ports, to public addresses only (checked on connect and on every redirect), with a time limit and a
  size cap (4 MiB for a picture, 20 MiB for a video), and only if the bytes are what the host said: a
  picture, or an MP4 or WebM video. It hands back a data URL, and the screen shows a picture as an image and
  a video as a silent loop with controls. The request starts with `Host`, as a browser's does: a host
  behind a bot filter refuses a client whose headers start another way.
- **Hardening:** sandboxed renderer with context isolation and no Node, IPC accepted only from the
  page the app loaded, every permission request denied, no navigation or new windows, no network
  requests (except the development server), and a strict Content-Security-Policy on the built page.
  Fonts are bundled, so nothing is fetched at run time.
- The orchestrator sees `source: desktop`. It accepts any source.

## Adding a card

1. Add its action to `ACTION_TYPES` in `src/shared/actions.ts` and a projection in
   `src/main/actions.ts`, with tests that show what it leaves out. Only add an action the orchestrator
   really routes: it sends an event it does not know to the chat domain.
2. Write the card in `src/renderer/cards/` with `useCard` and `CardFrame`, and its strings in both
   catalogs, the name of its workspace included. A workspace wraps its sections in one
   `WorkspacePanel` (`src/renderer/cards/workspace_panel.tsx`); a `CardFrame` is a section of it, not
   a card of its own, and registers its reload with the panel's one refresh.
3. Add one line to `CARDS` in `src/renderer/cards/registry.ts`: its kind, the id of its name in the
   sidebar, the component and, for its dot, the names of the services behind it as the status reports
   them. The order of the lines is the order of the sidebar; keep health last, at the foot. The screen
   starts on `START` (Talk) and falls back to `FALLBACK` (System) when the orchestrator does not
   answer; the status itself is read by `StatusProvider` in `src/renderer/status_context.tsx`, not by
   a card.

## Look

Dead-channel cyberpunk, chosen by the owner: the page is the grey-blue of a television tuned to
nothing, and neon only ever means something (cyan is up, magenta is a problem, amber has not been
checked). The name has a cyan and a magenta ghost behind it that drifts out of register as the
system gets worse. Type is B612 Mono for everything and Bodoni Moda italic for the name, both
bundled under the SIL Open Font License. Motion is one terminal-style print of the rows and a
blinking cursor, and both stop under `prefers-reduced-motion`. The tokens are at the top of
`src/renderer/styles.css`.

### Document classification

Open a document from the research base, then expand **Classification** to load
its current form/content labels, per-axis provenance and questions. The allowed
answers come from Knowledge; the desktop has no independent taxonomy. Select
only the answers you know and save them. Other axes stay pending. Nothing is
chosen by default, and classification does not upload, read or embed content.

`knowledge.classify` is allowlisted for both reads and explicit answers. Reads
need no input gesture; a payload containing answers requires the same recent
real click/key press as other desktop writes. The main process validates and
projects a bounded snapshot, retaining the revision needed for submission and
excluding document text and unrelated service fields. Updates use the existing
revision-checked domain API. Errors require reloading current questions before
another submission; earlier selections are cleared and never retried automatically.
The browser demo includes a local classification fixture.

This adds interface behavior only. No public event, service schema, environment
variable or compose setting changes. It requires an orchestrator with the
existing `knowledge.classify` action. Rolling back the desktop leaves stored
metadata intact. The offset-based document text view and broader per-document
detail contract remain separate follow-up work.

### Browsing experience memory

The **Notebook** lists saved records as it opens, newest first, in pages of 20.
Filter by All, Waiting or Settled (the service includes expired records among
settled). **Inspect** opens one record's action/detail, frozen
acceptance prediction and baseline, separate example-similarity score, observed
outcome and timestamp, context, and domain-owned feedback guidance.

Missing predictions remain “Not recorded”; a recorded zero remains 0%. Context
and excerpts render as plain text. Listings carry short excerpts only; opening
one record explicitly requests bounded full context. Older services that do not
return full context show the list excerpt with a clear label. Inspection never
records feedback automatically.

The main process allowlists `memory.list`/`memory.get` for reads and
`memory.resolve` for explicit feedback, bounds their payloads and projections,
and drops unrelated service fields. The domain interprets
feedback; the desktop translates its codes. The browser demo contains seven
synthetic pending experiences. Roll out the Memory service and orchestrator before
rebuilding/restarting desktop for full context and guidance. Rolling back leaves
stored experiences untouched; no routes, schema, configuration or compose changes.


### Giving experience feedback (primary interface)

Desktop is the primary interface for feedback. Open **Notebook → Inspect** for the record you recognize, then choose:

- **Accept result** when you actually kept the result.
- **Save actual result** after entering what you used instead. Memory compares
  it with the original detail; the desktop never invents the verdict.
- **Not used** when you explicitly know the result was not used.

Review the native confirmation before recording. The first verdict is retained;
a concurrent or earlier verdict is displayed without being overwritten. A saved
verdict shows a confirmation and **Refresh memory** reloads both the counts and
list. A missing record, disabled service, cancellation and failure are separate
from success. After a timeout, refresh before retrying because the write may
already have completed. Pending and expired-without-verdict records may receive
feedback; records with a saved verdict have no write controls.

This records an experience outcome; it does not create or edit a Knowledge
hashtag training example. Repeated suggestions currently share a media-derived
reference. If you cannot identify the experience, leave it unknown rather than
assigning an outcome to a guessed attempt. See
[experience feedback](../../dokja_docs/experience-feedback.md) for the secondary
TUI and coding-agent paths. Existing event contracts, SQLite schema, settings and
compose files are unchanged. Rebuild Memory for authoritative concurrent-write
receipts, then rebuild/restart desktop and TUI. No rollout runs automatically.


WhatsApp visibility regression: a missing `WHATSAPP_INTERFACE_STATUS_ENDPOINT`
previously removed the interface from the orchestrator's status reply. The
orchestrator now emits the known interface catalog with an explicit configuration
flag, independently of the routing switch. Rebuild/restart the orchestrator and
desktop for this fix. Existing configured interfaces retain their link/unlink and
QR controls. No credentials, environment defaults, ports, schema or routes change.
### Starting and stopping local containers

With the default local orchestrator endpoint, **System → Modules** uses
a host background controller even when the application orchestrator is stopped.
Opening desktop starts only that small controller and reads status; it does not
start containers. Choose **Start** for one module. The controller starts only its
required dependencies, waits for application readiness, and reports progress or
an actionable failure. **Stop** retains data and shared dependencies. Closing
desktop leaves accepted operations and containers running.

The initial inventory is orchestrator, Memory, Knowledge, Meme, Book, chat inference,
Feeds, Voice, Communications, WhatsApp, Discord and the scheduler in `docker-compose.lite.yml`. Memory/Knowledge also need local
embeddings and successful model initialization. Discord and the scheduler answer or post once running, so their
Start first shows, inside the row, what they will do with the switches and jobs already saved, and asks. WhatsApp
starts only when selected explicitly, and its auth/history volumes are retained. Running instances are recognized
as consumers so their required dependencies are preserved. One operation runs at
a time; a repeated identical pending action is idempotent. Failures permit an
explicit retry. A timeout triggers status refresh, never blind resubmission.

Each module appears once, with one primary Start/Stop control. Its collapsed
**Options** hold activity, request-handling pause/resume and WhatsApp linking
where applicable. Automation and other interfaces follow in the same card.
Starting a process does not override a deliberately paused capability.
`running` means the container exists without a controller readiness check;
`ready` means the application acknowledged its readiness probe. If the engine
cannot be inspected, the UI shows unknown state and disables writes until refresh.

Build/start from the desktop directory as usual. The build now also compiles the
Go host controller, so Go 1.25, native ZeroMQ development libraries and pkg-config
are required at build time. A running Docker/compatible Podman engine and
`docker compose` command are required at runtime. Existing containers are reused;
recreating them for image/configuration changes is a separate deployment action.
The first startup may build images or initialize models and can take minutes.

The shell finds the nearest checkout containing the lite Compose file. For a
packaged build launched elsewhere, supply the trusted `--runtime-root` flag with
an absolute checkout path. The renderer cannot choose that path or supply shell
commands. Local controls are hidden for Windows and remote/custom orchestrator
endpoints in this release. The browser demo uses synthetic stopped modules.

The controller uses a private Unix socket, an OS instance lock and private
ownership state under `$XDG_STATE_HOME/dokja/` (default `~/.local/state/dokja/`),
scoped to the canonical checkout and shared with the CLI and TUI. It runs as the current user with no root helper, TCP control port or
Docker socket exposed to the renderer. It starts lazily and continues after the
window closes; no login/boot autostart is installed. See the
[controller documentation](../../dokja_orch/runtime/README.md) and
[production runbook](../../dokja_docs/production-runbook.md) for operation limits.

Content providers appear under Knowledge → Sources as processing plugins. Runtime
controls manage Feeds as their host; Book/Meme are not separate desktop modules.
See the [plugin migration contract](../../dokja_docs/feeds-content-plugins.md).

Operational settings are stored in the shared local database through the runtime
controller, so CLI, TUI and desktop see the same values. Allowed legacy `.env`
options are imported once; tokens and webhook addresses remain outside this API.
List settings use separate add/remove entries; their changes are reviewed before
saving.
The embedding profile is visible and fixed. Manual Compose still uses legacy
environment configuration; see the [runbook](../../dokja_docs/production-runbook.md).

System → Providers reads the same inventory as CLI and TUI when the view opens,
never before. A CLI found on PATH is not proof of authentication
or successful inference. See [provider inventory](../../dokja_docs/provider-inventory.md).

In that view, Edit opens the selected registration. Review and save
discovery eligibility and up to 16 unverified model declarations, each with its
own add/remove control. The form keeps its original revision; concurrent edits
require reload and explicit retry. Older controllers without a configuration
revision remain read-only. Changes do not
select a capability model or alter the fixed embedding profile.

The Ollama row is the local model engine. It lists each model with its size, and marks the embedding
model memory and knowledge use, which is never offered for chat. Below the list:
- **Start Ollama** appears when the engine is stopped, so a model can be chatted with while memory and
  knowledge are off.
- **Download a model** takes a name from the Ollama library. The shell asks first, in a window of its
  own that names the model and warns about the size. Then a line shows the phase and percentage, with
  **Cancel download**. When it ends, the model joins the list with **Use for chat**.
- **Remove** sits on every model but the embedding one and the one in use for chat. The shell asks
  first.

Every one of these changes needs a real press. A controller without the model actions shows the list
only.

Chat requires an explicit enabled provider and declared model. Select it in Desktop
System → Inference providers → Chat provider and model, or run
`dokja-cli providers chat <provider> <model>`. Missing/disabled configuration
shows a setup error. Archived profile editing no longer selects live inference.
