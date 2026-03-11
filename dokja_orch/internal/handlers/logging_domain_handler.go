package handlers

import (
	"context"
	"fmt"
	"read_books/internal/core"
	"read_books/internal/logger"
)

type LoggingDomainHandler struct {
	domain core.Domain
}

func NewLoggingDomainHandler(domain core.Domain) *LoggingDomainHandler {
	return &LoggingDomainHandler{domain: domain}
}

func (h *LoggingDomainHandler) Domain() core.Domain {
	return h.domain
}

func (h *LoggingDomainHandler) Handle(_ context.Context, event core.Event, workflow core.WorkflowStep) (map[string]any, error) {
	logger.Info(fmt.Sprintf(
		"Domain %s handling event %s (%s) with action %s",
		h.domain,
		event.EventID,
		event.Type,
		workflow.Action,
	))
	return nil, nil
}
