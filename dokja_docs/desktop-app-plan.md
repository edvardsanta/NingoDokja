# Desktop App Plan

Status: proposed, 2026-10-02. The Phase 0 spike has run (results below) and the Phase 1 skeleton is in
`dokja_interfaces/desktop/` with one card, service health. The "Confirmed decisions" section records
what was agreed with the owner; everything else is a recommendation to review.

## Goal

A desktop interface for Ningo: a window that greets the owner, shows what Ningo has read and learned,
and later talks. It is one more interface next to Discord, the CLI and the TUI, so it adds no business
logic: it renders what the orchestrator returns and relays the owner's input.

The starting reference is a morning briefing: a greeting opens it, cards of different kinds arrive one
at a time, and the assistant offers the rest instead of dumping everything.

## Identity: a pattern, not a look

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
| Window | A normal window. Compositor window rules can make it float or stay pinned. Fullscreen ambient and overlay modes are out of v1. |
| Voice | Visual only in v1. Voice comes after the digest works (Phase 4). |
| Cards | Knowledge and books, memes, memory and health, plus the personal cards (weather, agenda, mail). Feeds are the first new card. |
| Look | Cyberpunk. The direction under Identity is a first pass to react to, not a final design. |

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
  size and time limit, and accepts only bytes that are a picture (SVG is left out).
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
