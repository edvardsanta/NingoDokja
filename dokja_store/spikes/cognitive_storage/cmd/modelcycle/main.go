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
	mode := flag.String("case", "pair", "pair, pair-no-memory, pair-no-diagnostic, pair-invalid, no-memory, contrary, missing, conflict, unauthorized")
	retrieval := flag.String("retrieval", "iterative", "single, bounded, iterative")
	maxHops := flag.Int("max-hops", 6, "retrieval depth budget (0..6)")
	timeout := flag.Duration("timeout", 60*time.Second, "per-provider deadline")
	flag.Parse()
	if *provider == "" || (*retrieval != "single" && *retrieval != "bounded" && *retrieval != "iterative") || *maxHops < 0 || *maxHops > 6 {
		fmt.Fprintln(os.Stderr, "explicit provider and valid retrieval/depth settings required")
		os.Exit(2)
	}
	chain := providers.Chain{}
	for _, name := range strings.Split(*provider, ",") {
		chain.Providers = append(chain.Providers, providers.Config{Name: name, Model: *model, Endpoint: *endpoint, Timeout: *timeout})
	}
	nodes := cognitive.Fixture()
	switch *mode {
	case "pair", "pair-no-memory", "pair-no-diagnostic", "pair-invalid", "no-memory", "unauthorized":
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
	c := cognitive.Config{Inference: chain, Mode: *retrieval, MaxHops: *maxHops, MaxIterations: 20, MaxCalls: 20, Deadline: 3 * (*timeout), Authorized: *mode != "unauthorized", Attempt: "first", Scope: "owner", Context: "synthetic-numeric-document", Tokens: []string{"１２", "3"}, Expected: 15}
	if *mode == "pair-invalid" {
		c.Tokens = []string{"invalid", "3"}
	}
	start := time.Now()
	first := cognitive.Run(context.Background(), store, c)
	report := struct {
		Case      string
		Retrieval string
		MaxHops   int
		First     cognitive.State
		Second    *cognitive.State `json:",omitempty"`
		ElapsedMS int64
	}{Case: *mode, Retrieval: *retrieval, MaxHops: *maxHops, First: first}
	if strings.HasPrefix(*mode, "pair") && first.Outcome == "completed" {
		store = secondStore(store, nodes, *mode)
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

// Counterfactual interventions happen only in this synthetic harness.
func secondStore(store *cognitive.Memory, nodes []cognitive.Node, mode string) *cognitive.Memory {
	switch mode {
	case "pair-no-memory":
		return cognitive.NewMemory(nodes)
	case "pair-no-diagnostic":
		for i := range store.Episodes {
			store.Episodes[i].Diagnostic = ""
		}
	}
	return store
}
