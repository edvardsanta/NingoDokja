---
name: run-desktop
description: Launch the Ningo desktop app (Electron) on the person's own screen against a real, isolated orchestrator, so they can see and try it. Use when asked to run, open, show or test the desktop app, or to confirm a desktop change works in the real app and not only in tests. Never starts the scheduler or anything that posts to Discord.
---

# Run the desktop app (Ningo Dokja)

The goal is a window on the person's screen, talking to a real orchestrator that cannot touch
anything outside the machine. Work from the worktree of the task, not the repository root.

## What must never be started

- **The scheduler** (`dokja_services/dokja_scheduler`) and any Discord interface or bot token.
  The scheduler fires `meme.dispatch.scheduled`, which delivers memes to real channels.
- Docker/Podman stacks. The sandbox can block podman's runtime directory, and `docker compose` may be
  only a podman wrapper. Run the binaries directly.

Without them the health card shows `scheduler` as stopped and the other services as errors. That is
the honest state of a bare orchestrator, and the switches still work.

## 1. The orchestrator

```bash
SP=<scratchpad>                         # any scratch directory, never the repository
cd dokja_orch
go build -o "$SP/va" ./cmd/va
```

If `go` fails with a stale `GOROOT` (the asdf shim can point at one after the environment changes),
set them from the version in `.tool-versions` and call that Go directly. `export` first: a prefix
assignment would not apply to `$GOROOT` in the same command word.

```bash
GO_HOME=$(asdf where golang)
export GOROOT=$GO_HOME/go GOPATH=$GO_HOME/packages
"$GOROOT/bin/go" build -o "$SP/va" ./cmd/va
```

```bash
cd "$SP"
VA_STATE_FILE="$SP/state.json" \
  VA_ZMQ_ENDPOINT=tcp://127.0.0.1:35557 \
  VA_ZMQ_REQUEST_ENDPOINT=tcp://127.0.0.1:35558 \
  VA_HTTP_ENDPOINT=127.0.0.1:38091 \
  nohup ./va > va.log 2>&1 &
echo $! > va.pid
```

- Loopback ports away from the defaults (5555, 5558, 8091), so a stack the person already runs is
  not hit. Check with `ss -ltn` first.
- `VA_STATE_FILE` is where the switches persist (`{"services":{"book":true}}` means `book` is OFF,
  `{}` means everything is on). Delete it to reset.
- Do not set `DISCORD_*`, `COMMUNICATIONS_*` or `DOKJA_COMMUNICATIONS_TOKEN`: the communications tab
  then says it is not configured, and nothing can be sent.

## 2. The desktop

```bash
cd dokja_interfaces/desktop
ls node_modules >/dev/null || ln -s <another-worktree>/dokja_interfaces/desktop/node_modules node_modules
pnpm run build                          # renderer and shell; `pnpm start` builds too but is not detached
nohup ./node_modules/.bin/electron . --ozone-platform=wayland \
  --remote-debugging-port=9333 --user-data-dir=$SP/electron-profile \
  --request-endpoint=tcp://127.0.0.1:35558 > $SP/electron.log 2>&1 &
```

- On a Wayland session (`$WAYLAND_DISPLAY` is set) the window opens on the person's screen with no extra
  display setup; on X11 drop `--ozone-platform=wayland`. A throwaway `--user-data-dir` keeps it apart
  from their own profile.
- The language follows the locale; `--lang=en` or `--lang=pt` forces one.
- The first `ningo.status` takes about five seconds: the orchestrator waits for each service that is
  not running to time out. The card shows "Loading" until then; this is not a hang.
- `pnpm run build` overwrites `dist/`. Restart the window afterwards (see 4) to show the new build.

## 3. Drive it

The debugging port is the way in; the E2E helper has a small client. Run this with
`node --import tsx script.mts` from `dokja_interfaces/desktop` (an `.mts` file allows top-level await):

```ts
import { Page } from "./tests/e2e/support.ts";
const page = await Page.connect(9333);
console.log(await page.evaluate(`[...document.querySelectorAll('button[role=switch]')]
  .map((b) => b.getAttribute('aria-label') + '=' + b.getAttribute('aria-checked'))`));
page.close();
```

- `page.click(x, y)` is a real mouse press, the kind the shell counts as the person being there.
  Before clicking an element, wait until `document.elementFromPoint(x, y)?.closest('button')` is that
  element: the rows play an entrance animation (`clip-path`) when a tab opens, and a clipped row takes
  no click.
- Read the effect in `$SP/state.json`, `$SP/electron.log` (one line per request, with the time it took)
  and `$SP/va.log`.
- Put back what a check changed (switch it on again) before handing the window over.

## 4. Restart and stop

```bash
pkill -f "user-data-dir=$SP/electron-profile"      # the window only
kill $(cat $SP/va.pid)                              # the orchestrator, if its pid was saved
pkill -f "$SP/va"                                   # otherwise by path
```

Kill what was started by PID or path, never by a broad name. Tell the person what is running (the
three ports, the state file) so they can stop it.

## Report

Say what was started and where, what the person can try, and what is deliberately missing
(scheduler, Discord, communications token). If something here had to change to work, update this file.
