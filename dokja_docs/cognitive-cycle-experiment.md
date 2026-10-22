# Cognitive cycle: diagnosis, experiment and evaluation

Status: isolated synthetic prototype, not production readiness. Work follows the
owner's request to test causal evidence, revised decisions and reusable observed
consequences. The existing allocation of general strategy to Knowledge and exact
application to Experience remains a hypothesis.

## A. Repository diagnosis

Inspected step 02 and its dependencies 00/01, its decision gate in step 03, the
accepted prototype decision in ADR 0005, the step 04 workspace, steps 06/08
hashtag composition/evaluation, current step 09 migration and step 10 storage
research, the existing experience-memory plan and proposed ADR 0010. ADR 0010's
four-concept grouping remains proposed and is not adopted by this experiment. Relevant implementation anchors:

| Evidence | Actual state | Limit |
| --- | --- | --- |
| `dokja_orch/internal/core/task.go`, `task_runner.go` | Typed, bounded workspace with evidence, proposals and receipts | Volatile; not a general iterative cognitive controller |
| `dokja_orch/internal/experiments/cognitiveloop` | Step 03 pure policy and supervised/domain-internal comparisons over fake capabilities | Neither topology is a production loop; shared supervision limits the independence of that comparison |
| `dokja_orch/internal/handlers/hashtag_task.go`, `hashtag_experience.go` | Retrieval and frozen shadow prediction before delivery; observed delivery followed by optional recording | No general multi-hop policy; background recording is not durable execution |
| `dokja_domain/dokja_memory/predict.go` | Narrow acceptance estimator and scoring | Not a general world model or calibrated predictor for this new task |
| `dokja_services/dokja_memory/memory/store.go` | SQLite episodes, first-verdict outcomes, recall | No complete versioned knowledge/procedure/action graph |
| `dokja_services/dokja_knowledge/store.py` | Document/chunk SQLite storage and retrieval | No general explicit reasoning graph |
| `dokja_store/spikes/cognitive_storage` | W1 measured PostgreSQL 17.11/pgvector 0.8.7 against SQLite/Neo4j | Synthetic filtered vector search, not W2/W4 multi-hop evidence |
| Step 10.1 and `multihop` reference | Bounded labelled paths, independent golden results, cycles and scope exclusions | Three-engine traversal comparison still pending |

PostgreSQL is established experimental infrastructure, not the current application
storage backend. The inspected compose stacks do not provide a PostgreSQL
application store. This experiment therefore uses a disposable instance and
connection-local temporary tables, not an application migration.

## B. Four graphs and proposed state

| Graph | Representation in this experiment | Owner and boundary |
| --- | --- | --- |
| Semantic/data | Versioned document, format, rule and observation nodes; explicit dependency/question references; observed episode references | Storage port returns scoped evidence, not truth or an action grant |
| Inference | Evidence and episode references consumed by each hypothetical prediction; missing premise; revised candidate | Pure `Decide`/`Predict` policies; fixture rules, not a general inference engine |
| Execution | Retrieve, recall, predict, revise evidence, decide, simulate, record, stop | Driver is the sole state writer and enforces budgets/authorization |
| Software | Driver → typed storage port; deterministic policy; in-memory/PostgreSQL adapters | No recursive service calls or package dependency cycles |

`cognitive.State` is the minimal versioned experiment workspace. It records goal,
scope, contextual applicability key, versioned evidence, observed episodes,
frozen hypothetical predictions, action, observed result, counters and revisioned
transitions. Every evidence reference has ID, version and provenance. Trace
entries are structured operation records, not generated reasoning prose.

New evidence invalidates the previous current prediction and triggers evaluation
again; earlier predictions remain frozen in the trace. Conflicting Boolean
assertions trigger clarification. An unchanged dependency cycle with insufficient
facts ends in abstention. Positive deadline/call/iteration bounds and at most six
hops bound execution. Storage honors context cancellation; late retrieval results
are discarded. This is cooperative cancellation, not isolation of arbitrary
uncooperative code. Missing and inaccessible evidence share a non-disclosing
unavailable result.

No history is promoted automatically to trustworthy general knowledge. Episodes
remain marked `Simulation`, context-bound and tied to evidence and the prediction
made before the action. They are not production observations. Recording uses a
stable attempt ID, preserves the first observation, rejects conflicting retries,
and reports a recording failure separately from the already observed action.

## Orchestration alternatives

