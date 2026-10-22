# Model-assisted cognitive experiment

Date: 2026-10-22. Step 10.3: in progress. This experiment validates ownership
boundaries and exposes an unreliable inference policy; it does **not** establish
successful model-assisted learning or production readiness.

## Implementation and responsibilities

- Domain: `dokja_domain/dokja_inference` defines bounded requests and validates
  revisions, evidence/episode citations, action catalogs and proposal semantics.
- Infrastructure: `dokja_store/spikes/cognitive_storage/providers` implements
  Codex and Claude Code process adapters and Ollama structured HTTP inference.
- Cognitive driver: the existing spike selects retrieval transitions, invokes
  inference, rejects invalid/late proposals, checks facts and authorization,
  performs local simulation and records observed outcomes. No provider receives
  storage handles or action dispatch capabilities.

These are modules in an experiment, not new deployment units. Existing event,
HTTP, desktop, database and Compose contracts are unchanged. The driver still
contains synthetic policy; production domain/orchestrator integration is pending.

Each inference receives a versioned snapshot with source/version references,
a catalog of two local strategies, eligible observed episodes and remaining call
budget. Attempts record provider, requested model, reported model (or
`unreported`), elapsed time and available token counts. Proposals cite the snapshot;
probabilities are hypothetical and uncalibrated. Invalid responses are retained
in the diagnostic trace but never applied as a prediction or observed experience.

Fallback is explicit configured order, within the same remaining budget. There
is no automatic fallback to the deterministic predictor when a configured model
fails. The deterministic predictor remains available only to existing baseline
runs with no inference provider.

CLI processes run in disposable directories, without configured tools, hooks,
plugins, project instructions or MCP integrations. Cancellation terminates the
process group; output and response sizes are bounded. This Linux-only prototype
uses installed authenticated CLIs, not a separate OS credential boundary. Only
synthetic input was sent. Tool-event rejection is a backstop, not proof that an
arbitrary future CLI version cannot expose a tool. Revalidate isolation on CLI
updates. Ollama receives no tools. No model downloads are performed.

## Reproduction

From `dokja_store/spikes/cognitive_storage`:

```sh
go test -race ./...
go vet ./...
go run ./cmd/modelcycle -provider codex -model gpt-6-luna -case pair -timeout 60s
go run ./cmd/modelcycle -provider codex -model gpt-6-luna -case no-memory -timeout 60s
go run ./cmd/modelcycle -provider codex -model gpt-6-luna -case contrary -timeout 60s
go run ./cmd/modelcycle -provider codex -model gpt-6-luna -case missing -timeout 60s
go run ./cmd/modelcycle -provider codex -model gpt-6-luna -case conflict -timeout 60s
go run ./cmd/modelcycle -provider codex -model gpt-6-luna -case unauthorized -timeout 60s
```

Use `-provider claude` for Claude Code, or `-provider ollama -model MODEL
-endpoint http://localhost:11434` for an already provisioned local model. CLI
availability and authentication are operator prerequisites. Provider chains use
one model flag for all entries; use compatible bindings or separate runs.

The model runner uses in-memory synthetic storage, with one store shared between
the paired tasks. It does not rerun the earlier PostgreSQL benchmark. The
[deterministic experiment](cognitive-cycle-experiment.md) remains the comparison
baseline, including its single/bounded/iterative and database measurements.
No latency comparison between those old runs and these concurrent live calls
establishes model or storage performance.

## Observations, including failures

Codex CLI 0.163.0; requested model `gpt-6-luna`. The JSONL transport did not report
the actual model identity; all results preserve `unreported` rather than asserting
that the requested model answered. Two paired runs used the final prompt. Five
control runs used the same prompt and ran concurrently. This is a small exploratory
sample, not an accuracy benchmark.

| Run | Observed behavior | Total wall time |
| --- | --- | ---: |
| Pair A, first task | Model requested `sample`; after retrieval proposed `fast`; simulation failed and recorded episode | Pair total 13,076 ms |
| Pair A, second task | Cited the recorded episode but returned contradictory action/missing fields; rejected | Included above |
| Pair B, first task | Requested `sample`, then proposed `fast`; observed failure recorded | Pair total 15,436 ms |
| Pair B, second task | Cited prior failure, proposed `fast` without sample; driver abstained on missing precondition | Included above |
| No memory | Requested sample, then invented an evidence request; rejected | 7,910 ms |
| Contrary sample | Requested sample, then requested that already-present ID again; rejected | 8,437 ms |
| Missing sample | Requested sample; retrieval unavailable; driver abstained | 6,341 ms |
| Conflicting facts | Requested sample; driver detected conflict and requested clarification | 4,564 ms |
| Unauthorized | Requested sample, then already-present document; rejected before authorization gate | 7,323 ms |

In pair B, revision 4 requested sample at hop 3; revision 6 proposed execution.
The first task's provider calls took 4,974 and 5,453 ms and reported 10,385/83 and
10,413/108 input/output tokens. The next task's call took 5,008 ms and reported
10,405/114 tokens. These counts include provider runtime overhead; no monetary
cost was measured. Recalled failure changed the model's explanation, but did
not produce a justified improved strategy. The no-memory control also failed,
so there is no demonstrated causal improvement from memory.

Earlier prompt-development attempts failed because relationship-only records
were interpreted as missing observations. Empty facts are now encoded as `{}`,
and the prompt explains `requires` and `question` as evidence IDs. This improved
some calls, but did not eliminate the error. A Codex development-feature warning
was initially rejected as an unsupported event; the adapter now suppresses that
warning explicitly. None of these failed attempts was silently repaired or
replaced by a deterministic answer.

Claude Code 2.1.307 returned a weekly quota-limit error, so live inference was
blocked. Ollama had no reachable server; no live model result was obtained.
All three adapter transports have fixture tests, which do not substitute for
live provider evaluations. The live unauthorized case did not reach its gate;
authorization enforcement is covered by deterministic integration tests only.

## Tests and interpretation

Passing checks: domain `go test -race ./...` and `go vet ./...`; spike
`go test -race ./...` and `go vet ./...`. Tests cover malformed/stale output,
foreign citations, catalog violations, deadlines, cancellation, CLI isolation
arguments and cleanup, tool-event rejection, explicit fallback budgets, copied
snapshots, observed-outcome reuse with a scripted provider, contrary evidence
and authorization. Scripted-provider success is not real-model success.

The evidence supports replacing a manual predictor with a real proposal provider
while preserving driver-owned state and controlled failure. It also demonstrates
a real evidence request influencing a subsequent inference and safe simulation.
It refutes the stronger claim that this prompt/contract reliably closes the
memory-to-improved-decision loop. Step 10.3 remains open.

Next experiments should test request-dependent constrained fields and explicit
availability metadata without prescribing the answer; capture diagnostic action
observations (not only success/failure) so an episode can justify a different
strategy; then repeat paired/no-memory controls across providers and seeds.
A failed ASCII parser alone does not prove normalization will succeed. Do not
encode that conclusion as an automatic domain rule to manufacture model success.
Permanent episode storage, broader prediction targets, calibration, production
routing, multi-provider live replacement and scalable W2/W4 traversal remain open.
