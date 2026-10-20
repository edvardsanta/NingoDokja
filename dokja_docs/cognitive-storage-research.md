# Research: storage for the cognitive graph

Date: 2026-10-19. Status: researched recommendation, not an adopted engine or
migration. Tracker: [steps.json](../steps.json), preparatory research for step 10.
Related: [ADR 0003](./decisions/0003-state-ownership.md),
[ADR 0005](./decisions/0005-knowledge-experience-content.md),
[ADR 0008](./decisions/0008-consolidation-by-kind-of-knowledge.md),
[ADR 0009 proposal](./decisions/0009-cognitive-storage-evaluation.md).

## Recommendation

Treat the cognitive graph as an explicit domain model. Compare **PostgreSQL with
pgvector** and **Neo4j** against the current SQLite baseline before choosing its
storage engine. PostgreSQL is the first candidate for an authoritative store
combining versioned records, relationships and vector retrieval. Neo4j is the
first alternative when bounded relationship traversal proves central or markedly
simpler. Neither is a measured winner yet.

Evaluate **Qdrant** as a replaceable vector index if filtered vector search is the
bottleneck. Keep **ArangoDB** as a multi-model alternative rather than requiring
another implementation in the first comparison. Do not introduce all of these
systems together. SQLite remains an operational baseline and a possible local
profile; it is not the presumed final architecture.

These recommendations are engineering inferences from the access patterns below
and official product documentation [R1–R13]. No vendor performance claims were
used. There are no database benchmarks, production measurements or capacity
results in this report. Hardware, corpus size, write concurrency, retention and
operating budget still need measurement or owner input.

## What the repository implements today

| State | Actual mechanism | Missing target behavior |
| --- | --- | --- |
| Knowledge documents | [Python store](../dokja_services/dokja_knowledge/store.py): SQLite, chunks, FTS5, model-tagged vectors; schema 2 adds metadata | Versioned claims/methods and typed evidence relationships are not implemented by the metadata JSON |
| Semantic retrieval | [Knowledge service](../dokja_services/dokja_knowledge/service.py): cached NumPy matrix, exact dot products and keyword fusion | No approximate vector index; changing the database file alone does not change this scan |
| Experience | [Memory store](../dokja_services/dokja_memory/memory/store.go): own SQLite file, immutable episode references, first-verdict resolution, prediction/baseline at decision time | No complete knowledge-version/procedure/evidence graph or actor/channel scope model |
| Episodic recall | Memory `Nearest` scans eligible model-matching vectors and keeps top candidates | No ANN index; a database migration must preserve eligibility and scoring semantics |
| Coordination | [Task workspace](../dokja_orch/internal/core/task.go) and runner hold evidence, proposals and receipts in memory | No durable recovery ledger; unscoped evidence is retained and counted, not isolated by storage |
| Feedback graph | [Reference experiment](../dokja_orch/internal/experiments/cognitiveloop/loop.go) models versions, scope, snapshots and hypothetical predictions | Experimental types are not a persisted production graph |
| Settings | [Shared store](../dokja_store/store.go) holds existing local state | No reason to migrate credentials/settings merely because cognitive retrieval changes |

A database graph, a vector index, a reasoning loop and a software dependency graph
serve different purposes. The HNSW graph of a vector index is not the domain's
knowledge graph. Neuroscience motivates memory and coordination roles; it does
not select an engine, establish a capacity requirement or validate a prediction.

## Queries that should drive the decision

These are proposed target workloads, not claims about existing public endpoints.

| ID | Question or operation | Required semantics |
| --- | --- | --- |
| W1 | Find evidence about an entity/topic/date in the current allowed context | Combine lexical/vector candidates with form/content, source and scope filters; cite retained source versions |
| W2 | Which sources support or contradict this claim, and what depends on it? | Traverse typed, directed relationships with depth, result and time bounds; retain conflicting assertions |
| W3 | Which version of a general method informed this prediction? | Resolve exact immutable versions and the evidence available when the decision was made |
| W4 | How did we apply that method in comparable situations? | Follow method → procedure → episode → observed outcome; distinguish applicability, similarity and success probability |
| W5 | What did we know when we acted, and what changed afterward? | Separate event/valid time from recorded time; retain version references and corrections |
| W6 | Resolve feedback twice or retry recording after a timeout | Preserve stable references, frozen predictions and first-verdict behavior under retries |
| W7 | Delete/correct a source or revoke its visibility | Remove/restrict derived retrieval paths, vectors and cached evidence; expose projection lag |
| W8 | Reclassify an unchanged document with a new classifier | Version/cache classification independently of chunk embeddings; preserve explicit user assertions |
| W9 | Restart during a workflow or index update | State explicitly which acceptance, receipts and projections survive; replay without inventing an observed outcome |

W1, W6 and current recall already have consumers. W2–W5 describe the intended
cognitive graph; W7–W9 require broader contracts and step 10 recovery work. The
initial 09.2 metadata persistence alone does not supply them.

