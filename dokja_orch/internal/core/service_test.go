package core

import (
	"context"
	"testing"
)

type captureHandler struct {
	domain Domain
	events []Event
	steps  []WorkflowStep
}

func (h *captureHandler) Domain() Domain {
	return h.domain
}

func (h *captureHandler) Handle(_ context.Context, event Event, workflow WorkflowStep) (map[string]any, error) {
	h.events = append(h.events, event)
	h.steps = append(h.steps, workflow)
	return map[string]any{"handled": true}, nil
}

func TestServiceProcessRoutesMessageEventsThroughModerationThenChat(t *testing.T) {
	moderation := &captureHandler{domain: DomainModeration}
	chat := &captureHandler{domain: DomainChat}
	service := DefaultService(moderation, chat)

	err := service.Process(context.Background(), Event{
		Source:  SourceDiscord,
		Type:    "message.created",
		User:    EventUser{ID: "u1", Name: "Alice"},
		Channel: EventChannel{ID: "c1"},
		Payload: map[string]any{"content": "hello"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(moderation.events) != 1 {
		t.Fatalf("expected moderation handler to receive 1 event, got %d", len(moderation.events))
	}
	if len(chat.events) != 1 {
		t.Fatalf("expected chat handler to receive 1 event, got %d", len(chat.events))
	}

	if moderation.steps[0].Action != "screen-event" {
		t.Fatalf("expected moderation action screen-event, got %q", moderation.steps[0].Action)
	}
	if chat.steps[0].Action != "generate-response" {
		t.Fatalf("expected chat action generate-response, got %q", chat.steps[0].Action)
	}

	processedEvent := chat.events[0]
	if processedEvent.EventID == "" {
		t.Fatal("expected event id to be generated")
	}
	if processedEvent.Context["workflow"] != "conversation" {
		t.Fatalf("expected workflow context to be conversation, got %#v", processedEvent.Context["workflow"])
	}

	result, err := service.ProcessWithResult(context.Background(), Event{
		Source:  SourceDiscord,
		Type:    "message.created",
		User:    EventUser{ID: "u1", Name: "Alice"},
		Channel: EventChannel{ID: "c1"},
		Payload: map[string]any{"content": "hello"},
	})
	if err != nil {
		t.Fatalf("unexpected process result error: %v", err)
	}
	if result.Workflow != "conversation" {
		t.Fatalf("expected workflow conversation, got %q", result.Workflow)
	}
	if result.Result["chat"] == nil {
		t.Fatalf("expected chat result, got %#v", result.Result)
	}
}

func TestServiceProcessRoutesMemeEvents(t *testing.T) {
	meme := &captureHandler{domain: DomainMeme}
	service := DefaultService(meme)

	err := service.Process(context.Background(), Event{
		Source:  SourceOrchestrator,
		Type:    "meme.fetch",
		Channel: EventChannel{ID: "c1"},
		Payload: map[string]any{"limit": 3},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(meme.events) != 1 {
		t.Fatalf("expected meme handler to receive 1 event, got %d", len(meme.events))
	}
	if meme.steps[0].Action != "fetch-memes" {
		t.Fatalf("expected meme action fetch-memes, got %q", meme.steps[0].Action)
	}
	if meme.events[0].Context["workflow"] != "meme" {
		t.Fatalf("expected workflow context meme, got %#v", meme.events[0].Context["workflow"])
	}
}

func TestServiceProcessValidation(t *testing.T) {
	service := DefaultService(&captureHandler{domain: DomainChat})

	if err := (*Service)(nil).Process(context.Background(), Event{}); err == nil {
		t.Fatal("expected nil service error")
	}

	err := service.Process(context.Background(), Event{
		Source: SourceDiscord,
	})
	if err == nil {
		t.Fatal("expected invalid event error")
	}
}
