package handlers

import (
	"context"
	communications "read_books/dokja_domain/dokja_communications"
	"read_books/internal/core"
)

type CommunicationsDomainHandler struct{ domain *communications.Domain }

func NewCommunicationsDomainHandler(service communications.Service, channels string) *CommunicationsDomainHandler {
	return &CommunicationsDomainHandler{domain: communications.New(service, channels)}
}

// WithConversations adds the conversations an interface keeps the text of (WhatsApp), so they
// are listed and read next to the Discord channels.
func (h *CommunicationsDomainHandler) WithConversations(conversations communications.Conversations) *CommunicationsDomainHandler {
	h.domain.WithConversations(conversations)
	return h
}
func (h *CommunicationsDomainHandler) Domain() core.Domain { return core.DomainCommunications }
func (h *CommunicationsDomainHandler) Handle(ctx context.Context, event core.Event, step core.WorkflowStep) (map[string]any, error) {
	return h.domain.Handle(ctx, step.Action, event.Payload)
}
