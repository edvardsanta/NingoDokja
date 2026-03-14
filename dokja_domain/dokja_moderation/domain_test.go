package moderation

import (
	"context"
	"errors"
	"testing"
)

type fakePolicy struct {
	input  ScreenInput
	result map[string]any
	err    error
}

func (f *fakePolicy) Screen(_ context.Context, input ScreenInput) (map[string]any, error) {
	f.input = input
	return f.result, f.err
}

func TestHandleScreensEventWithDefaultPolicy(t *testing.T) {
	domain := New(nil)

	result, err := domain.Handle(context.Background(), Request{
		Action: ActionScreenEvent,
		Event: Event{
			ID:      "evt-1",
			Source:  "discord",
			Type:    "message.created",
			Payload: map[string]any{"content": "hello"},
		},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result["decision"] != DecisionAllow {
		t.Fatalf("expected allow decision, got %#v", result["decision"])
	}
	if result["blocked"] != false {
		t.Fatalf("expected blocked false, got %#v", result["blocked"])
	}
	if result["message_chars"] != 5 {
		t.Fatalf("expected message_chars 5, got %#v", result["message_chars"])
	}
}

func TestHandleScreensEventWithCustomPolicy(t *testing.T) {
	policy := &fakePolicy{
		result: map[string]any{"decision": DecisionBlock, "blocked": true},
	}
	domain := New(policy)

	result, err := domain.Handle(context.Background(), Request{
		Action: ActionScreenEvent,
		Event: Event{
			ID:      "evt-2",
			Source:  "cli",
			Type:    "message.created",
			Payload: map[string]any{"message": "unsafe"},
		},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result["decision"] != DecisionBlock {
		t.Fatalf("expected block decision, got %#v", result["decision"])
	}
	if policy.input.Content != "unsafe" {
		t.Fatalf("expected content unsafe, got %q", policy.input.Content)
	}
}

func TestHandleSkipsWhenNoTextualContentExists(t *testing.T) {
	domain := New(nil)

	result, err := domain.Handle(context.Background(), Request{
		Action: ActionScreenEvent,
		Event: Event{
			Type:    "message.created",
			Payload: map[string]any{},
		},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result["status"] != StatusSkipped {
		t.Fatalf("expected skipped status, got %#v", result["status"])
	}
}

func TestHandleValidation(t *testing.T) {
	if _, err := (*Domain)(nil).Handle(context.Background(), Request{}); err == nil {
		t.Fatal("expected nil domain error")
	}

	domain := New(nil)
	if _, err := domain.Handle(context.Background(), Request{Action: "unknown", Event: Event{Type: "message.created"}}); err == nil {
		t.Fatal("expected unsupported action error")
	}

	expected := errors.New("boom")
	domain = New(&fakePolicy{err: expected})
	if _, err := domain.Handle(context.Background(), Request{
		Action: ActionScreenEvent,
		Event:  Event{Type: "message.created", Payload: map[string]any{"text": "hello"}},
	}); !errors.Is(err, expected) {
		t.Fatalf("expected propagated policy error, got %v", err)
	}
}
