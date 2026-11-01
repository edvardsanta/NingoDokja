# Semantic review of Knowledge application proposals

Step `08.2` adds `knowledge.application.review`: one measured model opinion on a
candidate that already passed the [argument assessor](knowledge-applicability.md).
It looks for what citation checks cannot see, such as a real quotation applied to
the wrong region, a prerequisite left out, or a date outside the allowed window.
A review is an opinion. It verifies nothing, executes nothing and writes nothing.

## Where it sits

The review is a separate opt-in action with its own task. `propose`, `assess` and
their responses are unchanged: they still return `semantic_status: unverified` and
`requires_semantic_review: true`.

Keeping it separate protects the [frozen comparison](knowledge-applicability.md):
a fixed strategy remains one provider call per run. When the comparison uses this
review, its calls are reported apart from strategy calls and do not count against
the strategy ceilings.

## Request

Use the normal orchestrator envelope with type `knowledge.application.review`:

```json
{
  "contract": "knowledge.application.review.request.v1",
  "assessment": {"contract": "knowledge.application.assessment.v1"},
  "proposed_by": {"provider": "codex", "requested_model": "example-model"}
}
```

`assessment` is the complete `knowledge.application.assess` payload, with pinned
sources, claims and method. A caller can build it from the claims and method in a
`propose` result. `proposed_by` is optional and caller-stated.

## Workflow

1. Re-read the pinned sources. A missing or changed source stops the task.
2. Run the deterministic assessor. A failure stops before any provider call.
3. Make one measured call with no fallback. The prompt holds the goal, constraints,
   sources, claims and method. The proposer's summary is left out.
4. Require a strict reply bound to this task and candidate, then anchor its
   quotations with the same unique-verbatim rule as proposals. One enclosing
   markdown fence is removed first, because real replies carried one despite the
   instruction. This applies to reviews only.
5. Derive the outcome by rule.

The reviewer lists the conditions a method needs before judging the candidate, then
marks which ones the candidate covers. That order is what looks for omissions, but it
is only an instruction: the candidate is in the same prompt, so this is not a blind
derivation.

## Result

`status: reviewed` returns `review`:

| Field | Meaning |
| --- | --- |
| `outcome` | `accepted`, `needs_revision` or `rejected`, derived by rule |
| `outcome_reasons` | Stable codes such as `condition_uncovered`, `prerequisite_refuted` |
| `reviewer_verdict`, `reason` | What the reviewer said, kept apart from the outcome |
| `required_conditions`, `uncovered_conditions` | Conditions, anchored citations and coverage |
| `claims`, `method` | One verdict and reason each |
| `unaddressed_conflicts` | Source text the candidate ignored, with citations |
| `assessment_applicability` | The deterministic assessor's result |
| `independence` | `same_model`, `different_model` or `unknown` |
| `inference` | Provider, requested model, calls, elapsed time and token usage |

`semantic_status` is `model_reviewed`, never a verified state. `can_execute` is always `false` and
`experience_status` is `not_consulted`.

The rule can only be stricter than the reviewer:

| Outcome | When |
| --- | --- |
| `rejected` | The reviewer rejects; the assessor reports `blocked`; or the method or a prerequisite claim is `misread`, `out_of_scope` or `stale` |
| `needs_revision` | Otherwise, for an unsupported method or claim, a refuted claim the method does not depend on, an uncovered condition, an unaddressed conflict, an incomplete assessment, or a reviewer request |
| `accepted` | None of the above |

A condition counts as covered only through the method or a claim the method depends
on. A claim beside the argument does not count.

`independence: same_model` is the expected case today, because Runtime has one chat
binding. A model reviewing its own proposal shares its blind spots, so treat that
result as weak evidence. `unknown` means the caller did not state `proposed_by`.

On failure the result is `status: not_reviewed` with the task summary and any usage.
Stable reasons are `provider_unavailable`, `token_budget_exceeded`,
`token_usage_unavailable` and `invalid_provider_review`. No partial review is returned.

