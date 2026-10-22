package cognitive

import (
	"context"
	"fmt"
	inference "read_books/dokja_domain/dokja_inference"
	"time"
)

type Config struct {
	Inference                        inference.Provider
	Mode                             string // single, bounded, iterative
	MaxHops, MaxIterations, MaxCalls int
	Deadline                         time.Duration
	Authorized                       bool // harness grant; never inferred from evidence
	Attempt, Scope, Context          string
	Tokens                           []string
	Expected                         int
}

func Run(parent context.Context, store Store, c Config) (s State) {
	s = State{Goal: "sum a synthetic document safely", Scope: c.Scope, Context: c.Context}
	finish := func(outcome, reason string) State { s.Outcome = outcome; s.transition("stop", reason); return s }
	if (c.Mode != "single" && c.Mode != "bounded" && c.Mode != "iterative") || c.MaxHops < 0 || c.MaxHops > 6 || c.MaxIterations < 1 || c.MaxCalls < 1 || c.Deadline <= 0 || c.Attempt == "" || c.Scope == "" || c.Context == "" {
		return finish("failed", "invalid configuration")
	}
	ctx, cancel := context.WithTimeout(parent, c.Deadline)
	defer cancel()
	call := func() bool {
		if ctx.Err() != nil || s.Calls >= c.MaxCalls {
			return false
		}
		s.Calls++
		return true
	}
	if !call() {
		return finish("budget_exhausted", "deadline or call limit")
	}
	root, err := store.Search(ctx, c.Scope)
	if err != nil || !valid(root, c.Scope) {
		return finish("failed", "root unavailable")
	}
	s.Nodes = append(s.Nodes, clone(root))
	s.transition("knowledge", "scoped vector seed")
	if !call() {
		return finish("budget_exhausted", "deadline or call limit")
	}
	episodes, err := store.Recall(ctx, c.Scope, c.Context)
	if err != nil {
		return finish("failed", "experience retrieval failed")
	}
	for _, e := range episodes {
		if e.Scope == c.Scope && e.Context == c.Context && e.Simulation {
			s.Episodes = append(s.Episodes, clone(e))
		}
	}
	s.transition("experience", fmt.Sprintf("%d eligible observed simulation episodes", len(s.Episodes)))
	depths := map[string]int{root.ID: 0}
	requested := map[string]bool{root.ID: true}
	// Fixed-depth baseline eagerly traverses declared dependency/question edges
	// before inference. The iterative policy fetches only its next justified gap.
	if c.Mode == "bounded" {
		for cursor := 0; cursor < len(s.Nodes); cursor++ {
			n := s.Nodes[cursor]
			if depths[n.ID] >= c.MaxHops {
				continue
			}
			for _, id := range append(append([]string(nil), n.Requires...), n.Question) {
				if id == "" || requested[id] {
					continue
				}
				requested[id] = true
				if !call() {
					return finish("budget_exhausted", "fixed traversal call limit")
				}
				next, err := store.Get(ctx, c.Scope, id)
				if ctx.Err() != nil {
					return finish("budget_exhausted", "late retrieval discarded")
				}
				if err != nil || !valid(next, c.Scope) || next.ID != id {
					s.transition("unavailable", "fixed traversal missing evidence")
					continue
				}
				depths[id] = depths[n.ID] + 1
				if depths[id] > s.Hops {
					s.Hops = depths[id]
				}
				s.Nodes = append(s.Nodes, clone(next))
				s.transition("knowledge", "fixed-depth edge from "+n.ID)
			}
		}
	}
	for iteration := 0; iteration < c.MaxIterations; iteration++ {
		s.Iterations = iteration + 1
		if ctx.Err() != nil {
			return finish("budget_exhausted", "deadline")
		}
		d := Decide(s)
		switch d.Operation {
		case "retrieve":
			if c.Mode == "bounded" {
				return finish("abstain", "fixed traversal left unresolved evidence")
			}
			depth := 0
			found := false
			for _, n := range s.Nodes {
				for _, id := range append(append([]string(nil), n.Requires...), n.Question) {
					if id == d.Target {
						v := depths[n.ID] + 1
						if !found || v < depth {
							depth = v
							found = true
						}
					}
				}
			}
			if !found {
				return finish("failed", "unjustified retrieval")
			}
			if c.Mode == "single" || depth > c.MaxHops {
				return finish("abstain", "retrieval depth limit")
			}
			if requested[d.Target] {
				return finish("stagnated", "repeated unresolved evidence request")
			}
			requested[d.Target] = true
			if !call() {
				return finish("budget_exhausted", "deadline or call limit")
			}
			n, err := store.Get(ctx, c.Scope, d.Target)
			if ctx.Err() != nil {
				return finish("budget_exhausted", "late retrieval discarded")
			}
			if err != nil || !valid(n, c.Scope) || n.ID != d.Target {
				return finish("abstain", "required evidence unavailable")
			}
			depths[n.ID] = depth
			if depth > s.Hops {
				s.Hops = depth
			}
			s.Nodes = append(s.Nodes, clone(n))
			s.Prediction = Prediction{}
			s.transition("knowledge", d.Reason)
		case "predict":
			if !call() {
				return finish("budget_exhausted", "deadline or call limit")
			}
			if c.Inference == nil {
				s.Prediction = Predict(s)
			} else {
				request := inferenceRequest(s, c)
				if err := inference.ValidateRequest(request); err != nil {
					return finish("inference_rejected", "invalid inference input")
				}
				result, err := c.Inference.Infer(ctx, clone(request))
				s.Inferences = append(s.Inferences, clone(result))
				if len(result.Attempts) > 0 {
					s.Calls += len(result.Attempts) - 1
				}
				if ctx.Err() != nil {
					return finish("budget_exhausted", "late inference discarded")
				}
				if err != nil {
					return finish("inference_failed", "configured inference unavailable; no deterministic fallback")
				}
				if len(result.Attempts) == 0 || len(result.Attempts) > request.RemainingCalls || inference.Validate(request, result.Proposal) != nil {
					return finish("inference_rejected", "invalid or stale inference result")
				}
				p := result.Proposal
				s.Prediction = Prediction{Status: p.Status, Model: result.Attempts[len(result.Attempts)-1].Model, Action: p.Action, Missing: p.Missing, Probability: p.Probability, Hypothetical: true, Episodes: append([]string(nil), p.Experiences...)}
				for _, id := range p.Evidence {
					for _, ref := range s.refs() {
						if ref.ID == id {
							s.Prediction.Inputs = append(s.Prediction.Inputs, ref)
						}
					}
				}
			}
			s.transition("predict", d.Reason)
		case "ready":
			s.Action = d.Target
			s.transition("decision", d.Reason)
			if !c.Authorized {
				return finish("authorization_required", "simulation grant absent")
			}
			if !call() {
				return finish("budget_exhausted", "deadline or call limit")
			}
			s.Observed, s.Diagnostic = ObserveSimulation(d.Target, c.Tokens, c.Expected)
			s.transition("simulate", "observed local computation; not production feedback")
			if s.Observed == "unknown" {
				return finish("outcome_unknown", "no resolved experience")
			}
			e := Episode{ID: c.Attempt, Scope: c.Scope, Context: c.Context, Action: s.Action, Outcome: s.Observed, Diagnostic: s.Diagnostic, Simulation: true, Evidence: s.refs(), Predicted: s.Prediction.Probability}
			if !call() {
				return finish("record_failed", "action observed but recording budget exhausted")
			}
			if err := store.Record(ctx, e); err != nil {
				return finish("record_failed", "action observed but recording failed")
			}
			s.transition("record", "simulation episode persisted with frozen prediction")
			return finish("completed", "observed result available to subsequent tasks")
		case "clarify":
			s.Conflicts++
			return finish("clarify", d.Reason)
		case "abstain":
			return finish("abstain", d.Reason)
		default:
			return finish("failed", "unknown transition")
		}
	}
	return finish("budget_exhausted", "iteration limit")
}
