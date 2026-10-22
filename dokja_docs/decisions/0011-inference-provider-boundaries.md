# ADR 0011: Dokja owns cognition; models provide inference capabilities

Date: 2026-10-21. Status: accepted responsibility boundary (owner direction).
Implementation: classifier adapters and an experimental general inference loop
exist; reliable model-assisted memory reuse remains pending in step 10.3. Refines [ADR 0005](./0005-knowledge-experience-content.md).

## Decision

Dokja owns task reasoning, state revisions, evidence selection, decisions,
authorization, observed outcomes and reuse of experience. Codex, Claude Code and
Ollama are interchangeable infrastructure mechanisms that provide bounded
inference capabilities. Their answers are proposals, not authoritative facts,
observed outcomes or instructions to execute a workflow.

| Layer | Responsibility |
| --- | --- |
| Domain | Define provider-independent inference requests/results, evidence and prediction semantics, validation, applicability and action preconditions. Return typed assessments, information needs and candidate actions. |
| Cognitive orchestration | Own task state and choose when to request inference, retrieve Knowledge or Experience, perform another hop, revise, stop, seek information/authorization or execute an authorized action. Record why a transition is justified and enforce task budgets. |
| Infrastructure | Implement Codex/Claude Code process adapters and Ollama transport, provider configuration, isolation, timeouts, cancellation, bounded output and usage reporting. Translate transport output into the domain contract. |
| Interface | Submit goals, show evidence/decisions and collect explicit feedback or authorization through the orchestrator. |

The flow remains `Interface -> Event/Request -> Orchestrator -> Domain -> Service`.
The orchestrator applies cognitive transition policy using typed domain
assessments; it does not acquire domain-specific truth or admissibility rules.
Domain requests for evidence are proposals to orchestration, not recursive calls
to other domains or services. Provider choice and fallback are explicit configured
capability bindings within a task budget, not a provider-owned cognitive loop.

Swapping providers must not change who owns state, grant additional permissions,
reset budgets or imply that all providers support identical capabilities.
Unsupported capabilities, malformed output, timeout and unavailable providers
must be explicit results. A fallback must preserve the request's scope and
semantics and identify the provider/model that actually answered.

## Contract requirements for the next implementation batch

The domain-owned contract must identify the requested capability, contract
version, task/state revision, authorized evidence references and versions,
constraints and remaining inference budget. Results must distinguish hypotheses,
predictions, missing information, candidate actions and unavailable/invalid
answers, with provider/model provenance and measured or unavailable usage.
A probability must name its target and method; similarity is not confidence.

Orchestration validates the result against the current state revision before
applying it. A late or stale answer cannot silently replace newer state. Only
observed action results enter resolved Experience; provider-generated success
claims remain hypothetical. Reuse of episodes does not itself establish learned
model quality or permission to promote claims into Knowledge.

CLI providers may be agent runtimes internally, but the inference adapter exposes
only the requested bounded capability. It must not hand them control of the
application workspace, memory writes, action dispatch or unrelated tools. The
existing classifier's isolation is a starting point to test, not proof that all
future adapters are isolated.

## Existing evidence and scope

- `dokja_services/dokja_feeds/plugins.example/agent-kinds/run.py` already hosts
  Claude Code, Codex and Ollama adapters for document classification.
- `dokja_services/dokja_feeds/feeds/classify.go` bounds provider proposals and
  reports answering-provider provenance.
- `dokja_domain/dokja_knowledge/classification.go` owns classification meaning,
  validation and precedence over provider suggestions.
- The [causal experiment](../cognitive-cycle-experiment.md) uses a deterministic
  fixture predictor. Its manual predictor remains the baseline for the
  [model-assisted evaluation](../model-assisted-cognition-experiment.md).

An experimental domain inference contract is implemented; live evaluation has
not established reliable outcome reuse. No new service, database,
provider order, public route, deployment setting or production model call is
introduced by this decision. The cognitive capability split remains provisional;
this responsibility boundary does not mandate three cognitive components or a
fixed processing sequence.

## Acceptance tests for integration

Use the same contract fixtures with every adapter and a deterministic fake.
Verify unsupported capability, invalid output, scope isolation, stale revision,
cancellation, timeout, explicit fallback provenance and cumulative budgets.
Demonstrate that replacing a provider preserves task ownership and authorization;
a provider request cannot trigger memory writes, extra hops or external actions
without an explicit orchestration transition. Repeat the causal evidence and
no-memory controls before claiming the integrated system learns from outcomes.
