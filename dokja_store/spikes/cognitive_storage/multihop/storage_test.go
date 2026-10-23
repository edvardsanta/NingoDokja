package multihop

import (
	"context"
	"errors"
	"math/rand"
	"os"
	"reflect"
	"testing"
	"time"
)

type factory struct {
	name string
	open func(context.Context, Graph) (Reader, error)
}

func factories(t *testing.T) []factory {
	out := []factory{{"sqlite", func(ctx context.Context, g Graph) (Reader, error) { return NewSQLite(ctx, g) }}}
	for _, name := range []string{"postgres", "neo4j"} {
		endpoint := os.Getenv(map[string]string{"postgres": "W2_POSTGRES_DSN", "neo4j": "W2_NEO4J_URI"}[name])
		if endpoint == "" {
			t.Log(name + " skipped: W2 endpoint not configured")
			continue
		}
		if name == "postgres" {
			out = append(out, factory{name, func(ctx context.Context, g Graph) (Reader, error) { return NewPostgres(ctx, endpoint, g) }})
		} else {
			out = append(out, factory{name, func(ctx context.Context, g Graph) (Reader, error) { return NewNeo4j(ctx, endpoint, g) }})
		}
	}
	return out
}
func TestStorageMatchesIndependentGolden(t *testing.T) {
	// These expectations are independent of both traversal implementations.
	want := []Path{{[]int{1, 2}, []string{"supported_by"}}, {[]int{1, 3}, []string{"contradicted_by"}}, {[]int{1, 2, 4}, []string{"supported_by", "depends_on"}}, {[]int{1, 3, 4}, []string{"contradicted_by", "depends_on"}}, {[]int{1, 2, 4, 9}, []string{"supported_by", "depends_on", "supported_by"}}, {[]int{1, 3, 4, 9}, []string{"contradicted_by", "depends_on", "supported_by"}}}
	for _, f := range factories(t) {
		t.Run(f.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			g := fixture()
			g.Edges = append(g.Edges, g.Edges[0])
			r, err := f.open(ctx, g)
			if err != nil {
				t.Fatal(err)
			}
			defer func() {
				if err := r.Close(); err != nil {
					t.Error(err)
				}
			}()
			for _, depth := range []int{1, 3, 6} {
				q := query(depth)
				expected := want
				if depth == 1 {
					expected = want[:2]
				}
				got, err := Traverse(ctx, r, q)
				if err != nil || got.Truncated || !reflect.DeepEqual(got.Paths, expected) {
					t.Fatal(depth, got, err)
				}
			}
			for _, root := range []int{5, 6, 7, 99} {
				q := query(6)
				q.Root = root
				got, err := Traverse(ctx, r, q)
				if err != nil || len(got.Paths) != 0 {
					t.Fatal(root, got, err)
				}
			}
			q := query(6)
			q.Limit = 2
			got, err := Traverse(ctx, r, q)
			if err != nil || !got.Truncated || !reflect.DeepEqual(got.Paths, want[:2]) {
				t.Fatal(got, err)
			}
			q.Limit = 6
			got, err = Traverse(ctx, r, q)
			if err != nil || got.Truncated || !reflect.DeepEqual(got.Paths, want) {
				t.Fatal(got, err)
			}
			q.MaxExpansions = 1
			got, err = Traverse(ctx, r, q)
			if !errors.Is(err, ErrBudget) || len(got.Paths) > 0 {
				t.Fatal(got, err)
			}
			stopped, cancel := context.WithCancel(ctx)
			cancel()
			got, err = Traverse(stopped, r, query(6))
			if !errors.Is(err, context.Canceled) || len(got.Paths) > 0 {
				t.Fatal(got, err)
			}
		})
	}
}
func TestStorageRandomizedGraphAndPaging(t *testing.T) {
	g := Graph{}
	for id := -5; id < 145; id++ {
		g.Nodes = append(g.Nodes, Node{ID: id, Scope: "owner", Current: true})
	}
	// Force several pages of invisible edges before the only visible target.
	for i := 1; i < len(g.Nodes)-1; i++ {
		g.Nodes[i].Scope = "guest"
		g.Edges = append(g.Edges, Edge{-5, g.Nodes[i].ID, "depends_on"})
	}
	g.Edges = append(g.Edges, Edge{-5, 144, "supported_by"})
	rng := rand.New(rand.NewSource(41))
	for _, f := range factories(t) {
		t.Run(f.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			r, err := f.open(ctx, g)
			if err != nil {
				t.Fatal(err)
			}
			q := Query{Root: -5, Scope: "owner", Depth: 6, Limit: 1, MaxExpansions: 1000}
			want, _ := Reference(ctx, g, q)
			got, err := Traverse(ctx, r, q)
			if err != nil || !reflect.DeepEqual(got, want) {
				t.Fatal(got, want, err)
			}
			q.MaxExpansions = 128
			got, err = Traverse(ctx, r, q)
			if !errors.Is(err, ErrBudget) || len(got.Paths) > 0 {
				t.Fatal(got, err)
			}
			if err = r.Close(); err != nil {
				t.Fatal(err)
			}
		})
	}
	// Dense cycles, duplicate endpoints with different labels, hidden intermediates.
	g = fixture()
	for i := 0; i < 50; i++ {
		g.Edges = append(g.Edges, Edge{1 + rng.Intn(9), 1 + rng.Intn(9), []string{"supported_by", "contradicted_by", "depends_on"}[rng.Intn(3)]})
	}
	for _, f := range factories(t) {
		t.Run(f.name+"/random", func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			r, err := f.open(ctx, g)
			if err != nil {
				t.Fatal(err)
			}
			defer r.Close()
			for _, depth := range []int{1, 3, 6} {
				for _, limit := range []int{1, 10, 100} {
					for _, budget := range []int{1, 10, 10000} {
						q := Query{Root: 1, Scope: "owner", Depth: depth, Limit: limit, MaxExpansions: budget}
						want, e := Reference(ctx, g, q)
						got, err := Traverse(ctx, r, q)
						if !errors.Is(err, e) || !reflect.DeepEqual(got, want) {
							t.Fatal(q, got, want, err, e)
						}
					}
				}
			}
		})
	}
}

