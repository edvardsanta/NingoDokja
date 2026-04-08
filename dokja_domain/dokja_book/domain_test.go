package book

import (
	"context"
	"errors"
	"testing"
)

type fakeService struct {
	summarizeInput map[string]any
	classifyInput  map[string]any
	summarizeErr   error
	classifyErr    error
}

func (f *fakeService) Summarize(_ context.Context, input map[string]any) (map[string]any, error) {
	f.summarizeInput = input
	if f.summarizeErr != nil {
		return nil, f.summarizeErr
	}
	return map[string]any{"status": "ok", "kind": "summary"}, nil
}

func (f *fakeService) Classify(_ context.Context, input map[string]any) (map[string]any, error) {
	f.classifyInput = input
	if f.classifyErr != nil {
		return nil, f.classifyErr
	}
	return map[string]any{"status": "ok", "kind": "classification"}, nil
}

func TestDomainSummarizeDelegatesToService(t *testing.T) {
	service := &fakeService{}
	domain := New(service)

	result, err := domain.Handle(context.Background(), Request{
		Action: ActionSummarizeBook,
		Event: Event{
			ID:      "event-1",
			Source:  "discord",
			Type:    "book.summary.requested",
			Payload: map[string]any{"title": "Clean Code"},
			Context: map[string]any{"user_goal": "study"},
		},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result["kind"] != "summary" {
		t.Fatalf("expected summary result, got %#v", result)
	}
	if service.summarizeInput["title"] != "Clean Code" {
		t.Fatalf("expected merged payload title, got %#v", service.summarizeInput["title"])
	}
	if service.summarizeInput["user_goal"] != "study" {
		t.Fatalf("expected merged context user_goal, got %#v", service.summarizeInput["user_goal"])
	}
}

func TestDomainClassifyDelegatesToService(t *testing.T) {
	service := &fakeService{}
	domain := New(service)

	result, err := domain.Handle(context.Background(), Request{
		Action: ActionClassifyBookResource,
		Event: Event{
			ID:      "event-2",
			Source:  "cli",
			Type:    "book.resource.classify",
			Payload: map[string]any{"filename": "book.epub"},
		},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result["kind"] != "classification" {
		t.Fatalf("expected classification result, got %#v", result)
	}
	if service.classifyInput["filename"] != "book.epub" {
		t.Fatalf("expected classify filename, got %#v", service.classifyInput["filename"])
	}
}

func TestDomainValidationAndErrors(t *testing.T) {
	if _, err := (*Domain)(nil).Handle(context.Background(), Request{}); err == nil {
		t.Fatal("expected nil domain error")
	}

	domain := New(nil)
	if _, err := domain.Handle(context.Background(), Request{}); err == nil {
		t.Fatal("expected nil service error")
	}

	svcErr := errors.New("boom")
	domain = New(&fakeService{summarizeErr: svcErr})
	if _, err := domain.Handle(context.Background(), Request{
		Action: ActionSummarizeBook,
		Event:  Event{Type: "book.summary.requested"},
	}); !errors.Is(err, svcErr) {
		t.Fatalf("expected summarize error propagation, got %v", err)
	}

	if _, err := New(&fakeService{}).Handle(context.Background(), Request{
		Action: "unknown",
		Event:  Event{Type: "book.unknown"},
	}); err == nil {
		t.Fatal("expected unsupported action error")
	}
}
