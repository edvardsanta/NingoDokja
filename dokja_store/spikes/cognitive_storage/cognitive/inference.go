package cognitive

import (
	inference "read_books/dokja_domain/dokja_inference"
	promptfiles "read_books/dokja_store/prompts"
)

func inferenceRequest(s State, c Config) inference.Request {
	r := inference.Request{Contract: inference.Version, TaskID: c.Attempt, Revision: s.Revision, Scope: s.Scope, Goal: s.Goal, RemainingCalls: c.MaxCalls - s.Calls + 1,
		RequiredFacts: []string{"sandbox_allowed", "sample_verified"},
		Actions: []inference.Action{
			{ID: "fast", Description: promptfiles.MustRead("inference_inferencerequest1")},
			{ID: "normalize", Description: promptfiles.MustRead("inference_inferencerequest2")},
		}, Evidence: []inference.Evidence{}, Experiences: []inference.Experience{}}
	for _, n := range s.Nodes {
		facts := clone(n.Facts)
		if facts == nil {
			facts = map[string]bool{}
		}
		r.Evidence = append(r.Evidence, inference.Evidence{Ref: inference.Ref{ID: n.ID, Version: n.Version, Source: n.Source}, Facts: facts, Requires: append([]string{}, n.Requires...), Question: n.Question})
	}
	for _, e := range s.Episodes {
		r.Experiences = append(r.Experiences, inference.Experience{ID: e.ID, Action: e.Action, Outcome: e.Outcome, Simulation: e.Simulation, Diagnostic: e.Diagnostic})
	}
	return r
}
