# Desktop App Plan

Status: proposed, 2026-10-02. The Phase 0 spike has run (results below) and the Phase 1 skeleton is in
`dokja_interfaces/desktop/` with one card, service health. The "Confirmed decisions" section records
what was agreed with the owner; everything else is a recommendation to review.

## Goal

A desktop workspace for Ningo: the screen where the owner works with Ningo and runs it. It shows what
Ningo has read and learned, holds the research base, the conversations and the switches, and later
talks. It is one more interface next to Discord, the CLI and the TUI, so it adds no business logic: it
renders what the orchestrator returns and relays the owner's input.

Superseded on 2026-10-19: the first goal was a window that greets the owner with a morning briefing
(cards arriving one at a time, the rest offered on request). That reads as a presenter, not a tool the
owner works in; see "Workspace replan (2026-10-19)" at the end.

## Identity: a pattern, not a look

The briefing patterns in this section are superseded by "Workspace replan (2026-10-19)". The rule that
a reference gives patterns and never identity still holds, and so does the visual direction below
(the owner kept it on 2026-10-19).

The reference is only a source of interaction patterns. Ningo is the arrogant philosopher described in
the [README](../README.md): talkative, well read, opinionated. It is not a hero.

Taken from the reference:

- a greeting opens a short briefing instead of a menu;
- cards of mixed kinds share one anatomy and arrive one at a time; the focused card then docks into a row;
- an offer ("I have more observations, want them?") before the rest is shown.

Not taken: the glowing orb, the blue holographic HUD, the system gauges and the hero tone.

Visual direction: cyberpunk, the owner's choice on 2026-10-02. The first pass is specific to Ningo
rather than neon on black, and is open to change:

- **Dead channel.** The page is the grey-blue of a television tuned to nothing (the opening line of
  *Neuromancer*), with fine scanlines and noise. Neon never decorates; it only means something: cyan is
  up, magenta is a problem, amber has not been checked.
- **The name is the one memorable thing.** It is set in a Didone italic with a cyan and a magenta ghost
  behind it, aligned when the system is well and drifting out of register as it gets worse.
- **Structure carries information.** Panels with two cut corners, the kind of card cut into its top
  edge, one segment per service.
- **Type.** B612 Mono, made for cockpit displays, for everything; Bodoni Moda italic for the name.
  Both are bundled under the SIL Open Font License, so nothing is fetched at run time.
- **Motion.** One terminal-style print of the rows and a blinking cursor while loading. Both stop under
  `prefers-reduced-motion`.

Principles that stay:

- One card anatomy for every kind: kind label, title, body, source and, when a text generator is
  available, Ningo's one-line take.
- Offer, don't dump: a short digest first, the rest on request.

## Confirmed decisions

| Topic | Decision |
|---|---|
| Naming | Neutral in code, docs and UI: feeds, digest, reading. No source, provider or URL is committed; sources are runtime inputs supplied by the owner. |
| Window | A normal window. Compositor window rules can make it float or stay pinned. Fullscreen ambient and overlay modes are out of v1. The workspace replan keeps it: a document or a conversation opens in a tab inside the work area, not in a second window (the owner, 2026-10-19). |
| Voice | Visual only in v1. Voice comes after the digest works (Phase 4). |
| Cards | Knowledge and books, memes, memory and health, plus the personal cards (weather, agenda, mail). Feeds are the first new card. |
| Look | Cyberpunk. The direction under Identity is a first pass to react to, not a final design. Kept by the owner on 2026-10-19: the workspace replan changes the layout, not the look. |

## Where it fits

The flow does not change: `Desktop -> Orchestrator -> Domain -> Service`. The app never fetches a feed,
calls a provider or holds a token.

| Card | Orchestrator today | Domain | Gap |
|---|---|---|---|
| Health | `ningo.status` | system | none |
| Memory | `memory.*` | `dokja_memory` | none |
| Knowledge, books | `knowledge.*`, `book.*` | `dokja_knowledge`, `dokja_book` | none |
| Memes | `meme.*` | `dokja_meme` | none |
| Feeds | `digest.*` | `dokja_digest` | none in v1 (service, domain and route are built; no source ships) |
| Weather, agenda, mail | none | none | services, plugins |

