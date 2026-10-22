package multihop

import (
	"context"
	"os"
	"reflect"
	"testing"
	"time"
)

func TestMeasurementChecksGeneratedFixture(t *testing.T) {
	g, roots, err := Forest(2)
	if err != nil {
		t.Fatal(err)
	}
	if len(g.Nodes) != 254 || len(roots) != 2 || len(g.Edges) != 258 {
		t.Fatal(len(g.Nodes), len(g.Edges), roots)
	}
	r, err := NewSQLite(context.Background(), g)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	measurements, err := Measure(context.Background(), r, g, roots, 3, 10, time.Second)
	if err != nil || len(measurements) != 3 {
		t.Fatal(measurements, err)
	}
	for _, m := range measurements {
		if m.CorrectQueries != 3 || m.P99MS < m.P50MS {
			t.Fatal(m)
		}
	}
}

func TestNeo4jLargeFixtureCleanup(t *testing.T) {
	uri := os.Getenv("W2_NEO4J_URI")
	if uri == "" {
		t.Skip("W2_NEO4J_URI not configured")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	probe, err := NewNeo4j(ctx, uri, fixture())
	if err != nil {
		t.Fatal(err)
	}
	defer probe.Close()
	g, _, err := Forest(1000)
	if err != nil {
		t.Fatal(err)
	}
	large, err := NewNeo4j(ctx, uri, g)
	if err != nil {
		t.Fatal(err)
	}
	runID := large.run
	if err = large.Close(); err != nil {
		t.Fatal("large fixture cleanup failed", err)
	}
	result, err := probe.query(ctx, `MATCH (n:DokjaW2 {run:$run}) RETURN count(n) AS remaining`, map[string]any{"run": runID})
	if err != nil || result.Records[0].AsMap()["remaining"].(int64) != 0 {
		t.Fatal("large fixture leaked nodes", result, err)
	}
	got, err := Traverse(ctx, probe, query(6))
	want, _ := Reference(ctx, fixture(), query(6))
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Fatal("cleanup modified sentinel fixture", got, err)
	}
}
