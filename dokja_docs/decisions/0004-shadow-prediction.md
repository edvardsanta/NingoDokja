# ADR 0004: explicit hashtag feedback and shadow prediction

Date: 2026-10-18. Status: accepted for prototype. Implementation: step `06`
([hashtag_task.go](../../dokja_orch/internal/handlers/hashtag_task.go)). Scheduled and
manual meme sends run the suggestion as a typed task whose shadow prediction is frozen
before delivery under a 1 s budget chosen by the owner; a miss or a Memory failure leaves
the attempt unscored. An attempt is recorded only after its meme reached at least one
channel. Feedback still resolves by the media-derived ref, so repeated media keeps the
one-open-attempt limitation below.
Related: [decision index](./README.md), [workspace](./0001-task-workspace.md),
[state ownership](./0003-state-ownership.md).

## Context

The [Memory predictor](../../dokja_domain/dokja_memory/predict.go) estimates an
action's acceptance probability from similar resolved contexts and a smoothed
baseline. [Hashtag experience](../../dokja_orch/internal/handlers/hashtag_experience.go)
records suggestions asynchronously using a media-derived reference. Repeated
media suggestions therefore do not currently have independent attempt identities.
Prediction precedes the experience insert inside the callback, but not reliably
the delivery action. The first experiment must distinguish these current
semantics from a controlled prediction/evaluation loop.

## Target and action identity

The initial prediction target is explicit operator agreement with a hashtag
suggestion. A positive label means the first eligible explicit operator tag for
that action agrees with the suggested tag under the existing domain comparison
rules. A different explicit tag is negative. Define an observation horizon of
30 days after the successfully executed action for the experiment. No explicit
feedback in that window is unknown, not negative; later feedback is reported
separately. Existing Memory expiry settings and rows are not rewritten to enforce
this experimental horizon.

Each experiment record identifies the task/attempt, proposed tag, media reference,
scope, action execution time/status, and feedback observation time. A failed or
unconfirmed delivery is not an accepted/replaced example. For the initial
controlled fixture run, allow one open action per scoped media reference. If
feedback cannot be attributed uniquely, mark it ambiguous and exclude it from
the primary prediction score; do not guess the intended attempt.

The current public tagging command has no attempt ID. Keep that API compatible;
the controlled experiment uses the one-open-attempt restriction. Repeated live
attempts or reliable cross-restart attribution require a later receipt/feedback
contract review. Do not retrofit a new reference format into existing Memory
rows during the prototype. Durable receipt persistence belongs to step `10`.

## Snapshot and baseline

Use the existing Memory-domain prediction method as the initial method. Its
baseline is `(accepted + 1) / (resolved + 2)` for the action, with resolved counts
following current domain verdict semantics. Record the method/configuration
version, counts, evidence references, similarity/support, probability, baseline,
and `insufficient`/degraded reason as one frozen snapshot.

The current estimator conditions on action type and context, not on a model of
each candidate tag. Describe it as a contextual acceptance estimate; it is not
a ranking model for competing tags. Retrieval similarity and the estimated
probability remain separate fields. Neither measures factual correctness.

Current Memory requests also lack actor/channel filtering. Run the initial
synthetic study against an isolated fixture store per evaluation cohort and do
not describe its results as scoped live prediction. Adding live cross-context
filtering requires a separate contract review; workspace scope alone does not
retroactively scope an existing Memory corpus.

Synthetic evaluation creates a before-action snapshot synchronously in the task
workspace. Candidate selection uses only information available before the action.
Outcome labels are then revealed in time order. Live shadow execution may use a
bounded prediction call; if it misses the budget, execute the existing approved
action without a usable snapshot and record that attempt as unscored. Do not
manufacture a retrospective prediction or let optional memory failure block the
product workflow. Volatile snapshots are sufficient for controlled fixtures,
but do not provide a reliable persistent live study.

## Evaluation and stopping criteria

Compare frozen predicted and baseline probabilities against eligible binary
outcomes using Brier scores. Report score counts, unknown/late/ambiguous feedback,
unscored attempts, support, and time cutoffs. Calibration summaries are useful
only with their sample sizes. Preserve the domain's provisional minimum-count
flags, including its current 30-scored threshold; this threshold alone is not
proof of statistical reliability or useful skill.

Use fixed time-respecting fixtures for the architecture experiment. Fit/tune any
new threshold on earlier data only and evaluate it on later held-out cases.
Exclude unresolved/future examples from retrieval and baseline counts at the
prediction time. Explicitly supplied `ignored` labels can retain existing domain
meaning in compatibility tests, but absence of feedback must never synthesize one.

Step `03` records existing behavior, an evaluation protocol, and latency/resource
budgets before implementation. Step `08` compares the existing path, workspace
only, and workspace plus shadow prediction on fixed inputs. Synthetic results
can verify mechanics and overhead; claims of behavioral improvement require
sufficient real later feedback and a study that survives the required horizon.

## Learning and non-goals

Predictions remain observational. They do not change hashtag selection, moderation,
delivery eligibility, or the existing configured retrieval threshold. Explicit
labels may continue to update Knowledge through its current domain operation;
automatic predictions are never promoted as their own ground truth.

No new learned model, reinforcement-learning controller, universal confidence
score, autonomous procedure promotion, new prediction service, or incorporation
of the untracked Rust scaffold is part of this experiment. Any broader learning
behavior needs its own target, evidence, decision record, and evaluation.

Verification must cover positive/replaced feedback, unknown/late/ambiguous labels,
missing snapshots, failed actions, repeated media, snapshot immutability, temporal
leakage, insufficient support, and Memory failure. Existing public Memory command
semantics remain supported. Rollback disables the prototype path and retains
the existing selection and feedback behavior.
