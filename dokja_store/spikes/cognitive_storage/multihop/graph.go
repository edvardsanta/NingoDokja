// Package multihop defines the isolated W2 correctness contract. It does not
// connect to the application stores or execute actions.
package multihop

import (
	"context"
	"errors"
	"fmt"
	"sort"
)

type Node struct {
	ID      int
	Scope   string
	Current bool
	Deleted bool
}

type Edge struct {
	From, To int
	Kind     string // supported_by, contradicted_by, depends_on
}

type Graph struct {
	Nodes []Node
	Edges []Edge
}
type Query struct {
	Root                        int
	Scope                       string
	Depth, Limit, MaxExpansions int
}

// Path preserves relationship labels: reachability is never a truth score.
type Path struct {
	Nodes []int
	Kinds []string
}
type Result struct {
	Paths     []Path
	Truncated bool
}

var ErrBudget = errors.New("traversal expansion budget exhausted")

// Reference enumerates simple paths breadth-first, with deterministic edge
// ordering. A hidden, deleted or superseded intermediate node blocks traversal.
// Limit truncation is explicit; cancellation and budget exhaustion return no
// partial answer. MaxExpansions also bounds work through high-degree nodes.
func Reference(ctx context.Context, g Graph, q Query) (Result, error) {
	if q.Scope == "" || q.Depth < 1 || q.Depth > 6 || q.Limit < 1 || q.MaxExpansions < 1 {
		return Result{}, errors.New("invalid traversal bounds or scope")
	}
	nodes := map[int]Node{}
	for _, n := range g.Nodes {
		if _, exists := nodes[n.ID]; exists {
			return Result{}, fmt.Errorf("duplicate node %d", n.ID)
		}
		nodes[n.ID] = n
	}
	edges := append([]Edge(nil), g.Edges...)
	sort.Slice(edges, func(i, j int) bool {
		if edges[i].From != edges[j].From {
			return edges[i].From < edges[j].From
		}
		if edges[i].To != edges[j].To {
			return edges[i].To < edges[j].To
		}
		return edges[i].Kind < edges[j].Kind
	})
	adjacency := map[int][]Edge{}
	for i, e := range edges {
		if _, ok := nodes[e.From]; !ok {
			return Result{}, errors.New("unknown edge source")
		}
		if _, ok := nodes[e.To]; !ok {
			return Result{}, errors.New("unknown edge target")
		}
		switch e.Kind {
		case "supported_by", "contradicted_by", "depends_on":
		default:
			return Result{}, errors.New("unknown relationship")
		}
		if i > 0 && edges[i-1] == e {
			continue
		}
		adjacency[e.From] = append(adjacency[e.From], e)
	}
	eligible := func(id int) bool { n, ok := nodes[id]; return ok && n.Scope == q.Scope && n.Current && !n.Deleted }
	if err := ctx.Err(); err != nil {
		return Result{}, err
	}
	if !eligible(q.Root) {
		return Result{}, nil
	}
	queue := []Path{{Nodes: []int{q.Root}}}
	out := Result{}
	expansions := 0
	for len(queue) > 0 {
		p := queue[0]
		queue = queue[1:]
		if len(p.Kinds) == q.Depth {
			continue
		}
		for _, e := range adjacency[p.Nodes[len(p.Nodes)-1]] {
			if err := ctx.Err(); err != nil {
				return Result{}, err
			}
			expansions++
			if expansions > q.MaxExpansions {
				return Result{}, ErrBudget
			}
			if !eligible(e.To) {
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
			next := Path{Nodes: append(append([]int(nil), p.Nodes...), e.To), Kinds: append(append([]string(nil), p.Kinds...), e.Kind)}
			if len(out.Paths) == q.Limit {
				out.Truncated = true
				return out, nil
			}
			out.Paths = append(out.Paths, next)
			queue = append(queue, next)
		}
	}
	return out, nil
}
