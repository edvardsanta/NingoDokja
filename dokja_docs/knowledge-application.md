# Cross-document application: evidence preparation

Step `08.2` now has an opt-in application entry point,
`knowledge.application.prepare`. It prepares versioned observations for a goal;
claim extraction, method selection, applicability assessment and learning remain
unimplemented. This is the first implementation batch under the
[cognitive realignment](cognitive-realignment.md), not completion of that step.

## Request and result

Send the existing event envelope to the orchestrator request ingress, including
its normal authentication and interface identity. The new payload is:

```json
{
  "contract": "knowledge.application.context.v1",
  "goal": "Determine when the retry method can be applied to this API",
  "constraints": ["Do not execute external actions"],
  "sources": [
    {"source_id": "note:retry-guide"},
    {"source_id": "note:api-contract"}
  ]
}
```

The sources must already exist in Knowledge. Feeds remains the external intake
side of Knowledge: use the existing acquisition/reading workflows to ingest
observations first. Preparation reads selected documents through `knowledge.list`
with `include_text`; it does not fetch URLs, ingest files or implicitly search the
whole store. The existing Knowledge service client and its selected SQLite or
PostgreSQL backend are reused.

Within the normal Knowledge result, inspect `status`:

- `prepared`: `application` contains the contract, goal, constraints and ordered
  source observations. Each observation has `source_id`, `source_ref`, `title`,
  `text`, `text_version`, and `trust: unassessed_source_content`.
- `not_prepared`: there is no `application`. The task record explains a failed,
  stopped or cancelled preparation. Partial source texts are not returned.
- Invalid request fields fail before reading the store. A disabled Knowledge
  service retains the existing orchestrator skip behavior.

The `task` record includes task/attempt/event identity, ingress scope, step
records and evidence references. Outputs are omitted from this record to avoid
publishing duplicate or partial document bodies. Store errors use a bounded,
static diagnostic rather than returning provider connection details.

`text_version` is `sha256:` followed by the SHA-256 of the UTF-8 `text` returned
by the existing document listing. Send that value alongside `source_id` in a
later request to reject changed text. This fingerprints the returned text, which
may be reconstructed from chunks. It is not the ingest hash, a metadata revision,
a historical version lookup, or an atomic snapshot across multiple documents.
An unpinned selection observes the text available at that read. Metadata and
source locator changes are not covered by the text fingerprint.

## Ownership and limits

The existing `knowledge.*` route selects Knowledge; the workflow engine chooses
`prepare-knowledge-application`. The handler hosts a typed task through
`core.RunTask`. It schedules one read per selection, then context assembly.
Knowledge validates selection, checks identity/text versions, and assembles the
observations. Services continue performing storage/integration mechanics.

| Bound | Value |
| --- | --- |
| Request payload, JSON encoded | 8 KiB |
| Goal | 1..2000 UTF-8 bytes, excluding whitespace-only values |
| Constraints | At most 16, each 1..500 bytes and nonblank |
| Distinct selected sources / read calls | 1..8 |
| Returned text per source | At most 16 KiB; no truncation |
| Aggregate returned text | At most 64 KiB; no truncation |
| Source locator / title | At most 2048 / 1000 bytes |
| Task steps / evidence references | At most 9 / 8 |
| Encoded workspace | 512 KiB, including intermediate outputs |
| Task deadline | 15 seconds, or the caller's earlier deadline |
| Provider calls / external actions / persistent writes | 0 / 0 / 0 |

These are provisional preparation bounds, not a frozen model evaluation budget.
Encoded workspace limits can stop a request below the raw text ceiling, for
example when text needs extensive JSON escaping. A source read materializes a
service response before validation; these are retained-state limits, not a
streaming transport memory guarantee. Late replies are discarded by the task
runner even when an integration ignores cancellation. Read failures stop the
task without starting further reads.

Scope is copied from the ingress envelope, never accepted in this payload.
Knowledge currently has no per-document scope/access-control model: its evidence
remains explicitly unscoped, counted in task records. A task scope does not grant
new access or establish tenant isolation. Source text, including embedded
instructions, is returned as unassessed data and never interpreted as commands.

`applicability: unassessed` and `experience_status: not_consulted` are explicit
limits. Preparation creates no claims, concepts, methods, predictions or episodes.
It does not interpret user constraints, resolve contradictions, infer truth from
retrieval, or declare a procedure successful. It returns no learning result.

## Verification and next batch

Domain tests cover invalid/duplicate selections, contract and size limits,
missing/mismatched documents, changed pinned text, provenance handling and store
diagnostic privacy. Orchestrator tests enter through `ProcessWithResult` and
exercise two-document preparation, aggregate limits, disabled Knowledge, failure
without partial publication and cancellation of a noncooperative integration.
The existing route baseline includes the new opt-in action.

Next, define the source-span/claim/method/applicability proposal contract and
freeze a representative nonpersonal corpus, held-out application cases, rubric
and comparison budget before invoking providers. Test supported application,
missing preconditions, contradictions, stale/foreign evidence and legitimate
abstentions. Actual attempt/outcome recording, eligible episode reuse and durable
restart controls remain required before step `08.3` can evaluate cognition.

No event envelope, existing action, service transport, schema, environment,
Compose service or running data is changed by this batch. Preparation stores no
new canonical state; the existing inference experiment's simulation-only
experience contract is not reused for real application episodes.

Implementation: [Knowledge contract](../dokja_domain/dokja_knowledge/application.go),
[orchestration task](../dokja_orch/internal/handlers/knowledge_application.go),
[domain tests](../dokja_domain/dokja_knowledge/application_test.go), and
[application-port tests](../dokja_orch/internal/handlers/knowledge_application_test.go).
