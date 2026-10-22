# Prototype architecture decisions

Date: 2026-10-18. Tracker: [steps.json](../../steps.json), step `02`.

These records select the design for the first cognitive macro-component
experiments. “Accepted for prototype” means an implementation choice within the
authorized refactoring scope. It does not claim that a prototype is implemented,
validated, deployed, or approved as a new repository-wide rule.

| Record | Decision | Status |
| --- | --- | --- |
| [0001](./0001-task-workspace.md) | Orchestrator-owned, scoped, volatile task workspace with explicit outcomes and budgets | Adopted (step 08) |
| [0002](./0002-plugin-lifecycle.md) | Versioned lifecycle metadata with capability-specific contracts and legacy adapters | Adopted for reading (step 08) |
| [0003](./0003-state-ownership.md) | Preserve current stores; add no durable prototype state; isolate the unresolved service-policy question | Accepted for prototype scope; broader rule change deferred |
| [0004](./0004-shadow-prediction.md) | Hashtag acceptance prediction with explicit feedback, frozen probabilities, and shadow evaluation | Kept in shadow (step 08) |
| [0005](./0005-knowledge-experience-content.md) | Orchestrator-supervised Knowledge–Prediction–Experience cycle with snapshot reuse and explicit stopping outcomes | Historical prototype choice; causal follow-up reviewed, permanent allocation open |
| [0006](./0006-reading-macro-component.md) | Reading is one macro-component grown from Feeds; Knowledge stores, Experience records the episode; no Book service | Accepted as target direction; migration pending |
| [0007](./0007-faculties-not-features.md) | Macro-components are faculties; Book, Meme and Digest are workflows over them | Accepted as target direction; migration pending. 0010 proposes replacing its faculty list |
| [0008](./0008-consolidation-by-kind-of-knowledge.md) | Separate form/content kinds with structured metadata; shared consolidation and summary methods | Representation accepted; Book retirement and provider/migration details open |
| [0009](./0009-cognitive-storage-evaluation.md) | Compare explicit cognitive graph workloads on SQLite, PostgreSQL+pgvector and Neo4j before engine selection | Proposed; research complete, benchmark and adoption pending |
| [0010](./0010-four-concepts.md) | Four concepts (Knowledge, Memory, Action, Rules) replace the nine faculties; reading is Knowledge intake | Proposed; awaiting the owner's review |

The design keeps large capabilities modular and gives them explicit contracts.
ADR 0005 was accepted after the step 03 [reference experiment](../step-03-baseline.md):
the orchestrator supervises the Knowledge–Prediction–Experience cycle. Its
[research report](../knowledge-prediction-experience-research.md) holds the
background; Book/source ownership is decided in ADR 0006.
Neuroscience supplies hypotheses about coordination and memory roles; engineering
measurements determine whether those hypotheses improve Dokja.

Baseline requirements from these records belong to step `03`. Internal workspace
implementation belongs to `04`, plugin compatibility to `05`, experiments to
`06` and `07`, and adoption decisions to `08`. Concrete tests and measurements
must precede claims of compatibility or improvement.

Compatibility gate for this decision batch: public envelope, actions/routes,
HTTP/ZeroMQ contracts, commands, SQLite schemas, environment variables, compose
configuration, and production behavior are untouched. Later implementations must
review every affected boundary. Existing source observations are recorded in the
[capability map](../capability-map.md); research and evidence limits are in the
[research report](../cognitive-macrocomponents-research.md).