## Bounds

| Bound | Value |
| --- | --- |
| Request | 33 KiB, with the existing assessment limits inside |
| Prompt | 40 KiB |
| Reply | 16 KiB |
| Acceptance budget | 24000 input / 12000 output tokens, after generation |
| Conditions / conflicts / citations each | 1..16 / 0..8 / 0..4 |
| Condition or conflict statement / reason | 600 / 400 UTF-8 bytes |
| Whole task / provider step | 90 / 85 seconds, shortened by the caller deadline |
| Provider attempts | Exactly 1, no retries or fallback |
| Canonical writes / external actions | 0 / 0 |

These budgets are separate from the proposal ceilings of 12000 and 6000. A full 40 KiB prompt plus the CLI's own context does not fit 12000 input
tokens, so the shared ceiling would spend a call and then always refuse large reviews. The review path is measured only, so it needs `codex` or `claude_code`;
`ollama` is refused before launch, like the measured proposal path.

The new action needs the matching orchestrator. No envelope, existing action,
existing response, port, environment variable, Compose service or schema changes.

## Verification

Domain tests cover the outcome rule, coverage, strict decoding, quotation anchoring,
fences and independence. Socket tests cover the separate bounds. Application tests
enter through `ProcessWithResult` and cover one measured call, counted failures,
changed sources, a missing reviewer and a late reply. These use fixture replies, so
they prove the plumbing and the rule, not the quality of a review.

One real-model probe was run on 2026-10-31. It called the domain's prompt builder and
decoder with the `claude` CLI using Runtime's isolation flags and `sonnet`. It did not
go through the orchestrator or Runtime. Candidates were written by hand on the two
development cases, each run twice, so a model did not review its own proposal:

| Candidate | Expected | Runs |
| --- | --- | --- |
| Good, `dev-supported` | `accepted` | 1 `accepted`, 1 `needs_revision` |
| Window and attempt limit omitted | Not accepted | 2 `needs_revision` |
| Goal changed to west | Not accepted | 2 `rejected` |
| Facts assumed in `dev-missing` | Not accepted | 2 `needs_revision` |
| Goal changed to status 422 | Not accepted | 2 `rejected` |

All 10 replies decoded. The prompt was changed between earlier runs of this probe, so
these are development results and not an evaluation. The good candidate's `accepted`
run took 31 seconds, with 5595 input and 3329 output tokens. Its other run stopped on
two uncovered conditions, one of them the no-execution constraint that the
instructions exclude, so false `needs_revision` results should be expected. The
frozen held-out cases were not used.

An earlier run in this probe, under weaker isolation, took up to 163 seconds. That
exceeds the 90-second task limit and would be recorded as a failure.

A second probe chained real proposals into real reviews on `dev-supported`, three
times, with the same model. One reply listed the method among the claims and was
refused, and the instructions were then corrected; that correction has not been
rerun. The other two ended `needs_revision`, at 39 seconds, about 7300 input and 4600
to 4900 output tokens. One flagged effective dates the candidate never mentions and
one flagged an endpoint the goal never names. No real proposal was accepted, so
this review is strict on these cases, and whether it is too strict is unmeasured.
The [comparison harness](knowledge-comparison.md) later ran two more reviews through the
Runtime path, with the corrected instruction. Both decoded and ended `needs_revision`.

## What this does not establish

- That the review is reliable. Repeated runs, held-out cases and same-model
  proposals belong to `08.3`.
- That a review improves decisions over the proposal alone.

Implementation: [domain contract and rule](../dokja_domain/dokja_knowledge/semantic_review.go),
[application workflow](../dokja_orch/internal/handlers/knowledge_application.go),
[measured client](../dokja_orch/internal/clients/measured_knowledge_proposal_client.go),
[domain tests](../dokja_domain/dokja_knowledge/semantic_review_test.go) and
[application-port tests](../dokja_orch/internal/handlers/knowledge_semantic_review_test.go).
