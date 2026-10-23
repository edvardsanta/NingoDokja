# Local inference-provider inventory

Step 11.4, discovery batch. This builds on the shared operational-settings lot
in ADR 0013. It provides an observation surface, not provider selection or a
production inference integration.

## Use

- Desktop: System → Inference providers → Inspect providers. Refresh is explicit.
- CLI: `dokja-cli providers [--runtime-root <checkout>]`.
- TUI: open settings with `c`, then providers with `p`; `r` refreshes and Esc returns.

Every interface uses `runtime.providers` through the private bootstrap controller.
The controller can answer while the application orchestrator is stopped. An
older controller without this additive action returns an error; rebuild and
restart it as described in the runbook. No inference, model pull, model load,
module start, setting update or memory write is requested by this operation.

## What discovery establishes

| Provider | Observation | What remains unknown |
| --- | --- | --- |
| Codex | Host executable found on the daemon's PATH, or unavailable | Authentication, quota, available account models and inference |
| Claude Code | Host executable found on the daemon's PATH, or unavailable | Authentication, quota, available account models and inference |
| Ollama | Models listed by Ollama in the selected running lite stack | Model/capability compatibility, generation/embedding success and latency |

CLI discovery does not execute either agent. No account-specific model names are
invented or presented as available. Every row reports `inference_status=not_tested`.
A listed model does not imply support for generation, classification, embeddings
or OCR. These contracts and validations remain capability-specific.

The lite stack's Ollama does not publish its server port to the host. Discovery
uses the existing project-scoped Compose inspection and, only when that service
is running, the fixed read command `exec -T dokja-ollama ollama list`. It does not
query an unrelated host Ollama, activate a profile/service or publish a new port.
An empty successful listing is distinct from a stopped service, inaccessible
engine, failed command, timeout or invalid response.

## Ownership and bounds

Interfaces render observations. The bootstrap orchestrator routes the request;
the Runtime domain owns the public inventory, labels and interpretation of
installation versus inference. Infrastructure discovers executables and queries
the selected stack. Cognitive orchestration still owns model calls, state,
evidence, memory, additional hops and actions (ADR 0011).

Discovery has a three-second context budget. Compose commands inherit existing
process-group cancellation. Model-list parsing accepts the current `NAME ID SIZE
MODIFIED` table header, at most 64 KiB of output and 64 entries. Unknown table
formats are refused. Names are validated and deduplicated by the domain; a
truncated list is marked explicitly. Replies contain no executable path, raw
command output, environment or credential. This CLI table format is a version
compatibility limit; it is not a stable JSON API contract.

Observations are fresh, on-demand and timestamped; they are not durable provider
registrations and are not reused as evidence of successful inference. The
settings migration remains the source for existing operational preferences.
This batch adds no database key, schema migration or environment variable.

## Evidence and remaining batches

Tests use hand-built executable lookups and engine responses. They exercise the
shared read route, no lifecycle operations, stopped/empty/failure states,
cancellation, malformed/version-sensitive tables, output limits, domain
normalization, omitted private fields and operator-triggered refresh. No live
provider authentication or inference call was performed for this batch.

Still pending in step 11.4:

1. Durable provider registrations/configuration and validated capability bindings
   in the shared database, with the same controls in all three interfaces.
2. Adoption of the model-assisted inference contract after its separate evaluation
   and review; configured preference must remain distinct from observed success.
3. Capability-specific OCR options and legacy classifier/provider migration.
4. Live provider/model validation with explicit unavailable, quota, unsupported
   and inference-failure results. No automatic fallback may expand permissions
   or reset a cognitive task's budgets.
