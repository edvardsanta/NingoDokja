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
