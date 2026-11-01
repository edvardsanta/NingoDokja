# Adaptive retrieval for Knowledge application

Step `08.2` adds `knowledge.application.propose.adaptive`: a proposal that finds its own
evidence from the goal, then retrieves once more if the assessment shows a gap. It sits
beside [`propose`](knowledge-provider-proposals.md), which keeps its selected documents.

## Request

Use the normal orchestrator envelope. There is no `sources` field:

```json
{
  "contract": "knowledge.application.adaptive.v1",
  "goal": "Determine when the supplied retry procedure applies to this API",
  "constraints": ["Do not execute external actions"]
}
```

## Workflow

| Step | What happens |
| --- | --- |
| `retrieve-1` | Search on the goal and read up to 3 relevant documents. With none, stop with `no_relevant_sources` and call no provider |
| `proposal-1` | One measured call. A failure stops the task, with its usage kept |
| `retrieve-2` | Knowledge decides by rule if there is a gap, then searches on the gap's text for up to 2 new documents |
| `proposal-2` | One measured call bound to revision 2, with all documents read |
| `result` | Publish the latest valid proposal with its full account |

A gap is `needs_evidence` or `clarify`. A conflicted, blocked or abstained result is not a
gap, because more text must not paper over it. The query comes from the claims the
assessor reports as missing, or the summary if there are none.

The search rank is a retrieval signal only. It says nothing about reliability, and
documents from a search reach the same assessment as selected ones.

## Result

`status: proposed` returns the usual `proposal` plus:

| Field | Meaning |
| --- | --- |
| `strategy` | `adaptive` |
| `stop_reason` | `resolved`, `revised`, `no_new_evidence`, `time_exhausted` or `revision_failed` |
| `rounds` | Per round: query, documents read and skipped, inference, disposition, applicability |
| `inference` | Total calls and usage. A total is null if any call's count is unknown |

`semantic_status: unverified`, `can_execute: false` and `experience_status: not_consulted`
stay as they are for `propose`. `revision` says which attempt was published.

If the second call fails, the first proposal is published with `revision_failed` and the
failed attempt stays counted. A second attempt starts only with at least 45 seconds left.

## Bounds

| Bound | Value |
| --- | --- |
| Documents | 3 first, 2 more; each at most 16 KiB and 18 KiB in all |
| Search | 12 hits per search, 2 searches |
| Gap query | 1000 UTF-8 bytes |
| Provider calls | At most 2, no fallback to unmeasured inference |
| Token ceilings | Existing 12000 input / 6000 output per call |
| Whole task / provider step | 90 / 60 seconds, shortened by the caller deadline |
| Canonical writes / external actions | 0 / 0 |

The action needs the matching orchestrator and a measured provider. It changes no
existing action, response, envelope, port, environment variable, Compose service or schema.

## Limits

- Documents over 16 KiB are skipped and reported, so books cannot be used yet.
- Knowledge has no per-document access model, so a search covers the whole Library. Only
  the hashtag examples kind is excluded. Other documents Ningo keeps for itself can appear,
  which is one reason nothing calls this action in a real conversation.
- A `clarify` often needs facts only the user has, so its second search can be wasted.
- A failed second call does not change the first proposal, which may still be incomplete.

Comparison results are in [the harness doc](knowledge-comparison.md).

Implementation: [domain rules](../dokja_domain/dokja_knowledge/adaptive.go),
[workflow](../dokja_orch/internal/handlers/knowledge_adaptive.go),
[domain tests](../dokja_domain/dokja_knowledge/adaptive_test.go) and
[application-port tests](../dokja_orch/internal/handlers/knowledge_adaptive_test.go).
