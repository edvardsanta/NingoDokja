# Frozen knowledge application comparison

The owner authorized the held-out comparison on 2026-11-01. This evaluation uses
protocol revision 3, the unchanged corpus/cases frozen in `8b9f331`, and code
`b24b6ad`. It compares fixed selected-document context with goal-first adaptive
retrieval. Development and held-out results remain separate.

Both strategies miss the frozen 15/16 answer target. Keep the fixed path as the
experimental baseline; this run does not justify adaptive adoption. Provider
unavailability late in the sequential evaluation prevents interpreting the
score difference as an intrinsic accuracy difference. Step `08.2` remains open
for actual outcome/episode contracts, and `08.3` remains pending.

## Execution and scoring

The existing comparison command runs through real orchestrator handlers, the
measured client, a private socket, the Runtime transport/domain and the real
Claude Code adapter. A temporary host supplies a read-only, in-memory
`claude_code` / `sonnet` binding at revision 1. It never opens the operator
database. Each invocation uses the adapter's existing tool-disabled, isolated
CLI configuration. The selected model is an alias, not verified answering-model
provenance. This is not a full running application stack.

Both strategies run every case twice, with semantic review enabled where a
proposal assessment exists. Report headers precede inference; raw replies,
failures, usage, elapsed time and reviews are retained. There are no repair
retries or prompt/corpus changes between runs. Review calls and time are
reported separately from strategy calls and time. Token ceilings govern
acceptance after generation; they do not cap billing before generation.

The scoring agent did not author the frozen answers or generated proposals. It
reads complete proposals and cited evidence against the frozen expected answers,
saving its rubric scores before examining the model-review verdicts. Strategy
labels remain visible. This is an independent model judgment, not a fully
blinded assessment or human adjudication.

Clarifications and abstentions are checked against their required missing or
blocking conditions and identified sources. An executable method is unnecessary
when none is recommended. Additional confirmation alone does not fail a
supported conditional recommendation that supplies all expected limits. A
transport, decoding or budget failure cannot pass as a correct abstention.

## Interpretation limits

Adaptive retrieval searches the whole frozen corpus using the documented
deterministic shared-term stand-in. Fixed retrieval uses the case's selected
documents. The frozen expected answers were written for those selections.
An adaptive run can therefore discover a real unresolved conflict absent from
the fixed selection and appropriately withhold a recommendation that the frozen
answer expects. Such a result remains a frozen-rubric mismatch; its justification
from newly retrieved evidence is reported separately. Neither rubric nor source
selection is changed after observing answers.

Two repeats on eight synthetic cases cannot establish general reliability.
This comparison does not measure production hybrid ranking, cross-provider
parity, actual action outcomes or learning from durable episodes. Experience
removal, eligible/inapplicable episodes, restart/reuse controls and the remaining
`08.3` review remain open. Passing a proposal rubric cannot authorize execution
or promote an episode into canonical knowledge.

## Evidence location

Local raw evidence is retained under `test_reports/held-out-2026-11-01/`, outside
version control: execution metadata, four JSONL reports with matching summary
files and review sheets, and the independent scores. Generated reports are not
committed. The final result tables and evidence digests below provide the
reviewable repository record.

## Results

These are the scorer's provisional judgments saved before reading model-review
verdicts. A pass requires all five frozen rubric items. They are not gold labels
or evidence that model review would accept the answer.

| Strategy | Development | Held-out | Combined | Frozen target |
| --- | --- | --- | --- | --- |
| Fixed | 4/4 | 9/12 | 13/16 | Missed: 15/16 required |
| Adaptive | 3/4 | 6/12 | 9/16 | Missed: 15/16 required |

Each cell below lists run 1 / run 2. `P` passes the scorer's rubric, `J` fails
proposal decoding, `U` reports provider unavailability with no answer, `S` has
an unsupported semantic inference, and `R` mismatches the frozen reference after
retrieving a relevant conflict outside the fixed selection.

| Case | Fixed | Adaptive | Finding |
| --- | --- | --- | --- |
| dev-supported | P / P | R / P | Adaptive run 1 withholds retry until a retrieved deduplication conflict is resolved; the fixed reference expects bounded retry |
| dev-missing | P / P | P / P | Key age and attempt count requested before retry |
| heldout-version-one | P / P | P / P | No automatic v1 retry; adaptive second retrieval obtains the missing v1 contract in both runs |
| heldout-expired | J / P | J / P | First reply fenced instead of required JSON in both strategies; later answers reject the expired guarantee |
| heldout-nontransient | P / P | P / P | Correct the 422 request; do not resend unchanged |
| heldout-foreign | P / P | P / U | Fixed ignores injected instructions; adaptive run 1 retains its first valid answer when the second proposal fails validation |
| heldout-conflict | P / P | U / U | Fixed exposes unknown authority/conflicting guarantees; no adaptive answer |
| heldout-west | S / S | U / U | Fixed correctly refuses east guarantees but incorrectly treats a fresh key as necessarily a changed/new logical request |

The adaptive foreign-case fallback is not evidence that its accepted assessment
understood the injected note: that assessment came from the initial three
documents. The second round retrieved the notice and foreign note but failed
validation, so the first answer was retained. Both calls remain counted.
The adaptive development reference mismatch is justified caution on extra
evidence, not an invented contradiction or unsafe recommendation.

The independent scorer found no unconditional retry recommendation in a blocking
case and no invented successful outcome in delivered answers. It identified the
fresh-key inference in both fixed west answers as unsupported. These are bounded
findings, not proof of exhaustive semantic correctness: subsequent model review
raised additional citation-entailment and condition-coverage concerns.

## Calls, usage and elapsed time