Proposed home: `dokja_interfaces/desktop/`, TypeScript, file names in `snake_case` like the Discord
interface (components included).

## Decisions to review

### UI stack

TypeScript, React and Vite. Interfaces here are TypeScript (Discord) or Go (CLI, TUI); a rich screen
is TypeScript. React for the size of its ecosystem; Svelte would also work.

The UI depends on a `Transport` port (`request(type, payload)`), never on the shell. It therefore also
runs in a plain browser for development and tests, and any shell can host it later. Unknown card kinds
render as a generic card, so a new source never needs a shell update.

### Shell: Electron for v1 (chosen by the Phase 0 spike)

- One language and one toolchain end to end (TypeScript, Node is already pinned in `.tool-versions`).
- Chromium on every platform: predictable rendering and audio, no dependence on the system WebKitGTK
  on Linux. In the spike every audio path worked untouched and the microphone was granted (Electron
  approves every permission by default, which a real app restricts; see Security); WebKitGTK denied the
  microphone and refused an `<audio>` element pointing at the shell's own protocol (see the results).
- A mature test path: Playwright drives Electron.
- Cost: weight. The spike measured about 50 MB more PSS than Tauri and a 336 MiB runtime on disk;
  acceptable on a development machine, revisit if it must stay open all day on a small one.

Alternatives:

- **Tauri 2:** stable and lighter. It worked in the spike, but needed about 20 lines of Rust for the
  microphone and has the WebKitGTK quirks listed in the results. Its mobile targets exist but are less
  mature than desktop, and it would be the first tracked Rust in the repo.
- **Wails v3:** Go glue could reuse the CLI requester, but it is in beta and desktop only. It was not
  tried; on Linux it also uses the system WebKitGTK, so expect similar webview behaviour and verify it
  if it becomes a candidate.

Switch rule (applied in Phase 0): choose Tauri only if audio and the microphone work without custom
native code and the idle RAM difference matters to the owner. As generated, Tauri failed the first
condition (the fix is small) and the RAM difference is small, so Electron stays. The choice is cheap to
reverse: the spike page ran unchanged in both shells behind an adapter of under ten lines each.

### Transport

- **v1:** the shell's main process sends ZeroMQ REQ to the orchestrator request endpoint, exactly like
  the TUI (`DOKJA_ORCH_REQUEST_ENDPOINT`), and the renderer reaches it over IPC. No orchestrator change.
  Verified in Phase 0: a `ningo.status` round-trip works from the Electron main process and from a
  Tauri command.
- **Later (PWA, mobile):** a new structured ingress in the orchestrator. The existing HTTP bridge is
  shaped for Discord (fixed source, text-only reply) and should not be stretched. The request port has
  no authentication, so remote clients need authentication first; that is a gate, not part of v1.

### i18n

English by default with a Portuguese catalog, the same policy as the TUI: stable message IDs, tests that
fail on missing or stale IDs and on Portuguese leaking into English mode.

### Security

- Renderer sandboxed: context isolation on, no Node integration, strict CSP, a preload that exposes
  only `request` and `preview`.
- Allow-list: the main process forwards only the listed read-only actions (`ningo.status`, `memory.*`,
  `knowledge.status` and `knowledge.search`, `meme.status` and `meme.list`, `digest.status` and
  `digest.items`), so a compromised screen cannot call `services.set`, `discord.send` or `digest.refresh`.
  Each action builds its own payload, so the screen cannot add fields or change the digest's rules, and
  each reply is projected to the fields its card reads, so channel IDs, provider profiles, plugin names
  and item addresses never reach the screen.
