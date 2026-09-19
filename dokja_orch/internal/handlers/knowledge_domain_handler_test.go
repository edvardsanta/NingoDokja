package handlers

import (
	"context"
	"read_books/internal/core"
	"testing"
)

type fakeKnowledgeService struct {
	eventType string
	payload   map[string]any
}

func (f *fakeKnowledgeService) Dispatch(_ context.Context, eventType string, payload map[string]any) (map[string]any, error) {
	f.eventType, f.payload = eventType, payload
	return map[string]any{"ok": true}, nil
}

func TestKnowledgeEventsFlowThroughTheOrchestrator(t *testing.T) {
	service := &fakeKnowledgeService{}
	orchestrator := core.DefaultService(NewKnowledgeDomainHandler(service))

	result, err := orchestrator.ProcessWithResult(context.Background(), core.Event{
		Source:  core.SourceCLI,
		Type:    "knowledge.search",
		Payload: map[string]any{"query": "virtue"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if service.eventType != "knowledge.search" || service.payload["query"] != "virtue" {
		t.Fatalf("service got %q %v", service.eventType, service.payload)
	}
	if result.Workflow != "knowledge" || result.Result["knowledge"] == nil {
		t.Fatalf("unexpected result %#v", result)
	}

	if _, err := orchestrator.ProcessWithResult(context.Background(), core.Event{
		Source: core.SourceCLI, Type: "knowledge.ingest", Payload: map[string]any{"title": "So titulo"},
	}); err == nil {
		t.Fatal("an ingest without a body must fail before reaching the service")
	}
}

func TestSwitchingKnowledgeOffSkipsItsEvents(t *testing.T) {
	service := &fakeKnowledgeService{}
	controls, err := core.NewControls("")
	if err != nil {
		t.Fatalf("new controls: %v", err)
	}
	if err := controls.SetService("knowledge", false); err != nil {
		t.Fatalf("set service: %v", err)
	}
	orchestrator := core.DefaultService(NewKnowledgeDomainHandler(service)).WithControls(controls)

	result, err := orchestrator.ProcessWithResult(context.Background(), core.Event{
		Source: core.SourceCLI, Type: "knowledge.list",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Result["skipped"] != true || service.eventType != "" {
		t.Fatalf("expected a skipped result without calling the service: %#v", result.Result)
	}
}
