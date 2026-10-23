# Communication service boundary and migration audit

Source review: 2026-10-22, base `cca73e1`. Decision:
[ADR 0012](decisions/0012-communication-services.md). Step: 09.4.
The initial inventory is a source audit, not a live platform or delivery test.
The outbound policy extraction below has automated domain/orchestrator coverage.

## Current implementation and target roles

| Area | Current implementation | Responsibility and migration concern |
| --- | --- | --- |
| Discord inbound | [message listener](../dokja_interfaces/discord/messages/message_listener.ts), [interactions](../dokja_interfaces/discord/commands/interactions.ts), [bridge](../dokja_interfaces/discord/transport/bridge.ts) | SDK input, guild/channel/DM filters, normalized messages/commands, HTTP request to orchestration, reply delivery. Separate application eligibility from protocol handling. |
| Discord outbound/status | [delivery server](../dokja_interfaces/discord/delivery/delivery_server.ts), [client](../dokja_orch/internal/clients/discord_interface_client.go) | Internal `/deliver` and `/status`, bot/webhook delivery and attachments. Preserve contract and receipt semantics; classification alone does not unify this with the read service. |
| WhatsApp inbound/replies | [inbound normalization](../dokja_interfaces/whatsapp/runtime/inbound.ts), [message handler](../dokja_interfaces/whatsapp/messages/message_handler.ts), [bridge](../dokja_interfaces/whatsapp/transport/bridge.ts) | Normalizes linked-device messages, isolates sessions by chat/sender, filters eligibility, suppresses duplicates/echoes and dispatches fixed message events. Preserve identity and response correlation. |
| WhatsApp platform state | [runtime](../dokja_interfaces/whatsapp/runtime/whatsapp_runtime.ts), [history](../dokja_interfaces/whatsapp/history/store.ts) | Linking/auth state, reconnect and configured local history. Keep auth/history volumes, aliases and retention unchanged during relocation. |
| WhatsApp control/output | [listener](../dokja_interfaces/whatsapp/runtime/status_server.ts), [sender](../dokja_interfaces/whatsapp/messages/desk_sender.ts), [send limits](../dokja_interfaces/whatsapp/runtime/send_limit.ts) | Status, linking, history and bounded text delivery to named conversations. Retain transport caps; application authorization belongs to domain policy. |
| Communications domain | [domain.go](../dokja_domain/dokja_communications/domain.go) | Currently owns configured read eligibility and merges named conversations; it does not yet own all send/link policy. Extend through normalized ports. |
| Communications read service | [server.py](../dokja_services/dokja_communications/server.py) | Authenticated Discord channel/history reads; no message sends. Keep its current role while migration evaluates reuse. |
| Orchestration | [routes](../dokja_orch/internal/core/router.go), [System handler](../dokja_orch/internal/handlers/system_domain_handler.go), [Communications handler](../dokja_orch/internal/handlers/communications_domain_handler.go) | Reads route to Communications; `communications.send`, legacy `discord.send` and linking still pass through System. This split is an extraction target, not evidence that delivery policy is already consolidated. |
| Local lifecycle | [Runtime domain](../dokja_domain/dokja_runtime/runtime.go), [Compose adapter](../dokja_services/dokja_runtime/compose.go) | Starts selected modules and preserves shared dependencies. Preserve independent platform modules and saved routing switches; moving code must not start platforms implicitly. |

## Boundaries to preserve

- Operator interfaces request communication through orchestration. A desktop does
  not call a platform SDK, bot or read-service endpoint directly.
- Platform gateways publish normalized ingress and execute requested transport
  operations. They do not choose cognitive workflows, call models or resolve
  experiences themselves.
- Policy extraction must keep existing eligibility restrictions until domain and
  adapter checks have tested equivalence. Changing a folder must not broaden
  guild/channel, direct-message, phone/group or named-conversation access.
- Keep event/source identifiers, session keys, bridge payloads, delivery/link/read
  contracts, credentials, legacy actions and environment names compatible.
- Preserve platform authentication, WhatsApp auth/history volumes and retention.
  Protocol/session storage and conversation history are distinct from Experience.
