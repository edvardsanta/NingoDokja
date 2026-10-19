// Package cognitiveloop is the step 03 reference experiment for ADR 0005. It compares
// two owners of the Knowledge–Prediction–Experience cycle over fake capabilities:
//
//   - Supervised (topology A): the orchestrator runs every operation as its own step
//     and asks the domain policy what to do next.
//   - DomainInternal (topology B): the orchestrator hands the domain a snapshot; the
//     domain revises and predicts on its own and returns only to ask for new evidence.
//
// Both use the same pure policy, so differences come from the topology. Nothing in the
// orchestrator imports this package; it is not production wiring.
package cognitiveloop

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"
)

type Scope struct {
	Actor   string
	Channel string
}

// Evidence is knowledge (a general method), experience (an observed procedure) or an
// observation (one premise and whether it holds).
type Evidence struct {
	ID        string
	Kind      string
	Scope     Scope
	Version   string
	Source    string
	Method    string
	Procedure string
	Requires  []string
	Premise   string
	Holds     bool
}

const (
	KindKnowledge   = "knowledge"
	KindExperience  = "experience"
	KindObservation = "observation"
)

type Candidate struct {
	Method    string
	Procedure string
	From      string
	Supports  []string
}

func (c *Candidate) key() string {
	if c == nil {
		return ""
	}
	return fmt.Sprintf("%s/%s", c.Method, c.Procedure)
}

// Prediction is always hypothetical: it estimates an outcome, it never observes one.
type Prediction struct {
	Probability  float64
	Missing      string
	Model        string
	Hypothetical bool
}

type Ports interface {
	Knowledge(ctx context.Context, scope Scope, goal string) ([]Evidence, error)
	Experience(ctx context.Context, scope Scope, goal string) ([]Evidence, error)
	Predict(ctx context.Context, candidate Candidate, evidence []Evidence) (Prediction, error)
	Acquire(ctx context.Context, scope Scope, premise string) (Evidence, error)
}

type Task struct {
	ID           string
	Goal         string
	Scope        Scope
	Observations []Evidence
}

type Budget struct {
	MaxIterations int
	MaxOperations int
	Deadline      time.Duration
}

const (
	OutcomeReady           = "ready"
	OutcomeAbstain         = "abstain"
	OutcomeFailed          = "failed"
	OutcomeBudgetExhausted = "budget_exhausted"
	OutcomeStagnated       = "stagnated"
	OutcomeOscillated      = "oscillated"
	stepNeedEvidence       = "need_evidence"
	stepRevise             = "revise"
)

const minPredictedSuccess = 0.5

type TraceEntry struct {
	Iteration int
	Operation string
	Detail    string
}

type Result struct {
	Outcome     string
	Reason      string
	Candidate   *Candidate
	Iterations  int
	Operations  map[string]int
	Trace       []TraceEntry
	Predictions []Prediction
	Dropped     []string
	Unavailable []string
	Bytes       int
	Elapsed     time.Duration
}

// workspace is the orchestrator-owned task state. Only the run loop writes it.
type workspace struct {
	task      Task
	budget    Budget
	knowledge []Evidence
	recalled  []Evidence
	premises  map[string]bool
	versions  map[string]string
	requested map[string]bool
	rejected  map[string]bool
	seen      []string
	stale     bool
	pending   string
	result    Result
}

func newWorkspace(task Task, budget Budget) *workspace {
	w := &workspace{
		task: task, budget: budget, stale: true, premises: map[string]bool{}, versions: map[string]string{},
		requested: map[string]bool{}, rejected: map[string]bool{},
		result: Result{Operations: map[string]int{}},
	}
	for _, observation := range task.Observations {
		w.observe(observation)
	}
	return w
}

func (w *workspace) observe(e Evidence) {
	if e.Kind == KindObservation && e.Premise != "" {
		w.premises[e.Premise] = e.Holds
		w.versions[e.ID] = e.Version
	}
}

func (w *workspace) trace(iteration int, operation, detail string) {
	w.result.Trace = append(w.result.Trace, TraceEntry{Iteration: iteration, Operation: operation, Detail: detail})
}

// inScope drops evidence from another actor or channel instead of trusting the store.
func (w *workspace) inScope(evidence []Evidence) []Evidence {
	kept := evidence[:0:0]
	for _, e := range evidence {
		if e.Scope != w.task.Scope {
			w.result.Dropped = append(w.result.Dropped, e.ID)
			continue
		}
		kept = append(kept, e)
	}
	sort.Slice(kept, func(i, j int) bool { return kept[i].ID < kept[j].ID })
	return kept
}

