# ADR 0012: platform gateways are communication services

Date: 2026-10-22. Status: accepted responsibility classification (owner direction).
Implementation: source audit and initial outbound policy extraction complete;
inbound/linking policy and package migration pending in step 09.4. This refines the communication boundary without accepting
all other proposals in [ADR 0010](0010-four-concepts.md).

## Decision

Desktop, CLI and TUI are Dokja's operator interfaces. Discord and WhatsApp are
external communication platforms; their bots/gateways are communication-service
adapters with inbound and outbound responsibilities. They are not separate
product concepts or cognitive components. A future platform follows the same
boundary; Teams has no implementation in the inspected checkout.

This classification does not prescribe one communication process, a new broker,
a common SDK or additional containers. Platform gateways may keep independent
lifecycles and dependencies. The existing read-only Communications service does
not become a universal messaging hub merely because the gateways are reclassified.

The invariant remains `Interface -> Event/Request -> Orchestrator -> Domain -> Service`.
An inbound gateway contains an ingress-adapter role: normalize platform input,
construct the existing event/request and submit it to orchestration. Moving its
package to infrastructure does not authorize it to invoke domains or other
application services directly. The orchestrator still owns workflow transitions.

| Responsibility | Target owner |
| --- | --- |
| Operator input, presentation, confirmation | Desktop, CLI, TUI |
| SDK/WebSocket/HTTP integration, attachments, connection, linking and protocol-specific identity translation | Communication infrastructure adapters |
| Conversation/destination eligibility, application send authorization and delivery constraints | Communications domain and applicable Rules policies |
| Workflow choice, routing, sequencing and reply correlation | Orchestrator |
| Generation of reply content | Domain-controlled generation capability and inference providers |
| Observed delivery outcomes and reusable episodes | Domain-defined receipts and Memory, through orchestration |

Transport authentication, protocol validation, maximum body sizes, SDK backoff,
echo suppression and provider rate-limit handling remain in adapters. Application
policy must not be duplicated independently for desktop sends, bot replies and
scheduled delivery. Additional adapter restrictions may narrow domain permission;
they cannot expand it. The migration must identify these two kinds of checks
before moving them.

## Input and output

Inbound:

```text
Platform -> gateway ingress adapter -> event/request -> orchestrator
         -> domain -> service -> reply -> gateway -> platform
```

Operator-initiated or scheduled output:

```text
Desktop/CLI/TUI or scheduler -> event/request -> orchestrator
                           -> communications policy -> gateway delivery -> platform
```

The first path preserves synchronous replies where currently implemented; a
reply is not a second inbound message. Model calls, memory writes, recipient
selection and retries with possible external effects stay under explicit
workflow/domain control. A timeout is not proof that a message was not delivered.
A platform acknowledgement is an observed transport result, not proof that a
person read or accepted the content.

## State and compatibility

Gateway session/authentication files, SDK connection state, bounded deduplication
caches and explicitly configured platform history are transport/platform state.
They do not become cognitive memory through a package move. Preserve their
current ownership and persistence until a separate migration specifies otherwise.
Linked-device sessions and history volumes must survive recreation and rollback.
No new durable delivery guarantee is claimed; durable attempts and receipts
remain part of step 10.

Keep the existing envelope, HTTP bridge input, source/interface identifiers,
session keys and public actions. In particular, `message.created`,
`communications.*`, `discord.send`, runtime module IDs and existing environment
names containing `INTERFACE` remain compatible. Directory classification is not
a reason to change a wire identifier. Do not expose linking credentials or raw
platform identities to operator renderers as part of the rename.

## Migration batches and verification

1. **Audit and decision:** record current inbound/outbound paths, policy and state
   owners. This record and the [source map](../communication-boundaries.md) complete
   that batch; they do not move runtime code.
2. **Policy/ports:** define normalized communication ports and extract application
   eligibility/authorization from System handlers and gateway filters, preserving
   existing stricter transport checks. Reuse the Communications domain rather than
   introducing another domain per platform. Add behavior parity tests first.
3. **Gateway relocation:** move the audited adapters beneath `dokja_services/` in a
   dedicated compatibility PR, update every affected build context, imports,
   package command, compose stack, runtime catalog and runbook together. Keep SDK
   dependencies and platform lifecycles independent. Preserve volume identities.
4. **Operator terminology:** present communication services/channels consistently
   in desktop, CLI, TUI and README. Preserve actual status, visibility before
   setup, Start/Stop and routing behavior; avoid duplicate module rows.
5. **Integration/recovery:** verify inbound chat, commands, replies, operator and
   scheduled sends, history/linking, disabled routing, unavailable gateways,
   deduplication/echo handling and uncertain delivery outcomes. Validate restart
   and rollback with synthetic sessions before any operator-data rollout.

No physical relocation, new action, endpoint, environment setting or deployment
change is introduced by this ADR. Canonical agent instructions continue to
identify actual directories until implementation moves them. Test fixtures and
local integration checks must precede any production migration.
