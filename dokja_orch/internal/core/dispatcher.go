package core

import (
	"context"
	"fmt"
)

type DomainHandler interface {
	Domain() Domain
	Handle(ctx context.Context, event Event, workflow WorkflowStep) (map[string]any, error)
}

type DomainDispatcher interface {
	Dispatch(ctx context.Context, event Event, workflow Workflow) (map[string]any, error)
}

type DefaultDomainDispatcher struct {
	handlers map[Domain]DomainHandler
}

func NewDomainDispatcher(handlers ...DomainHandler) *DefaultDomainDispatcher {
	dispatcher := &DefaultDomainDispatcher{
		handlers: make(map[Domain]DomainHandler, len(handlers)),
	}

	for _, handler := range handlers {
		if handler == nil {
			continue
		}
		dispatcher.handlers[handler.Domain()] = handler
	}

	return dispatcher
}

func (d *DefaultDomainDispatcher) Dispatch(ctx context.Context, event Event, workflow Workflow) (map[string]any, error) {
	if d == nil {
		return nil, fmt.Errorf("domain dispatcher is nil")
	}

	result := map[string]any{}
	for _, step := range workflow.Steps {
		handler, ok := d.handlers[step.Domain]
		if !ok {
			return nil, fmt.Errorf("no handler registered for domain %q", step.Domain)
		}

		handlerResult, err := handler.Handle(ctx, event, step)
		if err != nil {
			return nil, fmt.Errorf("dispatch %s: %w", step.Domain, err)
		}

		if len(handlerResult) > 0 {
			result[string(step.Domain)] = handlerResult
		}
	}

	return result, nil
}
