package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	conversation "read_books/dokja_domain/dokja_conversation"
	"read_books/internal/core"
)

func (h *ConversationDomainHandler) WithRealtime(engine *conversation.ConversationContextEngine) *ConversationDomainHandler {
	h.realtime = engine
	return h
}
func (h *ConversationDomainHandler) realtimeHandle(_ context.Context, event core.Event) (map[string]any, error) {
	if h.realtime == nil {
		return nil, fmt.Errorf("realtime observer unavailable")
	}
	if event.Type == "realtime.snapshot" {
		if event.Source != core.SourceDesktop {
			return nil, fmt.Errorf("observer snapshots require the local desktop")
		}
		return map[string]any{"sessions": h.realtime.Snapshot(), "retention": "ephemeral_15m", "transcription": "segment_final", "semantics": "unassigned"}, nil
	}
	inbound, _ := event.Context["gateway_inbound"].(bool)
	if event.Source != core.SourceDiscord || !inbound {
		return nil, conversation.ErrRealtime
	}
	guild, _ := event.Context["guild_id"].(string)
	id := payloadString(event.Payload, "session_id")
	var err error
	switch event.Type {
	case "realtime.start":
		err = h.realtime.Start(id, guild, payloadString(event.Payload, "voice_channel_id"), event.Channel.ID, event.User.ID)
	case "realtime.join", "realtime.leave":
		err = h.realtime.Consent(id, guild, event.Channel.ID, event.User.ID, event.Type == "realtime.join")
	case "realtime.stop":
		err = h.realtime.Stop(id, guild, event.Channel.ID, event.User.ID)
	case "realtime.observe":
		var m conversation.RealtimeMessage
		raw, _ := json.Marshal(event.Payload)
		if json.Unmarshal(raw, &m) != nil {
			return nil, conversation.ErrRealtime
		}
		m.SpeakerID = event.User.ID
		err = h.realtime.Observe(guild, event.Channel.ID, m)
	default:
		return nil, conversation.ErrRealtime
	}
	if err != nil {
		return nil, err
	}
	return map[string]any{"accepted": true}, nil
}