| Alternative | Benefit | Cost and limitations | Compatibility |
| --- | --- | --- | --- |
| Orchestrator-supervised state transitions | Each retrieval, prediction and action has an explicit trace/budget boundary | More transitions and provider latency; requires a domain-owned transition policy | Fits existing task ownership and supervised reference loop |
| Bounded domain reasoning over a supplied snapshot | Can reduce orchestration round trips | Must return spend/trace and request missing external evidence through orchestration | Compatible optimization; not measured as an independent implementation here |
| Fixed bounded graph retrieval then prediction | Simple and can be cheaper when dependencies are known | Fetches ahead of need; cannot adapt depth to newly discovered requirements | Implemented comparison baseline, not a universal pipeline |
| Recursive cross-service coordination | Locally convenient calls | Opaque budgets, ownership and circular waiting | Rejected under current repository boundaries; no experiment needs it |

No result below proves that Knowledge, Experience and Prediction need distinct
services, databases or mandatory processing stages. The isolated experiment
reuses the existing storage spike module, pgx/pgvector infrastructure, scoped
snapshot/port pattern and first-observation semantics. It does not import the old
reference policy, because that policy hard-codes the very strategy/procedure
allocation under investigation. Production Task APIs remain unchanged.

## C. Minimal causal scenario

Goal: sum a small synthetic numeric document in a local simulator.

1. Exact vector search retrieves only the document seed, containing a reference
   to its format. Vectors have three synthetic dimensions, not learned embeddings.
2. Reading that format discovers the rule reference: two dependent information
   hops are required to reach the rule; neither key is hard-coded in the driver.
3. The rule supplies a precondition and identifies an observation the predictor
   needs. The first prediction explicitly requests that observation. Retrieving
   it at hop three clears the missing premise and changes abstention to readiness.
4. The harness must independently authorize the simulated action. Evidence and
   predictions cannot supply this grant.
5. The fast strategy actually parses and sums tokens. Full-width digits cause
   its parser to fail. This is an observed computation, not a predicted verdict.
6. The episode is recorded with the frozen prediction and simulation marker.
   A later task recalls that failure in the same scope/context and selects a
   normalization strategy. Its computed sum is checked against the fixture's
   independently supplied expected total.

The policy associating a prior failure with the alternate strategy is deliberately
hand-written. This demonstrates reuse of observed experience, not learned model
weights or a discovered general procedure. Both strategies are safe local
functions; no arbitrary command, external message or application write is allowed.

### Counterfactual controls

- Remove the observation: abstain, no action or episode.
- Negate its premise: abstain rather than execute.
- Add a contradictory permission assertion: clarify, no action.
- Change the rule's question reference: retrieval targets the new key and stops
  when unavailable, demonstrating that earlier evidence controls later retrieval.
- Make the observation foreign-scope, deleted or superseded: it cannot authorize
  continuation; inaccessible intermediate evidence cannot be bypassed.
- Remove memory for the second task: it repeats the original fast strategy and
  failure. With the recorded episode it chooses normalization and succeeds.
- Change context: a previous episode no longer selects the alternate strategy.
- Remove authorization, exhaust budgets, cancel, fail recording or return a late
  retrieval: explicit terminal outcomes preserve observed-versus-unknown status.

Tests compare PostgreSQL outputs, traces and interventions with the deterministic
reference. The general W2 reference separately tests opposing path labels,
diamond paths, cycles, duplicates and a 10,000-edge hub.

## D. Evaluation protocol

The executable is `cmd/cognitivebench` in the existing cognitive storage spike.
Run from `dokja_store/spikes/cognitive_storage`:

```sh
go test -race ./cognitive ./multihop
go vet ./cognitive ./multihop ./cmd/cognitivebench
go run ./cmd/cognitivebench -engine memory -runs 100
# Set COGNITIVE_POSTGRES_DSN to a disposable local database with vector installed.
go run ./cmd/cognitivebench -engine postgres -runs 100
```

The PostgreSQL tests explicitly skip without `COGNITIVE_POSTGRES_DSN`. The harness
creates only temporary tables on its own connection. It neither creates the
extension nor resets application tables. An example disposable setup uses the
same `pgvector/pgvector:pg17` image as W1, localhost binding and temporary data.
Close the connection to discard experimental episodes and tables. No restart
recovery or durable acceptance is claimed.

Each measured condition runs 100 independent four-node fixtures, concurrency one.
Connection/table setup is outside the timed span. In the memory-reuse condition,
the first task is also outside that span, so its connection is warmer. Do not
interpret that condition's lower latency as a learning-induced speedup. All
conditions are synthetic, with zero LLM calls; token billing and general factual
accuracy are not evaluated. Capability-call counts and serialized trace bytes
serve as bounded cost proxies, not CPU or database memory measurements.

