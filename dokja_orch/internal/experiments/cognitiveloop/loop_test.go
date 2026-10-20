package cognitiveloop

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"
)

var home = Scope{Actor: "owner", Channel: "study"}
var elsewhere = Scope{Actor: "guest", Channel: "other"}

// fakePorts are deterministic capabilities. knowledge is called with the call number so
// a store can change between reads.
type fakePorts struct {
	mu         sync.Mutex
	calls      int
	knowledge  func(call int) []Evidence
	experience []Evidence
	predict    func(Candidate) Prediction
	acquire    map[string]Evidence
	fail       string
	slow       time.Duration
}

func (f *fakePorts) Knowledge(_ context.Context, _ Scope, _ string) ([]Evidence, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	if f.knowledge == nil {
		return nil, nil
	}
	return f.knowledge(f.calls), nil
}

func (f *fakePorts) Experience(context.Context, Scope, string) ([]Evidence, error) {
	return f.experience, nil
}

func (f *fakePorts) Predict(ctx context.Context, candidate Candidate, _ []Evidence) (Prediction, error) {
	if f.fail == "predict" {
		return Prediction{}, errors.New("memory unavailable")
	}
	if f.slow > 0 {
		time.Sleep(f.slow)
	}
	if f.predict == nil {
		return Prediction{Probability: 0.8, Model: "fake-v1"}, nil
	}
	return f.predict(candidate), nil
}

func (f *fakePorts) Acquire(_ context.Context, _ Scope, premise string) (Evidence, error) {
	return f.acquire[premise], nil
}

func method(id, source string, requires ...string) Evidence {
	return Evidence{ID: id, Kind: KindKnowledge, Scope: home, Version: "v1", Source: source,
		Method: "argument-outline", Requires: requires}
}

func knowledgeOf(evidence ...Evidence) func(int) []Evidence {
	return func(int) []Evidence { return evidence }
}

func procedure(id, name string, requires ...string) Evidence {
	return Evidence{ID: id, Kind: KindExperience, Scope: home, Version: "v1",
		Method: "argument-outline", Procedure: name, Requires: requires}
}

func observed(premise string, holds bool) Evidence {
	return Evidence{ID: fmt.Sprintf("obs:%s", premise), Kind: KindObservation, Scope: home,
		Version: "v1", Premise: premise, Holds: holds}
}

var defaultBudget = Budget{MaxIterations: 6, MaxOperations: 40, Deadline: time.Second}

type runner struct {
	name string
	run  func(context.Context, Task, Ports, Budget) Result
}

var topologies = []runner{
	{"A-supervised", Supervised}, {"A-rereading", SupervisedRereading}, {"B-domain-internal", DomainInternal},
}

func task(observations ...Evidence) Task {
	return Task{ID: "t1", Goal: "summarize", Scope: home, Observations: observations}
}

func traced(result Result, operation, detail string) bool {
	for _, entry := range result.Trace {
		if entry.Operation == operation && strings.Contains(entry.Detail, detail) {
			return true
		}
	}
	return false
}

type scenario struct {
	name      string
	task      Task
	ports     func() *fakePorts
	budget    Budget
	outcome   string
	from      string
	procedure string
	reason    string
	check     func(*testing.T, Result)
}

