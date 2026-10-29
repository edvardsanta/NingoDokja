package handlers

import (
	conversation "read_books/dokja_domain/dokja_conversation"
	"read_books/internal/core"
	"testing"
)

func TestRealtimeObservationIsSeparateFromOwnerArchiveAndAuthenticated(t *testing.T) {
	h := NewConversationDomainHandler(nil).WithRealtime(conversation.NewConversationContextEngine())
	event := core.Event{Type: "realtime.start", Source: core.SourceDiscord, User: core.EventUser{ID: "a"}, Channel: core.EventChannel{ID: "text"}, Payload: map[string]any{"session_id": "s", "voice_channel_id": "voice"}, Context: map[string]any{"guild_id": "guild"}}
	if _, err := h.Handle(t.Context(), event, core.WorkflowStep{}); err == nil {
		t.Fatal("unauthenticated observer accepted")
	}
	event.Context["gateway_inbound"] = true
	if _, err := h.Handle(t.Context(), event, core.WorkflowStep{}); err != nil {
		t.Fatal(err)
	}
	event.Type = "realtime.snapshot"
	if _, err := h.Handle(t.Context(), event, core.WorkflowStep{}); err == nil {
		t.Fatal("Discord read owner snapshot")
	}
	event.Source = core.SourceDesktop
	result, err := h.Handle(t.Context(), event, core.WorkflowStep{})
	if err != nil {
		t.Fatal(err)
	}
	if len(result["sessions"].([]conversation.RealtimeSession)) != 1 {
		t.Fatal("observer snapshot missing")
	}
	event.Source = core.SourceDiscord
	event.Type = "conversation.list"
	if _, err := h.Handle(t.Context(), event, core.WorkflowStep{}); err == nil {
		t.Fatal("observer bypassed archive availability or scope")
	}
}
