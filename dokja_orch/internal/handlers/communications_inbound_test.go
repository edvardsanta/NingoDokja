package handlers

import (
	"context"
	"errors"
	communications "read_books/dokja_domain/dokja_communications"
	"read_books/internal/core"
	"testing"
)

func TestInboundHandlerRefusesMissingAndUnlistedMetadata(t *testing.T) {
	h := NewCommunicationsDomainHandler(nil, "").WithInboundPolicy(communications.InboundPolicy{
		DiscordGuildIDs: []string{"guild"}, DiscordChannels: []string{"reading"},
	})
	step := core.WorkflowStep{Domain: core.DomainCommunications, Action: communications.ActionAuthorizeInbound}
	base := core.Event{Source: core.SourceDiscord, Channel: core.EventChannel{ID: "reading"}, Context: map[string]any{
		"interface": "discord", "entry_kind": "message", "guild_id": "guild", "is_direct_message": false, "mentions_bot": false,
	}}
	if _, err := NewCommunicationsDomainHandler(nil, "").Handle(context.Background(), base, step); !errors.Is(err, core.ErrInboundDenied) {
		t.Fatalf("unconfigured inbound policy authorized a message: %v", err)
	}
	if _, err := h.Handle(context.Background(), base, step); err != nil {
		t.Fatalf("allowed message refused: %v", err)
	}
	for name, mutate := range map[string]func(*core.Event){
		"missing direct marker": func(e *core.Event) { delete(e.Context, "is_direct_message") },
		"other channel":         func(e *core.Event) { e.Channel.ID = "other" },
		"source mismatch":       func(e *core.Event) { e.Source = core.SourceWhatsApp },
	} {
		t.Run(name, func(t *testing.T) {
			event := base
			event.Context = map[string]any{}
			for key, value := range base.Context {
				event.Context[key] = value
			}
			mutate(&event)
			if _, err := h.Handle(context.Background(), event, step); !errors.Is(err, core.ErrInboundDenied) {
				t.Fatalf("expected silent refusal, got %v", err)
			}
		})
	}
}
