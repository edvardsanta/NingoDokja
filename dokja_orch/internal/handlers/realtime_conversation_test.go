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

func TestVoiceChatStartOpensAVoiceChatJournalTheDesktopCanRead(t *testing.T) {
	h := NewConversationDomainHandler(nil).WithRealtime(conversation.NewConversationContextEngine())
	start := core.Event{Type: "realtime.start", Source: core.SourceDiscord, User: core.EventUser{ID: "a"}, Channel: core.EventChannel{ID: "text"},
		Payload: map[string]any{"session_id": "s", "voice_channel_id": "voice", "mode": "voice_chat"}, Context: map[string]any{"guild_id": "guild", "gateway_inbound": true}}
	if _, err := h.Handle(t.Context(), start, core.WorkflowStep{}); err != nil {
		t.Fatal(err)
	}
	snapshot := core.Event{Type: "realtime.snapshot", Source: core.SourceDesktop}
	result, err := h.Handle(t.Context(), snapshot, core.WorkflowStep{})
	if err != nil {
		t.Fatal(err)
	}
	if sessions := result["sessions"].([]conversation.RealtimeSession); len(sessions) != 1 || sessions[0].Mode != "voice_chat" {
		t.Fatalf("snapshot = %#v", sessions)
	}
	start.Payload = map[string]any{"session_id": "t", "voice_channel_id": "voice", "mode": "broadcast"}
	if _, err := h.Handle(t.Context(), start, core.WorkflowStep{}); err == nil {
		t.Fatal("an unknown realtime mode was accepted")
	}
}

func TestNingoSpokenReplyIsMarkedOnlyByTheAdapterRole(t *testing.T) {
	h := NewConversationDomainHandler(nil).WithRealtime(conversation.NewConversationContextEngine())
	ctx := map[string]any{"guild_id": "guild", "gateway_inbound": true}
	start := core.Event{Type: "realtime.start", Source: core.SourceDiscord, User: core.EventUser{ID: "a"}, Channel: core.EventChannel{ID: "text"},
		Payload: map[string]any{"session_id": "s", "voice_channel_id": "voice", "mode": "voice_chat"}, Context: ctx}
	if _, err := h.Handle(t.Context(), start, core.WorkflowStep{}); err != nil {
		t.Fatal(err)
	}
	line := func(id, role string, extra map[string]any) core.Event {
		payload := map[string]any{"session_id": "s", "message_id": id, "capture_sequence": 1, "text": "Nenhum.", "transcription_status": "final", "revision": 1,
			"timestamp_start": "2026-11-08T23:00:00Z", "timestamp_end": "2026-11-08T23:00:01Z", "speaker_role": role}
		for k, v := range extra {
			payload[k] = v
		}
		return core.Event{Type: "realtime.observe", Source: core.SourceDiscord, User: core.EventUser{ID: "bot", Name: "Ningo Dokja"}, Channel: core.EventChannel{ID: "text"}, Payload: payload, Context: ctx}
	}
	if _, err := h.Handle(t.Context(), line("reply", "ningo", nil), core.WorkflowStep{}); err != nil {
		t.Fatal(err)
	}
	if _, err := h.Handle(t.Context(), line("forged", "", map[string]any{"role": "ningo"}), core.WorkflowStep{}); err != nil {
		t.Fatal(err)
	}
	snapshot, _ := h.Handle(t.Context(), core.Event{Type: "realtime.snapshot", Source: core.SourceDesktop}, core.WorkflowStep{})
	messages := snapshot["sessions"].([]conversation.RealtimeSession)[0].Messages
	if messages[0].Role != "ningo" || messages[0].SpeakerID != "ningo" || messages[0].SpeakerName != "Ningo" {
		t.Fatalf("Ningo's reply = %#v", messages[0])
	}
	if messages[1].Role != "" || messages[1].SpeakerID != "bot" {
		t.Fatalf("a role in the message body made a line Ningo's: %#v", messages[1])
	}
}
