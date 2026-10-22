package cognitive

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"time"
)

type Measurement struct {
	Mode                                                                       string
	Runs, Completed, Successful, Hops, Calls, Iterations, Evidence, TraceBytes int
	LatencyMS                                                                  struct{ P50, P95, P99 float64 }
	Outcome, Action                                                            string
	Brier                                                                      *float64 `json:",omitempty"`
}
type Report struct {
	Engine                               string
	Synthetic                            bool
	Nodes, VectorDimensions, Concurrency int
	Measurements                         []Measurement
	ExampleFirst, ExampleSecond          State
}
type Factory func(context.Context) (Store, func(), error)

// Evaluate uses fresh stores for every independent trial. The memory condition
// alone performs a prior task. Setup and the prior task are outside timed spans.
func Evaluate(ctx context.Context, engine string, n int, factory Factory) (Report, error) {
	report := Report{Engine: engine, Synthetic: true, Nodes: 4, VectorDimensions: 3, Concurrency: 1}
	if n < 1 {
		return report, fmt.Errorf("runs must be positive")
	}
	for _, mode := range []string{"single", "bounded2", "bounded3", "iterative", "iterative-memory"} {
		m := Measurement{Mode: mode, Runs: n}
		times := []float64{}
		squared := 0.0
		scored := 0
		for i := 0; i < n; i++ {
			store, close, err := factory(ctx)
			if err != nil {
				return report, err
			}
			c := Config{Mode: "iterative", MaxHops: 6, MaxIterations: 20, MaxCalls: 20, Deadline: time.Second, Authorized: true, Attempt: "first", Scope: "owner", Context: "synthetic-numeric-document", Tokens: []string{"１２", "3"}, Expected: 15}
			switch mode {
			case "single":
				c.Mode = "single"
			case "bounded2":
				c.Mode = "bounded"
				c.MaxHops = 2
			case "bounded3":
				c.Mode = "bounded"
				c.MaxHops = 3
			}
			if mode == "iterative-memory" {
				first := Run(ctx, store, c)
				if first.Outcome != "completed" {
					close()
					return report, fmt.Errorf("warmup task failed: %s", first.Outcome)
				}
				if i == 0 {
					report.ExampleFirst = first
				}
				c.Attempt = "second"
			}
			began := time.Now()
			got := Run(ctx, store, c)
			times = append(times, float64(time.Since(began))/float64(time.Millisecond))
			close()
			if got.Outcome == "failed" || got.Outcome == "record_failed" || got.Outcome == "budget_exhausted" {
				return report, fmt.Errorf("%s trial failed: %s", mode, got.Outcome)
			}
			if got.Outcome == "completed" {
				m.Completed++
			}
			if got.Observed == "success" {
				m.Successful++
			}
			if got.Observed == "success" || got.Observed == "failed" {
				actual := 0.0
				if got.Observed == "success" {
					actual = 1
				}
				squared += math.Pow(got.Prediction.Probability-actual, 2)
				scored++
			}
			b, _ := json.Marshal(got.Trace)
			m.TraceBytes += len(b)
			m.Hops += got.Hops
			m.Calls += got.Calls
			m.Iterations += got.Iterations
			m.Evidence += len(got.Nodes)
			m.Outcome = got.Outcome
			m.Action = got.Action
			if mode == "iterative-memory" && i == 0 {
				report.ExampleSecond = got
			}
		}
		sort.Float64s(times)
		quantile := func(p float64) float64 { return times[int(math.Ceil(float64(n)*p))-1] }
		m.LatencyMS.P50 = quantile(0.5)
		m.LatencyMS.P95 = quantile(0.95)
		m.LatencyMS.P99 = quantile(0.99)
		m.TraceBytes /= n
		m.Hops /= n
		m.Calls /= n
		m.Iterations /= n
		m.Evidence /= n
		if scored > 0 {
			v := squared / float64(scored)
			m.Brier = &v
		}
		report.Measurements = append(report.Measurements, m)
	}
	return report, nil
}
