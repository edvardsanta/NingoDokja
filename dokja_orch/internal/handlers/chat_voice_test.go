package handlers

import (
	"context"
	chat "read_books/dokja_domain/dokja_chat"
	"read_books/internal/core"
	"strings"
	"testing"
)

func TestVoiceConversationSharesAttributedHistoryAcrossSpeakers(t *testing.T) {
	generator := &plainGenerator{}
	handler := NewChatDomainHandler(generator)
	event := chatEvent("second")
	event.Context = map[string]any{"guild_id": "guild", "entry_kind": "command", "session_key": "discord-voice:guild:room", "voice_turns": []chat.VoiceTurn{{"account-a", "Scholar", "first"}, {"account-b", "Reader", "Ningo, second"}}}
	event.User = core.EventUser{ID: "account-b", Name: "Reader"}
	if _, err := handler.Handle(t.Context(), event, chatStep); err != nil {
		t.Fatal(err)
	}
	state := handler.sessions.items[chatSessionBaseKey(event)]
	if len(state.messages) != 2 || strings.Count(state.messages[0].Content, "participant-") != 2 || strings.Contains(state.messages[0].Content, "account-") {
		t.Fatal(state.messages)
	}
	originalID := state.id
	event.User = core.EventUser{ID: "account-c", Name: "Third"}
	event.Context["voice_turns"] = []chat.VoiceTurn{{"account-c", "Third", "And why?"}}
	event.Payload["content"] = "continue"
	if _, err := handler.Handle(t.Context(), event, chatStep); err != nil {
		t.Fatal(err)
	}
	state = handler.sessions.items[chatSessionBaseKey(event)]
	if state.id != originalID || len(state.messages) != 4 {
		t.Fatal("speaker change reset voice history")
	}
	if strings.Contains(personaForEvent(event), discordReplyPresentation) || !strings.Contains(personaForEvent(event), "live Discord voice") {
		t.Fatal("voice got text formatting instructions")
	}
}

type cancelledVoiceGenerator struct{ cancel context.CancelFunc }

func (g cancelledVoiceGenerator) Generate(context.Context, string) (map[string]any, error) {
	g.cancel()
	return map[string]any{"reply": "late reply"}, nil
}
func TestInterruptedVoiceReplyIsNotSavedAsSpokenHistory(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	handler := NewChatDomainHandler(cancelledVoiceGenerator{cancel})
	event := chatEvent("hello")
	if _, err := handler.Handle(ctx, event, chatStep); err == nil {
		t.Fatal("late success accepted")
	}
	state := handler.sessions.items[chatSessionBaseKey(event)]
	if len(state.messages) != 1 || state.messages[0].Role != "user" {
		t.Fatal("cancelled reply entered history", state.messages)
	}
}
