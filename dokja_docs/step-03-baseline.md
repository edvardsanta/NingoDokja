# Step 03: behavior baseline and evaluation protocol

Date: 2026-10-18. Tracker: [steps.json](../steps.json), step `03`.
Decisions: [index](./decisions/README.md).

This report records how the current system behaves before ADRs 0006 and 0007
move anything, how later steps will be measured, and what the ADR 0005 reference
experiment showed. It describes behavior pinned by tests; it is not a runtime
audit of a deployed stack.

## 1. Behavior baselines

Each row names the tests that pin the behavior a migration must keep behind its
facade, or change deliberately.

| Boundary | Tests | Pinned behavior worth knowing |
| --- | --- | --- |
| Public feature events | [feature_routes_baseline_test.go](../dokja_orch/internal/core/feature_routes_baseline_test.go) | Every `book.*`, `meme.*`, `digest.*` and `knowledge.*` event and `meme.dispatch.scheduled` keeps its workflow, domain and action |
| Hashtag suggestion | [meme_hashtag_baseline_test.go](../dokja_orch/internal/handlers/meme_hashtag_baseline_test.go), [meme_hashtag_workflow_test.go](../dokja_orch/internal/handlers/meme_hashtag_workflow_test.go), [hashtag_experience_test.go](../dokja_orch/internal/handlers/hashtag_experience_test.go) | Below the threshold the nearest label is still returned with `relevant=false` and no reason; missing or unreadable media text never reaches Knowledge; search is bounded by 5 s; Memory failures never escape |
| Book | [test_book_summary_service.py](../dokja_services/dokja_book/test_book_summary_service.py), Book client/domain/handler Go tests | Classify and summarize response fields and the `pt-BR`/`study` defaults; summarize without content is refused. The strategy fields have no consumer (ADR 0006) |
| Meme pool | [test_pool_baseline.py](../dokja_services/dokja_meme/tests/test_pool_baseline.py), [test_attachment_fetch.py](../dokja_services/dokja_meme/tests/test_attachment_fetch.py) | Read-only `meme.fetch`/`meme.list` after the approved 09.4-h change; screening labels but does not filter; an unavailable screen fails closed without blocking |
| Digest | [Digest domain tests](../dokja_domain/dokja_digest/domain_test.go), [handler tests](../dokja_orch/internal/handlers/digest_domain_handler_test.go) | Freshness window, newest first, duplicate removal, source balance, stable paging, and the Feeds switch skipping the digest |

This records the original baseline: a sent mark was a fetch side effect rather
than delivery evidence. The owner approved changing it in step 09.4-h:
[fetch is now read-only and receipts require confirmed delivery](media-delivery-receipts.md).
The referenced tests now assert that revised contract. Historical flags are
preserved and must not be reinterpreted as verified delivery evidence.

## 2. Plugin compatibility and failure baseline

Existing tests already pin the plugin hosts that ADR 0006 merges into the
reading host. Step `05` adapters must keep them passing.

| Host | Tests | Behavior |
| --- | --- | --- |
| Feeds manifests | [manifest_test.go](../dokja_services/dokja_feeds/feeds/manifest_test.go) | Safe defaults; unknown fields refused; a command must be a runnable file inside the plugin directory; unusable manifests reported with a reason |
| Feeds runner | [runner_test.go](../dokja_services/dokja_feeds/feeds/runner_test.go) | Timeout kills the whole process group; output over the limit is stopped; only the manifest's environment reaches the plugin; stderr stays out of errors |
| Feeds service | [service_test.go](../dokja_services/dokja_feeds/feeds/service_test.go) | Last good result survives a failed run; one failing or hanging plugin delays or hides no other; disabled plugins never run |
| Knowledge extraction | [test_extractors.py](../dokja_services/dokja_knowledge/tests/test_extractors.py), [test_fetch.py](../dokja_services/dokja_knowledge/tests/test_fetch.py) | Damaged or unsafe files are reported, not raised; a crashing extractor becomes an extract error; fetching refuses non-public addresses and oversized bodies |
| Knowledge plugins | [test_sources.py](../dokja_services/dokja_knowledge/tests/test_sources.py), [test_example_plugin.py](../dokja_services/dokja_knowledge/tests/test_example_plugin.py) | Bad plugins are skipped at load; a failing fetcher plugin is reported without leaking its message; the example plugin cannot read outside its folder |