- Permissions denied by default: Electron approves every permission request unless a handler is set, so
  the session gets one that denies everything (Phase 4 allows only the microphone, for the app's own page).
- Feed content is untrusted: rendered as text, never as HTML. The shell's main process fetches images
  (as the TUI does) and hands them to the renderer, which never loads a remote image itself. It fetches
  only addresses the orchestrator itself listed in a meme page, over http or https on the default ports,
  to public addresses only (checked on connect and on every redirect, literal IP hosts included), with a
  time limit and a size cap (4 MiB for a picture, 20 MiB for a video), and accepts only bytes that match
  what the host said: a picture (SVG is left out) or an MP4 or WebM video. The page's policy allows
  `data:` media for the result and nothing remote.
- Nothing in the app opens a link yet, so a digest item reaches the screen without its address. Opening
  links is a separate step: an IPC channel that opens only an `http` or `https` address the orchestrator
  returned, the way the image fetch is gated.
- No token in the app. Provider tokens live in the services' environment or volumes.

## Backend gaps

Each one is its own PR, in layer order.

1. **Feeds service** (`dokja_services/dokja_feeds`, Go, built). It follows the sources the owner chooses
   and keeps their latest items in memory. The repository ships the mechanism only: a source is a
   plugin, a directory with a `plugin.json` manifest and a program, kept outside the repository (see the
   service's README for the contract). Two choices differ from the first draft of this plan:
   - **A new service, not an extension of `dokja_knowledge`.** Feed items are many and short-lived;
     put in the research base they would crowd its search and cost embeddings. The knowledge service
     keeps reading a feed once, on request.
   - **An in-service ticker, not a scheduler job.** The orchestrator answers one request at a time, so a
     request must never run a plugin. Each enabled plugin runs on its own interval inside the service and
     a request reads the last good result from memory.
   The plugin contract borrows from the extension layout of another open-source assistant (OpenClaw's
   `extensions/`), as a pattern only and with no code taken: a manifest read without running the plugin,
   nothing runs until its manifest enables it, a failed plugin stays listed with its reason and never
   hides the others, and secrets are passed by variable name. A plugin is an external program (a shell
   script, a static binary) that prints JSON, so it can be written in any language the image has; each run
   gets its own process group, a minimal environment, a time limit and an output limit.
2. **Digest domain** (`dokja_domain/dokja_digest`, Go, pure like `dokja_memory`, built). Drops items older
   than a window, orders newest first, drops duplicates (same address or same long title) and keeps one
   source from filling the first places. The UI never ranks.
3. **Orchestrator route** `digest.*` (built): `digest.status` and `digest.items`, with `feeds` a service
   that can be switched off and a `ningo.status` probe. `digest.refresh` does not exist; the service
   refreshes itself.
4. **Personal cards.** Weather, agenda and mail as stateless services; provider code and endpoints are
   runtime inputs or plugins outside the repository, and tokens stay in the service environment. Agenda
   can start from a read-only calendar feed supplied at runtime, which needs no OAuth. Mail is the most
   sensitive: start with counts, senders and subjects, and decide separately whether any mail text may
   reach a text generator.
5. **Voice route** (Phase 4). The orchestrator has no client for `dokja_voice` today, so add a `voice.*`
   route and client; the app asks the orchestrator for audio and never calls the service.

Gate for personal cards: the runbook already notes that the orchestrator request port has no
authentication and is published on every interface. Before the first personal card ships, bind the
orchestrator ports to loopback or add authentication, and return the minimum by default, the way
memory snippets need `include_context`.

## Phases

v1 covers Phases 0 to 3, personal cards included, behind the gate above. Phases 4 and 5 follow.

0. **Spike (done 2026-10-02, decided the shell).** One minimal page run in the system Electron and in
   Tauri: cards arriving one at a time, a short tone played on load, a microphone request, and one
   `ningo.status` request sent from the shell's native side. Results below.
1. **Skeleton and existing cards (built).** Shell, `Transport` over ZeroMQ, card registry, one tab per
   card, and the health, memory, knowledge and memes cards, with the memes' pictures fetched by the
   shell. No backend change. The book summaries are not in the knowledge card: they call the chat
   provider and their future is open, so the card shows the research base only. The orchestrator answers
   requests one at a time and a stopped service can take about five seconds to fail (the TUI keeps
   memory out of its panel refresh for that reason), so the transport queues requests, each card loads
   on its own with a timeout, and a card is mounted, and asks its service, only when its tab is first
   opened. Tests: the transport adapter and the shell's guards with the Node test runner (as the
   Discord interface does), components with Vitest, and a smoke test of the built shell.
2. **Feeds and digest (built, without sources).** Backend gaps 1 to 3, then the digest tab: how many
   sources work, fail or are off, the first eight entries newest first, "show more" up to 50, and the
   list of sources with the reason each one fails. With nothing followed the tab says why. The progressive
   reveal that speaks the digest aloud belongs to Phase 4. Open links and Ningo's own take (it needs a
   text generator) are not in this step.
