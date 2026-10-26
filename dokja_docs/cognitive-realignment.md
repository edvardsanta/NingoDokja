# Cognitive realignment: understand and apply knowledge across documents

Date: 2026-10-26. The owner requested returning to earlier architecture steps
because the application exposes information collections more clearly than
cognitive behavior. The owner selected understanding and applying knowledge
across documents as the first proof case, and clarified that Feeds is the
external integration side of Knowledge.

Step `08` is reopened as the active evaluation gate. Revisit the assumptions
from `02` and behavior criteria from `03`, retaining their historical records.
Existing infrastructure, contracts and completed migration batches remain in
place. This record changes the roadmap, not application code, owner data,
deployment or the canonical layer rules.

## Functional boundary

Feeds covers the external acquisition/integration wing of Knowledge: source
connectors, polling, supplied files, format extraction and perception adapters.
RSS is one adapter. A PDF and an item discovered through a subscription can
contribute evidence to the same internal model. Different acquisition paths do
not prescribe different reasoning or consolidation methods.

Acquisition produces observations with source, version/time and reader
provenance. Knowledge policy decides what these observations support and how
they may be retained or used. Fetching or classifying something does not
establish truth, applicability or successful assimilation.

This conceptual boundary preserves `Interface -> Event/Request -> Orchestrator
-> Domain -> Service`. Orchestration sequences registered domain steps and
their adapters. Feeds does not call Knowledge or Memory, select reasoning
strategies or promote its observations to authoritative knowledge. Communication
gateways retain their transport role. No route/provider relocation or facade
removal is introduced by this review.

## Established behavior and remaining gaps

| Area | Established behavior | Remaining cognitive gap |
| --- | --- | --- |
| External integration | Feeds acquisition/perception ports; `knowledge.read` sequences read, ingest and classify | Material is not generally assimilated into explicit concepts, claims or methods |
| Knowledge | Documents/chunks, provenance, classification, retrieval and the PostgreSQL adapter | No complete internal model of relationships, contradiction, applicability and uncertainty |
| Experience | Narrow hashtag outcomes, predictions, feedback and confirmed media receipt policy | No general application workflow reliably reuses an observed outcome to justify a better procedure |
| Working context | Bounded task workspace with evidence, proposals and outcomes | General evidence-driven continuation is not established in ordinary document use |
| Experiments | Dependent retrieval and causal controls in isolated fixtures | One successful model-assisted pair out of three final ordinary pairs does not establish reliable application learning |
| Consolidation | Structured metadata and explicit classification decisions | Generic consolidation in `09.3` remains pending; labels/summaries are not sufficient learning evidence |

Source anchors: [reading task](../dokja_orch/internal/handlers/knowledge_reading.go),
[Knowledge service](../dokja_services/dokja_knowledge/service.py),
[task runner](../dokja_orch/internal/core/task_runner.go),
[inference contract](../dokja_domain/dokja_inference/contract.go),
[model-assisted follow-up](model-assisted-cognition-follow-up.md), and
[step 09 record](step-09-consolidation.md).

The original research distinguishes working context, semantic knowledge,
episodic experience and procedural knowledge as functional ideas. Their mapping
to services, tables or navigation groups remains an engineering hypothesis.
Neither the nine-faculty list nor ADR 0010's four groups proves these processes
are implemented.

## First application consumer

The user supplies a question/application goal, a bounded set of documents and
relevant constraints. The vertical must exercise application ports and
orchestrated domain steps; extending a storage benchmark alone is insufficient.
An opt-in path can preserve existing behavior during evaluation.

Before implementation, freeze a representative, nonpersonal technical corpus
and held-out application cases. Review expected supported conclusions,
scope/date restrictions and legitimate abstentions independently of provider
answers. Do not encode the desired strategy in the output schema or select
evaluation cases after observing model success.

The minimum behavior is:

1. Acquire/select documents through Feeds/Knowledge, retaining original evidence
   and versioned references.
2. Represent candidate claims, concepts, relationships and methods separately
   from source artifacts. Specify the minimum representation and ownership
   before changing persistence.
3. Assemble bounded task context: goal, selected evidence, uncertainty,
   applicability constraints and unresolved dependencies. Similarity remains a
   retrieval signal, distinct from factual reliability or authorization.