## Logical model before physical schema

Candidate entities are `DocumentVersion`, `Claim`, `Entity`, `MethodVersion`,
`ProcedureVersion`, `Episode`, `Prediction`, `Observation` and `Attempt`.
Introduce an entity only with a concrete consumer. A document's mention of an
entity is not proof of an extracted claim; a proposed extraction needs its source
span, producer version and validation status.

Candidate relationships include `supports`, `contradicts`, `derived_from`,
`applies_method`, `used_procedure`, `predicted_for` and `observed_outcome`.
They are proposals to test, not a requirement to fill every edge. Each assertion
needs a stable ID, relation type, endpoint IDs/versions, provenance, visibility,
recorded time and applicable valid time. Corrections supersede assertions rather
than silently rewriting what an earlier task consumed.

Keep four distinctions explicit:

- A prediction describes a possible outcome. Only an observed result can resolve
  an episode. Similarity scores are neither truth nor calibrated probabilities.
- A news date anchor describes the news context. It does not imply expiry or
  equal the time it was ingested; old news remains retrievable under ADR 0008.
- User-selected form/content and classifier proposals have separate provenance.
  A classifier cache key should include text identity, classifier/provider version
  and relevant prompt/configuration version; user overrides are not cached inference.
- Snapshot reuse means an explicit set of consumed owner versions. Separate
  databases do not magically provide one atomic snapshot; even one database
  requires coordinated transaction boundaries. PostgreSQL isolation options [R5]
  are mechanisms to evaluate, not an implementation of ADR 0005 by themselves.

## Engine comparison

Capabilities below come from primary sources; the fit and operational judgments
are our assessment. None establishes relative latency or total cost.

| Candidate | Documented mechanism | Fit for Dokja | What would justify choosing it |
| --- | --- | --- | --- |
| SQLite baseline | Recursive CTE graph queries [R2]; WAL permits concurrent readers but one writer at a time [R1] | Local deployment and an inexpensive correctness baseline; current vector scans remain application work | Meets measured workloads and recovery requirements in the intended local profile |
| PostgreSQL + pgvector | Recursive SQL with cycle handling [R3], JSONB [R4], transaction isolation [R5], row policies [R6], exact/approximate vector search [R7] | First consolidated-store candidate; domain edges still need an explicit relational model | Required queries remain understandable, scoped retrieval meets quality targets, and concurrent writes/recovery meet budgets |
| Neo4j | Property-graph path patterns [R8], database transactions [R9] and vector indexes [R10] | Strong candidate for relationship-heavy evidence and provenance queries; investigate as an alternative authority before adding it as a second store | Demonstrates better bounded traversal or substantial query simplification on the same fixtures without losing temporal, feedback or isolation semantics |
| Qdrant alongside an authority | Point/payload filters [R11] and hybrid query composition [R12] | Candidate derived retrieval index; this study does not propose it as the authority for episode/claim relationships | Vector quality, filtering or scale fails the consolidated-store budget, and benefits exceed projection/recovery cost |
| ArangoDB alternative | AQL graph traversal with pruning [R13] | Multi-model alternative if the first pair leaves a concrete graph/document gap | A focused follow-up demonstrates benefit; deployment edition, version and licensing must be checked before adoption |

PostgreSQL's row policies can enforce record restrictions, but owners and
privileged roles can bypass them [R6]. Domain authorization remains required;
query fixtures must use the intended application role. The current unscoped
stores cannot be declared isolated merely by adding a new engine or stamping
legacy rows with the active task's scope.

Approximate search introduces another correctness dimension. pgvector documents
that filtering after an approximate index scan can underfill results; iterative
scans help but remain bounded [R7]. Neo4j documents approximate results and
visibility limitations for vector updates in the same transaction [R10]. Test
selective scopes and kinds, recent writes, deleted data and unavailable indexes.
A returned top-k set must still meet the domain's relevance and eligibility rules.
Qdrant payload indexes and filters [R11] are retrieval mechanisms, not grants of
access. Hydrate final evidence through its authoritative owner and recheck it.

## Ownership and deployment proposal

Preserve `Interface → Event/Request → Orchestrator → Domain → Service`. No domain
queries another faculty's tables, and no Reading plugin receives database handles.
Knowledge interprets knowledge/method relationships; Experience interprets
contextual procedures/outcomes; Prediction interprets estimates; orchestration
owns the bounded loop and consumed-version trace.

A shared PostgreSQL cluster could host separately owned schemas and credentials.
That shares infrastructure, not business ownership or a permission to join every
faculty's private tables. Cross-faculty references name owner, ID and version and
are resolved through ports sequenced by the orchestrator. Repository hosting
still requires resolving ADR 0003's deferred stateless-service rule before moving
owners or introducing durable coordination state.