3. **Personal cards.** Weather, then agenda, then mail, one PR each, ordered by sensitivity, after the
   gate above.
4. **Voice.** `voice.*` route, speech playback, and the "want to hear it?" offer. Microphone and wake
   word later, following [voice-planning.md](./voice-planning.md).
5. **Remote clients.** Structured HTTP ingress with authentication, then a PWA from the same UI and,
   later, mobile.

## Phase 0 results

Run on 2026-10-02 on Arch Linux under Hyprland (Wayland), with an AMD GPU on Mesa and one 1080p 75 Hz
monitor. The same page ran unchanged in both shells: the system Electron 43.7.0 (Chromium 150) and a
Tauri 2.12.1 release build (wry 0.57, WebKitGTK 2.52.6). `ningo.status` went to an orchestrator built
from `main` and bound to loopback; its services were stopped, so the request took about 5 s in every
run. Memory is summed over each app's whole process tree and sampled after 15 s idle. PSS shares common
pages between processes; RSS counts them once per process and overstates multi-process apps.

| | Electron | Tauri as generated | Tauri with a permission handler |
|---|---|---|---|
| Window after launch | 0.5 s | 0.75 s | 0.75 s |
| Native Wayland, no flags | yes | yes | yes |
| Processes | 8 | 3 | 3 |
| Idle memory, PSS (RSS) | 300 to 309 MB (715 to 727) | 250 to 266 MB (464 to 480) | 256 MB (470) |
| Frames at 75 Hz, calm and a blur/shadow stress phase | 74 to 75 fps | 75 fps | 75 fps |
| `<audio>` playing a bundled WAV, no click | works | fails (`NotSupportedError`) | fails |
| The same WAV through a `blob:` URL | works | works | works |
| Web Audio, no click | starts running | starts suspended; `resume()` works without a click | same |
| Sound reaches the audio server | yes | yes | yes |
| Microphone | granted, as Electron approves every request by default | denied (`NotAllowedError`) | granted (the handler allows every request) |
| ZeroMQ REQ round-trip | works | works | works |
| Size on disk | runtime 336 MiB | binary 7.6 MB, plus the system WebKitGTK (134 MiB, shared) | same |

What it means:

- **Switch rule.** As generated, Tauri fails the first condition: the microphone does not work without
  custom native code (about 20 lines of Rust and a direct dependency on the `webkit2gtk` crate, which
  has to track the version wry uses; the spike's handler allows every request, a real one filters by
  permission type). Electron grants it by default, but a real app should deny everything except the
  microphone for its own page, a few lines of main-process code (see Security), so the gap is smaller
  than it looks. The second condition fails too: the memory saving is about 50 MB of PSS, which does
  not matter here. What separates the two is WebKitGTK's audio quirks (`<audio>` from the shell's
  protocol, a suspended `AudioContext`), the version coupling to wry, and Tauri being the first tracked
  Rust in the repo. Electron stays.
- **Voice playback (Phase 4)** goes through Web Audio, not an `<audio>` element pointing at a bundled
  asset, and calls `resume()` before the first sound. Both work in both shells. The `<audio>` failure is
  the shell's protocol, not the codec: the same bytes played from a `blob:` URL.
- **ZeroMQ client.** Electron: `npm install zeromq` took about 2 s. The package ships N-API addons with
  libzmq inside and loaded in Electron 43 (a different Node ABI from the system Node) without a rebuild;
  about 35 lines in the main process. Tauri: the `zmq` crate builds libzmq from source and links it
  statically (a C++ toolchain and cmake at build time); a first release build took 1 min 30 s
  (272 crates), later builds 16 s; about 50 lines of Rust.
- **Shell-agnostic UI.** The page only saw a `status()` and a `report()` function, supplied by a preload
  in Electron and by a Tauri command in Tauri. That supports the `Transport` port in this plan.
- **Slow cards.** `ningo.status` held the request for about 5 s because services were stopped, which
  is the reason for the per-card timeout in Phase 1.

Limits: one machine, one compositor, one GPU vendor; a static page at idle, so the memory figures are
a floor; GPU memory is not counted; the two runs of each shell agree within about 6% on memory. Window
sizes differed between runs (the compositor tiled them at 926 by 892 or 1920 by 1080), and PSS for
WebKitGTK depends on what else maps the same libraries. The sound was seen reaching the audio server
but nobody listened to it, and the microphone stream was live with a near-silent level, which proves
the stream and not its quality. The audio-server probe in the second run adds a little noise to the
frame numbers.

