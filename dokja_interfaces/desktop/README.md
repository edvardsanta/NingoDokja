# Dokja Desktop

Desktop interface for Ningo. It is an interface adapter like the CLI, the TUI and Discord: it only
talks to the orchestrator over ZeroMQ request/reply and holds no business logic. The plan, the
decisions and the Phase 0 results are in [`dokja_docs/desktop-app-plan.md`](../../dokja_docs/desktop-app-plan.md).

Status: v1 without voice. Five tabs, each a card that asks its service only when the tab is first
opened, and then keeps what it showed:

| Tab | Asks | Shows |
|---|---|---|
| health | `ningo.status` | how every service is doing; the name's ghost follows it |
| digest | `digest.status`, `digest.items` | what the feeds service follows and the first entries it gave, newest first |
| memory | `memory.status`, `memory.stats` | the experience memory and how well its predictions score |
| knowledge | `knowledge.status`, `knowledge.search` | the research base and a search over it |
| memes | `meme.status`, `meme.list` | the queue, with pictures fetched by the shell |

The keys `1` to `5` and the arrows (with Home and End) switch tabs; a digit typed into a field stays
a digit. Every action is read-only, and nothing in the app opens a link or changes anything.

## Run

```bash
cd dokja_interfaces/desktop
pnpm install
pnpm start     # builds, then opens the window
```

It needs a running orchestrator. By default it asks `tcp://127.0.0.1:5558`, the port the compose
stacks publish. Without one the card says so after about three seconds. Each tab needs the service
behind it: a service that is stopped or switched off is said so on its tab, and the others keep working.

### The digest tab

The digest needs the feeds service (`dokja_services/dokja_feeds`) and, in the orchestrator, the
`digest.*` route. The repository ships no source: a source is a plugin you keep outside it (see the
service's README). With no plugin the tab says why there is nothing to read (no directory, no source,
none enabled) instead of showing an empty list. A source that fails is listed with its reason, and the
list opens by itself when one does.

Flags: `--request-endpoint` (default `DOKJA_ORCH_REQUEST_ENDPOINT` or `tcp://127.0.0.1:5558`),
`--lang en|pt`, and `--timeout` (the longest a request may wait, default `2m`; the health card asks for
15 s). Pass them after `--`, for example `pnpm start -- --lang=pt`.

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
allow-list never reaches the orchestrator, a reply is cut down to what a card reads, and a picture is
fetched only at an address the orchestrator listed. It needs a display and is skipped without one.

`dev:web` is the fast loop for the look. To run the real shell against it, start the dev server and
launch Electron with `DOKJA_DESKTOP_DEV_URL=http://localhost:5173` (only a local address is accepted).

## How it is built

```
screen (React)  ->  preload (three functions)  ->  main process  ->  orchestrator
src/renderer        src/preload                    src/main          ZeroMQ REQ
```

- **The screen** only knows a `Transport` (`src/shared/transport.ts`): `request(type, payload)`
  resolves with data or with an error code, never throws. In Electron the preload implements it
  over IPC; in a browser a fake does. Nothing in the screen depends on the shell.
- **The main process** owns the socket. It sends one request at a time and counts the wait in line
  against each request's deadline. Each request uses its own socket, because a REQ socket that timed
  out can never send again and a late reply must not be taken for the next answer. `immediate` mode
  makes it fail fast, as "unavailable", when nobody is listening.
- **The allow-list** (`src/shared/actions.ts`) names the only actions the screen may ask for. Anything
  else is refused before the socket is touched: the orchestrator has no authentication and also
  exposes administrative actions (`services.set`, `discord.send`, `scheduler.*`).
- **Payloads and projections** (`src/main/actions.ts`): each action builds its own payload, so the
  screen cannot add fields or change the digest's rules, and hands the screen only the fields a card
  reads, so channel IDs, provider profiles, plugin names and item addresses never reach it. Long texts
  are cut.
- **Pictures:** the screen never loads a remote image. `preview(url)` asks the shell, which fetches
  only an address the orchestrator itself listed in a meme page, over http or https on the default ports,
  to public addresses only (checked on connect and on every redirect), with a size and time limit,
  and only if the bytes are a picture. It hands back a data URL.
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
   catalogs, the name of its tab included.
3. Add one line to `CARDS` in `src/renderer/cards/registry.ts`: its kind, the id of its tab's name and the
   component. The order of the lines is the order of the tabs; keep health first, because its answer
   drives the name's pulse and it is the one card mounted at start.

## Look

Dead-channel cyberpunk, chosen by the owner: the page is the grey-blue of a television tuned to
nothing, and neon only ever means something (cyan is up, magenta is a problem, amber has not been
checked). The name has a cyan and a magenta ghost behind it that drifts out of register as the
system gets worse. Type is B612 Mono for everything and Bodoni Moda italic for the name, both
bundled under the SIL Open Font License. Motion is one terminal-style print of the rows and a
blinking cursor, and both stop under `prefers-reduced-motion`. The tokens are at the top of
`src/renderer/styles.css`.
