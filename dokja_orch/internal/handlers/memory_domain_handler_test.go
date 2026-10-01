package handlers

import (
	"bytes"
	"context"
	"log"
	"read_books/internal/core"
	"read_books/internal/logger"
	"strings"
	"testing"
)

// captureLogs turns the orchestrator logger on for one test and returns what it writes.
func captureLogs(t *testing.T) *bytes.Buffer {
	t.Helper()
	var logs bytes.Buffer
	previous := log.Writer()
	log.SetOutput(&logs)
	logger.Init()
	t.Cleanup(func() {
		logger.InitWithOptions(false, "")
		log.SetOutput(previous)
	})
	return &logs
}

type fakeMemoryService struct {
	calls     int
	eventType string
	payload   map[string]any
}

func (f *fakeMemoryService) Dispatch(_ context.Context, eventType string, payload map[string]any) (map[string]any, error) {
	f.calls++
	f.eventType, f.payload = eventType, payload
	return map[string]any{"ok": true}, nil
}

func TestMemoryEventsFlowThroughTheOrchestrator(t *testing.T) {
	service := &fakeMemoryService{}
	orchestrator := core.DefaultService(NewMemoryDomainHandler(service))

	result, err := orchestrator.ProcessWithResult(context.Background(), core.Event{
		Source:  core.SourceCLI,
		Type:    "memory.record",
		Payload: map[string]any{"ref": "e1", "action": "tag.suggest", "context": "some words"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if service.eventType != "memory.record" || service.payload["ref"] != "e1" {
		t.Fatalf("service got %q %v", service.eventType, service.payload)
	}
	if result.Workflow != "memory" || result.Result["memory"] == nil {
		t.Fatalf("unexpected result %#v", result)
	}
}

func TestMemoryRequestsAreValidatedBeforeReachingTheService(t *testing.T) {
	service := &fakeMemoryService{}
	orchestrator := core.DefaultService(NewMemoryDomainHandler(service))

	for _, event := range []core.Event{
		{Source: core.SourceCLI, Type: "memory.record", Payload: map[string]any{"action": "tag.suggest", "context": "words"}},
		{Source: core.SourceCLI, Type: "memory.resolve", Payload: map[string]any{"ref": "e1", "outcome": "maybe"}},
		{Source: core.SourceCLI, Type: "memory.unheard-of"},
	} {
		if _, err := orchestrator.ProcessWithResult(context.Background(), event); err == nil {
			t.Fatalf("%s: expected an error", event.Type)
		}
	}
	if service.calls != 0 {
		t.Fatal("an invalid request must never reach the service")
	}
}

func TestMemoryPredictAndStatsAreAnsweredByTheDomain(t *testing.T) {
	service := &fakeMemoryService{}
	orchestrator := core.DefaultService(NewMemoryDomainHandler(service))

	if _, err := orchestrator.ProcessWithResult(context.Background(), core.Event{
		Source: core.SourceCLI, Type: "memory.predict", Payload: map[string]any{"action": "tag.suggest", "context": "words"},
	}); err != nil {
		t.Fatalf("predict: %v", err)
	}
	if service.eventType != "memory.recall" {
		t.Fatalf("a prediction is built from a recall, the service got %q", service.eventType)
	}
	if _, err := orchestrator.ProcessWithResult(context.Background(), core.Event{Source: core.SourceCLI, Type: "memory.stats"}); err != nil {
		t.Fatalf("stats: %v", err)
	}
	if service.eventType != "memory.resolved" {
		t.Fatalf("a scorecard is built from resolved experiences, the service got %q", service.eventType)
	}
}

func TestReservedMemorySyncDoesNothingYet(t *testing.T) {
	service := &fakeMemoryService{}
	orchestrator := core.DefaultService(NewMemoryDomainHandler(service))

	if _, err := orchestrator.ProcessWithResult(context.Background(), core.Event{Source: core.SourceOrchestrator, Type: "memory.sync"}); err != nil {
		t.Fatalf("memory.sync was accepted before and must stay accepted: %v", err)
	}
	if service.calls != 0 {
		t.Fatal("memory.sync is reserved and must not reach the service")
	}
}

func TestSwitchingMemoryOffSkipsItsEvents(t *testing.T) {
	service := &fakeMemoryService{}
	controls, err := core.NewControls("")
	if err != nil {
		t.Fatalf("new controls: %v", err)
	}
	if err := controls.SetService("memory", false); err != nil {
		t.Fatalf("set service: %v", err)
	}
	orchestrator := core.DefaultService(NewMemoryDomainHandler(service)).WithControls(controls)

	result, err := orchestrator.ProcessWithResult(context.Background(), core.Event{Source: core.SourceCLI, Type: "memory.status"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Result["skipped"] != true || service.calls != 0 {
		t.Fatalf("expected a skipped result without calling the service: %#v", result.Result)
	}
}

func TestMemoryHandlerNeverLogsTheContext(t *testing.T) {
	logs := captureLogs(t)

	orchestrator := core.DefaultService(NewMemoryDomainHandler(&fakeMemoryService{}))
	if _, err := orchestrator.ProcessWithResult(context.Background(), core.Event{
		Source:  core.SourceCLI,
		Type:    "memory.record",
		Payload: map[string]any{"ref": "e1", "action": "tag.suggest", "context": "private words"},
	}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(logs.String(), "memory handler received") {
		t.Fatalf("the handler should still log what it handled (otherwise this test proves nothing): %q", logs.String())
	}
	if strings.Contains(logs.String(), "private") {
		t.Fatalf("a context is the user's own text and must not be logged: %q", logs.String())
	}
}

func TestUnconfiguredMemoryHandlerFailsClearly(t *testing.T) {
	if _, err := (*MemoryDomainHandler)(nil).Handle(context.Background(), core.Event{}, core.WorkflowStep{}); err == nil {
		t.Fatal("expected a not configured error")
	}
}

func TestMemoryResolveByObservationFlowsThroughTheOrchestrator(t *testing.T) {
	service := &answeringMemoryService{answers: map[string]map[string]any{
		"memory.get":     {"found": true, "detail": "#Label"},
		"memory.resolve": {"found": true, "resolved": true, "outcome": "accepted"},
	}}
	orchestrator := core.DefaultService(NewMemoryDomainHandler(service))

	result, err := orchestrator.ProcessWithResult(context.Background(), core.Event{
		Source: core.SourceCLI, Type: "memory.resolve", Payload: map[string]any{"ref": "e1", "observed": "#label"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(service.sent) != 2 || service.sent[1] != "memory.resolve" {
		t.Fatalf("expected a get then a resolve, sent %v", service.sent)
	}
	if answer := result.Result["memory"].(map[string]any); answer["matched"] != true {
		t.Fatalf("answer %v", answer)
	}
}

type answeringMemoryService struct {
	answers map[string]map[string]any
	sent    []string
}

func (a *answeringMemoryService) Dispatch(_ context.Context, eventType string, _ map[string]any) (map[string]any, error) {
	a.sent = append(a.sent, eventType)
	return a.answers[eventType], nil
}
