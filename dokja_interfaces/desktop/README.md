# Dokja Desktop

Desktop interface for Ningo. It is an interface adapter like the CLI, the TUI and Discord: it only
talks to the orchestrator over ZeroMQ request/reply and holds no business logic. The plan, the
decisions and the Phase 0 results are in [`dokja_docs/desktop-app-plan.md`](../../dokja_docs/desktop-app-plan.md).

Status: Phase 1 skeleton. One card (service health, from `ningo.status`) goes through the whole
path: screen, shell, orchestrator.

## Run

```bash
cd dokja_interfaces/desktop
pnpm install
pnpm start     # builds, then opens the window
```

It needs a running orchestrator. By default it asks `tcp://127.0.0.1:5558`, the port the compose
stacks publish. Without one the card says so after about three seconds.

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
```

`dev:web` is the fast loop for the look. To run the real shell against it, start the dev server and
launch Electron with `DOKJA_DESKTOP_DEV_URL=http://localhost:5173` (only a local address is accepted).

## How it is built

```
screen (React)  ->  preload (two functions)  ->  main process  ->  orchestrator
src/renderer        src/preload                  src/main          ZeroMQ REQ
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
- **Projections** (`src/main/actions.ts`) hand the screen only the fields a card reads, so channel
  IDs and provider profiles never reach it. Long error texts are cut.
- **Hardening:** sandboxed renderer with context isolation and no Node, IPC accepted only from the
  page the app loaded, every permission request denied, no navigation or new windows, no network
  requests (except the development server), and a strict Content-Security-Policy on the built page.
  Fonts are bundled, so nothing is fetched at run time.
- The orchestrator sees `source: desktop`. It accepts any source.

## Adding a card

1. Add its action to `ACTION_TYPES` in `src/shared/actions.ts` and a projection in
   `src/main/actions.ts`, with tests that show what it leaves out.
2. Write the card in `src/renderer/cards/` with `useCard` and `CardFrame`, and its strings in both
   catalogs.
3. Add one line to `CARDS` in `src/renderer/cards/registry.ts`.

## Look

Dead-channel cyberpunk, chosen by the owner: the page is the grey-blue of a television tuned to
nothing, and neon only ever means something (cyan is up, magenta is a problem, amber has not been
checked). The name has a cyan and a magenta ghost behind it that drifts out of register as the
system gets worse. Type is B612 Mono for everything and Bodoni Moda italic for the name, both
bundled under the SIL Open Font License. Motion is one terminal-style print of the rows and a
blinking cursor, and both stop under `prefers-reduced-motion`. The tokens are at the top of
`src/renderer/styles.css`.
