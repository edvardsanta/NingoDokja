# Local runtime integration

This is a stateless Go adapter linked into the host bootstrap orchestrator in
`dokja_orch/runtime`. It is not a separately running container and deliberately
has no Dockerfile: a container cannot bootstrap its own stopped container engine
or application stack. Runtime lifecycle policy belongs to
`dokja_domain/dokja_runtime`; this package executes fixed service operations and
readiness probes, plus validated settings persistence/projection.

The adapter runs `docker compose -f <trusted-root>/docker-compose.lite.yml` from
that checkout. Docker Compose and the installed Podman Compose provider are
supported. When only a local Unix `DOCKER_HOST` is configured, the adapter also
supplies it as `CONTAINER_HOST` for Podman's Docker-compatible CLI; an explicit
Podman target is preserved. Known remote/custom environment targets are refused.
Status uses project-scoped container IDs and Docker-compatible inspect
fields, rather than assuming their `compose ps` JSON formats match. Every ID and
module/service pairing is validated. Command output is bounded and never returned
as a raw response or logged. Runtime responses contain state and bounded errors,
not container environments, commands, labels or endpoint details.

Start targets one service with `up -d --no-deps --no-recreate --build`. The domain
supplies dependency order. Stop targets one service with a bounded shutdown wait;
there is no `down`, removal, pruning or volume deletion operation. Existing
containers are reused rather than recreated as a side effect of a runtime switch;
image/configuration deployment is a separate operation.

Readiness requires an application acknowledgement over the module's published
ZeroMQ endpoint or HTTP `/health`, not merely a running container. Ollama must
answer its local model-list operation and the embedding initializer must exit
successfully. Ordinary readiness has a two-minute deadline; model initialization
may take up to eight minutes within the overall ten-minute operation deadline.
The adapter cancels entire command process groups on timeout.

Run `go test -race ./...` and `go vet ./...` here. Native ZeroMQ headers and
library (`libzmq3-dev` and `pkg-config` on the CI Linux host) are required.

`DatabaseSettings` uses `dokja_store` for one revisioned runtime configuration key
in the existing settings table. It imports allowed legacy values once and projects
saved values into Compose subprocess environments, preserving unrelated variables
and credentials. It never rewrites `.env`. The default database is the checkout's
`.dokja/dokja.db`; an exported `DOKJA_DB_FILE` selects another file. This adapter
holds no mutable configuration cache. See [ADR 0013](../../dokja_docs/decisions/0013-database-runtime-settings.md)
for precedence, failure behavior and manual-Compose/rollback limits.

`ProviderDiscovery` locates host Codex/Claude Code executables without invoking
them and lists models only in the selected running Ollama container. It never
starts a service or performs inference. Unknown CLI table formats fail explicitly;
see [provider inventory](../../dokja_docs/provider-inventory.md) for limits.

`RenderMath` renders a bounded LaTeX math expression to a PNG using a Go parser,
without invoking CLI tools, a shell, or a TeX installation. Expressions are
limited to 1,024 bytes and 16 brace levels; commands are allowlisted and image
dimensions/bytes are capped before delivery. Unsupported expressions return an
error so the Chat domain's readable text fallback can be delivered.
