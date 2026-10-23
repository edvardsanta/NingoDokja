// w2bench measures bounded adjacency traversal on synthetic isolated fixtures.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"read_books/dokja_store/spikes/cognitive_storage/multihop"
	"runtime"
	"time"
)

func main() {
	engine := flag.String("engine", "sqlite", "sqlite, postgres, neo4j")
	trees := flag.Int("trees", 10, "number of disconnected 127-node trees")
	runs := flag.Int("runs", 100, "measured queries per depth")
	limit := flag.Int("limit", 256, "returned path bound")
	timeout := flag.Duration("timeout", 5*time.Second, "per-query deadline")
	flag.Parse()
	if err := run(*engine, *trees, *runs, *limit, *timeout); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
func run(engine string, trees, runs, limit int, timeout time.Duration) (err error) {
	g, roots, err := multihop.Forest(trees)
	if err != nil {
		return err
	}
	if runs < 1 || limit < 1 || timeout <= 0 {
		return fmt.Errorf("invalid measurement bounds")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Minute)
	defer cancel()
	start := time.Now()
	var reader multihop.Reader
	switch engine {
	case "sqlite":
		reader, err = multihop.NewSQLite(ctx, g)
	case "postgres":
		dsn := os.Getenv("W2_POSTGRES_DSN")
		if dsn == "" {
			return fmt.Errorf("W2_POSTGRES_DSN is required")
		}
		reader, err = multihop.NewPostgres(ctx, dsn, g)
	case "neo4j":
		uri := os.Getenv("W2_NEO4J_URI")
		if uri == "" {
			return fmt.Errorf("W2_NEO4J_URI is required")
		}
		reader, err = multihop.NewNeo4j(ctx, uri, g)
	default:
		return fmt.Errorf("unsupported engine")
	}
	if err != nil {
		return err
	}
	defer func() {
		if closeErr := reader.Close(); err == nil {
			err = closeErr
		}
	}()
	loadMS := time.Since(start).Milliseconds()
	measurements, err := multihop.Measure(ctx, reader, g, roots, runs, limit, timeout)
	if err != nil {
		return err
	}
	report := struct {
		Engine, GoVersion, Policy, Cache        string
		Nodes, Edges, Trees, Limit, Concurrency int
		LoadMS                                  int64
		Measurements                            []multihop.Measurement
	}{engine, runtime.Version(), "paged adjacency; shared bounded BFS", "five warmup queries per depth; no cache eviction", len(g.Nodes), len(g.Edges), trees, limit, 1, loadMS, measurements}
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(report)
}
