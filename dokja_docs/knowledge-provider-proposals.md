# Provider-backed Knowledge application proposals

Step `08.2` now has `knowledge.application.propose`: a goal and selected documents
can produce a model-generated, cited method proposal through the actual
orchestrator and existing Runtime inference port. This extends
[preparation](knowledge-application.md) and [argument assessment](knowledge-applicability.md).
It does not establish that the model understood the documents or that the method
will work. Semantic acceptance, actual outcomes and episode reuse remain open.

## Request and workflow

Use the normal orchestrator request envelope with type
`knowledge.application.propose`. Its payload is the existing preparation shape:

```json
{
  "contract": "knowledge.application.context.v1",
  "goal": "Determine when the supplied retry procedure applies to this API",
  "constraints": ["Do not execute external actions"],
  "sources": [
    {"source_id": "note:retry-guide"},
    {"source_id": "note:api-contract"}
  ]
}
```

Optional `text_version` values reject changed documents as in preparation. No
provider, model, task ID, scope or execution flag is accepted in this payload.
Sources must already exist in Knowledge; Feeds acquisition and reading remain
separate orchestrated operations. Existing `prepare` and caller-supplied `assess`
behavior is preserved.

The application workflow:

1. Reads and validates selected documents through Knowledge and its existing
   service client. Missing, changed or oversized evidence stops before inference.
2. Builds a Knowledge-owned instruction/data boundary with the task ID, revision
   and loaded observations. Source text remains untrusted data. If the complete
   prompt exceeds its budget, stop without truncation or a provider call.
3. Makes one nonstreaming call to Runtime through the existing private inference
   socket. Runtime uses its currently selected chat provider/model and existing
   isolated, tool-disabled adapters. No fallback, repair call or hidden retrieval
   is added by this workflow.
4. Requires a strict JSON reply bound to this task and revision. Unknown fields,
   stale replies, malformed/trailing data and oversized replies are rejected.
5. Anchors model-supplied exact quotations to unique byte ranges in the current
   source versions, then runs the existing Knowledge argument assessor. Missing
   or ambiguous quotations, including overlapping duplicate matches, are rejected.

The proposal generator shares Runtime's existing chat inference binding. Configure
that binding through the existing Inference providers settings. Provider discovery
alone is not configuration. A disabled provider or missing model is rejected by
Runtime's existing rules. The Knowledge service switch suppresses this workflow;
the chat service switch controls chat events, not Knowledge's inference use.
No new environment variable, socket, provider registration or Compose service is
introduced. The simulation-only experimental inference contract is not reused.

## Proposal and result

The internal reply contract is `knowledge.application.proposal.v1`:
`contract`, `task_id`, `revision` (1), `disposition`, `summary`, `proposal`.
Disposition is `propose`, `clarify` or `abstain`. The summary is a bounded public
justification, not a request for private reasoning. For clarification/abstention,
`proposal` must be null and no method assessment is invented.

For `propose`, the quoted proposal has claims and one method with prerequisite
claim IDs, following the assessment contract. Citations contain `source_id`,
`text_version`, `quote` and `relation`. The model does not calculate byte offsets;
Knowledge anchors only unique verbatim quotations before validating the spans.
This transport format does not replace the caller-supplied assessment contract.

A successful request returns `status: proposed` and `proposal`, whose disposition
must be inspected. A clarification is a completed inference response, not an
applicable procedure. A method proposal includes its provisional `assessment`
with missing/conflicting/contradicted prerequisites or conditional applicability.
Every disposition retains `semantic_status: unverified`,
`requires_semantic_review: true`, `can_execute: false` and
`experience_status: not_consulted`.

On failure, return `status: not_proposed` and a task summary without partial source
bodies or an invalid proposal. Provider errors and invalid replies expose only
bounded attempt metadata and stable failure reasons, not raw provider diagnostics.
Accepted proposal metadata reports provider ID, **requested** model, elapsed time,
one port call and outcome. This is not confirmation of the resolved model or
billed usage. `input_tokens`/`output_tokens` remain null because Runtime's current
chat response does not report them. A cancelled/noncooperative step can leave an
unknown outcome in the task record; its late metadata/reply is discarded too.

## Bounds and verification

| Bound | Value |
| --- | --- |
| Selection, request, sources and source text | Existing preparation bounds |
| Prompt instruction + data content | 24 KiB |
| Provider reply | 16 KiB |
| Summary | Nonblank, at most 1200 UTF-8 bytes |
| Claims, citations, method prerequisites | Existing assessment bounds |
| Task steps / evidence / workspace | At most 10 / 8 / 512 KiB |
| Whole task / provider step | 90 / 60 seconds, shortened by caller deadline |
| Provider port attempts | At most 1; no retries or fallback |
| Canonical Knowledge/Memory writes / external actions | 0 / 0 |

These are byte/time bounds, **not enforced token ceilings**. Runtime's own process
output limits and tool/event validation also apply. Provider processes may use
isolated temporary files; this workflow creates no canonical claims, episodes,
procedures or successful-outcome records.

Domain regressions cover binding, strict decoding, quote anchoring/ambiguity,
reply/prompt budgets, clarification/abstention and preservation of caller-owned
selections. Adapter tests exercise the actual private HTTP socket with a fixture
server, two-role message boundary, one call, metadata, error privacy, cancellation
and size checks. Orchestrator tests enter through `ProcessWithResult` and cover
source reads → provider → assessor, one-attempt failures, oversized/missing
evidence, disabled Knowledge and a noncooperative late reply. Both Go module race
suites and static checks pass.

No live provider call or held-out comparison was run in this batch. The corpus,
cases and comparison rubric frozen in `8b9f331` remain unchanged. Token accounting
and enforceable token limits must be added before claiming compliance with that
comparison protocol. Independent semantic review, prerequisite completeness,
additional retrieval/revision, actual attempt/outcome recording and durable,
eligible episode reuse remain required in `08.2`/`08.3`.

Implementation: [Knowledge provider contract](../dokja_domain/dokja_knowledge/provider_proposal.go),
[Runtime client adapter](../dokja_orch/internal/clients/knowledge_proposal_client.go),
[application workflow](../dokja_orch/internal/handlers/knowledge_application.go),
[domain tests](../dokja_domain/dokja_knowledge/provider_proposal_test.go),
[socket tests](../dokja_orch/internal/clients/knowledge_proposal_client_test.go), and
[application-port tests](../dokja_orch/internal/handlers/knowledge_provider_proposal_test.go).
