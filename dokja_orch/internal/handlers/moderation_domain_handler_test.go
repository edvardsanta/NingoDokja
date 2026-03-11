package handlers

import (
	"context"
	"read_books/internal/core"
	"testing"
)

func TestModerationDomainHandlerHandle(t *testing.T) {
	handler := NewModerationDomainHandler(nil)

	result, err := handler.Handle(context.Background(), core.Event{
		EventID: "evt-1",
		Source:  core.SourceDiscord,
		Type:    "message.created",
		Payload: map[string]any{"content": "ping"},
	}, core.WorkflowStep{
		Domain: core.DomainModeration,
		Action: "screen-event",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result["decision"] != "allow" {
		t.Fatalf("expected allow decision, got %#v", result["decision"])
	}
}

func TestModerationDomainHandlerValidation(t *testing.T) {
	if _, err := (*ModerationDomainHandler)(nil).Handle(context.Background(), core.Event{}, core.WorkflowStep{}); err == nil {
		t.Fatal("expected nil handler error")
	}

	handler := &ModerationDomainHandler{}
	if _, err := handler.Handle(context.Background(), core.Event{}, core.WorkflowStep{}); err == nil {
		t.Fatal("expected missing domain error")
	}
}
