# Experience Memory Plan

Status: proposed implementation plan. No runtime changes are included.

## Objective and existing foundation

Use accumulated knowledge and previous outcomes to support new analyses and
predictions. The first release must demonstrate: record a prediction, attach an
observed outcome, and retrieve that experience during a later analysis.

The existing [knowledge service](../dokja_services/dokja_knowledge/README.md)
already provides document ingestion, hybrid retrieval, source metadata, multiple
formats and external plugins. The orchestrator already supplies relevant passages
to chat. Reuse these capabilities.

Document updates currently replace their chunks. Source IDs alone therefore do
not preserve the evidence available when a prediction was made. Each experience
must retain the actual evidence excerpts used, with source references and hashes.

The untracked prediction scaffold is experimental context, not an adopted storage
or service contract. Its evidence-count confidence heuristic is not suitable for
evaluating prediction quality.

## First-release scope

- Explicitly record an analysis or prediction through an operator request.
- Persist its original text and evidence before background model processing.
- Use a local language model to propose structured fields and a searchable summary.
- Explicitly attach a sourced outcome and compare it with the original prediction.
- Retrieve previous experiences with their outcome status for later analyses.
- Expose inspection and correction through the CLI first.

Automatic capture of every conversation, autonomous outcome collection, personal
preference memory, model training and broad prediction-domain automation are outside
this release. The first evaluation set should represent the user's actual analyses;
no particular prediction subject has been selected yet.

## Responsibility boundaries

Follow `Interface → Event/Request → Orchestrator → Domain → Service`.

- A proposed memory domain owns experience validation, revisions, outcome linkage
  and the distinction between a source assertion and model interpretation.
- The orchestrator coordinates capture, background enrichment, indexing and recall.
  Domains do not call each other directly.
- The existing knowledge capability provides retrieval and a derived search index.
  Its document index is not the authoritative experience ledger.
- Experience persistence uses a repository port backed by `dokja_store`, with its
  existing migration mechanism. The knowledge service keeps its current database.
- Local inference is exposed through a stateless service integration. Inspect the
  existing text-generation service before extending its adapter or adding another.
- Prediction-specific scoring belongs to the prediction domain; the memory domain
  stores the evaluation and the scoring method/version.

Implement cross-layer changes in the repository's documented order: domain actions,
router, workflow steps, handlers and clients. Preserve the existing event envelope.

## Durable records

| Record | Required information |
| --- | --- |
| Experience | ID, scope, question, original analysis, creation time, revision |
| Prediction | Experience ID, explicit claim, target/horizon when supplied, declared probability when supplied |
| Evidence snapshot | Experience ID, source ID/reference, exact excerpt used, content hash, capture time, publication/event time when known |
| Outcome | Experience ID, observation, source/evidence, occurrence time and recording time |
| Evaluation | Prediction/outcome revisions, method/version, metrics where applicable, model interpretation separately |
| Enrichment job | Experience revision, state, attempts, model/prompt/schema versions and failure details |

Missing dates, probabilities and outcomes remain unknown. Do not invent them from
model confidence. Preserve separate occurrence and recording times. Revisions must
retain the original claim, evidence and earlier evaluations.

Use an explicit scope on records and retrieval. Initially support an operator-owned
corpus; do not automatically ingest private conversations into a shared corpus.
When historical analysis requests a cutoff, exclude knowledge recorded after it.
An experience may be recalled while pending, but must never be presented as a
confirmed success or failure until an outcome supports that classification.

## Local inference

Available environment: Linux, an 8 GB GPU, a 4 GB GPU, an eight-core CPU and 58 GB
of system RAM. GPU availability and driver compatibility still require verification.

Start evaluation with a 7–8 billion parameter model quantized to four bits on the
8 GB GPU, around 4,000 context tokens and one concurrent job. These are benchmark
candidates, not a guaranteed fit or a fixed deployment contract. Test a smaller
model if memory usage or latency is unsuitable. Evaluate a Vulkan backend first.

Keep the second GPU optional. Benchmark embeddings there only after establishing a
working baseline. Keep model names, endpoints and device selection configurable.

The model proposes extraction, summaries and retrospective interpretations. Code
validates the output schema, resolves evidence references, enforces dates/scopes and
computes defined metrics. Retrieved text is untrusted reference material.

Extraction must preserve the meaning of the original claim. Unsupported statements
or uncertain matches remain reviewable proposals. A model-generated explanation
for a failure is a hypothesis, not an established cause.

## Workflows and reliability

1. Capture validates the request and atomically stores the experience, evidence
   snapshots and durable enrichment job. Retrying the same request is idempotent.
2. A background worker requests structured output, validates it and stores a derived
   revision. Malformed output receives bounded retries, then a visible failed state.
3. The orchestrator indexes the accepted summary through the knowledge domain using
   a stable source ID. Persist indexing status so failures can be retried after restart.
4. Outcome recording preserves the observation independently of model availability.
   Evaluation links exact prediction and outcome revisions.
5. Recall retrieves candidates, resolves their current authoritative records and
   applies scope, time and status rules before assembling context and citations.

The conversation and capture path must remain usable when inference is unavailable.
Repeated delivery must not duplicate records or overwrite newer enrichment. A job
for an older revision cannot replace a newer one. Deletion must also invalidate
derived search entries; stale indexed entries must not bypass authoritative checks.

## Delivery sequence and acceptance

| Stage | Deliverable | Acceptance |
| --- | --- | --- |
| 1. Contracts and fixtures | Record schema, repository operations, event payload drafts, representative Portuguese examples | Examples distinguish evidence, claim, unknown fields and observed result |
| 2. Durable capture | Migrations, repository, domain actions, orchestrator routes and CLI | Duplicate capture is idempotent; restart retains records; editing source documents does not change evidence snapshots |
| 3. Local enrichment | Inference adapter, durable worker, validated extraction and summary indexing | Invalid output and unavailable inference preserve the original record; retries and outdated jobs behave correctly |
| 4. Outcomes and evaluations | Explicit outcome entry, revision linkage and defined scoring | Pending cases are not scored; corrections preserve history; an invented causal explanation cannot become a verified fact |
| 5. Recall integration | Existing retrieval plus authoritative experience resolution | A later analysis cites an earlier case and its actual status; scope/cutoff exclusions hold; deleted cases do not surface |

For binary forecasts with explicit probabilities and resolved outcomes, a Brier
score is a candidate metric. Do not apply it to narrative analyses, infer a missing
probability, or label a model as calibrated from a few examples. Report sample sizes
and unresolved cases alongside any aggregate metric.

## Evaluation and rollout

Build a small reviewed Portuguese dataset covering explicit and ambiguous forecasts,
missing deadlines/probabilities, conflicting evidence, unsuccessful predictions,
pending outcomes, corrections and instruction-like text inside source material.

Compare candidate local models on extraction fidelity, evidence traceability,
schema validity, handling of unknowns, retrieval usefulness, latency and peak device
memory. Set numerical performance targets after measuring this workload. No model
or hardware benchmark has been run as part of this plan.

Tests should exercise observable behavior across capture, restart, enrichment,
outcome recording and later recall. Include source replacement, duplicate requests,
scope isolation, historical cutoffs and stale-index cases. Test migrations against
an existing database copy and document rollback/restore before deployment.

All proposed contracts are additive. Review migration and wire compatibility before
implementation; any breaking change requires the dedicated worktree process.
Any new environment variables, volumes or inference services must update affected
compose files and the [production runbook](./production-runbook.md) together.
Production deployment remains a separate approval step.