## Compatibility

- Event envelope: unchanged.
- Orchestrator routes: additive (`digest.*`, later `voice.*`).
- Service HTTP contracts: new services only; none changed.
- CLI and Discord commands: unchanged.
- SQLite schema: new tables in new services only.
- Environment variables and compose: new ones (plugin directories, new services). Binding the
  orchestrator ports to loopback changes how remote CLI and TUI clients connect, so treat it as a
  compatibility decision. Compose files and the runbook change in the PR that introduces each.

Everything except the loopback binding is additive.

## Risks and open questions

- Ningo's take needs a text generator. The digest must work without one: items and sources first,
  the take added when a generator is available.
- Is a text conversation part of v1? It depends on the same text-generation question.
- Where feed following lives: decided, a new service (see Backend gaps).
- Overlay and ambient window modes are not verified in any candidate; they are out of v1.
- Event source for the desktop app: it sends `source: desktop`, which is additive. The orchestrator
  does not reject unknown sources: the only source-specific code is the chat handler reading the text
  from the context for known sources, and `discord.send`, which requires `cli`. If a text conversation
  arrives, add `desktop` to the chat handler's list.
- Mobile is assumed to be a PWA from the same UI, after authentication exists.


## Communications increment (2026-10-03)

The desktop now has a sixth tab for configured Discord conversations. It reads channel names and
paginated history through the new communications domain and read service, and sends through the
existing orchestrator delivery workflow. The shell confirms channel and content before sending,
and may attach only a meme listed by the orchestrator. Polling runs only on the visible latest
page and pauses on errors. These actions require a separate shared credential, stripped before
orchestrator dispatch and never exposed to the renderer; the shell uses only loopback or IPC.
See the desktop README and production runbook for configuration. No personal-account login,
local history database, attachment downloads or external link opening were introduced.

This supersedes the earlier read-only security description for this tab. Provider credentials
remain in the backend; only the restricted communications credential resides in the desktop's
main process. Existing ingress bindings are unchanged.

## Switches increment (2026-10-03)

