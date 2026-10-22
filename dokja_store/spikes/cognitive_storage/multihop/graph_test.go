package multihop

import (
	"context"
	"errors"
	"reflect"
	"testing"
)

func fixture() Graph {
	g := Graph{}
	for i := 1; i <= 9; i++ {
		g.Nodes = append(g.Nodes, Node{ID: i, Scope: "owner", Current: true})
	}
	g.Nodes[4].Scope = "guest"
	g.Nodes[5].Deleted = true
	g.Nodes[6].Current = false
	g.Edges = []Edge{{1, 2, "supported_by"}, {1, 3, "contradicted_by"}, {2, 4, "depends_on"}, {3, 4, "depends_on"}, {4, 1, "depends_on"}, {1, 5, "depends_on"}, {5, 8, "supported_by"}, {1, 6, "depends_on"}, {6, 8, "supported_by"}, {1, 7, "depends_on"}, {7, 8, "supported_by"}, {4, 9, "supported_by"}}
	return g
}
func query(depth int) Query {
	return Query{Root: 1, Scope: "owner", Depth: depth, Limit: 100, MaxExpansions: 1000}
}
func TestGoldenPaths(t *testing.T) {
	first := []Path{{[]int{1, 2}, []string{"supported_by"}}, {[]int{1, 3}, []string{"contradicted_by"}}}
	deeper := append(append([]Path(nil), first...), Path{[]int{1, 2, 4}, []string{"supported_by", "depends_on"}}, Path{[]int{1, 3, 4}, []string{"contradicted_by", "depends_on"}}, Path{[]int{1, 2, 4, 9}, []string{"supported_by", "depends_on", "supported_by"}}, Path{[]int{1, 3, 4, 9}, []string{"contradicted_by", "depends_on", "supported_by"}})
	for _, depth := range []int{1, 3, 6} {
		got, err := Reference(context.Background(), fixture(), query(depth))
		want := deeper
		if depth == 1 {
			want = first
		}
		if err != nil || got.Truncated || !reflect.DeepEqual(got.Paths, want) {
			t.Fatalf("depth %d: %+v %v", depth, got, err)
		}
	}
}
func TestBoundsAndCancellation(t *testing.T) {
	q := query(6)
	q.Limit = 2
	got, err := Reference(context.Background(), fixture(), q)
	if err != nil || !got.Truncated || len(got.Paths) != 2 {
		t.Fatalf("%+v %v", got, err)
	}
	q.Limit = 6
	got, err = Reference(context.Background(), fixture(), q)
	if err != nil || got.Truncated {
		t.Fatalf("exact limit: %+v %v", got, err)
	}
	q.MaxExpansions = 1
	got, err = Reference(context.Background(), fixture(), q)
	if !errors.Is(err, ErrBudget) || len(got.Paths) != 0 {
		t.Fatalf("budget: %+v %v", got, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err = Reference(ctx, fixture(), query(3)); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}
func TestHiddenRootAndDeterministicEdges(t *testing.T) {
	g := fixture()
	q := query(6)
	q.Root = 5
	got, err := Reference(context.Background(), g, q)
	if err != nil || len(got.Paths) != 0 {
		t.Fatalf("hidden root: %+v %v", got, err)
	}
	expected, _ := Reference(context.Background(), g, query(6))
	for i, j := 0, len(g.Edges)-1; i < j; i, j = i+1, j-1 {
		g.Edges[i], g.Edges[j] = g.Edges[j], g.Edges[i]
	}
	g.Edges = append(g.Edges, g.Edges[0])
	actual, err := Reference(context.Background(), g, query(6))
	if err != nil || !reflect.DeepEqual(actual, expected) {
		t.Fatalf("ordering: %+v %v", actual, err)
	}
}
func TestInvalidGraphAndBounds(t *testing.T) {
	for _, depth := range []int{0, 7} {
		if _, err := Reference(context.Background(), fixture(), query(depth)); err == nil {
			t.Fatal("accepted depth", depth)
		}
	}
	for _, edge := range []Edge{{1, 99, "depends_on"}, {99, 1, "depends_on"}, {1, 2, "predicted_success"}} {
		g := fixture()
		g.Edges = append(g.Edges, edge)
		if _, err := Reference(context.Background(), g, query(3)); err == nil {
			t.Fatal("accepted", edge)
		}
	}
}

func TestHighDegreeBudgetAndResultLimit(t *testing.T) {
	g := Graph{Nodes: []Node{{ID: 1, Scope: "owner", Current: true}}}
	for i := 2; i < 10002; i++ {
		g.Nodes = append(g.Nodes, Node{ID: i, Scope: "owner", Current: true})
		g.Edges = append(g.Edges, Edge{1, i, "supported_by"})
	}
	q := query(1)
	q.Limit = 10
	q.MaxExpansions = 11
	got, err := Reference(context.Background(), g, q)
	if err != nil || len(got.Paths) != 10 || !got.Truncated {
		t.Fatalf("fanout: %+v %v", got, err)
	}
	q.MaxExpansions = 10
	got, err = Reference(context.Background(), g, q)
	if !errors.Is(err, ErrBudget) || len(got.Paths) != 0 {
		t.Fatalf("budget: %+v %v", got, err)
	}
}
