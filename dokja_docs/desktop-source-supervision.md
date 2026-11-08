# Desktop source preparation and steering

> **Removed on 2026-11-08.** Desktop source supervision (the Library Apply view, the
> source supervisor, the `knowledge.application.supervision.*` requests and
> `DOKJA_SOURCE_SUPERVISION_TOKEN`) was removed because no flow consumed it. This page
> is kept as history. See the [Apply review](knowledge-apply-review.md).

The Library's **Apply** view connects to the bounded source supervisor through
normal orchestrator requests. This release provides an explicitly configured
owner-local interface. It validates sources for a goal; it does not execute a
method or establish cross-document understanding.

## Operator flow

1. Choose a goal, constraints and one to eight Library documents. **Check selected
   versions** invokes the existing read-only preparation workflow and reviews its
   actual source pins. No new supervised application exists yet.
2. **Start supervised preparation** submits those exact versions to the source
   supervisor. The view shows the current plan, deadline, cumulative read attempts
   and retained native receipts. Closing the view does not cancel the work.
3. Pause and wait for pending receipts before **Correct direction**. Supply reasons,
   edit the goal/constraints/document selection, and check the candidate versions.
   These checks run through the paused supervisor and consume its existing read,
   request/source-byte and time budgets. They produce candidate evidence, not
   execution receipts or an accepted plan.
4. **Apply correction** advances the plan/control revisions. A paused task remains
   paused until **Resume**. **Cancel preparation** also works during a candidate
   check. Unknown native results or journal writes block further progression.

Old receipts retain their original plan. Polls with an older sequence cannot
replace newer control state. A lost start/control reply retains the original
command for explicit retry; retries keep the same operation identity. Refresh
can discover an already accepted start or control without sending it again.

The browser development transport supports version-check previews but reports
supervision unavailable. The built Electron app uses the real authenticated path;
there is no simulated success fallback in production.

## Admission and transport

New additive orchestrator request types:

| Type | Request |
| --- | --- |
| `knowledge.application.supervision.status` | `{}` returns host identity, remaining admission quota and this operator's recent application/operation references; `{application_ref}` returns its snapshot |
| `knowledge.application.supervision.start` | `{host_ref, operation_ref, application}`; application is the pinned `knowledge.application.context.v1` request |
| `knowledge.application.supervision.preview` | `{application_ref, expected_revision, application}`; candidate selection may be unpinned; requires a settled, paused application |
| `knowledge.application.supervision.control` | `{application_ref, operation_ref, expected_revision, action, reason, application?}`; only redirect replaces the application |

All use the existing event envelope and ZeroMQ request ingress. The Desktop main
process allowlists/builds payloads and projects responses. Start, candidate preview
and control require a shell-observed recent input gesture; no extra confirmation
window is required for these read-only operations. Renderer claims of identity,
scope, credentials or success do not become authority.

A separate `DOKJA_SOURCE_SUPERVISION_TOKEN` authenticates the configured Desktop
operator. It is not the Discord, gateway or communications credential. The main
process reads only that key from the selected env file, with explicit process-env
precedence, and attaches it only to supervision envelopes. The renderer never
receives it. The client requires a loopback/IPC endpoint or a secure local tunnel.

The orchestrator checks the credential and Desktop source before routing, then
strips the credential from every event before context building, dispatch and
verbose responses. Native context carries a fixed `desktop:operator` principal;
user IDs and `is_owner` cannot mint it. This is an operator credential model,
not multi-user sign-in or a Discord-owner inference.

The host admits at most 16 source applications per process and retains their
operation fences instead of evicting them. Start replay checks both operation
identity and input digest. Its `host_ref` binds it to one orchestrator instance:
a retry cannot silently resurrect a lost start after a restart. The native
supervisor retains its aggregate limits and source/record permission separation.
Authenticated status and cancel/control remain available when Knowledge is
switched off; new source reads still honor the switch.

## Explicit configuration

| Setting | Consumer | Purpose |
| --- | --- | --- |
| `DOKJA_SOURCE_SUPERVISION_TOKEN` | Orchestrator and Desktop main | Matching dedicated random operator credential, 32–512 characters; placeholders are refused |
| `DOKJA_SOURCE_SUPERVISION_POSTGRES_DSN` | Orchestrator and explicit initializer only | Existing PostgreSQL database and its operator-selected TLS/connection policy |

Both are unset by default. Enabling just one orchestrator setting is a
configuration error. Invalid configuration, an unavailable database or a failed
namespace check disables this optional host and logs a sanitized error while the
rest of the orchestrator starts. Supervision requests are refused without a
journal; no unjournaled fallback is registered. Restore the database or fix the
configuration/schema, then restart the orchestrator to enable the host (there is
no automatic startup retry). No existing SQLite or Knowledge data is migrated. The
PostgreSQL journal uses its existing isolated namespace; startup only opens an
initialized version and never creates tables implicitly. Errors do not expose
credentials or native DSN diagnostics.

After choosing the stack's private env configuration, build the orchestrator
image. Initialize its namespace once, using the same Compose network and DSN.
For dev/lite, from the repository root:

```sh
docker compose -f docker-compose.lite.yml -f docker-compose.postgres.yml \
  run --rm --no-deps --entrypoint go dokja-orchestrator \
  run ./cmd/source-journal-init
```

For production, its image includes the explicit initializer:

```sh
docker compose --env-file .env.prod -f docker-compose.prod.yml \
  -f docker-compose.postgres.yml run --rm --no-deps \
  --entrypoint /app/source-journal-init dokja-orchestrator
```

Select the stack/env file that actually owns the database. If running the
initializer directly from the Go module with `go run ./cmd/source-journal-init`,
the DSN must be reachable from that host. The command initializes only the
application journal namespace and reports sanitized success/failure.

Restart/recreate the configured orchestrator after initialization. Launch Desktop
with the same credential file, for example from its directory:

```sh
pnpm start --env-file=../../.env.prod
```

No deployment, private env edit, operator database initialization or owner-data
migration is performed by this implementation. These commands describe the
explicit operator rollout after review.

## Session and persistence boundary

Native attempts/receipts persist in PostgreSQL. Controls, pending-work state,
command replay and prepared context remain in memory. Reopening the view or
Desktop can rediscover an application while the same orchestrator process lives.
An orchestrator restart invalidates the old host identity; the UI reports loss
and requires an explicit new preparation. It does not reconstruct controls,
resume unknown work or infer success from a missing response.

Durable supervisor recovery, multi-process coordination, unknown-write
reconciliation, backup/restore and coordinated deletion remain pending. This
owner-local opt-in surface does not enable a general public or multi-user API.
The global observer, semantic method execution and step `08.3` remain separate.

## Verification

Go tests exercise credential admission/redaction, scoped lookup, changed-command
conflicts, process fences, quotas, source switches and cumulative candidate-check
bounds. The disposable PostgreSQL suite exercises Desktop RPC pause/replay,
budgeted candidate checks and original native receipt storage.

Desktop tests cover payload/projection/credential boundaries, real-input gating,
strict native observations, source pin preservation, stale polls and exact-command
retry. The built Electron test drives the actual preload/IPC/main/socket path,
pauses, corrects, resumes and cancels; responsive checks cover 800 and 1536 pixels.
Backend transports and authentication in those fixtures are synthetic. No live
owner session, provider/method execution or production rollout is claimed.
