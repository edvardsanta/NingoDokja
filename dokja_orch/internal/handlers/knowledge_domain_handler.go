package handlers

import (
	"context"
	"fmt"
	knowledgedomain "read_books/dokja_domain/dokja_knowledge"
	"read_books/internal/core"
	"read_books/internal/logger"
)

type KnowledgeDomainHandler struct {
	domain *knowledgedomain.Domain
}

func NewKnowledgeDomainHandler(service knowledgedomain.Service) *KnowledgeDomainHandler {
	return &KnowledgeDomainHandler{domain: knowledgedomain.New(service)}
}

func (h *KnowledgeDomainHandler) Domain() core.Domain {
	return core.DomainKnowledge
}

func (h *KnowledgeDomainHandler) Handle(ctx context.Context, event core.Event, workflow core.WorkflowStep) (map[string]any, error) {
	if h == nil || h.domain == nil {
		return nil, fmt.Errorf("knowledge domain is not configured")
	}
	// The payload is the user's notes or question: log its shape, never its content.
	logger.Info(fmt.Sprintf(
		"knowledge handler received event_id=%s type=%s action=%s source=%s payload_keys=%d",
		event.EventID, event.Type, workflow.Action, event.Source, len(event.Payload),
	))

	return h.domain.Handle(ctx, knowledgedomain.Request{
		Action: workflow.Action,
		Event: knowledgedomain.Event{
			ID:      event.EventID,
			Source:  string(event.Source),
			Type:    event.Type,
			Payload: event.Payload,
			Context: event.Context,
		},
	})
}
