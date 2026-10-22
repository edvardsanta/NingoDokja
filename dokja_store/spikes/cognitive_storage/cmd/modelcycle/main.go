// modelcycle uses a real configured inference provider with synthetic data only.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"read_books/dokja_store/spikes/cognitive_storage/cognitive"
	"read_books/dokja_store/spikes/cognitive_storage/providers"
	"strings"
	"time"
)

func main() {
	provider := flag.String("provider", "", "explicit comma-separated provider order: claude,codex,ollama")
	model := flag.String("model", "", "provider model (omit to use CLI default)")
	endpoint := flag.String("endpoint", "", "Ollama endpoint")
	mode := flag.String("case", "pair", "pair, no-memory, contrary, missing, conflict, unauthorized")
	timeout := flag.Duration("timeout", 60*time.Second, "per-provider deadline")
	flag.Parse()
	if *provider == "" {
		fmt.Fprintln(os.Stderr, "explicit provider required")
		os.Exit(2)
	}
	chain := providers.Chain{}
	for _, name := range strings.Split(*provider, ",") {
		chain.Providers = append(chain.Providers, providers.Config{Name: name, Model: *model, Endpoint: *endpoint, Timeout: *timeout})
	}
	nodes := cognitive.Fixture()
	switch *mode {
	case "pair", "no-memory", "unauthorized":
	case "contrary":
		nodes[3].Facts["sample_verified"] = false
	case "missing":
		nodes[3].Deleted = true
	case "conflict":
		nodes[3].Facts["sandbox_allowed"] = false
	default:
		fmt.Fprintln(os.Stderr, "unsupported case")
		os.Exit(2)
	}
	store := cognitive.NewMemory(nodes)
	c := cognitive.Config{Inference: chain, Mode: "iterative", MaxHops: 6, MaxIterations: 20, MaxCalls: 20, Deadline: 3 * (*timeout), Authorized: *mode != "unauthorized", Attempt: "first", Scope: "owner", Context: "synthetic-numeric-document", Tokens: []string{"１２", "3"}, Expected: 15}
	start := time.Now()
	first := cognitive.Run(context.Background(), store, c)
	report := struct {
		Case      string
		First     cognitive.State
		Second    *cognitive.State `json:",omitempty"`
		ElapsedMS int64
	}{Case: *mode, First: first}
	if *mode == "pair" && first.Outcome == "completed" {
		c.Attempt = "second"
		second := cognitive.Run(context.Background(), store, c)
		report.Second = &second
	}
	report.ElapsedMS = time.Since(start).Milliseconds()
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	_ = enc.Encode(report)
	if failed(first) || (report.Second != nil && failed(*report.Second)) {
		os.Exit(1)
	}
}

func failed(s cognitive.State) bool {
	return s.Outcome == "inference_failed" || s.Outcome == "inference_rejected" || s.Outcome == "budget_exhausted"
}
