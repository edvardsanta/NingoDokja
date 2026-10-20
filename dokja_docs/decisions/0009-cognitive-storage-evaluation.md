# ADR 0009: evaluate storage against the cognitive graph's queries

Date: 2026-10-19. Status: proposed; no engine adoption or migration authorized by
this record. Research: [storage comparison](../cognitive-storage-research.md).
Refines the evaluation plan for [ADR 0003](./0003-state-ownership.md),
[ADR 0005](./0005-knowledge-experience-content.md) and
[ADR 0008](./0008-consolidation-by-kind-of-knowledge.md).

## Context

The owner challenged continued reliance on SQLite while the cognitive
architecture becomes a Knowledge–Prediction–Experience graph. Step 09.2 adds
structured document metadata, not the complete graph. Preserving the current
stores during incremental work must not become an implicit long-term choice.

The current Knowledge and Experience mechanisms use application-side exact vector
scans and lack a full persisted model of versioned evidence relationships.
Changing engines without defining that model would preserve the semantic gap.

## Proposal

1. Define the smallest consumer-backed graph contracts and owner boundaries.
2. Run the research protocol's W1–W9 correctness/operations cases against SQLite,
   PostgreSQL+pgvector and Neo4j. PostgreSQL is the first consolidated authority
   candidate; Neo4j is the first graph-focused alternative, not a presumed extra
   service. This order is a hypothesis, not a performance result.
3. Evaluate Qdrant only if vector retrieval has a measured gap. Consider ArangoDB
   in a focused follow-up if the first comparison leaves a multi-model need.
4. Select the target from evidence, query complexity and operating requirements.
   Prefer one adequate authority per record type. Introduce projections only
   with explicit synchronization, deletion, lag and recovery contracts.
5. Resolve the repository-hosting rule deferred by ADR 0003 before moving state
   ownership or adding durable coordination infrastructure.

The research supplies provisional benchmark targets, not accepted production
budgets. It does not claim PostgreSQL or Neo4j is faster than SQLite for Dokja.

## Decision gates

- Queries and fixtures exercise real intended consumers; unimplemented graph
  consumers are identified rather than represented as current traffic.
- Every candidate preserves observed versus predicted outcomes, method/evidence
  versions, temporal meaning, explicit user classification and feedback semantics.
- Scope and legacy unscoped data have an explicit treatment; no engine is assumed
  to repair the current missing scope model automatically.
- Engine/extension versions, relevant editions, hardware, correctness, retrieval
  quality, resource use, concurrency and restore behavior are recorded.
- Any migration names owners, public-contract impact, backup, reconciliation,
  cutover and rollback, including writes accepted after cutover.

## Compatibility

Documentation and tracker only. No envelope, route, HTTP/ZeroMQ contract,
command, SQLite schema, dependency, environment variable or compose change.
Existing stores and the step 09.2 classifier/cache work remain in place. The
orchestrator continues to supervise the cognitive loop; graph queries do not
become workflow execution. No changes to the canonical architecture rules are
made by this proposal.

## Next action

Implement an isolated, synthetic-data storage spike after agreeing the graph's
first consumer and the scope/durability requirements. Use the research's exact
correctness cases and provisional scale ladder. Record results before adopting
an engine; step 10 implementation remains pending.
