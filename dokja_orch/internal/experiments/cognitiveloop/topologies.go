package cognitiveloop

import (
	"context"
	"time"
)

func start(ctx context.Context, task Task, budget Budget) (context.Context, context.CancelFunc, *workspace, time.Time) {
	ctx, cancel := context.WithTimeout(ctx, budget.Deadline)
	return ctx, cancel, newWorkspace(task, budget), time.Now()
}

func (w *workspace) view() view {
	return view{knowledge: w.knowledge, recalled: w.recalled, premises: w.premises, rejected: w.rejected}
}

func (w *workspace) stop(outcome string, err error, began time.Time) Result {
	w.result.Elapsed = time.Since(began)
	return w.finish(outcome, err.Error(), nil)
}

// Supervised is topology A: every retrieval, prediction and acquisition is its own
// orchestrated step, and the domain policy is asked again after each one. Knowledge and
// Experience are read again only after new evidence arrived.
func Supervised(parent context.Context, task Task, ports Ports, budget Budget) Result {
	return supervised(parent, task, ports, budget, true)
}

// SupervisedRereading is topology A without snapshot reuse: it reads Knowledge and
// Experience on every iteration. It is kept to measure what reuse saves.
func SupervisedRereading(parent context.Context, task Task, ports Ports, budget Budget) Result {
	return supervised(parent, task, ports, budget, false)
}

func supervised(parent context.Context, task Task, ports Ports, budget Budget, reuse bool) Result {
	ctx, cancel, w, began := start(parent, task, budget)
	defer cancel()
	for iteration := 1; ; iteration++ {
		if iteration > budget.MaxIterations {
			w.result.Elapsed = time.Since(began)
			return w.finish(OutcomeBudgetExhausted, "iteration budget spent", nil)
		}
		w.result.Iterations = iteration
		if w.stale || !reuse {
			if stop, err := w.snapshot(ctx, ports, iteration); err != nil {
				return w.stop(stop, err, began)
			}
		}
		candidate, missing, reason := propose(w.view())
		if candidate == nil {
			w.result.Elapsed = time.Since(began)
			return w.finish(OutcomeAbstain, reason, nil)
		}
		if outcome, repeated := w.progress(candidate); repeated {
			w.result.Elapsed = time.Since(began)
			return w.repeated(outcome, candidate)
		}
		if len(missing) > 0 {
			if stop, err := w.acquire(ctx, ports, iteration, missing[0]); err != nil {
				return w.stop(stop, err, began)
			}
			continue
		}
		prediction, stop, err := call(ctx, w, iteration, "predict", func(ctx context.Context) (Prediction, error) {
			return ports.Predict(ctx, *candidate, append(append([]Evidence{}, w.knowledge...), w.recalled...))
		})
		if err != nil {
			return w.stop(stop, err, began)
		}
		prediction.Hypothetical = true
		w.result.Predictions = append(w.result.Predictions, prediction)
		next := decide(candidate, prediction, w.premises)
		switch next.step {
		case stepNeedEvidence:
			if stop, err := w.acquire(ctx, ports, iteration, next.premise); err != nil {
				return w.stop(stop, err, began)
			}
		case stepRevise:
			w.rejected[next.reject] = true
			w.trace(iteration, stepRevise, next.reason)
		default:
			w.result.Elapsed = time.Since(began)
			return w.finish(next.step, next.reason, candidate)
		}
	}
}

// DomainInternal is topology B: the orchestrator takes one snapshot per round and the
// domain revises and predicts over it on its own. The domain comes back only to ask for
// evidence it cannot compute, which the orchestrator acquires.
func DomainInternal(parent context.Context, task Task, ports Ports, budget Budget) Result {
	ctx, cancel, w, began := start(parent, task, budget)
	defer cancel()
	for round := 1; ; round++ {
		if round > budget.MaxIterations {
			w.result.Elapsed = time.Since(began)
			return w.finish(OutcomeBudgetExhausted, "iteration budget spent", nil)
		}
		w.result.Iterations = round
		if stop, err := w.snapshot(ctx, ports, round); err != nil {
			return w.stop(stop, err, began)
		}
		next, candidate, stop, err := w.reason(ctx, ports, round)
		if err != nil {
			return w.stop(stop, err, began)
		}
		if next.step != stepNeedEvidence {
			w.result.Elapsed = time.Since(began)
			return w.finish(next.step, next.reason, candidate)
		}
		if outcome, repeated := w.progress(candidate); repeated {
			w.result.Elapsed = time.Since(began)
			return w.repeated(outcome, candidate)
		}
		if stop, err := w.acquire(ctx, ports, round, next.premise); err != nil {
			return w.stop(stop, err, began)
		}
	}
}

// reason is the domain's bounded computation over the round's snapshot. Revisions stay
// inside it; each is still traced so the round is explainable.
func (w *workspace) reason(ctx context.Context, ports Ports, round int) (decision, *Candidate, string, error) {
	for {
		candidate, missing, reason := propose(w.view())
		if candidate == nil {
			return decision{step: OutcomeAbstain, reason: reason}, nil, "", nil
		}
		if len(missing) > 0 {
			return decision{step: stepNeedEvidence, premise: missing[0]}, candidate, "", nil
		}
		prediction, stop, err := call(ctx, w, round, "predict", func(ctx context.Context) (Prediction, error) {
			return ports.Predict(ctx, *candidate, append(append([]Evidence{}, w.knowledge...), w.recalled...))
		})
		if err != nil {
			return decision{}, nil, stop, err
		}
		prediction.Hypothetical = true
		w.result.Predictions = append(w.result.Predictions, prediction)
		next := decide(candidate, prediction, w.premises)
		if next.step != stepRevise {
			return next, candidate, "", nil
		}
		w.rejected[next.reject] = true
		w.trace(round, stepRevise, next.reason)
	}
}