Neither host isolates a hanging in-process Knowledge plugin or a native crash;
that gap is recorded in the [research](./cognitive-macrocomponents-research.md)
and is part of the step `07` experiment.

## 3. Measurement protocol

Later steps compare the existing path, the workspace path and the workspace with
shadow prediction on the same fixed inputs. Measure:

| Quantity | Method |
| --- | --- |
| Latency | Wall time per task and per operation, p50 and p95 over at least 30 runs of each fixed fixture against the lite stack, with warm caches reported separately from cold |
| Invocation count | Counting wrappers at the orchestrator client layer (`dokja_orch/internal/clients`), per capability and per task |
| Output size | JSON bytes of each capability response and of the task's final result |
| Scope isolation | Two concurrent tasks with different actor/channel against a store holding both; zero cross-scope evidence in either trace, and foreign items reported as dropped |

Agree the latency and invocation budgets from the measured existing path before
judging an experiment. Synthetic fixtures verify mechanics and overhead only.

Prediction evaluation follows [ADR 0004](./decisions/0004-shadow-prediction.md):
frozen snapshots, Brier score against the frozen baseline, unknown feedback is
not negative, and sample sizes are reported with every calibration summary.

## 4. ADR 0005 reference experiment

[cognitiveloop](../dokja_orch/internal/experiments/cognitiveloop/) runs one pure
domain policy under three owners of the cycle, over deterministic fake
capabilities. Nothing in the orchestrator imports it.

- **A, Supervised:** each retrieval, prediction and acquisition is an
  orchestrated step; Knowledge and Experience are re-read only after new
  evidence.
- **A, rereading:** the same without snapshot reuse.
- **B, DomainInternal:** the orchestrator snapshots once per round; the domain
  revises and predicts internally and returns only to request evidence.

Run with `go test ./internal/experiments/cognitiveloop -run TestCompareTopologies -v`
in `dokja_orch`.

### Result

All three reach the same outcome and candidate in all 15 scenarios: cold start,
knowledge-only, experience-driven candidate, missing premise, premise exposed by
prediction, contrary evidence (fallback and abstention), revision, stagnation,
oscillation, dependency failure, late result, iteration budget, foreign scope
and hypothetical predictions. Equivalent book, text and feed content selects the
same method with its own provenance, concurrent tasks share no state, and only
an observed outcome becomes experience.

Operation counts are identical except where the domain revises without new
evidence:

| Scenario | A, Supervised | A, rereading | B, DomainInternal |
| --- | --- | --- | --- |
| A poorly predicted procedure is revised away | 4 operations, 531 bytes | 6 operations, 934 bytes | 4 operations, 531 bytes |

Snapshot reuse removes the only cost difference. Under these fakes the
topologies are equivalent in outcome and cost.

### Limits

- **B shares A's supervision code.** The domain-internal loop writes the same
  workspace and goes through the same call wrapper for budget, deadline, late
  results and scope filtering. Its trace completeness, cancellation and budget
  behavior are therefore equal by construction, not measured. A real B would
  have to emit its trace, honor cancellation and report its spend itself.
- **Latency is not measured.** The fakes answer instantly; operation count is
  the only cost proxy.
- **Code coupling is not measured.**
- **Retried product actions are not covered, by design:** the loop has no action
  port and never executes a side effect.

### Recommendation for ADR 0005

Make the orchestrator-supervised loop (A, with snapshot reuse) the cycle owner.
It matches B on every measured outcome and cost, and it keeps supervision,
budgets, cancellation and trace in one place that already owns workflows, rather
than reimplementing them in each faculty domain. A domain may still revise over a
snapshot it was given within one step, as an optimization, provided it returns
its trace and spend. That is not a separate cycle owner.

The owner accepted this recommendation; ADR 0005 records the decision.

## 5. Checks

| Check | Result |
| --- | --- |
| `go vet ./...` and `go test ./... -count=1` in `dokja_orch` | Passed |
| `go test -race` for the experiment and the hashtag baseline | Passed |
| Meme service `pytest tests/` (dokja-lab-ci conda env) | 12 passed |
| Book service tests | 7 passed with a stub `transformers` module injected for the run; the env lacks `transformers` and the tests inject a fake classifier, so the real zero-shot model was not exercised |
| Feeds and Knowledge plugin tests | Cited, not re-run in this step |
| Live stack measurements | Not run; the protocol above defines them for later steps |