4. Compare support and contradiction, check prerequisites and decide whether to
   retrieve more, revise, apply a method, ask or abstain. Domains validate;
   orchestration controls continuation and budgets.
5. Produce a supported application result with evidence references and an
   applicability decision.
6. Record the attempted procedure and actual observed result, retaining a
   pre-action prediction when available. User acceptance, task correctness and
   external effect confirmation remain different outcomes.
7. In a later eligible case, show whether that observation justifies a changed
   choice. An inapplicable episode must not influence selection merely because
   its text is similar. No experience is an explicit knowledge-only case.
8. Propose reusable consolidation with supporting versions and scope; validate
   on held-out cases before promotion. One failed application neither falsifies
   its source document nor justifies a universal procedure.

The cycle is conditional and may revisit evidence. It is not a mandatory
document -> feed -> memory pipeline or a requirement to write on every read.

## Required evaluation

| Case/control | Required observation |
| --- | --- |
| Equivalent content through different source adapters | Same supported conclusion/applicability, distinct acquisition provenance |
| Useful evidence beyond the first retrieved document | Additional retrieval resolves a dependency before application |
| Contrary, missing, stale or foreign evidence | Visible uncertainty/revision/refusal/abstention; no silent conversion to support |
| No applicable experience | Knowledge-only application or abstention, without an invented remembered procedure |
| Applicable observed episode versus memory-removed control | Measure whether a justified choice changes; a difference alone is not improvement |
| Irrelevant, foreign or contradicted episode | No unsupported strategy change |
| Prediction versus actual observation | Original prediction stays frozen; observed result remains distinct |
| Held-out cases and paraphrased goals | Applicability beyond memorizing the original fixture |
| Simple and fixed bounded retrieval baselines | Compare correctness, abstentions, unsupported claims, calls, latency and cost |
| Repeated runs, invalid/late output and exhausted budgets | Report every result, enforce bounds and preserve state |

Freeze corpus, rubric, reliability target and evaluation budget before running
comparisons. Blocking scope/authorization/state-integrity controls must pass.
If adaptation provides no measured benefit, keep the simpler supported path and
record that result. Review restart/reuse and durable observation ownership
before any runtime learning claim; connection-local demonstrations are limited.

## Revised work order

1. `08.1`: reconcile concepts with actual behavior and record the owner-selected
   consumer. This source diagnosis and roadmap correction are done; behavioral
   acceptance is not claimed.
2. `08.2`: define knowledge/episode/task-context contracts, freeze corpus and
   rubric, then implement one opt-in application vertical. Reuse workspace,
   source ports and inference validation where appropriate. Review contracts,
   persistence and authority before implementation.
3. `08.3`: run controls, compare baselines, inspect failures and decide which
   boundaries and promotion rules the evidence supports. This gate precedes
   broad adoption, new cognitive navigation and cache design.
4. Resume `09.3` generic consolidation using the validated model and consumer;
   reassess remaining `09.4` grouping and `10.3` inference work against it.
5. Resume `10.4-b` Memory PostgreSQL and durable receipts once the state contract
   is defined. Existing Knowledge PostgreSQL support remains available.
6. Derive UI from inspectable questions, concepts, support/conflicts, experiences
   and decisions. Sources/documents remain accessible provenance views; moving
   tabs alone does not pass the cognitive gate.

Completed batches keep their status and evidence. Previously in-progress,
unfinished tracker work becomes pending behind this review gate, retaining its
prior status. Independent operational fixes remain possible within their own
authorized scope. No runtime rollback or data cutover is performed by replanning.

## Storage and cache limits

PostgreSQL remains the accepted storage direction. A possible SQLite TTL cache
is a derived, discardable projection, separate from preserved migration/rollback
files. Expiry controls freshness, not cognitive forgetting, outcome resolution
or consolidation. Canonical knowledge and episodes acquire no TTL from this
proposal; retained news remains temporal knowledge. Concrete cache TTLs await
their read/invalidation contract. This review does not read or change `.env`,
databases, running services or external-source configuration.

The first `08.2` implementation batch adds [bounded application evidence
preparation](knowledge-application.md) through the existing application ports.
It prepares source observations only; the cross-document reasoning, episode and
evaluation acceptance criteria above remain open.
