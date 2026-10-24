# Host runtime controller

A small bootstrap orchestrator runs outside Compose, so the local interfaces
(desktop, CLI and TUI) can start or stop modules while the main application
orchestrator is stopped. All three reach the same daemon and get the same control. Its restricted local
router accepts the existing event envelope shape and dispatches `runtime.modules`
and `runtime.set` to the Runtime domain and stateless Compose adapter:

`Desktop/CLI/TUI → local request → bootstrap orchestrator → Runtime domain → Compose adapter`.

Conversational/domain requests continue through the main orchestrator unchanged.
No application domain logic, arbitrary event dispatch, shell command API or remote
control endpoint is introduced. The daemon does not initialize application
containers at startup and does not stop them when desktop closes.

The Electron build compiles `cmd/runtime` into its generated `dist/runtime`
directory. At the first runtime read, the main process starts or reconnects to
one background daemon per canonical local checkout. Only the local lite stack is
supported initially, on Unix hosts with Go 1.25 and native ZeroMQ available at
build time and `docker compose` (Docker or compatible Podman) available at runtime.
Windows and remote/custom application endpoints do not expose container controls.

The daemon binds a Unix socket in `$XDG_STATE_HOME/dokja/runtime-<checkout id>/`
(default `~/.local/state/dokja/...`), shared by every local interface of that user.
The desktop carries over the ownership journal earlier builds kept in its own data
directory once. The CLI and TUI launch the daemon when none answers, from
`DOKJA_RUNTIME_BIN`, `dokja-runtime` on `PATH`, or the desktop build's
`dist/runtime/dokja-runtime`; inside a container they refuse, since the daemon
drives the host's engine.
The directory is mode 0700, the socket is mode 0600, and an OS file lock prevents
duplicate processes or unsafe stale-socket replacement. Requests carry a protocol
version and project-bound response identity. Browser origins and unknown actions
are rejected; input and output have bounds. There is no TCP listener or root
helper. It runs with the same user and engine permissions as desktop.

`runtime.settings` lists the stack settings the interfaces may change and
`runtime.settings.set` (`values`: setting id → text) saves them to the shared local
SQLite database and recreates the running modules that read them; see the runbook.
The controller imports allowed legacy `.env` options once, then projects database
values into Compose child environments. Credentials remain in `.env`; the embedding
profile is fixed. See [ADR 0013](../../dokja_docs/decisions/0013-database-runtime-settings.md).

`runtime.set` accepts a known `module` and boolean `enabled`. It acknowledges the
operation immediately; desktop polls `runtime.modules` until ready, stopped or
failed. One operation runs at a time, repeated identical transitions are
idempotent, and a client disconnect does not cancel accepted work. Explicit On
can retry readiness. Startup never rolls back by stopping unrelated containers.

Dependency and ownership rules are in `dokja_domain/dokja_runtime`. Private
`ownership.json` records dependencies created by this controller and explicitly
selected root modules. Stopping a module also stops unused owned dependencies,
while preserving borrowed dependencies and those required by another running
module or recognized external interface/scheduler. A requested in-use dependency
is refused. Persistent data and volumes are retained. Reconnect/restart reads
actual container state; it does not assume that a previously running process is
ready or automatically resume starting containers.

For foreground diagnosis, build and launch the binary with trusted absolute
`--root` and `--state-dir` paths. Stop the daemon with the process's normal SIGTERM
when changing its build or local project configuration; this leaves application
containers alone. Remove neither a live socket nor its ownership file. Startup
can recover a stale socket only after acquiring the process lock. Closing desktop
leaves background work running, but no OS login/boot autostart is installed.

Run `go test -race ./...` and `go vet ./...` in this nested Go module. The root
orchestrator module's `go test ./...` does not traverse nested modules; CI and
`scripts/test_all.sh` explicitly include it.

`runtime.providers` is a read-only local-provider inventory for every operator
interface. Infrastructure discovers host CLIs and queries only the selected
stack’s running Ollama. The Runtime domain reports installation/listing separately
from inference, which is never performed by discovery. See
[provider inventory](../../dokja_docs/provider-inventory.md).

The local Ollama's models are managed with five actions. Every one answers the same status: the
embedding model, the last download if any, and what this controller last did to the engine.

- `runtime.models.status` reads that status.
- `runtime.models.engine.start` starts the engine (the internal module `embeddings`) on its own, so a
  model can be downloaded and chatted with while memory and knowledge are off. Stopping it stays with
  the modules that need it.
- `runtime.models.pull` with `{"model": name}` starts a download in the background and answers at once.
  - Poll the status for its phase (`starting`, `downloading`, `verifying`, `writing`), its percentage,
    and its end (`done`, `failed` with a plain reason, or `cancelled`).
  - One download runs at a time, only while the engine runs, and for at most two hours.
- `runtime.models.cancel` ends the pull command and kills the `ollama pull` process it started in the
  container, which ending `docker compose exec` alone would leave running. A new pull waits until the
  cancelled one has stopped.
  - Not verified: whether the Ollama server stops a download when its client dies, and whether it keeps
    the part already downloaded.
- `runtime.models.remove` with `{"model": name}` deletes a model. It refuses:
  - the model downloading now;
  - the model chat is bound to;
  - the embedding model while memory or knowledge run.

Names must match the model-name pattern, so no flag or shell text reaches the engine. The pull's exit
status says whether it worked. From its output only phases, byte counts and "not found" are read; its
text is never passed on. Without a terminal the CLI redraws in place with escapes and no line breaks.
`dokja_services/dokja_runtime/testdata` holds a recording of that for a missing model; a downloading
layer's line is read from the CLI's known format, not yet from a recording. `runtime.providers` also
gives each Ollama model's size and names the embedding model.
