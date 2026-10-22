# ADR 0005: Knowledge, Experience and Prediction cycle

Date: 2026-10-18. Status: accepted for prototype (owner decision after step 03).
Implementation: step `04`. Refines ADRs 0001 and 0003.
Related: [decision index](./README.md), [workspace](./0001-task-workspace.md),
[state ownership](./0003-state-ownership.md), [capability map](../capability-map.md).

## Current review scope (2026-10-21)

The owner requested a fresh causal experiment. The historical prototype topology
choice below remains a record of step 03; it does not settle cognitive capability
boundaries. In particular, general strategy versus exact application is a
hypothesis, not an established Knowledge/Experience division. Neither three
services nor three databases nor a fixed faculty sequence follows from this ADR.

The [new evaluation](../cognitive-cycle-experiment.md) demonstrates dependent
retrieval, prediction revision, a simulated action and reuse of its observed
result in PostgreSQL. Evidence interventions and a no-memory control establish
causality within the synthetic fixture. A fixed three-hop baseline also succeeds
at retrieval with fewer prediction calls, so adaptive-loop superiority is not
established. The predictor and experience-selection rule are hand-written;
production readiness and generalized learning remain unvalidated.

Proposed revision: preserve orchestration ownership and domain transition policy
as the next integration hypothesis, but defer permanent cognitive responsibility
boundaries and storage adoption to broader evaluations. No production topology,
public contract or deployment is changed by this review.

## Context

The user clarified the intended abstraction: Knowledge defines the macro model
or strategy, and Experience says how to apply that knowledge to the specific
situation. Treating Book as a separate cognitive peer of Knowledge would mix a
content specialization with a general cognitive responsibility.

The user subsequently identified a dependency question: Knowledge, Prediction,
and Experience can form a graph with feedback cycles before an action is ready.
The allocation below is a hypothesis to investigate, not an accepted dependency
topology. Current implementation boundaries remain documented as facts.

## Candidate allocation to investigate

The [feedback-graph research](../knowledge-prediction-experience-research.md)
distinguishes semantic, inference, runtime, and software dependency graphs.
It proposes comparing an orchestrator-supervised iterative workflow with bounded
computation inside a domain. Neither topology is accepted by this record.

Experience can inform prediction, prediction can change evidence needs, and
observed results can revise later knowledge and experience. The example below
must not become a mandatory one-pass sequence.

Separate three axes in the design:

| Axis | Responsibility | Example |
| --- | --- | --- |
| Cognitive capability | General knowledge, contextual experience, selection and coordination | Retrieve a general summarization method and apply a relevant previous procedure |
| Content/source specialization | Acquire and represent a kind of input | Book extraction, feed polling, page fetching, audio transcription |
| Product operation | Express the user's goal using those capabilities | Summarize a book, answer a question, compose a digest |

Knowledge holds facts, concepts, relationships, and general methods with
provenance. A general method can specify an abstract sequence and its purpose;
it is data interpreted by domain logic, not arbitrary executable plugin code.
The existing document/chunk store is an initial mechanism, not the definition
or full implementation of this macro responsibility.

Experience links a knowledge/method reference to a concrete context, procedure,
parameters, observed constraints, and outcome. It supports choosing how to apply
the method: which steps worked, with which tools/settings, and under which
conditions. It includes contextual application beyond merely counting outcomes.
The current episodic store is the starting point; derived procedures and their
selection still need typed contracts and evaluation.

Domain logic interprets retrieved knowledge and experience, selects an applicable
procedure, and enforces preconditions. The orchestrator sequences registered
domain actions. Services and provider plugins implement acquisition, retrieval,
and execution mechanisms. Experience remains scoped evidence: it cannot override
current authorization or policy, and an inapplicable episode must not dictate
an action just because its text is similar.

When there is no applicable experience, the domain instantiates the general
method using explicit defaults or abstains when required information is missing.
Record this as knowledge-only application, without inventing a remembered
procedure. Observing the result can create a new experience for later use.

## Book and Feeds in this model

Decided in [ADR 0006](./0006-reading-macro-component.md): reading is one
macro-component grown from the Feeds host, Knowledge stores what was read with
its provenance, and Experience records the reading episode. Book is not a
separate service. This record keeps only the Knowledge–Prediction–Experience
cycle question.

## Example

Goal: summarize an argumentative document.