Single retrieval, eager fixed-depth two/three-hop retrieval and state-directed
iteration share the same evidence eligibility, domain policy and authorization
rules. The three-hop baseline is necessary: a shallow baseline alone would
manufacture an advantage for iteration. W1 vector results remain separate from
this causal evaluation.

### Measured results (2026-10-21)

PostgreSQL 17.11, pgvector 0.8.7, Linux x86_64; 100 repetitions per condition.
The fresh-store conditions perform first-use queries; the memory condition uses
a connection warmed by its prerequisite task. These are microbenchmarks, not
capacity measurements or independent task-quality samples.

| Condition | Hops | Policy iterations | Calls | Evidence recall | Computation success | p50 / p95 / p99 ms |
| --- | ---: | ---: | ---: | --- | --- | --- |
| single | 0 | 1 | 2 | 1/4 | 0/100 | 0.672 / 0.873 / 0.984 |
| bounded2 | 2 | 2 | 5 | 3/4 | 0/100 | 0.929 / 1.106 / 1.481 |
| bounded3 | 3 | 2 | 8 | 4/4 | 0/100 | 1.417 / 1.628 / 1.771 |
| iterative | 3 | 6 | 9 | 4/4 | 0/100 | 1.469 / 1.744 / 2.242 |
| iterative-memory | 3 | 6 | 9 | 4/4 | 100/100 | 0.714 / 0.929 / 1.011 |

Single and bounded2 correctly abstain; zero computation success does not mean
an unsafe decision. Bounded3 and iterative both authorize only a safe simulation,
whose fast parser then fails. Iterative-memory observes a successful normalized
sum in all repetitions. Fixed depth three uses one fewer prediction call than
iteration; this fixture supplies no benefit for adaptive retrieval over that
baseline. Retrieval repeats are zero by key deduplication; evidence recall is
measured against the four fixture nodes, not a real document corpus.

Frozen prediction Brier errors are 0.6400 before experience and 0.1225 after it.
These are deterministic fixture scores from hand-written probabilities, not
calibration, generalization, or statistical evidence of learned intelligence.
Traces average 447, 1629, 3128, 3724 and 3746 bytes respectively. The in-memory
control reproduces every outcome; its p95 values are 0.015, 0.025, 0.067, 0.053
and 0.074 ms. Database CPU/RSS, billing, cold-cache isolation and concurrent load
were not measured.

Validation: full storage spike `go test -race ./...` and `go vet ./...` passed
with the cognitive PostgreSQL tests enabled. Existing W1 PostgreSQL/Neo4j tests
were not enabled; W1 SQLite tests ran. The old orchestrator reference-loop tests
passed. Mutations disabling episode use or contrary-premise checks each caused
the corresponding causal regression test to fail; original code was restored.

Acceptance evidence: dependent keys and prediction revisions, first/second task
and no-memory control are in `TestDependentEvidenceAndReusableConsequences`;
evidence removal/contradiction/scope/version interventions have dedicated tests
in both adapters. Cycles, budgets, cancellation, absent authorization, late
responses and recording failures have explicit stopping tests. PostgreSQL
trace equivalence and first-observation retry behavior are also verified.


## E. Architectural recommendation and open work

Use the existing orchestrator-owned workspace and a domain-owned typed transition
policy as the next integration hypothesis. Preserve exact evidence/model versions
and distinguish unavailable, contrary and conflicting information. Keep prediction
hypothetical and action authorization independent. Read Experience through a
scoped port, and persist execution receipts before claiming durable learning.

The causal result supports reusing observed episodes in later decisions. It does
not establish a universal Knowledge/Experience split, calibrated prediction,
production graph schema, general factual reasoning, or the superiority of one
cycle owner. A three-hop bounded baseline can solve the same retrieval problem;
use iteration where an independently measured task needs adaptive evidence.

Pending: diverse held-out tasks, adversarial/stale experience, temporal validity,
model calibration, non-Boolean conflicting claims, oscillating hypotheses,
independent domain-internal implementation, SQL/Cypher bounded traversal at
scale, database resource/concurrency measurements, durable action recovery,
production authorization and desktop inspection. Step 10.1's SQLite/PostgreSQL/
Neo4j graph benchmark remains pending. This vertical slice does not replace it.

The ADR 0005 update is a review proposal grounded in these limits, not a new
production topology decision. Source references consulted: PostgreSQL 17 manual,
section 7.8 (recursive queries and cycle handling), and pgvector upstream README
(exact vector search and filtered queries); no recursive SQL performance claim
is made by this key-following experiment.
