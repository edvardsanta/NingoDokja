# ADR 0003: preserve state owners during the volatile prototype

Date: 2026-10-18. Status: accepted for prototype scope.
Broader service-responsibility rule: deferred, not amended by this record.
Implementation: pending.
Related: [decision index](./README.md), [workspace](./0001-task-workspace.md),
[prediction](./0004-shadow-prediction.md).

## Context

The canonical instructions describe services as stateless integrations. The
current implementation also includes service-owned SQLite stores and an in-memory
collection cache. The [capability map](../capability-map.md) records these facts.
Research terminology alone cannot authorize new business policy in services or
a new persistence model.

## Decision

Keep existing stores and their current access contracts while introducing only
volatile task coordination. New workspace state lives inside orchestration and
does not create another service, database, volume, or cross-service shared file.
Business meaning and eligibility remain domain responsibilities. Provider
workers added by the prototype do not own conversation state or decide policy.

| State | Current mechanism | Prototype treatment |
| --- | --- | --- |
| Task workspace | No general typed workspace yet | New volatile orchestration-owned attempt state; discarded after termination |
| Conversation history | Handler/session-manager memory | Retain existing lifecycle; no move to plugin hosts |
| Semantic documents and indexes | Knowledge SQLite store | Continue through existing domain/service ports |
| Experiences and outcomes | Memory SQLite store | Continue current recording/resolution contracts; do not change schema |
| Feed observations | Host memory, last successful result per plugin | Keep rebuildable cache and explicit timestamps |
| Provider profiles/settings | Shared SQLite accessed by local tools and Chat AI | Preserve existing local credential editing and masked orchestrator reads |
| Scheduler configuration | Orchestrator-owned state read by scheduler | Preserve current control and timer semantics |
| Recipes | Existing registered workflow definitions | Versioned source/configuration, with no automatic promotion from recalled text |

Repositories and caches are mechanisms, while domains own interpretation and
policy. That distinction helps describe the current code; it does not silently
replace the literal stateless-service rule. This record authorizes no expansion
of the existing persistence exceptions.

## Deferred policy question

Before adding durable workspace/receipt storage or relocating repositories,
record and agree a follow-up decision resolving whether repository adapters may
be hosted by services under the canonical architecture rules. Options include
explicitly documenting policy-free repository hosts, or relocating ownership
behind a different adapter boundary. A resulting change to the canonical rule
must be explicit and accompany the implementation.

This does not block the volatile workspace, plugin adapters, source baseline, or
synthetic experiments. It does block claiming durable recovery, adding a shared
workspace database, or moving domain session state into services on the basis of
this prototype decision. Step `10` includes this decision before persistence work.

## Compatibility and verification

Existing schema versions, environment variables, paths, secrets handling,
network contracts, and compose volumes remain unchanged for the first experiment.
No database copying or automatic data migration is part of steps `02` through
`05`. Any later write-path change must identify its owner, schema compatibility,
backup/rollback procedure, and affected production documentation.

Verify that fresh workspaces do not reuse previous task state, termination frees
bounded state, plugin calls do not gain repository handles, and existing Memory
and Knowledge callers retain their contracts. Record crash behavior honestly:
volatile state can disappear while an external operation may already have run.
Retrying such an operation is not safe merely because a workspace was lost.

Rollback removes the prototype composition; existing stores and schemas require
no conversion. Deletion, retention, and feedback corrections continue to follow
the existing domain contracts rather than a newly invented consolidation policy.
