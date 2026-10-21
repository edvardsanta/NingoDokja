// Command w1bench runs the W1 storage spike (ADR 0009) on one generated fixture and writes
// the results as JSON. PostgreSQL and Neo4j are reached at the addresses given; nothing is
// started here.
//
//	go run ./cmd/w1bench -chunks 100000 -dim 1024 -out results.json \
//	  -postgres postgres://postgres@127.0.0.1:55432/postgres -neo4j bolt://127.0.0.1:57687
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	spike "read_books/dokja_store/spikes/cognitive_storage"
)

type report struct {
	Started  string               `json:"started"`
	Config   spike.Config         `json:"config"`
	Host     map[string]string    `json:"host"`
	Fixture  map[string]int       `json:"fixture"`
	Engines  []spike.EngineResult `json:"engines"`
	Failures map[string]string    `json:"failures,omitempty"`
}

func main() {
	config := spike.Config{}
	flag.Uint64Var(&config.Seed, "seed", 1, "fixture seed")
	flag.IntVar(&config.Chunks, "chunks", 10000, "chunks of current documents")
	flag.IntVar(&config.Dim, "dim", 64, "vector dimension")
	flag.IntVar(&config.Queries, "queries", 200, "W1 queries")
	engines := flag.String("engines", "sqlite,postgres,neo4j", "engines to run")
	postgres := flag.String("postgres", "", "PostgreSQL DSN")
	neo4jURI := flag.String("neo4j", "", "Neo4j bolt URI")
	out := flag.String("out", "", "JSON results file (default stdout)")
	flag.Parse()

	ctx := context.Background()
	f := spike.Generate(config)
	gold := spike.ComputeGold(f)
	cases := map[string]int{"docs": len(f.Docs), "chunks": len(f.Chunks), "deleted_docs": len(f.Deleted), "queries": len(f.Queries)}
	for _, d := range f.Docs {
		if d.Case != "" {
			cases[d.Case+"_docs"]++
		}
	}
	r := report{Started: time.Now().UTC().Format(time.RFC3339), Config: f.Config, Fixture: cases, Failures: map[string]string{},
		Host: map[string]string{"go": runtime.Version(), "os": runtime.GOOS + "/" + runtime.GOARCH, "cpus": fmt.Sprint(runtime.NumCPU())}}

	dir, err := os.MkdirTemp("", "w1bench")
	if err != nil {
		fail(err)
	}
	defer os.RemoveAll(dir)
	for _, name := range strings.Split(*engines, ",") {
		var store spike.Store
		switch strings.TrimSpace(name) {
		case "sqlite":
			store = spike.NewSQLite(filepath.Join(dir, "w1.db"))
		case "postgres":
			store = spike.NewPostgres(*postgres)
		case "neo4j":
			store = spike.NewNeo4j(*neo4jURI)
		default:
			fail(fmt.Errorf("unknown engine %q", name))
		}
		fmt.Fprintf(os.Stderr, "%s: %d chunks, dim %d\n", store.Name(), len(f.Chunks), f.Config.Dim)
		result, err := spike.Run(ctx, store, f, gold)
		store.Close()
		if err != nil {
			r.Failures[store.Name()] = err.Error()
			fmt.Fprintf(os.Stderr, "%s failed: %v\n", store.Name(), err)
			continue
		}
		r.Engines = append(r.Engines, result)
		for _, m := range result.Modes {
			fmt.Fprintf(os.Stderr, "  %-16s exact %3d/%d recall %.3f underfilled %3d leaks %d/%d p50 %.1f p95 %.1f ms\n",
				m.Mode, m.ExactMatch, m.Queries, m.MeanRecall, m.Underfilled, m.Foreign, m.Ineligible, m.P50Ms, m.P95Ms)
		}
	}
	body, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		fail(err)
	}
	if *out == "" {
		fmt.Println(string(body))
		return
	}
	if err := os.WriteFile(*out, append(body, '\n'), 0o644); err != nil {
		fail(err)
	}
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, err)
	os.Exit(1)
}
