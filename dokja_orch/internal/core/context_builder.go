package core

import (
	"context"
	"time"
)

type ContextBuilder interface {
	Build(ctx context.Context, event Event, route Route) (map[string]any, error)
}

type DefaultContextBuilder struct{}

func NewDefaultContextBuilder() *DefaultContextBuilder {
	return &DefaultContextBuilder{}
}

func (b *DefaultContextBuilder) Build(_ context.Context, event Event, route Route) (map[string]any, error) {
	contextMap := make(map[string]any, len(event.Context)+5)
	for key, value := range event.Context {
		contextMap[key] = value
	}

	contextMap["event_id"] = event.EventID
	contextMap["source"] = event.Source
	contextMap["workflow"] = route.Workflow
	contextMap["route_domains"] = domainsToStrings(route.Domains)
	contextMap["received_at"] = time.Now().UTC().Format(time.RFC3339)

	return contextMap, nil
}
