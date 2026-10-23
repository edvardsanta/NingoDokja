package handlers

import (
	"context"
	communications "read_books/dokja_domain/dokja_communications"
	"read_books/internal/core"
)

type CommunicationsDomainHandler struct {
	domain            *communications.Domain
	inbound           communications.InboundPolicy
	inboundConfigured bool
}

func NewCommunicationsDomainHandler(service communications.Service, channels string) *CommunicationsDomainHandler {
	return &CommunicationsDomainHandler{domain: communications.New(service, channels)}
}

// WithConversations adds the conversations an interface keeps the text of (WhatsApp), so they
// are listed and read next to the Discord channels.
func (h *CommunicationsDomainHandler) WithConversations(conversations communications.Conversations) *CommunicationsDomainHandler {
	h.domain.WithConversations(conversations)
	return h
}
func (h *CommunicationsDomainHandler) WithInboundPolicy(policy communications.InboundPolicy) *CommunicationsDomainHandler {
	h.inbound = policy
	h.inboundConfigured = true
	return h
}
func (h *CommunicationsDomainHandler) Domain() core.Domain { return core.DomainCommunications }
func (h *CommunicationsDomainHandler) Handle(ctx context.Context, event core.Event, step core.WorkflowStep) (map[string]any, error) {
	if step.Action == communications.ActionAuthorizeInbound {
		if !h.inboundConfigured {
			return nil, core.ErrInboundDenied
		}
		intent, ok := inboundIntent(event)
		if !ok || !h.inbound.AuthorizeInbound(intent) {
			return nil, core.ErrInboundDenied
		}
		return nil, nil
	}
	return h.domain.Handle(ctx, step.Action, event.Payload)
}

func inboundIntent(event core.Event) (communications.InboundIntent, bool) {
	entry, entryOK := event.Context["entry_kind"].(string)
	platform, platformOK := event.Context["interface"].(string)
	if !entryOK || !platformOK || platform != string(event.Source) {
		return communications.InboundIntent{}, false
	}
	r := communications.InboundIntent{Platform: platform, Entry: entry, ChannelID: event.Channel.ID}
	switch platform {
	case "discord":
		var ok bool
		r.Direct, ok = event.Context["is_direct_message"].(bool)
		if !ok {
			return communications.InboundIntent{}, false
		}
		r.GuildID, _ = event.Context["guild_id"].(string)
		if entry == "message" {
			r.Mentioned, ok = event.Context["mentions_bot"].(bool)
			if !ok {
				return communications.InboundIntent{}, false
			}
		}
	case "whatsapp":
		var ok bool
		r.ChatKind, ok = event.Context["chat_kind"].(string)
		if !ok {
			return communications.InboundIntent{}, false
		}
		r.SelfChat, ok = event.Context["is_self_chat"].(bool)
		if !ok {
			return communications.InboundIntent{}, false
		}
		r.Mentioned, ok = event.Context["mentions_bot"].(bool)
		if !ok {
			return communications.InboundIntent{}, false
		}
		r.SenderNumbers, ok = event.Context["sender_numbers"].([]string)
		if !ok {
			return communications.InboundIntent{}, false
		}
	default:
		return communications.InboundIntent{}, false
	}
	return r, true
}
