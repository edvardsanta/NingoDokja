package ingress

import (
	"os"
	"read_books/internal/core"
	"testing"
	"time"
)

func TestBuildResponsePayloadCompactSingleDomain(t *testing.T) {
	t.Setenv("VA_RESPONSE_MODE", "")

	payload := buildResponsePayload(core.ProcessResult{
		Event: core.Event{
			EventID:   "event-1",
			Timestamp: time.Now(),
			Source:    core.SourceCLI,
			Type:      "meme.fetch",
		},
		Workflow: "meme",
		Domains:  []string{"meme"},
		Result: map[string]any{
			"meme": map[string]any{"count": 3},
		},
	})

	compact, ok := payload.(CompactProcessResult)
	if !ok {
		t.Fatalf("expected compact payload, got %#v", payload)
	}
	if compact.EventID != "event-1" || compact.Domain != "meme" {
		t.Fatalf("unexpected compact payload: %#v", compact)
	}
	result, _ := compact.Result.(map[string]any)
	if result["count"] != 3 {
		t.Fatalf("expected flattened result, got %#v", compact.Result)
	}
}

func TestBuildResponsePayloadVerbose(t *testing.T) {
	t.Setenv("VA_RESPONSE_MODE", "debug")

	original := core.ProcessResult{
		Event: core.Event{
			EventID: "event-1",
			Source:  core.SourceCLI,
			Type:    "message.created",
		},
		Workflow: "conversation",
		Domains:  []string{"chat"},
		Result: map[string]any{
			"chat": map[string]any{"reply": "hello"},
		},
	}

	payload := buildResponsePayload(original)
	verbose, ok := payload.(core.ProcessResult)
	if !ok {
		t.Fatalf("expected verbose payload, got %#v", payload)
	}
	if verbose.Event.EventID != original.Event.EventID {
		t.Fatalf("expected original payload, got %#v", verbose)
	}
}

func TestVerboseResponsesEnabled(t *testing.T) {
	t.Setenv("VA_RESPONSE_MODE", "verbose")
	if !verboseResponsesEnabled() {
		t.Fatal("expected verbose mode enabled")
	}

	if err := os.Unsetenv("VA_RESPONSE_MODE"); err != nil {
		t.Fatalf("unset env: %v", err)
	}
	if verboseResponsesEnabled() {
		t.Fatal("expected verbose mode disabled")
	}
}