The desktop is meant to be the one place from which the operator runs Ningo, so the allow-list no
longer keeps it from switching things. The health card now carries a switch for every service the
orchestrator can switch and, below the services, the scheduled jobs with a switch and an interval each.
This supersedes the allow-list sentence under Security above: `services.set` and `scheduler.jobs.set`
are on the list as changes. They need a real click or key press just before, like every change, and
open no confirmation window, because the operator can switch them back at once. `scheduler.jobs.announce`
(the scheduler's own heartbeat), `discord.send`, `digest.refresh` and `chat.profile.use` stay off the list.

A job change sends only what the screen supplied: the orchestrator reads an interval that is present as a
change and an empty one as "back to the default", so a switch must never carry an interval and an interval
must never be empty. The screen reads `ningo.status` again after every change instead of trusting its own
guess. Not covered yet: switching the communications service (it is not among the orchestrator's known
services), feed sources (they live on the feeds-service branch) and chat profiles (their reply carries a
provider address the shell deliberately strips).

## Interfaces increment (2026-10-11)

Status: all four increments are built: 1 (branch `feat/interfaces-health`), 2
(`feat/desktop-whatsapp-link`), 3 (`feat/whatsapp-conversations`) and 4 (`feat/whatsapp-send`). Each
increment is its own branch, stacked in this order. Nothing has been linked to a real account.

The Switches increment made the desktop the one place from which the operator runs Ningo. The people
talk to Ningo through the Discord and WhatsApp interfaces, so the desktop should show their
state, switch them, link WhatsApp, and, for the conversations the operator names, show and send
messages. The flow does not change: the desktop talks to the orchestrator, the orchestrator talks to
the interfaces.

### Rules that hold for every increment

- **One status vocabulary.** Every interface runs a small internal listener that is always on, whether
  or not it delivers anything, and answers `GET /status` with `{status, state, detail}`. `state` comes
  from a closed list per interface and never carries a phone number, a chat id, a token or a link code:
  - discord: `connected`, `connecting`, `disconnected`
  - whatsapp: `connected`, `connecting`, `waiting_for_link`, `logged_out`, `replaced`, `refused`, `off`

  An interface that does not answer is shown as `unreachable` by the orchestrator, with a fixed
  sentence. The listener port is not published in Compose.
- **What a switch means.** An interface that is off neither answers nor receives deliveries. It answers
  nothing at all in the chat: the "switched off" reply that services give is wrong here, because it
  would post in every channel the bot sits in. Credentials and the linked session are untouched, and the
  interface keeps running so the switch can be flipped back from the desktop. A scheduled or manual
  delivery to a channel of an interface that is off is left out and reported as skipped, not failed.
- **Authentication.** `ningo.status` is unauthenticated and the request port is published, so it carries
  only the vocabulary above. Anything that carries a link code, history or a send travels under the
  `communications.` event type prefix, which the existing credential gate already covers, and the
  credential is stripped before dispatch like today.
- **One alias map.** Increments 3 and 4 share `alias=chat` entries in the WhatsApp interface's own
  configuration. Only aliased chats are recorded, listed and sendable; the chat id never leaves the
  interface on those paths, only the alias does.

### Increment 1: interfaces in the health tab

- Each interface gets its always-on status listener. Discord reports the gateway state, not that the
  process is up, and answers on the port of its delivery server. WhatsApp gets a new listener.
- The WhatsApp runtime stops exiting when the session is logged out or replaced, or when a link code
  expires: it stays alive in that state and says so. This is what lets a later increment link from the
  desktop, since the desktop cannot restart a container.
- The orchestrator probes each configured interface, adds `discord` and `whatsapp` to the
  switchable names, and enforces the meaning above in the service gate and at delivery. An interface
  without a configured endpoint is not listed.
- The health tab already appends any service the orchestrator reports; it now groups the entries marked
  as interfaces under their own heading, and both locales get the new labels.

### Increment 2: link WhatsApp from the desktop

Built. The interface offers the rows of the code's picture (never the code string) on `GET /link`,
only while it waits for one, plus `POST /link/start` and `POST /link/reset`; the orchestrator relays
them as `communications.link.status|start|reset` behind the communications credential; the shell draws
the picture from validated rows and the renderer shows it in a panel on the WhatsApp row.

- The interface exposes the current link code only while it waits for one, on its internal listener.
  The orchestrator relays it under `communications.`; the shell's main process draws it as an image and
  the renderer receives only that image, never the code. The code is a short-lived way to attach a
  device to an account, so it never reaches a log, a status reply or the page's own state.
- Starting a link and wiping the session are separate actions. Wiping a session is destructive: it
  needs a real click or key press and a native confirmation that names the interface.

### Increment 3: conversations in the communications tab

Built. `DOKJA_WHATSAPP_CONVERSATIONS` names the chats (`alias=<number or group id>`); only those are
recorded, as text, in a folder of the interface's own with a retention (30 days by default); the
interface answers `GET /conversations` and `GET /history`; the communications domain accepts
`whatsapp:<alias>` next to a Discord channel id; the tab lists and reads them, without a composer.

- **This supersedes the line above that says no local history database is introduced, for aliased
  WhatsApp chats only.** WhatsApp has no history to read from a server, so what the interface saw is
  all there is. Discord keeps reading its own history from its API and stores nothing here.
- What is stored: text only, after the interface's policy has accepted the message, for aliased chats,
  including the bot's replies and the desktop's own sends. No media, no phone numbers, no sender other
  than `owner`, `bot` or `member`. It lives in the interface's own volume with a default retention and a
  purge, and the tab shows the retention.
- The communications domain accepts a platform-prefixed channel id (`discord:<id>`, `whatsapp:<alias>`).
  A bare numeric id still means Discord, so existing clients and requests do not change.

### Increment 4: send from the desktop

Built. `POST /deliver` on the interface (alias and text, one named conversation, 6 a minute and 40 an
hour), `communications.send` accepting one `whatsapp:<alias>` target in the orchestrator (only that
event type, text only, never "all", the alias must be one the interface names), a text-only composer,
and a confirmation window that names the alias and says the message leaves from the owner's account.

- `communications.send` accepts `whatsapp:<alias>`; the interface delivers only to its aliases, at a
  low fixed rate, text only and up to the same length as Discord. The shell confirms the alias and the
  text natively before anything is sent, the same way it confirms a Discord send.
- It sends as the owner's own account. A burst of automated messages is what gets a number
  restricted, so the rate limit is part of the feature, not an option.

### Compatibility

Additive: new optional environment variables, new internal listeners, additional values in an existing
status reply and in the switchable names, and an optional platform prefix on the channel id of
communications requests. The event envelope, the existing ports, the Discord text commands and the HTTP
contracts of the services do not change. Existing `VA_STATE_FILE` files stay valid: they list only the
services that are off.

## Workspace replan (2026-10-19)

Status: Phase A is built, on stacked branches: `feat/desktop-workspace` (the sidebar, System at its
foot with the problem count, the dots, the new keys, and the memes picked in Messages instead of a
workspace of their own), `feat/desktop-messages` (Messages as a messenger), `feat/desktop-knowledge-drop`
(a file dropped anywhere on Knowledge) and `feat/desktop-names` (panels named after their workspaces,
and an empty research base that says where a file goes). Phase B waits for the chat plan; Phase C is
reshaped in three steps below.

The owner's feedback on the built app: it came out as a presenter (a greeting, a briefing, a big name,
a strip of tabs) when the idea is a human-computer interface, a screen the owner works in. The owner
pointed to a self-hosted AI workspace as the new reference. As before, a reference gives interaction
patterns only: Ningo stays the arrogant philosopher, and neither its look, its feature set nor its
code is taken.

