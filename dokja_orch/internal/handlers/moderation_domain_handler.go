package handlers

import (
	"context"
	"fmt"
	moderationdomain "read_books/dokja_domain/dokja_moderation"
	"read_books/internal/core"
	"read_books/internal/logger"
)

type ModerationDomainHandler struct {
	domain *moderationdomain.Domain
}

func NewModerationDomainHandler(policy moderationdomain.Policy) *ModerationDomainHandler {
	return &ModerationDomainHandler{domain: moderationdomain.New(policy)}
}

func (h *ModerationDomainHandler) Domain() core.Domain {
	return core.DomainModeration
}

func (h *ModerationDomainHandler) Handle(ctx context.Context, event core.Event, workflow core.WorkflowStep) (map[string]any, error) {
	if h == nil || h.domain == nil {
		return nil, fmt.Errorf("moderation domain is not configured")
	}

	// TODO: Once moderation decisions are richer, the orchestrator should use
	// this result to short-circuit or reshape downstream workflows instead of
	// treating moderation as a mostly observational step.
	logger.Info(fmt.Sprintf(
		"moderation handler received event_id=%s type=%s action=%s source=%s",
		event.EventID,
		event.Type,
		workflow.Action,
		event.Source,
	))

	response, err := h.domain.Handle(ctx, moderationdomain.Request{
		Action: workflow.Action,
		Event: moderationdomain.Event{
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
		"moderation handler completed event_id=%s type=%s action=%s result_keys=%d",
		event.EventID,
		event.Type,
		workflow.Action,
		len(response),
	))
	return response, nil
}