1. Acquisition extracts its content and structure, whether it came from a book
   file, a supplied article, or a feed item with usable content.
2. Knowledge supplies a general method: identify thesis, supporting arguments,
   counterarguments, and unresolved questions.
3. Experience supplies an applicable procedure, for example processing a long
   scanned document in bounded sections using OCR before applying that method.
4. Domain logic checks that the current document and available capabilities
   satisfy the procedure, then returns concrete approved steps and parameters.
5. Orchestration executes those steps through domains/services and records the
   resulting observations and feedback as a new contextual experience.

Source type determines acquisition behavior. It must not independently define
the general summarization strategy or override a user's explicit goal.

## Workspace and learning implications

An application decision identifies the selected knowledge/method version,
applicable experience references, context match and rejected preconditions,
concrete actions/parameters, and fallback reason when no experience applies.
Keep method identity distinct from plugin identity and prediction method version.

General methods belong to Knowledge; contextual application evidence belongs
to Experience; reviewed executable workflow definitions remain orchestrator-owned.
A learned concrete procedure must be validated before it is made executable.
The existing shadow acceptance predictor remains a narrow evaluation feature;
this refinement does not claim that it implements contextual procedure selection.

## Verification and compatibility

Add baseline cases where equivalent supplied content from book/text/feed
representations leads to the same general-method selection while preserving
different acquisition/provenance behavior. Add cases for applicable experience,
experience with failed preconditions, no experience, and a conflicting explicit
user goal. A fresh workspace must record why the selected procedure applies.

These tests are planned for subsequent steps. No routes, endpoints, schemas,
environment variables, compose files, or runtime policies change in this decision
batch. Storage changes for procedure references require their own compatibility
review; current stores are not migrated implicitly.

## Evidence from step 03

The [reference experiment](../step-03-baseline.md#4-adr-0005-reference-experiment)
ran one pure policy under an orchestrator-supervised loop (A, with and without
snapshot reuse) and a domain-internal loop (B) over deterministic fakes. All
reached the same outcome and candidate in 15 scenarios. With snapshot reuse, A
matched B's operation count everywhere; the only difference came from re-reading
Knowledge and Experience without new evidence.

Limits: B shared A's supervision code, so its trace, cancellation and budget
behavior were equal by construction; latency and code coupling were not
measured.

Recommendation: the orchestrator-supervised loop with snapshot reuse owns the
cycle; a domain may revise over a supplied snapshot within one step if it
returns its trace and spend. The owner accepted it.

## Decision

The owner chose topology A. The decision gate is resolved as follows:

| Question | Resolution |
| --- | --- |
| Cycle owner | The orchestrator supervises the cycle: each retrieval, prediction and evidence acquisition is an orchestrated step, and the faculty domain decides the next step from typed results. |
| Update schedule | Knowledge and Experience are read into a snapshot and re-read only after new evidence arrives. Every result names the versions it consumed. |
| Readiness and stopping | Non-terminal steps: `need_evidence`, `revise`. Terminal outcomes: `ready`, `abstain`, `failed`, `budget_exhausted`, `stagnated` (state signature repeated), `oscillated` (an older signature returned). Unavailable evidence is reported apart from contrary or foreign-scope evidence. |
| Observed versus simulated | Predictions are always hypothetical. Only an observed outcome of a ready, executed proposal becomes experience; an unknown outcome stays an unresolved attempt. |
| Domain-internal revision | Allowed within one step over the snapshot it was given, as an optimization, if the domain returns its trace and spend. It is not a second cycle owner. |
| First prediction target | Hashtag acceptance, as defined in [ADR 0004](./0004-shadow-prediction.md). |
| Typed edges | Prototype evidence kinds are knowledge (general method), experience (observed procedure) and observation (premise). Richer edge types wait for a consumer that needs them. |

These are internal prototype contracts. The public envelope, routes and service
contracts do not change in step `04`.

## Decision gate (history)

Resolve the first task/prediction target, typed edge meanings, temporal update
schedule, cycle owner, readiness/stopping rules, and separation of observed
experience from simulated branches. Book/source ownership is decided in ADR 0006.

The research and synthetic experiment design can be recorded while this proposal
remains unaccepted. Step `03` may capture existing behavior and evaluate pure,
fake-capability reference loops without adopting a production topology. Results
feed this record before step `04` integrates cognitive-loop contracts. This avoids requiring
a final decision before collecting the evidence needed to make it.
