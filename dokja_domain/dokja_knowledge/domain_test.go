package knowledge

import (
	"context"
	"strings"
	"testing"
)

type recordingService struct {
	eventType string
	payload   map[string]any
	calls     int
}

func (r *recordingService) Dispatch(_ context.Context, eventType string, payload map[string]any) (map[string]any, error) {
	r.calls++
	r.eventType = eventType
	r.payload = payload
	return map[string]any{"ok": true}, nil
}

func TestHandleForwardsEachActionAsItsServiceEvent(t *testing.T) {
	cases := []struct {
		action    string
		eventType string
		payload   map[string]any
	}{
		{ActionIngestKnowledge, "knowledge.ingest", map[string]any{"title": "T", "body": "B"}},
		{ActionSearchKnowledge, "knowledge.search", map[string]any{"query": "q"}},
		{ActionListKnowledge, "knowledge.list", nil},
		{ActionDeleteKnowledge, "knowledge.delete", map[string]any{"source_id": "note:t"}},
		{ActionInspectKnowledge, "knowledge.status", nil},
		{ActionReindexKnowledge, "knowledge.reindex", nil},
	}
	for _, tc := range cases {
		service := &recordingService{}
		_, err := New(service).Handle(context.Background(), Request{Action: tc.action, Event: Event{Payload: tc.payload}})
		if err != nil {
			t.Fatalf("%s: %v", tc.action, err)
		}
		if service.eventType != tc.eventType {
			t.Fatalf("%s: forwarded %q, want %q", tc.action, service.eventType, tc.eventType)
		}
	}
}

func TestHandleRejectsMissingFieldsBeforeCallingTheService(t *testing.T) {
	cases := []struct {
		action  string
		payload map[string]any
		field   string
	}{
		{ActionIngestKnowledge, map[string]any{"title": "T"}, "body"},
		{ActionIngestKnowledge, map[string]any{"title": "  ", "body": "B"}, "title"},
		{ActionSearchKnowledge, map[string]any{"query": " "}, "query"},
		{ActionSearchKnowledge, nil, "query"},
		{ActionDeleteKnowledge, map[string]any{"source_id": 3}, "source_id"},
	}
	for _, tc := range cases {
		service := &recordingService{}
		_, err := New(service).Handle(context.Background(), Request{Action: tc.action, Event: Event{Payload: tc.payload}})
		if err == nil || !strings.Contains(err.Error(), tc.field) {
			t.Fatalf("%s %v: expected an error naming %q, got %v", tc.action, tc.payload, tc.field, err)
		}
		if service.calls != 0 {
			t.Fatalf("%s: the service must not be called for an invalid request", tc.action)
		}
	}
}

func TestHandleRejectsUnknownActionsAndMissingWiring(t *testing.T) {
	if _, err := New(&recordingService{}).Handle(context.Background(), Request{Action: "nope", Event: Event{Type: "knowledge.x"}}); err == nil {
		t.Fatal("expected an unsupported action error")
	}
	if _, err := New(nil).Handle(context.Background(), Request{Action: ActionListKnowledge}); err == nil {
		t.Fatal("expected a not configured error")
	}
	if _, err := (*Domain)(nil).Handle(context.Background(), Request{}); err == nil {
		t.Fatal("expected a nil domain error")
	}
}