type cancelReader struct {
	Reader
	cancel context.CancelFunc
}

func (r cancelReader) Expand(ctx context.Context, id int, scope string, c Cursor, limit int) ([]Candidate, error) {
	rows, err := r.Reader.Expand(ctx, id, scope, c, limit)
	r.cancel()
	return rows, err
}
func TestCancellationDiscardsFetchedPage(t *testing.T) {
	for _, f := range factories(t) {
		t.Run(f.name, func(t *testing.T) {
			r, err := f.open(context.Background(), fixture())
			if err != nil {
				t.Fatal(err)
			}
			defer r.Close()
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			got, err := Traverse(ctx, cancelReader{r, cancel}, query(6))
			if !errors.Is(err, context.Canceled) || len(got.Paths) > 0 {
				t.Fatal(got, err)
			}
		})
	}
}

func TestStorageRejectsInvalidFixtures(t *testing.T) {
	g := fixture()
	g.Nodes = append(g.Nodes, g.Nodes[0])
	if _, err := NewSQLite(context.Background(), g); err == nil {
		t.Fatal("duplicate node accepted")
	}
	g = fixture()
	g.Edges = append(g.Edges, Edge{1, 99, "depends_on"})
	if _, err := NewSQLite(context.Background(), g); err == nil {
		t.Fatal("unknown endpoint accepted")
	}
}

func TestStorageHighDegreeBounds(t *testing.T) {
	g := Graph{Nodes: []Node{{ID: 1, Scope: "owner", Current: true}}}
	for i := 2; i < 10002; i++ {
		g.Nodes = append(g.Nodes, Node{ID: i, Scope: "owner", Current: true})
		g.Edges = append(g.Edges, Edge{1, i, "supported_by"})
	}
	for _, f := range factories(t) {
		t.Run(f.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			r, err := f.open(ctx, g)
			if err != nil {
				t.Fatal(err)
			}
			defer r.Close()
			q := Query{Root: 1, Scope: "owner", Depth: 1, Limit: 10, MaxExpansions: 11}
			got, err := Traverse(ctx, r, q)
			if err != nil || !got.Truncated || len(got.Paths) != 10 || got.Paths[9].Nodes[1] != 11 {
				t.Fatal(got, err)
			}
			q.MaxExpansions = 10
			got, err = Traverse(ctx, r, q)
			if !errors.Is(err, ErrBudget) || len(got.Paths) > 0 {
				t.Fatal(got, err)
			}
		})
	}
}

func TestFixtureCleanupPreservesOtherRun(t *testing.T) {
	for _, f := range factories(t) {
		t.Run(f.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			first, err := f.open(ctx, fixture())
			if err != nil {
				t.Fatal(err)
			}
			second, err := f.open(ctx, fixture())
			if err != nil {
				_ = first.Close()
				t.Fatal(err)
			}
			defer second.Close()
			if err = first.Close(); err != nil {
				t.Fatal(err)
			}
			want, _ := Reference(ctx, fixture(), query(6))
			got, err := Traverse(ctx, second, query(6))
			if err != nil || !reflect.DeepEqual(got, want) {
				t.Fatal("cleanup touched another run", got, err)
			}
		})
	}
}
