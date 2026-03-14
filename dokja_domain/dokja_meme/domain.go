package meme

import (
	"context"
	"fmt"
)

const (
	ActionFetchMemes        = "fetch-memes"
	ActionRefreshMemePool   = "refresh-meme-pool"
	ActionInspectMemeStatus = "inspect-meme-service"
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
	Fetch(ctx context.Context, limit *int) (map[string]any, error)
	RefreshPool(ctx context.Context, maxItemsPerScraper int) (map[string]any, error)
	Status(ctx context.Context) (map[string]any, error)
}

type Domain struct {
	service Service
}

func New(service Service) *Domain {
	return &Domain{service: service}
}

func (d *Domain) Handle(ctx context.Context, request Request) (map[string]any, error) {
	if d == nil {
		return nil, fmt.Errorf("meme domain is nil")
	}
	if d.service == nil {
		return nil, fmt.Errorf("meme service is not configured")
	}

	switch request.Action {
	case ActionFetchMemes:
		return d.service.Fetch(ctx, intPointer(request.Event.Payload["limit"]))
	case ActionRefreshMemePool:
		maxItems := intValue(request.Event.Payload["max_items_per_scraper"], 20)
		return d.service.RefreshPool(ctx, maxItems)
	case ActionInspectMemeStatus:
		return d.service.Status(ctx)
	default:
		return nil, fmt.Errorf("unsupported meme action %q for event type %q", request.Action, request.Event.Type)
	}
}

func intPointer(value any) *int {
	if parsed, ok := parseInt(value); ok {
		return &parsed
	}
	return nil
}

func intValue(value any, fallback int) int {
	if parsed, ok := parseInt(value); ok {
		return parsed
	}
	return fallback
}

func parseInt(value any) (int, bool) {
	switch typed := value.(type) {
	case int:
		return typed, true
	case int8:
		return int(typed), true
	case int16:
		return int(typed), true
	case int32:
		return int(typed), true
	case int64:
		return int(typed), true
	case float32:
		return int(typed), true
	case float64:
		return int(typed), true
	default:
		return 0, false
	}
}
