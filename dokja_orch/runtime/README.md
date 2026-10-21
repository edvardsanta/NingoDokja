# Host runtime controller

A small bootstrap orchestrator runs outside Compose, so desktop can start or stop
modules while the main application orchestrator is stopped. Its restricted local
router accepts the existing event envelope shape and dispatches `runtime.modules`
and `runtime.set` to the Runtime domain and stateless Compose adapter:

`Desktop → local request → bootstrap orchestrator → Runtime domain → Compose adapter`.

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

The daemon binds a Unix socket inside the desktop user's private data directory.
The directory is mode 0700, the socket is mode 0600, and an OS file lock prevents
duplicate processes or unsafe stale-socket replacement. Requests carry a protocol
version and project-bound response identity. Browser origins and unknown actions
are rejected; input and output have bounds. There is no TCP listener or root
helper. It runs with the same user and engine permissions as desktop.

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