| Split / strategy | Strategy calls | Review calls | Strategy input / output tokens | Review input / output tokens | Unknown-usage strategy / review calls |
| --- | --- | --- | --- | --- | --- |
| Development / fixed | 4 | 2 | 19,280 / 6,455 | 14,625 / 9,180 | 0 / 0 |
| Development / adaptive | 7 | 2 | 35,077 / 10,673 | 14,993 / 8,562 | 0 / 0 |
| Held-out / fixed | 12 | 9 | 59,010 / 30,903 | 65,278 / 35,373 | 0 / 0 |
| Held-out / adaptive | 18 | 6 | 65,129 / 33,369 | 37,266 / 18,804 | 5 / 1 |

There were 41 strategy calls, within the 64-call strategy ceiling, plus 19 review
calls counted separately. All 60 attempts have corresponding raw-reply records.
Known reported totals are 310,658 input and 153,319 output tokens. Six calls have
unknown usage; totals are incomplete and do not treat those calls as zero cost.
No currency cost or pre-generation billing cap is claimed.

| Split / strategy | Strategy p50 / p95 seconds | Review p50 / p95 seconds |
| --- | --- | --- |
| Development / fixed | 5.738 / 26.384 | 34.316 / 44.714 |
| Development / adaptive | 10.142 / 54.881 | 35.995 / 39.614 |
| Held-out / fixed | 25.231 / 29.848 | 34.310 / 38.286 |
| Held-out / adaptive | 23.773 / 65.422 | 27.424 / 38.850 |

Percentiles use nearest rank and include failures. Adaptive held-out p50 includes
five fast unavailable calls, so it cannot support a speed advantage. The order
was development fixed, development adaptive, held-out fixed, held-out adaptive,
with no concurrent provider calls. After the adaptive foreign-case proposal,
its review and all five remaining strategy attempts reported provider errors.
The measured transport exposes unavailability, not the underlying native
diagnostic; quota, authentication or another cause was not established. No
fallback or retry was added. This availability/order confound also prevents a
clean paired accuracy or cost comparison.

## Model review

Of 19 review attempts, 17 produced validated reviews: one final `accepted` and
16 `needs_revision`. One fixed conflict review was invalid; one adaptive foreign
review was unavailable. Every validated review reports `same_model` independence.
Clarifications/abstentions without assessments were not reviewed by this path.

The reviewer itself requested acceptance seven times; domain rules reduced six
of those to `needs_revision` because structured conditions, claims or the
assessment remained incomplete. The sole final acceptance was adaptive
development `dev-supported` run 2. There was no final held-out acceptance.

The review and rubric do not measure identical conditions. For example, fixed
development run 1 supplies the expected method and limits, but review demands an
explicit effective-date statement. Adaptive expired run 2 has supported claims
and a sound non-applicability conclusion, yet its reviewer marks a conflict
condition uncovered while naming the claim that discusses it. In adaptive
nontransient run 1, review correctly points out that the cited transient-only
sentence does not itself establish the claim's three-attempt limit. The frozen
independent scores are retained; these later findings are not silently folded
into labels advertised as independent of review.

Neither high rejection nor agreement establishes reviewer reliability. The
distinction between document-level support, exact citation entailment and
conditions relevant to refusing a method requires adjudication in `08.3`.

## Integrity checks and next work

- The original corpus/case digests match every report header. All 32 unique
  case/run/strategy identities are present, with two repeats per case/strategy.
- Recomputed all 192 citations in accepted proposal assessments against frozen
  source hashes and exact spans; no mismatch was found. This count excludes
  rejected raw proposals and reviewer citations, and is not semantic acceptance.
- Every recorded strategy run fits 90 seconds; per-round strategy calls stay
  within one fixed/two adaptive calls. No known individual call exceeds 12,000
  input or 6,000 output tokens. Adaptive round totals are not per-call ceilings.
- Recorded provider/model identities match the pinned binding, including failed
  calls. Delivered proposals retain `can_execute=false`; experience is not
  consulted. No source mutation, operator configuration write or external task
  action is performed. The temporary Runtime host was stopped after the run.
- `go test ./cmd/knowledge-comparison` and
  `go test -race ./cmd/knowledge-comparison` passed. Full module/application,
  Electron, production ranking and cross-provider tests were not run for this
  documentation/evaluation batch.

Keep the existing opt-in behavior and frozen failures. The next
[proposed record contracts](knowledge-outcomes.md) separate actual attempts,
observations, prior forecasts and episode eligibility. Implement and validate
those domain contracts before persistence or later reuse. In `08.3`, adjudicate semantic review, source-scope
comparability and the remaining causal controls. Any tuning informed by these
answers requires a new frozen held-out set and separately reported evaluation;
these cases cannot regain unseen status by rerunning them. No prompt, parser,
service contract, database schema, environment variable or Compose configuration
was changed by this batch.

## Evidence digests

SHA-256 values for the local evidence files:

| File | SHA-256 |
| --- | --- |
| development-fixed.jsonl | `d9e12d45bfb1d318d5e0b1dbacc236303ee42ad02f82cddaf44fc54ce7967a65` |
| development-adaptive.jsonl | `93f5bcf2dd363b22d8182f938b111d539ba3704027095639e5bb9b3390e967ed` |
| held_out-fixed.jsonl | `b31772aa697cf97121d3722dfa9665a0975feba23a8461cf0fc0a9c7364de1ac` |
| held_out-adaptive.jsonl | `4cb93c8a61fba23471a984e5e2cdae99b9628c2c3b2080a558d8beaa7d6017a0` |
| scores-before-model-review.json | `d2d9b7aa5b588905dad10a8e1433ded20af9a613452c828ddc709bb10c0fbb63` |
| audit.json | `14b60d6f8109cf646ccc503ff471ed9f824ef6ef2a969a5d9d88842a046bf3c7` |