func scenarios() []scenario {
	return []scenario{
		{
			name:    "cold start without knowledge abstains",
			task:    task(),
			ports:   func() *fakePorts { return &fakePorts{} },
			outcome: OutcomeAbstain, reason: "no general method",
		},
		{
			name:    "knowledge without experience applies the general method",
			task:    task(),
			ports:   func() *fakePorts { return &fakePorts{knowledge: knowledgeOf(method("k1", "text"))} },
			outcome: OutcomeReady, from: "knowledge-only",
		},
		{
			name: "applicable experience changes the candidate",
			task: task(observed("scanned", true)),
			ports: func() *fakePorts {
				return &fakePorts{knowledge: knowledgeOf(method("k1", "book")),
					experience: []Evidence{procedure("e1", "sectioned-ocr", "scanned")}}
			},
			outcome: OutcomeReady, from: "experience", procedure: "sectioned-ocr",
			check: func(t *testing.T, r Result) {
				if strings.Join(r.Candidate.Supports, ",") != "k1,e1" {
					t.Fatalf("candidate should cite its knowledge and experience: %v", r.Candidate.Supports)
				}
			},
		},
		{
			name: "a missing premise is asked for before settling",
			task: task(),
			ports: func() *fakePorts {
				return &fakePorts{knowledge: knowledgeOf(method("k1", "book")),
					experience: []Evidence{procedure("e1", "sectioned-ocr", "scanned")},
					acquire:    map[string]Evidence{"scanned": observed("scanned", true)}}
			},
			outcome: OutcomeReady, from: "experience", procedure: "sectioned-ocr",
			check: func(t *testing.T, r Result) {
				if !traced(r, stepNeedEvidence, "scanned") {
					t.Fatalf("the evidence request should be traced: %#v", r.Trace)
				}
			},
		},
		{
			name: "a prediction can expose a missing premise",
			task: task(),
			ports: func() *fakePorts {
				ports := &fakePorts{knowledge: knowledgeOf(method("k1", "text")),
					acquire: map[string]Evidence{"language": observed("language", true)}}
				asked := false
				ports.predict = func(Candidate) Prediction {
					if !asked {
						asked = true
						return Prediction{Probability: 0.8, Missing: "language"}
					}
					return Prediction{Probability: 0.8}
				}
				return ports
			},
			outcome: OutcomeReady, from: "knowledge-only",
			check: func(t *testing.T, r Result) {
				if !traced(r, stepNeedEvidence, "language") {
					t.Fatalf("the premise the prediction exposed should be requested: %#v", r.Trace)
				}
			},
		},
		{
			name: "contrary evidence falls back to the general method",
			task: task(),
			ports: func() *fakePorts {
				return &fakePorts{knowledge: knowledgeOf(method("k1", "book")),
					experience: []Evidence{procedure("e1", "sectioned-ocr", "scanned")},
					acquire:    map[string]Evidence{"scanned": observed("scanned", false)}}
			},
			outcome: OutcomeReady, from: "knowledge-only",
		},
		{
			name: "contrary evidence against the method abstains",
			task: task(),
			ports: func() *fakePorts {
				return &fakePorts{knowledge: knowledgeOf(method("k1", "book", "has_text")),
					acquire: map[string]Evidence{"has_text": observed("has_text", false)}}
			},
			outcome: OutcomeAbstain, reason: "has_text does not hold",
		},
		{
			name: "a poorly predicted procedure is revised away",
			task: task(observed("scanned", true)),
			ports: func() *fakePorts {
				return &fakePorts{knowledge: knowledgeOf(method("k1", "book")),
					experience: []Evidence{procedure("e1", "sectioned-ocr", "scanned")},
					predict: func(c Candidate) Prediction {
						if c.Procedure == "sectioned-ocr" {
							return Prediction{Probability: 0.2}
						}
						return Prediction{Probability: 0.7}
					}}
			},
			outcome: OutcomeReady, from: "knowledge-only",
			check: func(t *testing.T, r Result) {
				if !traced(r, stepRevise, "sectioned-ocr") {
					t.Fatalf("the revision should be traced: %#v", r.Trace)
				}
			},
		},
		{
			name: "unavailable evidence stagnates instead of looping",
			task: task(),
			ports: func() *fakePorts {
				return &fakePorts{knowledge: knowledgeOf(method("k1", "book")),
					experience: []Evidence{procedure("e1", "sectioned-ocr", "scanned")}}
			},
			outcome: OutcomeStagnated, reason: "unresolved premise: scanned",
			check: func(t *testing.T, r Result) {
				if len(r.Dropped) != 0 || len(r.Unavailable) == 0 || r.Unavailable[0] != "scanned" {
					t.Fatalf("missing evidence is unavailable, not foreign: dropped %v, unavailable %v", r.Dropped, r.Unavailable)
				}
			},
		},
		{
			name: "a flapping store is reported as oscillation",
			task: task(),
			ports: func() *fakePorts {
				return &fakePorts{
					knowledge: func(call int) []Evidence {
						k := method("k1", "book")
						k.Version = fmt.Sprintf("v%d", call%2)
						return []Evidence{k}
					},
					experience: []Evidence{procedure("e1", "sectioned-ocr", "scanned")}}
			},
			outcome: OutcomeOscillated,
		},
		{
			name:    "a failing dependency stops with its reason",
			task:    task(),
			ports:   func() *fakePorts { return &fakePorts{knowledge: knowledgeOf(method("k1", "text")), fail: "predict"} },
			outcome: OutcomeFailed, reason: "predict failed",
		},
		{
			name: "a late result is discarded at the deadline",
			task: task(),
			ports: func() *fakePorts {
				return &fakePorts{knowledge: knowledgeOf(method("k1", "text")), slow: 200 * time.Millisecond}
			},
			budget:  Budget{MaxIterations: 6, MaxOperations: 40, Deadline: 30 * time.Millisecond},
			outcome: OutcomeBudgetExhausted, reason: "deadline",
			check: func(t *testing.T, r Result) {
				if len(r.Predictions) != 0 || !traced(r, "predict", "late result discarded") {
					t.Fatalf("a late prediction must not be applied: %#v", r)
				}
			},
		},
		{
			name: "the iteration budget ends an unfinished task",
			task: task(),
			ports: func() *fakePorts {
				return &fakePorts{knowledge: knowledgeOf(method("k1", "book")),
					experience: []Evidence{procedure("e1", "sectioned-ocr", "scanned")},
					acquire:    map[string]Evidence{"scanned": observed("scanned", true)}}
			},
			budget:  Budget{MaxIterations: 1, MaxOperations: 40, Deadline: time.Second},
			outcome: OutcomeBudgetExhausted, reason: "iteration budget",
		},
		{
			name: "evidence from another scope is dropped",
			task: task(),
			ports: func() *fakePorts {
				foreign := procedure("e-guest", "guest-shortcut")
				foreign.Scope = elsewhere
				return &fakePorts{knowledge: knowledgeOf(method("k1", "text")), experience: []Evidence{foreign}}
			},
			outcome: OutcomeReady, from: "knowledge-only",
			check: func(t *testing.T, r Result) {
				if len(r.Dropped) == 0 || r.Dropped[0] != "e-guest" {
					t.Fatalf("foreign experience must be dropped and reported: %v", r.Dropped)
				}
			},
		},
		{
			name: "predictions are always marked hypothetical",
			task: task(),
			ports: func() *fakePorts {
				return &fakePorts{knowledge: knowledgeOf(method("k1", "text")),
					predict: func(Candidate) Prediction { return Prediction{Probability: 0.9, Hypothetical: false} }}
			},
			outcome: OutcomeReady, from: "knowledge-only",
			check: func(t *testing.T, r Result) {
				for _, p := range r.Predictions {
					if !p.Hypothetical {
						t.Fatalf("a prediction escaped as an observation: %#v", p)
					}
				}
			},
		},
	}
}

