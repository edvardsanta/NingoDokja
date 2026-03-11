package core

import (
	"context"
	"fmt"
	"strings"
)

type EventRouter interface {
	Route(ctx context.Context, event Event) (Route, error)
}

type RuleBasedEventRouter struct{}

func NewRuleBasedEventRouter() *RuleBasedEventRouter {
	return &RuleBasedEventRouter{}
}

func (r *RuleBasedEventRouter) Route(_ context.Context, event Event) (Route, error) {
	eventType := strings.TrimSpace(event.Type)
	if eventType == "" {
		return Route{}, fmt.Errorf("event type is required")
	}

	switch {
	case eventType == "meme.dispatch.scheduled":
		return Route{
			Workflow: "meme-dispatch",
			Domains:  []Domain{DomainSystem},
		}, nil
	case strings.HasPrefix(eventType, "ningo."):
		return Route{
			Workflow: "ningo",
			Domains:  []Domain{DomainSystem},
		}, nil
	case strings.HasPrefix(eventType, "message."):
		return Route{
			Workflow: "conversation",
			Domains:  []Domain{DomainModeration, DomainChat},
		}, nil
	case strings.HasPrefix(eventType, "meme."):
		return Route{
			Workflow: "meme",
			Domains:  []Domain{DomainMeme},
		}, nil
	case strings.HasPrefix(eventType, "memory."):
		return Route{
			Workflow: "memory",
			Domains:  []Domain{DomainMemory},
		}, nil
	case strings.HasPrefix(eventType, "automation."):
		return Route{
			Workflow: "automation",
			Domains:  []Domain{DomainAutomation},
		}, nil
	case strings.HasPrefix(eventType, "moderation."):
		return Route{
			Workflow: "moderation",
			Domains:  []Domain{DomainModeration},
		}, nil
	default:
		return Route{
			Workflow: "default",
			Domains:  []Domain{DomainChat},
		}, nil
	}
}
