# Ningo Dokja

A local, modular assistant for research, experience memory and communication.
Inspired by *Omniscient Reader*, Dokja keeps the personality of an arrogant
philosopher while the project explores how an assistant can use knowledge,
reason from previous outcomes and act under explicit controls.

The **desktop is the main workspace**. CLI, TUI, Discord and WhatsApp
provide other entry points into the same orchestrated capabilities. Features
and setup vary by interface.

[Get started](#get-started) · [Capabilities](#what-you-can-do) ·
[Architecture](#architecture) · [Documentation](#documentation)

## What you can do

| Area | Available behavior | Setup |
| --- | --- | --- |
| Knowledge | Add notes, addresses and files; search stored passages; browse documents; inspect classification and answer pending classification questions | Knowledge module; embeddings for semantic retrieval |
| Sources and reading | Follow configured sources, browse a digest and extract documents through plugins | Feeds module and operator-enabled plugins |
| Experience memory | Inspect recorded experiences, their context, actions, predictions and outcomes; provide explicit feedback; inspect prediction statistics | Memory module; outcomes stay pending until observed or explicitly resolved |
| Conversation | Talk through supported interfaces, with optional retrieved Knowledge context | Orchestrator, Chat AI and a configured provider |
| Messages and media | Browse configured conversations, send through supported delivery paths and work with memes | Platform credentials, eligible channels/conversations and the required modules/plugins |
| System | Inspect readiness, start or stop local modules, change supported stack settings and manage scheduled jobs | Local runtime controller and a compatible container engine |

Capabilities depend on the modules and plugins you enable. Source collectors and
model-backed content providers need their own configuration; the repository does
not ship a ready-to-use set of external sources. See the
[capability map](dokja_docs/capability-map.md) for current ownership and contracts.

## Get started

The local desktop path uses the **lite stack**. You can open the desktop while
application services are stopped, then start the modules you need from System.

### Requirements

- A local checkout on a Unix host for container lifecycle control.
- Go **1.25.0** and Node.js **24.12.0**, as recorded in
  [.tool-versions](.tool-versions), plus pnpm.
- Native ZeroMQ development headers, `pkg-config` and a C build toolchain for
  the host runtime controller compiled with the desktop.
- A running Docker engine, or compatible Podman setup, with `docker compose`
  available to the same user who launches the desktop.
- Python **3.12.2** for host-run Python components; containerized components
  provide their own environments.

The local controller currently manages the lite stack. Windows and remote/custom
orchestrator endpoints do not provide these container controls.

### Launch the desktop

From the repository root:

```sh
cd dokja_interfaces/desktop
pnpm install --frozen-lockfile
pnpm start
```

The build includes the host runtime controller. Opening System starts or
reconnects to that background daemon; it does not start all application modules.

1. Open **System** and inspect the module states.
2. Use **Settings** for supported non-secret configuration, including model
   choices and messaging preferences. Credentials remain local configuration.
3. Start the application orchestrator and the modules needed for your task.
   Starting a module brings up its required dependencies; readiness is checked.
4. Open Knowledge, Memory or Messages once their backing modules are ready.

Knowledge and Memory may initialize an embedding model on first start, which
requires a model download and can take longer than ordinary startup. Chat and
messaging require their provider/platform configuration. An enabled routing
switch alone does not mean its container is running.

Settings are stored in the checkout's ignored `.env`. Saving a setting may
recreate running modules that read it; the interface shows the affected modules.
Closing the desktop leaves the daemon and running modules active. Use System to
stop modules; persistent data and dependencies still in use are preserved.

For configuration, ownership rules and troubleshooting, see the
[local runtime guide](dokja_orch/runtime/README.md),
[desktop guide](dokja_interfaces/desktop/README.md) and
[runbook](dokja_docs/production-runbook.md#desktop-local-runtime-controller).

### Other interfaces and deployment

| Interface | Documentation / implementation |
| --- | --- |
| CLI | [Commands, module controls and settings](dokja_interfaces/cli/README.md) |
| TUI | [Terminal workspace](dokja_interfaces/tui/README.md) |
| Discord | [Bot setup and delivery](dokja_docs/discord-interface.md) |
| WhatsApp | [Linked-device setup and conversations](dokja_docs/whatsapp-interface.md) |

The repository contains separate [lite](docker-compose.lite.yml),
[development](docker-compose.dev.yml) and [production](docker-compose.prod.yml)
Compose definitions. Use the [production runbook](dokja_docs/production-runbook.md)
for deployment configuration and checks; desktop module control manages the
local lite stack.

## Architecture

Every interface submits an event or request to the orchestrator:

```text
Interface → Event / Request → Orchestrator → Domain → Service
```

- **Interfaces** accept input and render replies.
- **The orchestrator** owns routing, workflow execution and transitions.
- **Domains** own business rules, validation and action preconditions.
- **Services** provide integration mechanisms such as model inference,
  extraction, retrieval and platform access. State ownership is explicit for
  the stores behind these capabilities.

Synchronous requests use the HTTP bridge or ZeroMQ request/reply. Asynchronous
events use ZeroMQ pub/sub. The host runtime controller provides a restricted
local control path so modules can start even when the application orchestrator
is stopped. Interfaces still go through orchestration and domain policy.

The refactoring explores Knowledge, Memory, Action and Rules as
[product concepts](dokja_docs/decisions/0010-four-concepts.md). That proposal is
not a one-to-one mapping to containers, databases or packages. Existing Book,
Meme and Digest workflows retain compatibility paths while responsibilities move
into shared capabilities.

### Cognitive development

The intended direction is a controlled cycle: retrieve knowledge and experience,
form and revise predictions, identify missing evidence, decide whether to
continue or act, then observe and reuse results. The task state determines the
next operation; a fixed Knowledge → Prediction → Experience pipeline is not the
goal.

Dokja should own that state, evidence, authorization and observed outcomes.
Codex, Claude Code and Ollama are interchangeable inference mechanisms under the
[provider responsibility boundary](dokja_docs/decisions/0011-inference-provider-boundaries.md).
A provider proposal is not an observed fact or permission to act.

This remains active engineering work. The repository includes a
[synthetic causal-cycle experiment](dokja_docs/cognitive-cycle-experiment.md),
[storage measurements](dokja_docs/step-10-storage-spike.md) and a
[multi-hop validation contract](dokja_docs/step-10-multihop.md). These experiments
do not establish a production cognitive loop, general learning or a decision to
migrate application storage. [steps.json](steps.json) records implementation,
validation evidence and remaining work.

## Repository layout

| Path | Responsibility |
| --- | --- |
| [dokja_orch/](dokja_orch/) | Go orchestrator, ingress, workflows and clients |
| [dokja_orch/runtime/](dokja_orch/runtime/) | Host bootstrap orchestrator for local module control |
| [dokja_domain/](dokja_domain/) | Domain policies and contracts |
| [dokja_services/](dokja_services/) | Integrations and service implementations |
| [dokja_interfaces/](dokja_interfaces/) | Desktop, CLI, TUI and messaging interfaces |
| [dokja_store/](dokja_store/) | Shared Go persistence and isolated storage experiments |
| [dokja_lab/](dokja_lab/) | Python research sandbox and migration sources |
| [dokja_legacy/](dokja_legacy/) | Historical implementation retained as a migration reference |
| [dokja_docs/](dokja_docs/) | Architecture, decisions, evaluations and operations |
| [scripts/](scripts/) | Test orchestration and secret scanning |

## Development and verification

Each Go module has its own `go.mod`; run checks inside the module you change.
For example, from `dokja_orch`:

```sh
go test ./...
go vet ./...
```

The runtime controller is a nested module and needs its own checks. For desktop
work, run these from `dokja_interfaces/desktop` after installing dependencies:

```sh
pnpm typecheck
pnpm test
pnpm build
pnpm test:e2e
```

To preview the desktop UI with fixture data, use `pnpm dev:web` in that directory.
For the repository-wide test runner, from the root:

```sh
scripts/test_all.sh
```

The runner writes reports under `test_reports/` and reports skipped suites when
prerequisites are missing. Optional integration tests and storage benchmarks may
need external services. Passing unit tests does not imply a live stack was tested.

Read [AGENTS.md](AGENTS.md) for module commands, architecture constraints and
contribution conventions. Run [secret and policy checks](dokja_docs/secret-scanning.md)
before publishing changes. Keep credentials, private plugins and runtime databases
out of Git.

## Documentation

- [Documentation index](dokja_docs/README.md)
- [Capability ownership and extension paths](dokja_docs/capability-map.md)
- [Domain responsibilities](dokja_docs/domain-responsibilities.md)
- [Events and requests](dokja_docs/events-vs-requests.md)
- [Workflows](dokja_docs/workflows.md)
- [Architecture decisions](dokja_docs/decisions/README.md)
- [Configuration and operations](dokja_docs/production-runbook.md)
- [Feeds plugins and reading](dokja_services/dokja_feeds/README.md)
- [Content plugin migration](dokja_docs/feeds-content-plugins.md)
- [Refactoring progress](steps.json)

## License

[MIT](LICENSE).
