package handlers

import (
	"context"
	"errors"
	"read_books/internal/core"
	"strings"
	"testing"
)

type classificationService struct {
	calls    []string
	metadata map[string]any
	revision string
}

func (s *classificationService) Dispatch(_ context.Context, event string, payload map[string]any) (map[string]any, error) {
	s.calls = append(s.calls, event)
	switch event {
	case "knowledge.metadata.get":
	case "knowledge.metadata.patch":
		if payload["revision"] != s.revision {
			return nil, errors.New("classification_conflict")
		}
		for key, value := range payload["patch"].(map[string]any) {
			s.metadata[key] = value
		}
		s.revision = strings.Repeat("b", 64)
	default:
		return nil, errors.New("unexpected service event")
	}
	return map[string]any{"source_id": "study", "revision": s.revision, "metadata": s.metadata}, nil
}
func TestKnowledgeClassificationResumesThroughTheOrchestrator(t *testing.T) {
	service := &classificationService{metadata: map[string]any{"form": "book", "content_kind": nil}, revision: strings.Repeat("a", 64)}
	orchestrator := core.DefaultService(NewKnowledgeDomainHandler(service))
	query := core.Event{Source: core.SourceCLI, Type: "knowledge.classify", Payload: map[string]any{"source_id": "study"}}
	result, err := orchestrator.ProcessWithResult(context.Background(), query)
	if err != nil {
		t.Fatal(err)
	}
	answer := result.Result["knowledge"].(map[string]any)
	if answer["classification"].(map[string]any)["needs_user_input"] != true || service.calls[0] != "knowledge.metadata.get" {
		t.Fatalf("answer=%#v calls=%v", answer, service.calls)
	}
	query.Payload = map[string]any{"source_id": "study", "revision": answer["revision"], "answers": map[string]any{"content_kind": "research"}}
	result, err = orchestrator.ProcessWithResult(context.Background(), query)
	if err != nil {
		t.Fatal(err)
	}
	answer = result.Result["knowledge"].(map[string]any)
	if answer["classification"].(map[string]any)["status"] != "classified" || service.metadata["form"] != "book" || len(service.calls) != 2 || service.calls[1] != "knowledge.metadata.patch" {
		t.Fatalf("answer=%#v calls=%v", answer, service.calls)
	}
	if _, err := orchestrator.ProcessWithResult(context.Background(), query); err == nil {
		t.Fatal("stale answer must fail")
	}
}
