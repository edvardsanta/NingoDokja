package handlers

import (
	"context"
	"fmt"
	memorydomain "read_books/dokja_domain/dokja_memory"
	"read_books/internal/core"
	"read_books/internal/logger"
)

// syncMemoryAction is reserved for syncing a conversation into memory; nothing emits it yet.
const syncMemoryAction = "sync-memory"

type MemoryDomainHandler struct {
	domain *memorydomain.Domain
}

func NewMemoryDomainHandler(service memorydomain.Service) *MemoryDomainHandler {
	return &MemoryDomainHandler{domain: memorydomain.New(service)}
}

func (h *MemoryDomainHandler) Domain() core.Domain {
	return core.DomainMemory
}

func (h *MemoryDomainHandler) Handle(ctx context.Context, event core.Event, workflow core.WorkflowStep) (map[string]any, error) {
	if h == nil || h.domain == nil {
		return nil, fmt.Errorf("memory domain is not configured")
	}
	// The payload is the user's own text: log its shape, never its content.
	logger.Info(fmt.Sprintf(
		"memory handler received event_id=%s type=%s action=%s source=%s payload_keys=%d",
		event.EventID, event.Type, workflow.Action, event.Source, len(event.Payload),
	))

	if workflow.Action == syncMemoryAction {
		return nil, nil
	}

	return h.domain.Handle(ctx, memorydomain.Request{
		Action: workflow.Action,
		Event: memorydomain.Event{
			ID:      event.EventID,
			Source:  string(event.Source),
			Type:    event.Type,
			Payload: event.Payload,
			Context: event.Context,
		},
	})
}
