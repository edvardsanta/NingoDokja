# Cited application proposals and applicability

This is the next read-only `08.2` batch after
[evidence preparation](knowledge-application.md). Source observations, proposed
claims and a proposed method are different objects. Citation verification and
semantic justification are different checks.

## Contract selected before implementation

The additive `knowledge.application.assess` request will contain:

- `contract: knowledge.application.assessment.v1`;
- `application`: the existing preparation request with a goal, constraints and
  selected sources, each with a required `text_version` pin;
- `proposal`: claims and one method, submitted explicitly by a caller. The
  proposal is not trusted state or an execution instruction.

A claim has an ID, a statement and zero or more source-span citations. A citation
contains source ID, text version, zero-based UTF-8 byte offsets (`start`, inclusive;
`end`, exclusive), a verbatim quotation, and the proposed relation `supports` or
`contradicts`. A method has an ID, description, source-span citations and a
nonempty list of claim IDs representing its proposed prerequisites. Method
citations propose support only. Concepts, free-form relationships and actual
episodes are still later contracts; this does not persist them under new names.

Every citation must resolve to the exact text/version loaded in this request.
Unknown sources, changed versions, incorrect offsets/quotes, malformed references,
duplicate IDs or citations, extra fields and excess state are rejected.
Uncited claims remain explicitly unresolved. Selecting or quoting text does not
verify the proposed relation, claim meaning, method meaning, prerequisite
completeness, scope/date interpretation or compatibility with user constraints.
Those are retained as unverified semantic assertions for later evaluation.

The domain derives citation states per claim: `supported`, `contradicted`,
`conflicted` or `unknown`, based only on the validated proposed relations. The
method's provisional applicability is `blocked` for a contradicted prerequisite,
`conflicted` for a conflicting prerequisite, `needs_evidence` for unknown
prerequisites or an uncited method, otherwise `conditional`. All results require
semantic review and have `can_execute: false`. No result means that a provider's
interpretation or an external effect was confirmed.

The orchestrator re-reads pinned sources, schedules assessment after preparation,
and retains the task deadline, state limits and cancellation rules. No write,
provider call, new service API, environment change or schema migration is needed.
`prepare` remains compatible and keeps `applicability: unassessed`.

## Frozen evaluation inputs and rubric

The authored [corpus](../dokja_domain/dokja_knowledge/testdata/application/corpus_v1.json)
and [cases](../dokja_domain/dokja_knowledge/testdata/application/cases_v1.json)
are fixed before implementing the assessor or invoking providers. They represent
one narrow but complete technical application problem: combine a procedure, an
API guarantee and operations constraints. They are synthetic documents, not
factual advice about an external API. Two development cases may guide integration;
six held-out natural-language cases are reserved for provider comparisons, not
prompt tuning. Expected answers were authored independently of provider output.

A later change to these inputs creates a new corpus/case version and requires
separate results; do not edit the expected answer to match a run. The fixture
commit establishes the freeze. Contract tests may validate file structure;
assessor regression tests use development examples and adversarial mutations,
not held-out provider answers. This batch does not run a model comparison.

Freeze the following comparison protocol before any provider run:

| Item | Bound or criterion |
| --- | --- |
| Strategies | Fixed selected-document context vs bounded adaptive retrieval |
| Cases / repeats | All 8 cases, 2 independent runs per strategy: 32 runs |
| Provider calls | Fixed: 1 per run; adaptive: at most 3; total ceiling 64 |
| Per-call token ceilings | 12000 input / 2000 output; overflow is a recorded failure |
| Per-run elapsed ceiling | 90 seconds; no hidden retries or uncounted repair calls |
| Corpus mutation / external effects | None |
| Blocking integrity checks | 100% valid identities, source pins/spans, scopes, cancellation and budgets |
| False applicability | Zero unconditional recommendations in unsafe, missing or unresolved-conflict cases |
| Answer rubric | Correct disposition; all necessary prerequisites; supported method/limits with citations; explicit uncertainty; no invented outcome |
| Initial pass target | At least 15 of 16 runs per strategy satisfy every answer-rubric item, plus every blocking check |
| Utility comparison | Report per-case correctness/abstention, unsupported claims, calls, latency and tokens; retain simpler strategy absent demonstrated benefit |

These are evaluation ceilings and provisional acceptance criteria, not a claim
of general reliability. Provider/model/settings and fixture commit must be
recorded before comparison, with all raw proposals and failures accounted for.
Held-out evaluation and independent semantic review remain pending in `08.3`.
Experience-removal, eligible/inapplicable episode and restart/reuse controls are
additional required evaluations when those application capabilities exist.
