package handlers

import (
	"context"
	"fmt"
	digestdomain "read_books/dokja_domain/dokja_digest"
	"read_books/internal/core"
	"read_books/internal/logger"
)

type DigestDomainHandler struct {
	domain *digestdomain.Domain
}

func NewDigestDomainHandler(service digestdomain.Service) *DigestDomainHandler {
	return &DigestDomainHandler{domain: digestdomain.New(service)}
}

func (h *DigestDomainHandler) Domain() core.Domain {
	return core.DomainDigest
}

func (h *DigestDomainHandler) Handle(ctx context.Context, event core.Event, workflow core.WorkflowStep) (map[string]any, error) {
	if h == nil || h.domain == nil {
		return nil, fmt.Errorf("digest domain is not configured")
	}
	logger.Info(fmt.Sprintf(
		"digest handler received event_id=%s type=%s action=%s source=%s payload_keys=%d",
		event.EventID, event.Type, workflow.Action, event.Source, len(event.Payload),
	))

	return h.domain.Handle(ctx, digestdomain.Request{
		Action: workflow.Action,
		Event: digestdomain.Event{
			ID:      event.EventID,
			Source:  string(event.Source),
			Type:    event.Type,
			Payload: event.Payload,
			Context: event.Context,
		},
	})
}