### Patterns taken

- **A sidebar of workspaces instead of a tab strip.** It is always visible, lists every workspace by
  name, and carries a short state on each entry (a dot and a word such as `off` or `idle`).
- **One work area.** The selected workspace fills the rest of the window at full size, with no card
  frame around it. The current cards become these views.
- **A composer docked at the bottom.** A single input where the owner asks Ningo something. It is the
  main way in once a text conversation exists (Phase B below).
- **Context menus on rows.** Right-click or the menu key on a document, a meme or a conversation gives
  the actions that row already has (open, delete, send), instead of buttons on every row.
- **Empty states that say what to do next**, not a greeting: "no source is followed; add a plugin to
  the feeds service", "the research base is empty; drop a file here".
- **Later, tabs in the work area.** A document or a conversation opens in a tab of its own on top of
  the work area, like a browser tab, so several stay open and the owner moves between them.

Not taken: email, calendar, model management and side-by-side model comparison, and the reference's
visual style. The look stays the dead-channel cyberpunk under Identity, applied to the new layout: the
sidebar and the work area are the same chamfered panels, and neon still only means a state.

### Layout

```
+--------------+----------------------------------------------+
| Ningo        |                                              |
|              |   selected workspace, full size              |
| Digest     • |                                              |
| Knowledge  • |                                              |
| Memory     • |                                              |
| Messages   • |                                              |
|              |                                              |
|              | +------------------------------------------+ |
| System  2 ⚠  | | Ask Ningo...                          ↵  | |
+--------------+-+------------------------------------------+-+
```

| Tab today | Workspace | Change |
|---|---|---|
| health | System, plus the sidebar's foot | The switches, jobs and WhatsApp linking move to System unchanged. The sidebar's foot shows how many services are down; a click opens System. The name's ghost keeps following the health pulse. |
| digest | Digest | Unchanged view; the empty state says why nothing is followed. |
| knowledge | Knowledge | The add form becomes a drop zone over the whole view plus a button; document rows get the context menu. |
| memory | Memory | Unchanged view. |
| communications | Messages | Conversation list on the left of the view, history and composer of that conversation on the right, like any messenger. |
| memes | none, picked in Messages | Memes are something Ningo sends, not a place to work in (the owner, 2026-10-19). The queue becomes a picker of previews in the Messages composer; the meme service's state and switch stay on System. Built with Phase A. |

The dots on the sidebar come only from the one `ningo.status` the System view already reads, never
from each workspace's own status request: the orchestrator answers one request at a time, and a
workspace nobody opened must still not ask its service.

### What does not change

- The flow, the `Transport` port, the main-process allow-list, the per-action payloads and the
  projection of each reply.
- A change needs a real click or key press; destructive ones keep their native confirmation.
- A workspace is mounted the first time it opens and then kept, as the tabs are today.
- One normal window. Several open views are tabs inside it (Phase C), never a second window.

