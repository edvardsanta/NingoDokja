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

## Running it

From `dokja_orch`, with a measured provider selected in Runtime (`codex` or
`claude_code`):

```sh
go run ./cmd/knowledge-comparison -split development -runs 2 -out ../test_reports/comparison-dev.jsonl
```

| Flag | Default | Meaning |
| --- | --- | --- |
| `-split` | `development` | `development` or `held_out` |
| `-strategy` | `fixed` | Only `fixed` exists; others are refused |
| `-runs` | 2 | Independent runs per case |
| `-case` | all | One case, development only |
| `-review` | true | Also request a semantic review, reported apart |
| `-allow-held-out` | false | Required for `held_out` |
| `-out` | required | New JSONL report |
| `-fixtures` | repository path | Frozen fixture directory |

`DOKJA_INFERENCE_SOCKET` selects the Runtime socket, as it does for the orchestrator.

## What it enforces

- Fixture digests must match the frozen files, or the run is refused.
- `held_out` needs explicit consent and runs every case exactly twice.
- A plan above the 64-call ceiling is refused. Review calls are counted apart.
- A report is never overwritten, and each run is written as soon as it ends.
- The corpus is served by an in-memory port that refuses everything except
  `knowledge.list`, so a run cannot read or change the Library.
- Failed runs stay in the report. Unknown usage is counted on its own, never as zero.

A record holds the case, run, protocol revision, fixture digests, status, stable
reason, provider, requested model, usage, elapsed time, the proposal and its review.
`rubric: pending` marks every record, because the answer rubric needs independent
scoring. `strategy_calls` counts attempts, so it includes calls refused before launch.

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

## Not done

- The adaptive strategy does not exist, so no comparison has run.
- Held-out cases have not been run. Run them once the adaptive strategy exists.
- Rubric scoring is manual and independent, and nothing here automates it.

Implementation: [harness](../dokja_orch/cmd/knowledge-comparison/comparison.go) and
[tests](../dokja_orch/cmd/knowledge-comparison/comparison_test.go).
