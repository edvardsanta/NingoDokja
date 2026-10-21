package handlers

import (
	"context"
	"fmt"
	knowledgedomain "read_books/dokja_domain/dokja_knowledge"
	"read_books/internal/core"
	"read_books/internal/logger"
)

type KnowledgeDomainHandler struct {
	domain  *knowledgedomain.Domain
	reading *ReadingTask
}

func NewKnowledgeDomainHandler(service knowledgedomain.Service) *KnowledgeDomainHandler {
	return &KnowledgeDomainHandler{domain: knowledgedomain.New(service)}
}

// WithReading serves knowledge.read through the reading host. Without it, knowledge.read is
// refused and every other knowledge event is unchanged.
func (h *KnowledgeDomainHandler) WithReading(task *ReadingTask) *KnowledgeDomainHandler {
	h.reading = task
	return h
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

	if workflow.Action == knowledgedomain.ActionReclassifyKnowledge {
		if h.reading == nil {
			return nil, fmt.Errorf("knowledge.reclassify needs the reading host")
		}
		return h.reading.Reclassify(ctx, event)
	}
	if workflow.Action == knowledgedomain.ActionReadKnowledge && h.reading != nil {
		return h.reading.Read(ctx, event)
	}
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
