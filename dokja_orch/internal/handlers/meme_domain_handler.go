package handlers

import (
	"context"
	"fmt"
	memedomain "read_books/dokja_domain/dokja_meme"
	"read_books/internal/core"
	"read_books/internal/logger"
)

type MemeDomainHandler struct {
	domain *memedomain.Domain
}

func NewMemeDomainHandler(service memedomain.Service) *MemeDomainHandler {
	return &MemeDomainHandler{domain: memedomain.New(service)}
}

func (h *MemeDomainHandler) Domain() core.Domain {
	return core.DomainMeme
}

func (h *MemeDomainHandler) Handle(ctx context.Context, event core.Event, workflow core.WorkflowStep) (map[string]any, error) {
	if h == nil || h.domain == nil {
		return nil, fmt.Errorf("meme domain is not configured")
	}
	logger.Info(fmt.Sprintf(
		"meme handler received event_id=%s type=%s action=%s source=%s",
		event.EventID,
		event.Type,
		workflow.Action,
		event.Source,
	))

	response, err := h.domain.Handle(ctx, memedomain.Request{
		Action: workflow.Action,
		Event: memedomain.Event{
			ID:      event.EventID,
			Source:  string(event.Source),
			Type:    event.Type,
			Payload: event.Payload,
			Context: event.Context,
		},
	})
	if err != nil {
		return nil, err
	}

	logger.Info(fmt.Sprintf(
		"meme handler completed event_id=%s type=%s action=%s result_keys=%d",
		event.EventID,
		event.Type,
		workflow.Action,
		len(response),
	))
	return response, nil
}
