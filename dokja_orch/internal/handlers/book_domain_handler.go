package handlers

import (
	"context"
	"fmt"
	bookdomain "read_books/dokja_domain/dokja_book"
	"read_books/internal/core"
	"read_books/internal/logger"
)

type BookDomainHandler struct {
	domain *bookdomain.Domain
}

func NewBookDomainHandler(service bookdomain.Service) *BookDomainHandler {
	return &BookDomainHandler{domain: bookdomain.New(service)}
}

func (h *BookDomainHandler) Domain() core.Domain {
	return core.DomainBook
}

func (h *BookDomainHandler) Handle(ctx context.Context, event core.Event, workflow core.WorkflowStep) (map[string]any, error) {
	if h == nil || h.domain == nil {
		return nil, fmt.Errorf("book domain is not configured")
	}
	logger.Info(fmt.Sprintf(
		"book handler received event_id=%s type=%s action=%s source=%s",
		event.EventID,
		event.Type,
		workflow.Action,
		event.Source,
	))

	response, err := h.domain.Handle(ctx, bookdomain.Request{
		Action: workflow.Action,
		Event: bookdomain.Event{
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
		"book handler completed event_id=%s type=%s action=%s result_keys=%d",
		event.EventID,
		event.Type,
		workflow.Action,
		len(response),
	))
	return response, nil
}
