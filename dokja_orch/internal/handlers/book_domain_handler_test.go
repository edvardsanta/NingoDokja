package handlers

import (
	"context"
	"read_books/internal/core"
	"testing"
)

type fakeBookService struct{}

func (f *fakeBookService) Summarize(_ context.Context, input map[string]any) (map[string]any, error) {
	return map[string]any{"status": "ok", "title": input["title"], "kind": "summary"}, nil
}

func (f *fakeBookService) Classify(_ context.Context, input map[string]any) (map[string]any, error) {
	return map[string]any{"status": "ok", "filename": input["filename"], "kind": "classification"}, nil
}

func TestBookDomainHandlerDelegatesToDomainService(t *testing.T) {
	handler := NewBookDomainHandler(&fakeBookService{})

	result, err := handler.Handle(context.Background(), core.Event{
		EventID: "event-1",
		Source:  core.SourceCLI,
		Type:    "book.summary.requested",
		Payload: map[string]any{"title": "Clean Code", "content": "Meaningful names matter."},
	}, core.WorkflowStep{
		Domain: core.DomainBook,
		Action: "summarize-book",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result["kind"] != "summary" {
		t.Fatalf("expected summary result, got %#v", result)
	}
}
