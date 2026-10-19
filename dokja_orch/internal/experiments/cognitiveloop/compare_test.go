package cognitiveloop

import (
	"context"
	"fmt"
	"strings"
	"testing"
)

// TestCompareTopologies is the ADR 0005 measurement: every topology must reach the same
// outcome and candidate on every scenario; the log lists what each one spent.
// Run with: go test ./internal/experiments/cognitiveloop -run TestCompareTopologies -v
func TestCompareTopologies(t *testing.T) {
	rows := []string{"scenario | topology | outcome | iterations | knowledge | experience | predict | acquire | total ops | bytes | trace"}
	for _, sc := range scenarios() {
		budget := sc.budget
		if budget.MaxIterations == 0 {
			budget = defaultBudget
		}
		var results []Result
		for _, topology := range topologies {
			result := topology.run(context.Background(), sc.task, sc.ports(), budget)
			results = append(results, result)
			ops := result.Operations
			rows = append(rows, fmt.Sprintf("%s | %s | %s | %d | %d | %d | %d | %d | %d | %d | %d",
				sc.name, topology.name, result.Outcome, result.Iterations, ops["knowledge"], ops["experience"],
				ops["predict"], ops["acquire"], ops["total"], result.Bytes, len(result.Trace)))
		}
		for _, other := range results[1:] {
			if other.Outcome != results[0].Outcome || other.Candidate.key() != results[0].Candidate.key() {
				t.Errorf("%s: topologies disagree: %s %q vs %s %q", sc.name,
					results[0].Outcome, results[0].Candidate.key(), other.Outcome, other.Candidate.key())
			}
		}
	}
	t.Log(fmt.Sprintf("\n%s", strings.Join(rows, "\n")))
}
