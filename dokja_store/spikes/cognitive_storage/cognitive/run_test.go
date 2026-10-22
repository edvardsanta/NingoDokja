package cognitive

import (
	"context"
	"os"
	"reflect"
	"testing"
	"time"
)

func config(mode, attempt string) Config {
	depth := 6
	if mode == "bounded" {
		depth = 2
	}
	return Config{Mode: mode, MaxHops: depth, MaxIterations: 20, MaxCalls: 20, Deadline: time.Second, Authorized: true, Attempt: attempt, Scope: "owner", Context: "synthetic-numeric-document", Tokens: []string{"１２", "3"}, Expected: 15}
}
func TestDependentEvidenceAndReusableConsequences(t *testing.T) {
	ctx := context.Background()
	store := NewMemory(Fixture())
	single := Run(ctx, store, config("single", "single"))
	bounded := Run(ctx, store, config("bounded", "bounded"))
	if single.Outcome != "abstain" || bounded.Outcome != "abstain" || len(store.Episodes) != 0 {
		t.Fatal(single, bounded)
	}
	first := Run(ctx, store, config("iterative", "first"))
	if first.Outcome != "completed" || first.Hops != 3 || first.Action != "fast" || first.Observed != "failed" {
		t.Fatalf("first: %+v", first)
	}
	var ps []Prediction
	for i, tr := range first.Trace {
		if tr.Revision != i+1 {
			t.Fatal("revision", tr)
		}
		if tr.Prediction != nil {
			ps = append(ps, *tr.Prediction)
		}
	}
	if len(ps) != 2 || ps[0].Missing != "sample" || ps[1].Missing != "" || len(ps[1].Inputs) != 4 {
		t.Fatal(ps)
	}
	second := Run(ctx, store, config("iterative", "second"))
	if second.Action != "normalize" || second.Observed != "success" || len(second.Prediction.Episodes) != 1 || second.Prediction.Episodes[0] != "first" {
		t.Fatalf("second: %+v", second)
	}
	control := Run(ctx, NewMemory(Fixture()), config("iterative", "without-memory"))
	if control.Action != first.Action || control.Observed != first.Observed {
		t.Fatal("memory ablation not causal")
	}
	if len(store.Episodes) != 2 || store.Episodes[0].Predicted != 0.8 || store.Episodes[1].Predicted != 0.65 {
		t.Fatal(store.Episodes)
	}
}
func TestEvidenceInterventionsChangeSearchAndDecision(t *testing.T) {
	for _, tc := range []struct {
		name, outcome string
		change        func([]Node)
	}{
		{"missing", "abstain", func(n []Node) { n[3].Deleted = true }},
		{"contrary", "abstain", func(n []Node) { n[3].Facts["sample_verified"] = false }},
		{"conflict", "clarify", func(n []Node) { n[3].Facts["sandbox_allowed"] = false }},
		{"foreign", "abstain", func(n []Node) { n[3].Scope = "guest" }},
		{"superseded", "abstain", func(n []Node) { n[3].Current = false }},
		{"changed-question", "abstain", func(n []Node) { n[2].Question = "different-sample" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			n := Fixture()
			tc.change(n)
			m := NewMemory(n)
			got := Run(context.Background(), m, config("iterative", tc.name))
			if got.Outcome != tc.outcome || got.Observed != "" || len(m.Episodes) != 0 {
				t.Fatalf("%+v", got)
			}
		})
	}
}
func TestCyclesAndBudgetsStopWithoutAction(t *testing.T) {
	n := Fixture()
	n[1].Requires = []string{"document"}
	m := NewMemory(n)
	// A dependency cycle with no sufficient facts terminates in abstention.
	got := Run(context.Background(), m, config("iterative", "cycle"))
	if got.Outcome != "abstain" || got.Observed != "" {
		t.Fatal(got)
	}
	for _, change := range []func(*Config){func(c *Config) { c.MaxIterations = 1 }, func(c *Config) { c.MaxCalls = 1 }, func(c *Config) { c.Authorized = false }} {
		c := config("iterative", "bounded")
		change(&c)
		m = NewMemory(Fixture())
		got = Run(context.Background(), m, c)
		if got.Outcome != "budget_exhausted" && got.Outcome != "authorization_required" {
			t.Fatal(got)
		}
		if len(m.Episodes) != 0 {
			t.Fatal(m.Episodes)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	got = Run(ctx, NewMemory(Fixture()), config("iterative", "cancel"))
	if got.Outcome != "budget_exhausted" {
		t.Fatal(got)
	}
}
func TestMemoryScopeAndFirstObservation(t *testing.T) {
	m := NewMemory(Fixture())
	first := Run(context.Background(), m, config("iterative", "attempt"))
	e := m.Episodes[0]
	if err := m.Record(context.Background(), e); err != nil {
		t.Fatal(err)
	}
	e.Outcome = "success"
	if err := m.Record(context.Background(), e); err == nil {
		t.Fatal("overwrote observation")
	}
	c := config("iterative", "new-context")
	c.Context = "different"
	got := Run(context.Background(), m, c)
	if got.Action != first.Action {
		t.Fatal("context leaked")
	}
}
func TestPostgresVertical(t *testing.T) {
	dsn := os.Getenv("COGNITIVE_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("PostgreSQL not configured")
	}
	ctx := context.Background()
	p, err := NewPostgres(ctx, dsn, Fixture())
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close(ctx)
	mem := NewMemory(Fixture())
	for _, mode := range []string{"single", "bounded", "iterative", "iterative"} {
		c := config(mode, mode)
		if mode == "iterative" {
			c.Attempt = time.Now().Format("150405.000000000")
		}
		want := Run(ctx, mem, c)
		got := Run(ctx, p, c)
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("postgres mismatch\ngot %+v\nwant %+v", got, want)
		}
	}
}

func TestBoundedDepthThreeMatchesDecisionWithFewerPredictions(t *testing.T) {
	c := config("bounded", "fixed-three")
	c.MaxHops = 3
	a := Run(context.Background(), NewMemory(Fixture()), c)
	b := Run(context.Background(), NewMemory(Fixture()), config("iterative", "adaptive"))
	if a.Outcome != b.Outcome || a.Action != b.Action || a.Observed != b.Observed || a.Hops != b.Hops {
		t.Fatalf("fixed=%+v iterative=%+v", a, b)
	}
}

type failingStore struct {
	Store
	recordFail bool
	late       bool
}

func (f failingStore) Record(ctx context.Context, e Episode) error {
	if f.recordFail {
		return context.Canceled
	}
	return f.Store.Record(ctx, e)
}
func (f failingStore) Get(ctx context.Context, scope, id string) (Node, error) {
	if f.late {
		<-ctx.Done()
		return Fixture()[1], nil
	}
	return f.Store.Get(ctx, scope, id)
}
func TestFailureReceiptsAndLateEvidence(t *testing.T) {
	m := NewMemory(Fixture())
	s := Run(context.Background(), failingStore{Store: m, recordFail: true}, config("iterative", "record-failure"))
	if s.Outcome != "record_failed" || s.Observed != "failed" || len(m.Episodes) != 0 {
		t.Fatal(s)
	}
	c := config("iterative", "late")
	c.Deadline = time.Millisecond
	s = Run(context.Background(), failingStore{Store: m, late: true}, c)
	if s.Outcome != "budget_exhausted" || len(s.Nodes) != 1 || s.Observed != "" {
		t.Fatal(s)
	}
}
func TestPostgresInterventionsAndFirstObservation(t *testing.T) {
	dsn := os.Getenv("COGNITIVE_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("PostgreSQL not configured")
	}
	for _, scenario := range []string{"missing", "contrary", "conflict", "foreign", "superseded", "changed-question"} {
		t.Run(scenario, func(t *testing.T) {
			n := Fixture()
			switch scenario {
			case "missing":
				n = n[:3]
			case "contrary":
				n[3].Facts["sample_verified"] = false
			case "conflict":
				n[3].Facts["sandbox_allowed"] = false
			case "foreign":
				n[3].Scope = "guest"
			case "superseded":
				n[3].Current = false
			case "changed-question":
				n[2].Question = "different-sample"
			}
			ctx := context.Background()
			p, err := NewPostgres(ctx, dsn, n)
			if err != nil {
				t.Fatal(err)
			}
			defer p.Close(ctx)
			c := config("iterative", scenario)
			want := Run(ctx, NewMemory(n), c)
			got := Run(ctx, p, c)
			if !reflect.DeepEqual(got, want) {
				t.Fatal("engine changed intervention", got, want)
			}
			episodes, err := p.Recall(ctx, c.Scope, c.Context)
			if err != nil || len(episodes) != 0 {
				t.Fatal(episodes, err)
			}
		})
	}
	ctx := context.Background()
	p, err := NewPostgres(ctx, dsn, Fixture())
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close(ctx)
	c := config("iterative", "receipt")
	Run(ctx, p, c)
	es, err := p.Recall(ctx, c.Scope, c.Context)
	if err != nil || len(es) != 1 {
		t.Fatal(es, err)
	}
	if err = p.Record(ctx, es[0]); err != nil {
		t.Fatal(err)
	}
	es[0].Outcome = "success"
	if err = p.Record(ctx, es[0]); err == nil {
		t.Fatal("overwrote first observation")
	}
	foreign, err := p.Recall(ctx, "guest", c.Context)
	if err != nil || len(foreign) != 0 {
		t.Fatal("scope leak", foreign, err)
	}
}
