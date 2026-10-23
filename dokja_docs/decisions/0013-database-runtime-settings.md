# ADR 0013: shared database-backed runtime settings

Date: 2026-10-22. Status: accepted operational configuration direction (owner
request); first compatibility implementation delivered in step 11.3. Provider
inventory/discovery and persistent registrations are documented in the follow-up
guides; capability bindings remain pending.

## Responsibility and scope

Desktop, CLI and TUI use the same runtime requests. They present and confirm
changes; the bootstrap orchestrator routes them, the Runtime domain validates
values and lifecycle impact, and infrastructure persists and projects them into
Compose. Interfaces do not open this configuration snapshot directly or call
containers. Discord and WhatsApp remain communication services under ADR 0012.

Operational choices belong in the shared database. Credentials and bootstrap
information stay outside this settings API. The initial catalog covers existing
communication policies and capability-specific model names. It does not make an
OCR model, embedding model, classifier and conversational provider interchangeable.
The approved embedding profile is `bge-m3`, visible but fixed for this version.
Changing vector space requires a separately evaluated migration/reindex plan.

Future provider inventory should describe Codex, Claude Code and Ollama and their
available capabilities/models. Domain inference contracts and cognitive
orchestration retain their responsibilities from ADR 0011. A database preference
cannot authorize an action or transfer task-state ownership to a provider.

## Storage and compatibility

Reuse `dokja_store` and the existing SQLite `settings` table. One key,
`runtime.configuration.v1`, contains a versioned JSON object with stable setting
IDs, a revision and values. No table or `user_version` change is introduced, so
older profile readers can still open the database. Updates use compare-and-swap;
concurrent writers must reload after a conflict rather than lose an update.

The controller defaults to `<checkout>/.dokja/dokja.db`; its process environment
may select `DOKJA_DB_FILE`. Relative paths resolve against the checkout. Prefer an
absolute custom path and ensure other clients/container mounts use the same file.
The runtime does not load the database path from `.env`. Existing profile keys and
credentials in the shared database are not returned by the runtime settings API.

On the first settings read or Compose operation, import only catalogued variables
from `.env`, overlaid by managed variables exported to the daemon. Drop tokens,
webhook addresses and unrelated variables. Validate before inserting once;
invalid values or a different legacy embedding profile refuse import without
rewriting input or silently choosing a different model. Later `.env` and shell
edits do not override the imported managed values. A missing legacy file imports
empty values, preserving the catalog/Compose defaults.

Existing services still consume their environment contracts. Immediately before
a controller Compose command, the adapter projects database values into that
child's environment, replacing only managed keys. No generated env file or
service-to-database coupling is added. Empty values preserve Compose defaults;
unknown/invalid stored settings and database failures refuse the operation.
Webhook channel labels are still read from the legacy file, without returning
addresses. Legacy-file read errors therefore also block settings/projection.

Saving retains existing lifecycle behavior: recreate running readers, retain
stale markers for stopped readers or failed recreation, and retry on explicit
Start. It does not start stopped modules merely to apply a preference.

## Rollout, rollback and limits

Back up the shared database and legacy configuration before rollout. Rebuild
interfaces/controller and stop the old daemon once with SIGTERM before reopening
an interface; a running daemon does not replace itself when its binary changes.
No operator configuration or live containers were changed during verification.

Manual Compose commands, production stacks and older daemon builds still read
legacy environment values. They do not automatically see later database edits.
For rollback, the old build can open the unchanged schema but ignores this new
key; reconcile desired operational values into its legacy configuration explicitly
before recreating containers. Keep the database backup, credentials and session
volumes private. This lot does not provide an export command.

The database commit and ownership-journal update are separate operations. A crash
between saving settings and marking readers stale can leave a reader on the old
configuration; existing stale markers handle recreation failures, but do not close
that crash window. Durable configuration-application recovery remains pending.
This is a local lite-stack configuration migration, not production rollout or a
complete provider/model management system.

## Verification

Reproducible tests use disposable databases/files and simulated engines. They
cover import precedence and import-once behavior, persistence after reopen,
profile/schema compatibility, concurrent revision conflicts, invalid/corrupt
input, unchanged legacy content/mode, credential omission, child-process
projection and refusal to launch without a valid database. A bootstrap-router
test writes from CLI, reopens persistence and reads the same value from all three
interface sources. Interface tests verify the fixed embedding profile and refuse
its edit. See step 11.3 for the executed suites and remaining work.

The follow-up [registration batch](../provider-registrations.md) uses an
independent `runtime.providers.v1` snapshot in the same table. Operator-declared
models and discovery eligibility remain separate from observed availability and
inference results; no legacy inference consumer changes its binding yet.
