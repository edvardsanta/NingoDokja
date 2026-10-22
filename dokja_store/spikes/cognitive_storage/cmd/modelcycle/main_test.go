package main

import (
	"read_books/dokja_store/spikes/cognitive_storage/cognitive"
	"testing"
)

func TestFailureClassification(t *testing.T) {
	for _, outcome := range []string{"inference_failed", "inference_rejected", "budget_exhausted"} {
		if !failed(cognitive.State{Outcome: outcome}) {
			t.Fatal(outcome)
		}
	}
	if failed(cognitive.State{Outcome: "completed", Observed: "failed"}) {
		t.Fatal("observed simulation failure is valid evidence")
	}
}

func TestMemoryCounterfactuals(t *testing.T) {
	for _, mode := range []string{"pair", "pair-no-memory", "pair-no-diagnostic"} {
		store := cognitive.NewMemory(cognitive.Fixture())
		store.Episodes = []cognitive.Episode{{ID: "first", Outcome: "failed", Diagnostic: "parse_error_full_width_digits"}}
		next := secondStore(store, cognitive.Fixture(), mode)
		if mode == "pair-no-memory" {
			if len(next.Episodes) != 0 {
				t.Fatal(next)
			}
			continue
		}
		if len(next.Episodes) != 1 || next.Episodes[0].Outcome != "failed" {
			t.Fatal(next)
		}
		if (next.Episodes[0].Diagnostic == "") != (mode == "pair-no-diagnostic") {
			t.Fatal(mode, next)
		}
	}
}
