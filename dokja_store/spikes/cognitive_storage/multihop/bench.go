package multihop

import (
	"context"
	"errors"
	"fmt"
	"math"
	"reflect"
	"runtime"
	"sort"
	"time"
)

// Forest creates disconnected depth-six binary trees with diamonds, opposing
// labels, cycles and ineligible intermediate nodes. Root selection is explicit.
func Forest(trees int) (Graph, []int, error) {
	if trees < 1 || trees > 10000 {
		return Graph{}, nil, errors.New("trees must be 1..10000")
	}
	g := Graph{}
	roots := []int{}
	for tree := 0; tree < trees; tree++ {
		offset := tree * 127
		roots = append(roots, offset+1)
		for i := 1; i <= 127; i++ {
			n := Node{ID: offset + i, Scope: "owner", Current: true}
			if i > 1 {
				if i%13 == 0 {
					n.Scope = "guest"
				}
				if i%17 == 0 {
					n.Current = false
				}
				if i%19 == 0 {
					n.Deleted = true
				}
			}
			g.Nodes = append(g.Nodes, n)
			if i <= 63 {
				g.Edges = append(g.Edges, Edge{offset + i, offset + 2*i, "depends_on"}, Edge{offset + i, offset + 2*i + 1, "supported_by"})
			}
		}
		g.Edges = append(g.Edges, Edge{offset + 1, offset + 2, "contradicted_by"}, Edge{offset + 3, offset + 4, "depends_on"}, Edge{offset + 127, offset + 1, "depends_on"})
	}
	return g, roots, nil
}

type Measurement struct {
	Depth                        int
	Queries                      int
	P50MS, P95MS, P99MS          float64
	ClientAllocatedBytesPerQuery uint64
	TruncatedQueries             int
	CorrectQueries               int
}

// Measure prepares the reference outside timing, warms five queries per depth,
// and runs one caller sequentially. Allocation counts include client/driver only.
func Measure(ctx context.Context, r Reader, g Graph, roots []int, runs, limit int, timeout time.Duration) ([]Measurement, error) {
	if len(roots) == 0 || runs < 1 || limit < 1 || timeout <= 0 {
		return nil, errors.New("invalid measurement bounds")
	}
	roots = roots[:min(8, len(roots))]
	out := []Measurement{}
	for _, depth := range []int{1, 3, 6} {
		queries := []Query{}
		gold := []Result{}
		for _, root := range roots {
			q := Query{Root: root, Scope: "owner", Depth: depth, Limit: limit, MaxExpansions: 10000}
			expected, err := Reference(ctx, g, q)
			if err != nil {
				return nil, err
			}
			queries = append(queries, q)
			gold = append(gold, expected)
		}
		run := func(i int) (Result, error) {
			sub, cancel := context.WithTimeout(ctx, timeout)
			defer cancel()
			return Traverse(sub, r, queries[i%len(queries)])
		}
		for i := 0; i < 5; i++ {
			if _, err := run(i); err != nil {
				return nil, err
			}
		}
		m := Measurement{Depth: depth, Queries: runs}
		times := make([]float64, 0, runs)
		var before, after runtime.MemStats
		runtime.ReadMemStats(&before)
		for i := 0; i < runs; i++ {
			started := time.Now()
			got, err := run(i)
			times = append(times, float64(time.Since(started).Nanoseconds())/1e6)
			if err != nil {
				return nil, err
			}
			if !reflect.DeepEqual(got, gold[i%len(queries)]) {
				return nil, fmt.Errorf("depth %d query %d differs from reference", depth, i)
			}
			m.CorrectQueries++
			if got.Truncated {
				m.TruncatedQueries++
			}
		}
		runtime.ReadMemStats(&after)
		m.ClientAllocatedBytesPerQuery = (after.TotalAlloc - before.TotalAlloc) / uint64(runs)
		sort.Float64s(times)
		percentile := func(p float64) float64 { return times[int(math.Ceil(p*float64(len(times))))-1] }
		m.P50MS, m.P95MS, m.P99MS = percentile(.5), percentile(.95), percentile(.99)
		out = append(out, m)
	}
	return out, nil
}
