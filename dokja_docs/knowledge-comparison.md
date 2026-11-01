# Knowledge application comparison harness

`dokja_orch/cmd/knowledge-comparison` runs the frozen cases from
[the applicability protocol](knowledge-applicability.md) through the real Knowledge
handlers, the measured client and the Runtime inference socket. It records every run
for later scoring. It does not score answers.

## Protocol revision 2

Revision 1 was frozen in `8b9f331`. Revision 2 changes one item, before any held-out
run and on the owner's decision:

| Item | Revision 1 | Revision 2 |
| --- | --- | --- |
| Per-call output ceiling | 2000 tokens | 6000 tokens |

Reported output includes model thinking. All 6 real `dev-supported` proposals used
3188 to 3867 tokens, so revision 1 could not reach its target with that model. The
evidence is [these development measurements](knowledge-provider-proposals.md#real-proposals-on-the-development-cases).
The corpus, cases, input ceiling, call ceilings, run counts, rubric and pass target are
unchanged, and no held-out answer informed the change.

## Protocol revision 3

Revision 1 said "bounded adaptive retrieval, at most 3 calls" and did not define what
adaptive starts from. That choice decides what the comparison measures, so the owner
fixed it before any comparison run. No held-out goal or answer informed this definition.

| Item | Definition |
| --- | --- |
| Start | The goal alone. The case's document selection is not used |
| First retrieval | Search on the goal, then read the top 3 relevant documents |
| Gap | By rule: `needs_evidence`, or `clarify`. Conflicted, blocked and abstained are not gaps |
| Second retrieval | One search on the gap's own text, reading at most 2 new documents |
| Provider calls | At most 2, inside the original ceiling of 3 |
| Search in the comparison | Deterministic stand-in over the 6 frozen documents |

Real proposals take about 30 seconds, so a third call would not fit the unchanged
90-second run. A gap is found by rule for the same reason, and costs no call.

The stand-in ranks by distinct shared terms, with corpus order breaking ties. It keeps
runs comparable, but it is not the real hybrid search, so results say nothing about
real ranking quality. Treat that as a limit of every adaptive result.

## Running it

From `dokja_orch`, with a measured provider selected in Runtime (`codex` or
`claude_code`):

```sh
go run ./cmd/knowledge-comparison -split development -runs 2 -out ../test_reports/comparison-dev.jsonl
```

| Flag | Default | Meaning |
| --- | --- | --- |
| `-split` | `development` | `development` or `held_out` |
| `-strategy` | `fixed` | `fixed` or `adaptive` |
| `-runs` | 2 | Independent runs per case |
| `-case` | all | One case, development only |
| `-review` | true | Also request a semantic review, reported apart |
| `-allow-held-out` | false | Required for `held_out` |
| `-provider`, `-model` | none | Binding recorded before the first call; required for `held_out` |
| `-out` | required | New JSONL report; the sheet goes beside it as `.review.md` |
| `-fixtures` | repository path | Frozen fixture directory |

`DOKJA_INFERENCE_SOCKET` selects the Runtime socket, as it does for the orchestrator.

## What it enforces

- Fixture digests must match the frozen files, or the run is refused.
- `held_out` needs explicit consent and runs every case exactly twice.
- A plan above the 64-call ceiling is refused, counting each strategy's maximum calls.
  Review calls are counted apart.
- A report is never overwritten, and each run is written as soon as it ends.
- The corpus is served by an in-memory port that refuses everything except
  `knowledge.list` and `knowledge.search`, so a run cannot read or change the Library.
- Failed runs stay in the report. Unknown usage is counted on its own, never as zero.

The report opens with a header holding the protocol revision, split, strategy, binding,
cases and fixture digests, written before any provider call. A run whose reply names
another binding is flagged and counted in `binding_mismatches`.

A record holds the case, run, protocol revision, fixture digests, status, stable
reason, provider, requested model, usage, elapsed time, the proposal, its review and
every raw reply, including refused ones. `rubric: pending` marks every record, because
the answer rubric needs independent scoring.

The `.review.md` sheet is for a scorer who did not write the proposals. It holds the
expected answer, which never reaches a model, and the rubric boxes. It leaves out the
model reviewer's verdict. Neither file is overwritten. `strategy_calls` counts attempts, so it includes calls refused before launch.

## Development run

Run on 2026-10-31 with `claude_code` and `sonnet`, two runs per development case,
through the real handlers, measured client, private socket, Runtime handler, Runtime
domain and Runtime adapter. The host was a temporary process using those real parts on
a private socket, with a pinned binding in place of settings, because the operator's
selected chat provider does not report token usage. The operator's Runtime and settings
were not changed. This was not the full orchestrator process.

| Case | Disposition | Applicability | Output tokens | Proposal time | Review |
| --- | --- | --- | --- | --- | --- |
| `dev-supported` run 1 | `propose` | `conditional` | 3117 | 29 s | `needs_revision`, 40 s |
| `dev-supported` run 2 | `propose` | `conditional` | 3135 | 29 s | `needs_revision`, 36 s |
| `dev-missing` run 1 | `clarify` | | 331 | 5 s | |
| `dev-missing` run 2 | `clarify` | | 331 | 5 s | |

Input was about 4800 tokens per proposal and 7500 per review. Both reviews stopped on
an uncovered date-window condition. Counting the earlier probes, same-model review has
not yet accepted a real proposal; whether that is right or too strict is an `08.3`
question. The rubric has not been applied to these answers.

## Adaptive development run

Run on 2026-11-01 under revision 3, on the same kind of temporary host, two runs per
development case. These are development results, and the rubric was not applied.

| Case | Round 1 | Round 2 | Final | Calls | Time |
| --- | --- | --- | --- | --- | --- |
| `dev-supported` run 1 | `conditional` | not needed | `conditional` | 1 | 26 s |
| `dev-supported` run 2 | `needs_evidence` | Read the foreign and unresolved notes | `conflicted` | 2 | 64 s |
| `dev-missing` run 1 | `clarify` | Read `api-v1` and the unresolved note | `clarify` | 2 | 10 s |
| `dev-missing` run 2 | `clarify` | Read `api-v1` and the unresolved note | `clarify` | 2 | 9 s |

The goal search found the three relevant documents in all four runs, with this stand-in
search. All runs fit the 90-second limit and the token ceilings. Second retrieval helped
none of these runs, and in `dev-supported` run 2 it moved a mostly supported proposal to
`conflicted`. On `dev-missing` it spent a call on facts only the user can supply. Whether
adaptive retrieval earns its cost is what the held-out comparison decides. Both reviews
ended `needs_revision` on an uncovered date condition, as with the fixed strategy.

## Not done

- The held-out comparison has not run, for either strategy.
- Rubric scoring is manual and independent. The sheet prepares it and nothing automates it.

Implementation: [harness](../dokja_orch/cmd/knowledge-comparison/comparison.go) and
[tests](../dokja_orch/cmd/knowledge-comparison/comparison_test.go).
