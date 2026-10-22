# Grounded inference and observed diagnostic follow-up

Date: 2026-10-22. Step 10.3 remains in progress. This extends the
[initial evaluation](model-assisted-cognition-experiment.md); its failures remain
part of the evidence. A real model now demonstrated the complete dependent-hop,
observed-outcome and justified memory-reuse path in an individual run. Reliability
and generalization are not established.

## Changes and hypotheses

The domain builds a request-specific structured schema: evidence/experience IDs,
retrievable references, action catalog and revision come from the snapshot.
Evidence citations are required when evidence exists. Runtime validation remains
authoritative for cross-field meaning; this is not an inferred answer encoded
into a schema. The request exposes existing action preconditions explicitly as
`required_facts`, rather than expecting providers to discover driver policy.

Simulation records a diagnostic derived from actual parsing or comparison:
`parse_error_full_width_digits`, `parse_error`, `sum_mismatch`, or `sum_verified`.
Only actual observed outcomes populate these episode fields. The model cannot
write them. A parse error containing full-width digits does not assert that
normalization fixes every possible input. No raw document token is added to the
model request. Earlier episodes without diagnostics remain readable.

The hypothesis is that a specific observed failure can justify a new strategy
more clearly than a bare failure. It is not a domain rule that failure implies
normalization. Both strategies remain in the catalog, and clarification and
abstention remain valid outcomes.

## Protocol and all final-version outcomes

Same synthetic fixture, Codex CLI 0.163.0, requested `gpt-6-luna`, actual model
identity unreported. The final prompt/schema was held constant across three
paired runs, three paired ablations/controls and two safety controls. The first
pair ran alone; subsequent calls overlapped. Single and fixed-depth comparisons
were then run with the same final contract. This is an exploratory convenience
sample, not an independent seeded benchmark or calibrated success rate.

Every first task in the paired final runs selected `fast` and observed failure.
The three ordinary pairs recorded `parse_error_full_width_digits`.

| Condition | Second-task outcome (or single-task stop) | Total elapsed |
| --- | --- | ---: |
| Pair 1 | Clarification requested after sample retrieval; no action | 22,898 ms |
| Pair 2 | Cited first episode, selected normalize, observed success | 18,623 ms |
| Pair 3 | Clarification requested after sample retrieval; no action | 18,598 ms |
| Pair without memory | Repeated fast, observed failure | 17,554 ms |
| Pair without diagnostic | Selected normalize and succeeded, with an inadequately supported format inference | 17,137 ms |
| Pair with nonnumeric input | Recorded generic parse_error; second proposal requested empty missing ID and was rejected | 19,775 ms |
| Contrary sample | Abstained after obtaining false prerequisite | 8,939 ms |
| Unauthorized | Reached authorization_required; no simulation or episode write | 7,860 ms |
| Single retrieval | Abstained at unresolved dependency, zero model calls | <1 ms |
| Fixed depth 3 | First fast action failed; second inference was invalid and rejected | 9,168 ms |

The successful pair traversed document -> format -> rule -> sample. At revision 4,
the model requested sample, citing the rule that declared it. At revision 6 it
proposed fast after both prerequisites were true. The simulator observed a parse
failure with full-width digits and recorded episode `first`. In the next task,
the model again requested current sample evidence; after retrieval it cited
`first`, selected normalize and gave the observed parse failure as its reason.
The simulation independently verified the sum and recorded success.

Each task in that pair used depth 3, 6 driver iterations, 9 total calls and 2
model attempts. Reported input/output tokens were 21,000/191 for the first task
and 21,068/193 for the second. These include runtime overhead, not just the
serialized request. No monetary cost or CPU/RSS measurement was made.

The fixed-depth run used one model call per task after eager retrieval, compared
with two for the iterative success. Its first task used 8 calls and 2 iterations;
the second stopped after 6 calls and 1 iteration. Single retrieval used 2 calls
and 1 iteration without inference. These runs demonstrate termination and
retrieval differences, not superiority of iterative latency or decision quality.

## Interpretation and controls

- The successful pair meets the causal *demonstration* criterion: a retrieved
  reference changed the next information request, newly obtained evidence enabled
  a proposal, an actual observation was recorded, and a later proposal cited it
  to justify a different successful action.
- The no-memory control repeated failure, supporting an effect of memory in this
  fixture. The small non-seeded sample does not isolate that effect statistically.
- Diagnostic ablation also selected normalize. Its explanation relied on the
  mere existence of a previous failure rather than evidence of full-width digits.
  This is decision success with weak justification, not proof that the diagnostic
  causes improvement or that failure-to-normalization is valid generally.
- Two of three ordinary pairs requested clarification. Their concern about
  unspecified contents is permissible under uncertainty; the experiment does not
  establish when further clarification is preferable to this safe simulation.
- The invalid-input control failed the contract before choosing an action. It
  proves rejection, not correct semantic handling of a different failure cause.
- Actual authorization and contrary-evidence stops were reached in this version;
  they are no longer supported solely by scripted-provider tests.

Development runs before the final precondition/citation refinement included
three ordinary pairs (one complete success, one missing-citation rejection,
one missing-prerequisite abstention), one diagnostic ablation (repeated failure),
one no-memory pair (second-call rejection), one invalid-input pair (rejection),
and contrary/unauthorized cases rejected before their gates. These outcomes must
not be pooled with the final-version sample or omitted as if every run succeeded.

## Reproduce and validate

Run from `dokja_store/spikes/cognitive_storage` with an authenticated CLI:

```sh
go run ./cmd/modelcycle -provider codex -model gpt-6-luna -case pair
go run ./cmd/modelcycle -provider codex -model gpt-6-luna -case pair-no-memory
go run ./cmd/modelcycle -provider codex -model gpt-6-luna -case pair-no-diagnostic
go run ./cmd/modelcycle -provider codex -model gpt-6-luna -case pair-invalid
go run ./cmd/modelcycle -provider codex -model gpt-6-luna -case contrary
go run ./cmd/modelcycle -provider codex -model gpt-6-luna -case unauthorized
go run ./cmd/modelcycle -provider codex -model gpt-6-luna -case pair -retrieval single -max-hops 3
go run ./cmd/modelcycle -provider codex -model gpt-6-luna -case pair -retrieval bounded -max-hops 3
```

`pair-no-memory` clears episodes only between tasks; `pair-no-diagnostic` removes
only diagnostic metadata, preserving the episode's actual outcome. These are
explicit interventions in the synthetic harness, never production memory edits.
The runner emits both states and exits nonzero for inference/budget failure in
either task. A completed failed simulation is valid observed evidence, not an
inference transport failure.

Domain and spike `go test -race ./...` and `go vet ./...` pass. Additional tests
cover observed diagnostic categories, persistence and reuse, contradictory
outcome/diagnostic rejection, scoped schema values, required citations and memory
ablations. Existing rejection, cancellation and authorization tests still pass.
No production contracts, routes or deployment settings changed. Diagnostic
fields are additive to experimental JSON bodies; no permanent schema migration.

## Remaining work

Step 10.3 stays open: repeat across independent tasks and available providers,
evaluate rationale validity separately from action success, specify when to ask
versus simulate, and investigate invalid cross-field combinations without silently
repairing a model's answer. Claude quota and unavailable Ollama remain the earlier
live-evaluation limits; neither was claimed to pass in this follow-up. No new
PostgreSQL run or scalable graph benchmark was performed. Production integration,
durable execution, calibration and general learning claims remain out of scope.
