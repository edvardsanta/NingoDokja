// Package cognitive is an isolated, synthetic vertical experiment. No application
// handler imports it; all actions are pure local simulations.
package cognitive

import (
	"context"
	"encoding/json"
	"fmt"
	inference "read_books/dokja_domain/dokja_inference"
	"sort"
	"strconv"
	"strings"
)

type Ref struct{ ID, Version, Source string }
type Node struct {
	Ref
	Scope, Kind string
	Current     bool
	Deleted     bool
	Requires    []string
	Facts       map[string]bool
	Question    string
}
type Episode struct {
	ID, Scope, Context, Action, Outcome string
	Diagnostic                          string `json:",omitempty"`
	Simulation                          bool
	Evidence                            []Ref
	Predicted                           float64
}
type Store interface {
	Search(context.Context, string) (Node, error)
	Get(context.Context, string, string) (Node, error)
	Recall(context.Context, string, string) ([]Episode, error)
	Record(context.Context, Episode) error
}
type Prediction struct {
	Status                 string
	Model, Action, Missing string
	Probability            float64
	Inputs                 []Ref
	Episodes               []string
	Hypothetical           bool
}
type Transition struct {
	Revision          int
	Operation, Reason string
	Evidence          []Ref
	Prediction        *Prediction `json:",omitempty"`
}
type State struct {
	Inferences                                         []inference.Result `json:",omitempty"`
	Revision                                           int
	Goal, Scope, Context                               string
	Nodes                                              []Node
	Episodes                                           []Episode
	Prediction                                         Prediction
	Trace                                              []Transition
	Diagnostic                                         string `json:",omitempty"`
	Outcome, Action, Observed                          string
	Hops, Calls, Iterations, DuplicateReads, Conflicts int
}

func (s *State) refs() []Ref {
	r := []Ref{}
	for _, n := range s.Nodes {
		r = append(r, n.Ref)
	}
	return r
}
func (s *State) transition(op, reason string) {
	s.Revision++
	t := Transition{Revision: s.Revision, Operation: op, Reason: reason, Evidence: s.refs()}
	if op == "predict" {
		p := s.Prediction
		p.Inputs = append([]Ref(nil), p.Inputs...)
		p.Episodes = append([]string(nil), p.Episodes...)
		t.Prediction = &p
	}
	s.Trace = append(s.Trace, t)
}
func (s State) fact(key string) (bool, bool, bool) {
	seen, value, conflict := false, false, false
	for _, n := range s.Nodes {
		if v, ok := n.Facts[key]; ok {
			if seen && value != v {
				conflict = true
			}
			seen = true
			value = v
		}
	}
	return value, seen, conflict
}
func (s State) contains(id string) bool {
	for _, n := range s.Nodes {
		if n.ID == id {
			return true
		}
	}
	return false
}

// Predict is a deliberately uncalibrated fixture model, not an LLM or production
// success estimate. Memory changes the candidate, never grants authorization.
func Predict(s State) Prediction {
	p := Prediction{Model: "synthetic-v1", Action: "fast", Probability: 0.8, Inputs: s.refs(), Hypothetical: true}
	for _, e := range s.Episodes {
		if e.Scope == s.Scope && e.Context == s.Context && e.Simulation && e.Outcome == "failed" && e.Action == "fast" {
			p.Action = "normalize"
			p.Probability = 0.65
			p.Episodes = append(p.Episodes, e.ID)
		}
	}
	for _, n := range s.Nodes {
		if n.Question != "" && !s.contains(n.Question) {
			p.Missing = n.Question
			break
		}
	}
	return p
}

type Decision struct{ Operation, Target, Reason string }

// Decide is pure domain policy. The driver owns state and executes these results.
// New operations depend on current evidence; there is no universal faculty chain.
func Decide(s State) Decision {
	keys := map[string]bool{}
	for _, n := range s.Nodes {
		for k := range n.Facts {
			keys[k] = true
		}
	}
	ordered := []string{}
	for k := range keys {
		ordered = append(ordered, k)
	}
	sort.Strings(ordered)
	for _, k := range ordered {
		_, _, conflict := s.fact(k)
		if conflict {
			return Decision{"clarify", "", "conflicting assertions: " + k}
		}
	}
	for _, n := range s.Nodes {
		for _, id := range n.Requires {
			if !s.contains(id) {
				return Decision{"retrieve", id, "unresolved dependency of " + n.ID}
			}
		}
	}
	if s.Prediction.Model == "" {
		return Decision{"predict", "", "evaluate evidence and comparable episodes"}
	}
	if s.Prediction.Status == "abstain" || s.Prediction.Status == "clarify" {
		return Decision{s.Prediction.Status, "", "model proposal requests stopping; no action"}
	}
	if s.Prediction.Missing != "" {
		return Decision{"retrieve", s.Prediction.Missing, "prediction requires an observation"}
	}
	allowed, known, _ := s.fact("sandbox_allowed")
	checked, verified, _ := s.fact("sample_verified")
	if !known || !verified {
		return Decision{"abstain", "", "required facts unavailable"}
	}
	if !allowed || !checked {
		return Decision{"abstain", "", "precondition rejected"}
	}
	return Decision{"ready", s.Prediction.Action, "required evidence present; simulated action only"}
}

// Simulate observes the actual result of a small deterministic local computation.
// Both strategies try to sum the fixture's tokens. Predictions never supply the
// verdict. It performs no external I/O and cannot execute arbitrary action names.
func Simulate(action string, tokens []string, want int) string {
	outcome, _ := ObserveSimulation(action, tokens, want)
	return outcome
}

// ObserveSimulation reports what the parser/comparison actually observed. It
// never recommends the next strategy or records a provider-generated verdict.
func ObserveSimulation(action string, tokens []string, want int) (string, string) {
	if action != "fast" && action != "normalize" {
		return "unknown", ""
	}
	sum := 0
	for _, token := range tokens {
		if action == "normalize" {
			token = strings.Map(func(r rune) rune {
				if r >= '０' && r <= '９' {
					return '0' + r - '０'
				}
				return r
			}, token)
		}
		n, err := strconv.Atoi(token)
		if err != nil {
			diagnostic := "parse_error"
			if strings.ContainsFunc(token, func(r rune) bool { return r >= '０' && r <= '９' }) {
				diagnostic = "parse_error_full_width_digits"
			}
			return "failed", diagnostic
		}
		sum += n
	}
	if sum == want {
		return "success", "sum_verified"
	}
	return "failed", "sum_mismatch"
}
func clone[T any](v T) T { b, _ := json.Marshal(v); var out T; _ = json.Unmarshal(b, &out); return out }
func valid(n Node, scope string) bool {
	return n.ID != "" && n.Version != "" && n.Source != "" && n.Scope == scope && n.Current && !n.Deleted
}
func missing(id string) error { return fmt.Errorf("evidence unavailable: %s", id) }