### Phases

A. **Shell restructure (desktop only, no backend change).** The sidebar replaces `Tabs`, health
   splits into System and the sidebar's foot, keys and i18n labels follow, and the README's tab table is rewritten. One PR. Tests: sidebar keyboard and
   ARIA with Vitest, the existing end-to-end smoke updated.
B. **Composer.**
   - B0 (dropped by the owner on 2026-10-19): a command line over existing actions before chat
     exists. The composer waits for the chat plan instead.
   - B1: a text conversation with Ningo. It is gated on the chat plan (`dokja_docs/chat-plan.md` on the
     unmerged branch `docs/chat-plan`), whose decisions are still open. It needs `desktop` in the chat handler's source
     list (`dokja_orch/internal/handlers/chat_domain_handler.go`, additive), a request action for one
     chat turn that the shell can allow-list, and the session and history ownership that chat-plan
     decides. The cross-layer path (domain, route, workflow step, handler) is designed there, not here.
C. **Document view, in tabs of the work area.** Reshaped on 2026-10-19 with the criteria of the
   knowledge work. Conversations get no tabs: Messages already shows them, and a discussion about a
   document uses the existing conversation model, not a thread of the desktop's own. A context menu
   with only Delete adds little, so none is built until it has a real action (C3).

   The cards keep their chamfered panel instead of losing their frame: it is the cyberpunk look the
   owner kept, and the panel's legend names the workspace.

   - **C1, desktop only, buildable on main.** "Open" on a document of the Knowledge list adds a tab to
     the work area, beside the list, which stays the first tab; a tab closes with its button (Ctrl+W
     already closes the window through Electron's default menu, so no key is taken). The tab shows the
     title, kind, source reference, tags, passages and the retained text. `knowledge.list` already
     accepts `include_text`, and the desktop's contract changes in the shell only: the payload passes
     `include_text: true` only with `limit: 1`, at the row's own offset; the shell checks the document
     that comes back is the one asked for (the list may have changed: then the tab says so and asks for
     a refresh), caps the text and says when it cut it, and the screen renders it as text, never as
     markup. The tab says plainly that the text is rebuilt from the stored passages and is not a
     faithful copy of the original file.
   - **C2, after `feat/knowledge-metadata` merges.** Listed documents then carry `metadata` and a
     `classification` with `status`, `missing_axes` and `questions`. The tab shows the defining
     metadata (form, content kind, topics, entities, date anchor) with where each value came from, and
     the pending questions with their allowed values, read-only. The shell's projection adds both,
     bounded like every other field.
   - **C3, needs a contract from the knowledge track.** Answering a question from the desktop needs a
     request that sets explicit metadata on a stored document by its id, without its content: today
     metadata travels only with an ingest or a reading, and the desktop does not hold the original
     file. Once that exists, the tab answers the questions, and a context menu on a row that needs
     input ("Classify…", beside Delete) earns its place. Designing that request belongs to the
     knowledge work, not to this plan.

Phases 3 to 5 above (personal cards, voice, later work) become new workspaces or composer abilities on
this shell. They are not cards in a briefing.

### Compatibility

- Phase A: no route, envelope, environment, compose or schema change. The documented keys change: the
  bare digits `1` to `6` and the left and right arrows over the tab strip are replaced by `Ctrl+1` to
  `Ctrl+5` and up and down arrows in the sidebar. A composer that always takes text cannot also take
  bare digits.
- Phase B1: `source: desktop` on chat requests (additive) and one new allow-listed action.
- Phase C1: no backend change. The shell's `knowledge.list` payload may now carry `include_text`
  (only with `limit: 1`) and its projection a capped `text`.
- Phase C2: no backend change on top of `feat/knowledge-metadata`; the projection adds `metadata` and
  `classification`.
- Phase C3: a new knowledge request, owned by the knowledge track.

### Owner decisions

Answered on 2026-10-19: the look stays cyberpunk; memes have no workspace and are picked in Messages;
the composer waits for the chat plan (no B0); open views are tabs in the work
area, not separate windows, and only documents get them (Phase C, with the knowledge work's criteria); the morning briefing is dropped, with no "Today" workspace. The owner
will plan a replacement later, separately from this replan.

No question is open.