func (w *workspace) finish(outcome, reason string, candidate *Candidate) Result {
	w.result.Outcome, w.result.Reason, w.result.Candidate = outcome, reason, candidate
	return w.result
}

// signature identifies the reasoning state; a repeat means no progress was made.
func (w *workspace) signature(candidate *Candidate) string {
	parts := []string{candidate.key()}
	for _, e := range append(append([]Evidence{}, w.knowledge...), w.recalled...) {
		parts = append(parts, fmt.Sprintf("%s@%s", e.ID, e.Version))
	}
	premises := make([]string, 0, len(w.premises))
	for premise, holds := range w.premises {
		premises = append(premises, fmt.Sprintf("%s=%t", premise, holds))
	}
	sort.Strings(premises)
	return strings.Join(append(parts, premises...), "|")
}

func (w *workspace) repeated(outcome string, candidate *Candidate) Result {
	reason := "the reasoning state repeated without new evidence"
	if w.pending != "" {
		reason = fmt.Sprintf("%s; unresolved premise: %s", reason, w.pending)
	}
	return w.finish(outcome, reason, candidate)
}

func (w *workspace) progress(candidate *Candidate) (string, bool) {
	current := w.signature(candidate)
	for i := len(w.seen) - 1; i >= 0; i-- {
		if w.seen[i] == current {
			if i == len(w.seen)-1 {
				return OutcomeStagnated, true
			}
			return OutcomeOscillated, true
		}
	}
	w.seen = append(w.seen, current)
	return "", false
}

// call runs one capability operation under the task deadline and operation budget. A
// result that arrives after the deadline is discarded, never applied.
func call[T any](ctx context.Context, w *workspace, iteration int, operation string, fn func(context.Context) (T, error)) (T, string, error) {
	var zero T
	if w.result.Operations["total"] >= w.budget.MaxOperations {
		return zero, OutcomeBudgetExhausted, fmt.Errorf("operation budget of %d spent", w.budget.MaxOperations)
	}
	w.result.Operations["total"]++
	w.result.Operations[operation]++
	type answer struct {
		value T
		err   error
	}
	done := make(chan answer, 1)
	go func() {
		value, err := fn(ctx)
		done <- answer{value, err}
	}()
	select {
	case <-ctx.Done():
		w.trace(iteration, operation, "late result discarded")
		return zero, OutcomeBudgetExhausted, fmt.Errorf("%s did not finish before the deadline", operation)
	case got := <-done:
		if got.err != nil {
			return zero, OutcomeFailed, fmt.Errorf("%s failed: %w", operation, got.err)
		}
		if size, err := json.Marshal(got.value); err == nil {
			w.result.Bytes += len(size)
		}
		w.trace(iteration, operation, "ok")
		return got.value, "", nil
	}
}

// snapshot reads Knowledge and Experience for the current state.
func (w *workspace) snapshot(ctx context.Context, ports Ports, iteration int) (string, error) {
	knowledge, stop, err := call(ctx, w, iteration, "knowledge", func(ctx context.Context) ([]Evidence, error) {
		return ports.Knowledge(ctx, w.task.Scope, w.task.Goal)
	})
	if err != nil {
		return stop, err
	}
	recalled, stop, err := call(ctx, w, iteration, "experience", func(ctx context.Context) ([]Evidence, error) {
		return ports.Experience(ctx, w.task.Scope, w.task.Goal)
	})
	if err != nil {
		return stop, err
	}
	w.knowledge, w.recalled, w.stale = w.inScope(knowledge), w.inScope(recalled), false
	return "", nil
}

func (w *workspace) acquire(ctx context.Context, ports Ports, iteration int, premise string) (string, error) {
	w.requested[premise], w.stale, w.pending = true, true, premise
	w.trace(iteration, stepNeedEvidence, premise)
	observation, stop, err := call(ctx, w, iteration, "acquire", func(ctx context.Context) (Evidence, error) {
		return ports.Acquire(ctx, w.task.Scope, premise)
	})
	if err != nil {
		return stop, err
	}
	if observation.Premise == "" {
		// Nothing was found: unavailable evidence, not contrary or foreign evidence.
		w.result.Unavailable = append(w.result.Unavailable, premise)
		w.trace(iteration, "acquire", fmt.Sprintf("%s unavailable", premise))
		return "", nil
	}
	if kept := w.inScope([]Evidence{observation}); len(kept) == 1 {
		w.observe(kept[0])
	}
	return "", nil
}