- Preserve send limits, replay/echo protection, unavailable and disabled states,
  missing-configuration visibility and first-observed outcome semantics. Do not
  turn a delivery timeout into an automatic resend or fabricated failed receipt.

## Acceptance evidence required before code migration completes

| Area | Required evidence |
| --- | --- |
| Policy extraction | Same accepted/refused destinations for inbound replies, desktop/CLI sends and scheduled delivery; no permission expansion on adapter replacement |
| Ingress | Fixed/supported event types, isolated chat/user sessions, command parity, duplicate/echo suppression and disabled-route behavior |
| Egress | Delivery payload/response parity, platform refusal, rate limits, partial delivery and unknown outcome without automatic resend |
| State | Restart and rollback preserve synthetic linked sessions/history and volume identities; no live session reset during tests |
| Lifecycle | Independent Start/Stop, shared dependency preservation, unconfigured service visibility and readiness failures |
| Operator controls | Desktop/CLI/TUI confirmation and credential boundaries remain enforced; terminology does not add duplicate service rows |
| Compatibility | All compose variants, Dockerfiles, package scripts, imports, discovery paths and runbook agree with the relocated code |

Existing tests under the two gateway directories, Communications domain/service,
orchestrator clients/handlers and Runtime provide starting regression coverage.
They have not been rerun for this documentation-only audit. Teams is not present
in the current implementation and receives no implied migration or capability.

## Outbound policy extraction (step 09.4-b, partial)

`dokja_domain/dokja_communications/delivery.go` now owns typed send intent,
channel/conversation delivery ports, configured destination validation, stable
target deduplication, disabled-platform filtering, explicit desktop/TUI bypass
eligibility, shared safe-only decisions and named text-conversation restrictions.
Ports contain no SDK or HTTP types. Legacy handler port names remain aliases, so
existing client implementations continue to satisfy them.

System handlers normalize the existing event, supply the configured destinations
and routing-state snapshot, ask the domain for a plan, obtain any required safety
observation and execute eligible operations. Both manual and scheduled sends use
the domain safety decision. Current response/error shapes, partial delivery,
attachment fetching, receipts and timeout behavior remain unchanged. A source or
interface label is not authentication; existing ingress credentials and desktop
confirmation remain required and are not replaced by the domain origin check.

Checks passed: Communications domain `go test -race ./...` and `go vet ./...`;
orchestrator `go test -race ./...` and `go vet ./...`; final handler-focused race
and vet checks after sharing the safety decision. Tests cover target-set refusal
before effects, order/deduplication, disabled routing, the explicit bypass matrix,
text-only named conversations, safety gating, partial failure and timeout without
retry or marking sent. All transports are fakes; no live message was sent and no
account, session or configuration changed.

Named conversation routing enablement now passes through
`Communications.DeliveryPolicy.AuthorizeConversation` before an adapter list or
send. The handler retains the configured-adapter check and fixed unavailable
error. Linking authorization now passes through `Communications.LinkPolicy` before the
System handler calls a registered adapter. The domain accepts only a configured,
available platform and the three existing link operations; ingress credentials,
adapter session state and the reply contract are unchanged. Domain and handler
tests cover refusal before an adapter call. This does not make a source label an
authentication credential.

The outbound and link policy extraction completed without moving platform code
or introducing a new delivery guarantee. Gateway relocation is the next batch.

## Inbound eligibility migration (breaking, step 09.4-b)

The gateway bridge now carries normalized message or command kind, Discord DM
and mention facts, or WhatsApp chat kind, self-chat, sender-number and mention
facts. Communications owns the allow/deny policy; orchestration runs it before
the existing workflow and answers denied input silently. Old requests without
the fields are denied. Adapters retain echo, duplicate, stale-message and empty
text checks, while the bridge requires a separate gateway bearer credential so
untrusted callers cannot claim eligible sender facts. Compose passes the same
application allowlists to the orchestrator. This is a coordinated gateway and
orchestrator rollout, with no mixed-version compatibility promise.