func TestScenariosUnderBothTopologies(t *testing.T) {
	for _, topology := range topologies {
		for _, sc := range scenarios() {
			t.Run(fmt.Sprintf("%s/%s", topology.name, sc.name), func(t *testing.T) {
				budget := sc.budget
				if budget.MaxIterations == 0 {
					budget = defaultBudget
				}
				result := topology.run(context.Background(), sc.task, sc.ports(), budget)
				if result.Outcome != sc.outcome || !strings.Contains(result.Reason, sc.reason) {
					t.Fatalf("expected %s (%q), got %s (%q); trace %#v", sc.outcome, sc.reason, result.Outcome, result.Reason, result.Trace)
				}
				if sc.from != "" && (result.Candidate == nil || result.Candidate.From != sc.from || result.Candidate.Procedure != sc.procedure) {
					t.Fatalf("expected a %s candidate %q, got %#v", sc.from, sc.procedure, result.Candidate)
				}
				if sc.check != nil {
					sc.check(t, result)
				}
			})
		}
	}
}

func TestEquivalentContentFromAnySourceSelectsTheSameMethod(t *testing.T) {
	for _, topology := range topologies {
		for _, source := range []string{"book", "text", "feed"} {
			ports := &fakePorts{knowledge: knowledgeOf(method(fmt.Sprintf("k-%s", source), source))}
			result := topology.run(context.Background(), task(), ports, defaultBudget)
			if result.Outcome != OutcomeReady || result.Candidate.Method != "argument-outline" ||
				result.Candidate.Supports[0] != fmt.Sprintf("k-%s", source) {
				t.Fatalf("%s/%s: same method with its own provenance expected, got %#v", topology.name, source, result)
			}
		}
	}
}

func TestConcurrentTasksDoNotShareState(t *testing.T) {
	for _, topology := range topologies {
		var wg sync.WaitGroup
		results := make([]Result, 2)
		for i, premise := range []bool{true, false} {
			wg.Add(1)
			go func(i int, holds bool) {
				defer wg.Done()
				ports := &fakePorts{knowledge: knowledgeOf(method("k1", "book")),
					experience: []Evidence{procedure("e1", "sectioned-ocr", "scanned")}}
				results[i] = topology.run(context.Background(), task(observed("scanned", holds)), ports, defaultBudget)
			}(i, premise)
		}
		wg.Wait()
		if results[0].Candidate.From != "experience" || results[1].Candidate.From != "knowledge-only" {
			t.Fatalf("%s: each task must reason over its own premises: %#v / %#v", topology.name, results[0].Candidate, results[1].Candidate)
		}
	}
}

func TestOnlyAnObservedOutcomeBecomesExperience(t *testing.T) {
	ready := Result{Outcome: OutcomeReady, Candidate: &Candidate{Method: "argument-outline", Procedure: "sectioned-ocr"},
		Predictions: []Prediction{{Probability: 0.9, Hypothetical: true}}}
	if _, ok := RecordObserved(task(), ready, ""); ok {
		t.Fatal("a predicted success must not be recorded as experience")
	}
	if _, ok := RecordObserved(task(), ready, "unknown"); ok {
		t.Fatal("an unknown outcome stays an unresolved attempt")
	}
	if _, ok := RecordObserved(task(), Result{Outcome: OutcomeAbstain}, "accepted"); ok {
		t.Fatal("an abstention executed nothing to observe")
	}
	experience, ok := RecordObserved(task(), ready, "accepted")
	if !ok || experience.Procedure != "sectioned-ocr" || experience.Scope != home || experience.Source != "observed:accepted" {
		t.Fatalf("unexpected observed experience: %#v, %v", experience, ok)
	}
}
