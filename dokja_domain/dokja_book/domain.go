package book

import (
	"context"
	"fmt"
)

const (
	ActionSummarizeBook        = "summarize-book"
	ActionClassifyBookResource = "classify-book-resource"
)

type Event struct {
	ID      string
	Source  string
	Type    string
	Payload map[string]any
	Context map[string]any
}

type Request struct {
	Event  Event
	Action string
}

type Service interface {
	Summarize(ctx context.Context, input map[string]any) (map[string]any, error)
	Classify(ctx context.Context, input map[string]any) (map[string]any, error)
}

type Domain struct {
	service Service
}

func New(service Service) *Domain {
	return &Domain{service: service}
}

func (d *Domain) Handle(ctx context.Context, request Request) (map[string]any, error) {
	if d == nil {
		return nil, fmt.Errorf("book domain is nil")
	}
	if d.service == nil {
		return nil, fmt.Errorf("book service is not configured")
	}

	switch request.Action {
	case ActionSummarizeBook:
		return d.service.Summarize(ctx, mergedInput(request.Event))
	case ActionClassifyBookResource:
		return d.service.Classify(ctx, mergedInput(request.Event))
	default:
		return nil, fmt.Errorf("unsupported book action %q for event type %q", request.Action, request.Event.Type)
	}
}

func mergedInput(event Event) map[string]any {
	input := make(map[string]any, len(event.Context)+len(event.Payload)+3)
	for key, value := range event.Context {
		input[key] = value
	}
	for key, value := range event.Payload {
		input[key] = value
	}
	input["event_id"] = event.ID
	input["event_type"] = event.Type
	input["event_source"] = event.Source
	return input
}
