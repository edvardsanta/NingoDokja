package spike

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"
)

// ModeResult is how one search mode of one engine did on every query of a fixture.
type ModeResult struct {
	Mode        Mode               `json:"mode"`
	Queries     int                `json:"queries"`
	ExactMatch  int                `json:"exact_match"`
	MeanRecall  float64            `json:"mean_recall_at_k"`
	Underfilled int                `json:"underfilled"`
	Foreign     int                `json:"foreign_scope_rows"`
	Ineligible  int                `json:"ineligible_rows"`
	P50Ms       float64            `json:"p50_ms"`
	P95Ms       float64            `json:"p95_ms"`
	P99Ms       float64            `json:"p99_ms"`
	ByQuery     map[string]Summary `json:"by_query"`
}

// Summary is one query type's share of a mode's result.
type Summary struct {
	Queries     int     `json:"queries"`
	MeanRecall  float64 `json:"mean_recall_at_k"`
	Underfilled int     `json:"underfilled"`
	P95Ms       float64 `json:"p95_ms"`
	// UsesVectorIndex tells, for the first query of this type, whether the plan reads the
	// vector index; empty when the engine cannot show its plan.
	UsesVectorIndex string `json:"uses_vector_index,omitempty"`
	Plan            string `json:"plan,omitempty"`
}

// EngineResult is one engine on one fixture.
type EngineResult struct {
	Engine    string            `json:"engine"`
	Describe  map[string]string `json:"describe"`
	LoadMs    float64           `json:"load_ms"`
	DeleteMs  float64           `json:"delete_ms"`
	IndexMs   float64           `json:"index_ms"`
	Modes     []ModeResult      `json:"modes"`
	Docs      int               `json:"docs"`
	Chunks    int               `json:"chunks"`
	Dimension int               `json:"dimension"`
}

// Planner is an engine that can show its query plan; a plan naming chunks_vec reads the
// vector index.
type Planner interface {
	Plan(ctx context.Context, q Query, mode Mode) (string, error)
}

// Run loads the fixture into one engine, deletes through the engine as an application
// would, builds its vector index, warms each mode with one pass and times the second.
func Run(ctx context.Context, s Store, f Fixture, gold Gold) (EngineResult, error) {
	result := EngineResult{Engine: s.Name(), Docs: len(f.Docs), Chunks: len(f.Chunks), Dimension: f.Config.Dim}
	if err := s.Reset(ctx, f.Config.Dim); err != nil {
		return result, fmt.Errorf("reset: %w", err)
	}
	started := time.Now()
	if err := s.Load(ctx, f); err != nil {
		return result, fmt.Errorf("load: %w", err)
	}
	result.LoadMs = ms(time.Since(started))
	started = time.Now()
	for _, id := range f.Deleted {
		if err := s.Delete(ctx, id); err != nil {
			return result, fmt.Errorf("delete %s: %w", id, err)
		}
	}
	result.DeleteMs = ms(time.Since(started))
	started = time.Now()
	if err := s.BuildIndex(ctx); err != nil {
		return result, fmt.Errorf("index: %w", err)
	}
	result.IndexMs = ms(time.Since(started))

	for _, mode := range s.Modes() {
		for _, q := range f.Queries {
			if _, err := s.Query(ctx, q, mode); err != nil {
				return result, fmt.Errorf("%s warm-up: %w", mode, err)
			}
		}
		m := ModeResult{Mode: mode, Queries: len(f.Queries), ByQuery: map[string]Summary{}}
		var all []time.Duration
		byName := map[string][]time.Duration{}
		recall := map[string]float64{}
		for qi, q := range f.Queries {
			started := time.Now()
			hits, err := s.Query(ctx, q, mode)
			elapsed := time.Since(started)
			if err != nil {
				return result, fmt.Errorf("%s query %d: %w", mode, qi, err)
			}
			check := Compare(f, gold.Hits[qi], gold.Eligible[qi], q, hits)
			all = append(all, elapsed)
			byName[q.Name] = append(byName[q.Name], elapsed)
			recall[q.Name] += check.Recall
			m.MeanRecall += check.Recall
			m.Foreign += check.Foreign
			m.Ineligible += check.Ineligible
			s := m.ByQuery[q.Name]
			s.Queries++
			if check.Underfilled {
				m.Underfilled++
				s.Underfilled++
			}
			m.ByQuery[q.Name] = s
			if check.ExactMatch {
				m.ExactMatch++
			}
		}
		m.MeanRecall /= float64(len(f.Queries))
		m.P50Ms, m.P95Ms, m.P99Ms = percentile(all, 50), percentile(all, 95), percentile(all, 99)
		for name, s := range m.ByQuery {
			s.MeanRecall = recall[name] / float64(s.Queries)
			s.P95Ms = percentile(byName[name], 95)
			m.ByQuery[name] = s
		}
		if planner, ok := s.(Planner); ok {
			for _, q := range f.Queries {
				summary := m.ByQuery[q.Name]
				if summary.Plan != "" {
					continue
				}
				plan, err := planner.Plan(ctx, q, mode)
				if err != nil {
					return result, fmt.Errorf("%s plan: %w", mode, err)
				}
				summary.Plan, summary.UsesVectorIndex = plan, fmt.Sprint(strings.Contains(plan, "chunks_vec"))
				m.ByQuery[q.Name] = summary
			}
		}
		result.Modes = append(result.Modes, m)
	}
	result.Describe = s.Describe(ctx)
	return result, nil
}

func ms(d time.Duration) float64 { return float64(d.Microseconds()) / 1000 }

func percentile(values []time.Duration, p int) float64 {
	if len(values) == 0 {
		return 0
	}
	sorted := append([]time.Duration(nil), values...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i] < sorted[j] })
	index := (len(sorted)*p + 99) / 100
	return ms(sorted[max(0, min(index-1, len(sorted)-1))])
}
