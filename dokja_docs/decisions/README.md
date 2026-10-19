# Prototype architecture decisions

Date: 2026-10-18. Tracker: [steps.json](../../steps.json), step `02`.

These records select the design for the first cognitive macro-component
experiments. “Accepted for prototype” means an implementation choice within the
authorized refactoring scope. It does not claim that a prototype is implemented,
validated, deployed, or approved as a new repository-wide rule.

| Record | Decision | Status |
| --- | --- | --- |
| [0001](./0001-task-workspace.md) | Orchestrator-owned, scoped, volatile task workspace with explicit outcomes and budgets | Accepted for prototype |
| [0002](./0002-plugin-lifecycle.md) | Versioned lifecycle metadata with capability-specific contracts and legacy adapters | Accepted for prototype |
| [0003](./0003-state-ownership.md) | Preserve current stores; add no durable prototype state; isolate the unresolved service-policy question | Accepted for prototype scope; broader rule change deferred |
| [0004](./0004-shadow-prediction.md) | Hashtag acceptance prediction with explicit feedback, frozen probabilities, and shadow evaluation | Accepted for prototype |
| [0005](./0005-knowledge-experience-content.md) | Investigate Knowledge–Prediction–Experience graph, feedback cycles, and Book/source specialization | Proposed; research and decision required |

The design keeps large capabilities modular and gives them explicit contracts.
ADR 0005 remains a research proposal: the Knowledge–Prediction–Experience cycle
and Book/source classification have not been accepted as implementation contracts.
Its [research report](../knowledge-prediction-experience-research.md) identifies
the topology comparison and the evidence required before adoption.
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
