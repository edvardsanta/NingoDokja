# Persistent provider registrations

Step 11.4, provider-configuration batch. This extends the discovery inventory
without selecting a model for a capability or changing existing inference calls.

## Configuration and observation

The supported adapters remain Codex, Claude Code and the selected lite-stack
Ollama. Each registration contains its stable ID, an enabled flag and up to 16
operator-declared model names. Names are normalized, deduplicated and sorted.
An empty list means no models have been declared; no account models are invented.

A declaration is operator input, not evidence that a model exists, is accessible
or satisfies an inference contract. `declared_models` remains separate from the
observed `models` list. Every inventory row continues to report
`inference_status=not_tested`. This registration does not change the approved
embedding profile or create a capability binding.

Disabling a registration skips that adapter's discovery and reports the existing
unavailable state with reason `disabled`. It does not stop a process/container,
revoke credentials or change legacy classifier/chat consumers. Re-enabling allows
inspection on the next explicit inventory read; it does not perform inference,
install a provider or load/pull a model. Cognitive scheduling and action authority
remain with Dokja under ADR 0011.

## Ownership and persistence

Operator interfaces submit `runtime.providers.configure` to the private bootstrap
orchestrator. The Runtime domain validates the registration and revision; the
infrastructure adapter writes through `dokja_store`. Interfaces do not open this
snapshot directly or call provider integrations.

The shared SQLite `settings` table gains one independent key,
`runtime.providers.v1`, using the existing versioned snapshot/CAS mechanism. There
is no table or schema-version change. The key has its own revision; operational
settings, chat profiles and secrets remain untouched. On first read it initializes
three enabled registrations with empty declarations, preserving previous discovery.
No legacy inference environment variables are imported or rewritten by this batch.

`runtime.providers` adds `configuration_revision`, and each provider adds
`enabled` and `declared_models`. Configure requires that exact revision, a known
provider ID, an explicit boolean `enabled` and a list of model names. Its reply
contains only the new revision and provider ID. Invalid or stale requests write
nothing. A conflict requires reload and explicit retry; there is no silent merge
or automatic retry of operator intent.

Corrupt/unreadable provider configuration blocks inventory and editing instead of
falling back to enabled defaults. It remains independent of `.env` parsing, though
Ollama inspection still uses the controller's existing Compose environment path.
Configuration reads and writes do not invoke provider discovery themselves.

## Compatibility and rollout

The configure action and response fields are additive. Existing envelopes, action
IDs, service HTTP contracts, database schema, environment variables, Compose
files and embedding consumers remain compatible. Older clients can read the
inventory; they do not understand the new registration fields/reason. New clients
must refuse editing if an older controller returns no configuration revision.

Rebuild/restart the controller and interfaces for this feature. The controller
uses the same shared database path described in ADR 0013. Back up that database
before rollout. Older builds ignore the additional key, so rollback restores their
previous discovery behavior; disabling a registration is not a security boundary
for those builds or for legacy inference consumers. No production rollout or
operator-data migration was performed during development.

## Remaining work

Capability-specific bindings, validation using real inference providers, richer
OCR configuration and migration of legacy inference consumers remain separate
batches. This configuration is an explicit input to that work, not evidence that
those consumers have already migrated.

## CLI and TUI editing

Supported provider IDs are `codex`, `claude_code` and `ollama`.
Use `dokja-cli providers configure <provider> --enabled=false|true --models=<names>`
(with `--yes` to skip confirmation). Omitted fields retain their saved value; an
explicit empty `--models=` clears declarations. TUI settings → providers allows
row selection and `e` to edit; Ctrl+T toggles discovery, Enter reviews, and a
confirmation saves. Both interfaces retain the read revision and refuse stale
writes without retry. An older controller without a revision remains read-only.

## Desktop editing

After System → Inference providers → Inspect providers, select Edit for a
registration. Change discovery eligibility or comma-separated declarations,
review the effect and confirm Save registration. The shell requires recent real
input and projects only known fields. The draft retains the revision at the time
Edit was selected; a stale write is not automatically retried or rebased. A
successful save reloads the inventory. An error preserves the draft and requires
operator-directed refresh/retry. No account credentials or executable paths are
accepted by this form.

## Verification evidence

Go race tests and vet passed for the shared store, Runtime domain, infrastructure
adapter, bootstrap orchestrator, CLI and TUI. A further TUI regression verifies
that a late inventory reply cannot change an in-progress draft's original
revision. Tests cover persistence after reopen, independent configuration keys,
stale writers, corrupt/refused values, shared routes and disabled discovery.

Desktop typecheck, 221 Node tests, 227 UI tests and production build passed.
Component/IPC coverage includes recent-input gating, known-field projection,
confirmation, cancellation, successful save/reload, old-controller refusal and
conflict handling without automatic retry. The repository aggregate reports
15 passed suites, zero failed and one skipped: the Python lab requires pytest,
which is not installed. Store and TUI are verified separately.

Native Electron remains non-green: the initial parallel run cancelled 27 tests
after app-start failures (three passed). Sequential execution passes 28 and fails
two existing scenarios. The parent branch reproduces the communications fixture
error (`unexpected event communications.send`) and, when running the full smoke
suite, the same Digest timeout (23 passed, one failed). No native registration
scenario was added. These baseline gaps remain separate follow-up work; they are
not counted as passing. Provider authentication/inference and operator data were
not exercised.

## Chat consumer migration

The subsequent chat migration now uses an explicit `runtime.chat.v1` binding and
`runtime.chat` / `runtime.chat.configure` actions. Enabled declarations are validated
on selection and every generation call. Registration alone still does not select a
model or prove inference. See [rollout and compatibility](production-runbook.md#chat-provider-migration).
