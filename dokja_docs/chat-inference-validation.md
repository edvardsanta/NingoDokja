# Bounded chat inference validation

Step 11.4 follow-up, measured on 2026-10-23. This is adapter/domain verification,
not production readiness or completion of the cognitive learning loop.

## Corrective behavior

The chat path now uses the same explicit CLI feature isolation already exercised
by the cognitive experiment. In addition to shell/browser controls, the Codex
invocation disables hooks, plugins, agents, memory/skill discovery and web search,
and ignores local user configuration and execution rules. The temporary working
directory remains empty at launch and is removed afterward.

The adapter reads bounded JSONL events and requires a completed inference turn
before accepting the final reply file. Tool, failed, malformed and unknown events
are refused even if that file exists. Trace validation is an acceptance gate;
launch controls are what prevent tool execution. It is not a sandbox guarantee
against arbitrary behavior by an installed executable.

Both the adapter and Runtime domain refuse late success after cancellation.
The domain still owns the 90-second budget, selected registration validation and
reply bounds. No fallback or task-state update is introduced.

## Reproducible checks

Run `go test -race ./...` and `go vet ./...` separately in:

- `dokja_domain/dokja_runtime`
- `dokja_services/dokja_runtime`
- `dokja_orch/runtime`

Regression tests first reproduced acceptance of a cancelled provider's late reply
and a tool-bearing Codex trace with a final reply file. Controlled executables
then verify launch flags, input roles, isolated working directories, cleanup,
actual subprocess cancellation and output overflow. These fixtures do not access
real model authentication or invoke providers.

The opt-in `TestLiveChatProvider` uses a four-message synthetic conversation and
requires exact recovery of an earlier marker. It creates only in-memory domain
registration/binding fixtures, makes one selected-provider inference call, then
checks that a disabled registration prevents another call. Ordinary test runs
skip it. It does not open the operator database or change the active binding.

From `dokja_services/dokja_runtime`:

```sh
go test -v -run '^TestLiveChatProvider$' -count=1 -timeout 110s \
  -args -live-chat-provider codex -live-chat-model gpt-6-luna

go test -v -run '^TestLiveChatProvider$' -count=1 -timeout 110s \
  -args -live-chat-provider claude_code -live-chat-model sonnet
```

For Ollama, explicitly supply a generation-capable installed model and
`-live-chat-root` pointing to the existing lite checkout. The test uses that
stack's running model service; it does not start containers or download models.
Opting in consumes actual provider inference and uses the operator's existing
host authentication. No private prompt, credentials, raw trace or personal path
is logged by the test.

## Observations

| Target | Result | Evidence and limit |
| --- | --- | --- |
| Codex, requested `gpt-6-luna` | Passed | One call; exact earlier-marker recovery from four messages; 2,737 ms; disabled-registration control blocked another call. The reply contract reports the requested model, not an independently verified answering-model identity. |
| Claude Code, requested `sonnet` | Failed | Selected adapter exited without an accepted reply. A separate synthetic diagnostic returned exit 1 and `is_error=true`; no reliable authentication/quota/argument category was established. Failure is not counted as successful validation. |
| Ollama | Not run | The observed installed catalog contained only `bge-m3:latest`; no generation model was installed or selected for this check. Embedding similarity is not chat inference. |

Codex's fixture selection was temporary. The operator's saved Claude selection,
provider declarations, application chat history and live services were unchanged.
A successful synthetic inference does not prove multi-hop decision quality,
prediction calibration, durable memory reuse or cross-provider equivalence.

## Remaining work

Diagnose the selected Claude provider's error without exposing raw credentials or
provider output, and evaluate an explicitly installed Ollama generation model.
Measure answering-model provenance and provider capability compatibility before
promoting general cognitive bindings. Continue separate work on OCR/legacy
consumers, native interface baselines and durable conversation history.

There are no event-envelope, route, HTTP response, schema, environment, Compose or
volume changes. Rebuild/restart the host runtime to use the corrected adapter;
no production rollout or operator configuration change was performed here.
