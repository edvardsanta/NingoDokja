package multihop

import (
	"context"
	"errors"
	"fmt"
	"sort"
)

// Reader is an immutable fixture snapshot. Expand includes ineligible targets:
// their edges still consume the examination budget. Pagination is by (To,Kind).
// Readers support one caller at a time; separate instances isolate concurrent runs.
type Reader interface {
	Root(context.Context, int, string) (bool, error)
	Expand(context.Context, int, string, Cursor, int) ([]Candidate, error)
	Close() error
}
type Cursor struct {
	To   int
	Kind string
	Set  bool
}
type Candidate struct {
	To       int
	Kind     string
	Eligible bool
}

// Traverse runs identical bounded BFS against each storage engine. It neither
// preloads the graph nor delegates inference to reachability. This first adapter
// comparison measures paged adjacency queries, not native recursive SQL/Cypher.
func Traverse(ctx context.Context, r Reader, q Query) (Result, error) {
	if q.Scope == "" || q.Depth < 1 || q.Depth > 6 || q.Limit < 1 || q.MaxExpansions < 1 {
		return Result{}, errors.New("invalid traversal bounds or scope")
	}
	if err := ctx.Err(); err != nil {
		return Result{}, err
	}
	eligible, err := r.Root(ctx, q.Root, q.Scope)
	if err != nil {
		return Result{}, err
	}
	if !eligible {
		return Result{}, nil
	}
	queue := []Path{{Nodes: []int{q.Root}}}
	out := Result{}
	examined := 0
	for head := 0; head < len(queue); head++ {
		p := queue[head]
		if len(p.Kinds) == q.Depth {
			continue
		}
		cursor := Cursor{}
		for {
			if err := ctx.Err(); err != nil {
				return Result{}, err
			}
			size := min(127, q.MaxExpansions-examined, q.Limit-len(out.Paths)) + 1
			candidates, err := r.Expand(ctx, p.Nodes[len(p.Nodes)-1], q.Scope, cursor, size)
			if err != nil {
				return Result{}, err
			}
			if len(candidates) > size {
				return Result{}, errors.New("adapter exceeded page bound")
			}
			for _, e := range candidates {
				if err := ctx.Err(); err != nil {
					return Result{}, err
				}
				if cursor.Set && (e.To < cursor.To || (e.To == cursor.To && e.Kind <= cursor.Kind)) {
					return Result{}, errors.New("adapter returned unordered or repeated edge")
				}
				cursor = Cursor{To: e.To, Kind: e.Kind, Set: true}
				examined++
				if examined > q.MaxExpansions {
					return Result{}, ErrBudget
				}
				if !e.Eligible {
					continue
				}
				cycle := false
				for _, id := range p.Nodes {
					if id == e.To {
						cycle = true
						break
					}
				}
				if cycle {
					continue
				}
				if len(out.Paths) == q.Limit {
					out.Truncated = true
					return out, nil
				}
				next := Path{Nodes: append(append([]int{}, p.Nodes...), e.To), Kinds: append(append([]string{}, p.Kinds...), e.Kind)}
				out.Paths = append(out.Paths, next)
				queue = append(queue, next)
			}
			if len(candidates) < size {
				break
			}
		}
	}
	if err := ctx.Err(); err != nil {
		return Result{}, err
	}
	return out, nil
}

func normalized(g Graph) (Graph, error) {
	nodes := map[int]bool{}
	for _, n := range g.Nodes {
		if nodes[n.ID] {
			return Graph{}, fmt.Errorf("duplicate node %d", n.ID)
		}
		nodes[n.ID] = true
	}
	out := Graph{Nodes: append([]Node{}, g.Nodes...)}
	unique := map[Edge]bool{}
	for _, e := range g.Edges {
		if !nodes[e.From] || !nodes[e.To] {
			return Graph{}, errors.New("unknown edge endpoint")
		}
		if e.Kind != "supported_by" && e.Kind != "contradicted_by" && e.Kind != "depends_on" {
			return Graph{}, errors.New("unknown relationship")
		}
		if !unique[e] {
			out.Edges = append(out.Edges, e)
			unique[e] = true
		}
	}
	sort.Slice(out.Edges, func(i, j int) bool {
		a, b := out.Edges[i], out.Edges[j]
		if a.From != b.From {
			return a.From < b.From
		}
		if a.To != b.To {
			return a.To < b.To
		}
		return a.Kind < b.Kind
	})
	return out, nil
}