If a second engine is justified, nominate one authority per record type. Treat
graph/vector copies as rebuildable projections. Reliable projection updates need
an owner transaction plus a durable change/outbox record, idempotent consumers,
versioned upserts, deletions/tombstones and a reported lag watermark. This is a
proposed step 10 design, not a claim that current fire-and-forget events provide
those guarantees. Do not synchronously dual-write two stores and assume atomicity.

## Reproducible comparison plan

Run a correctness-first spike with the same generated corpus and logical
queries on SQLite, PostgreSQL+pgvector and Neo4j. Freeze server/extension versions,
container image digests, schema/index settings, seed, hardware and memory limits.
Use no owner database or private messages. Keep actual numbers in a separate
results artifact; do not replace missing results with vendor benchmarks.

1. Define repository ports and golden results for W1–W9. Fixtures include cycles,
   contradictions, a superseded method, a prediction without an outcome, duplicate
   feedback, expired unresolved episodes, two scopes with near-identical vectors,
   deletion, a classifier change and a partial projection. No adapter selects policy.
2. Use deterministic precomputed vectors for correctness and exact-search gold
   results. Model quality is a separate evaluation. Compare approximate recall
   only against eligible exact neighbors; record recall@10 and result underfill.
3. Proposed scale ladder: 10k/100k/1m chunks, 1k/10k/100k episodes and
   10k/100k/1m edges. These are stress fixtures, not estimates of the user's data.
   Include a high-degree entity, skewed scopes and bounded 1/3/6-hop traversals.
4. Measure warm and cold p50/p95/p99, throughput, memory/RSS, index build time,
   disk size, projection lag and restart/rebuild time. Exercise 1/4/16 concurrent
   clients with isolated reads, writes and mixed ingestion/feedback traffic.
5. Require all golden correctness cases, no foreign-scope evidence, stable
   first-verdict semantics, unchanged decision-time probabilities and bounded
   traversal. Provisional targets for the spike: retrieval p95 ≤250 ms excluding
   model/network generation, recall@10 ≥0.95 where at least ten eligible rows
   exist, and no lost acknowledged writes under the configured durability mode.
   These targets need owner/workload validation; they are not production SLOs.
6. Kill/restart during ingest, feedback and projection updates. Measure visible
   inconsistency, retry duplicates and recoverable loss; restore a backup and
   reconcile IDs, versions, edges and vectors. An external send is not made
   exactly-once by a database transaction.
7. Choose from measured results plus query/adapter complexity and operational
   burden. Prefer one adequate authority; add a projection only for a demonstrated
   gap. Record the accepted engine, versions, topology and budgets in a follow-up
   decision before changing production configuration.

If multi-hop evidence is not yet consumed, report that absence. Do not benchmark
an invented graph workload and claim it justifies an immediate production graph
service. It can still identify the architecture's next missing contract.

## Migration and rollback gate

Before any engine migration: settle owner boundaries and legacy visibility,
export stable IDs, freeze embeddings/model identities, reconcile source and
classification versions, and test backups. Add a repository adapter without
changing the public envelope. Backfill into an isolated target, compare reads,
reconcile updates through a durable mechanism, then cut over one owner at a time.

An exact rollback requires either a writer freeze or a verified reverse/replay
path for writes accepted after cutover. An old SQLite backup alone would lose
those writes. Keep embedding comparison tolerances and duplicate-verdict behavior
explicit. Update every affected compose file and production runbook alongside
any eventual configuration, port, volume or service change. None is changed by
this research; no target schema, service or migration is deployed here.

## Sources and limits

Primary documentation consulted on 2026-10-19. Repository policy forbids external
URLs in committed documents; the official product, manual/version and exact
section below identify the sources. This is a documentation review, not a
version/edition compatibility certification. Pin candidate builds in the spike.

| ID | Primary source and section |
| --- | --- |
| R1 | SQLite official documentation, “Write-Ahead Logging”, concurrency |
| R2 | SQLite official documentation, “The WITH Clause”, “Queries Against A Graph” |
| R3 | PostgreSQL 18 manual, 7.8 “WITH Queries”, recursive queries and cycle detection |
| R4 | PostgreSQL 18 manual, 8.14 “JSON Types” |
| R5 | PostgreSQL 18 manual, 13.2 “Transaction Isolation” |
| R6 | PostgreSQL 18 manual, 5.9 “Row Security Policies” |
| R7 | pgvector upstream README, “Indexing”, “Filtering”, “Iterative Index Scans”; version references must be pinned when testing |
| R8 | Neo4j current Cypher Manual, “Variable-length paths” |
| R9 | Neo4j current Operations Manual, “Database transactions” |
| R10 | Neo4j current Cypher Manual, “Vector indexes”, “Limitations and known issues” |
| R11 | Qdrant official documentation, “Filtering”, payload indexes and conditions |
| R12 | Qdrant official documentation, “Hybrid Queries” |
| R13 | ArangoDB 3.12 manual, AQL “Graph traversals”, pruning |
