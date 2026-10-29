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

func (r *RuleBasedEventRouter) Route(ctx context.Context, event Event) (Route, error) {
	route, err := r.route(ctx, event)
	if err != nil {
		return Route{}, err
	}
	if inbound, _ := event.Context["gateway_inbound"].(bool); inbound {
		// The bridge marks its own events. Authorization must precede chat and
		// fixed command workflows, including messages that map to meme actions.
		route.Domains = append([]Domain{DomainCommunications}, route.Domains...)
	}
	return route, nil
}

func (r *RuleBasedEventRouter) route(_ context.Context, event Event) (Route, error) {
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
	case eventType == "services.set", eventType == "scheduler.jobs.set", eventType == "scheduler.jobs.announce",
		eventType == "chat.profiles.list", eventType == "chat.profile.use":
		return Route{
			Workflow: "admin",
			Domains:  []Domain{DomainSystem},
		}, nil
	case eventType == "communications.send":
		return Route{Workflow: "communications-send", Domains: []Domain{DomainSystem}}, nil
	case strings.HasPrefix(eventType, "communications.link."):
		return Route{Workflow: "interface-link", Domains: []Domain{DomainSystem}}, nil
	case strings.HasPrefix(eventType, "voice."):
		return Route{Workflow: "voice-audio", Domains: []Domain{DomainCommunications}}, nil
	case strings.HasPrefix(eventType, "communications."):
		return Route{Workflow: "communications", Domains: []Domain{DomainCommunications}}, nil
	case strings.HasPrefix(eventType, "music."):
		// Music in a voice channel is a workflow of Communications (it decides the queue), not a
		// domain of its own. Its events come from people in a channel, so they are not under the
		// operator's communications.* routes.
		return Route{Workflow: "music", Domains: []Domain{DomainCommunications}}, nil
	case eventType == "discord.send":
		return Route{
			Workflow: "discord-send",
			Domains:  []Domain{DomainSystem},
		}, nil
	case strings.HasPrefix(eventType, "book."):
		return Route{
			Workflow: "book",
			Domains:  []Domain{DomainBook},
		}, nil
	case strings.HasPrefix(eventType, "knowledge."):
		return Route{
			Workflow: "knowledge",
			Domains:  []Domain{DomainKnowledge},
		}, nil
	case strings.HasPrefix(eventType, "realtime."):
		return Route{Workflow: "realtime-observer", Domains: []Domain{DomainConversation}}, nil
	case strings.HasPrefix(eventType, "conversation."):
		return Route{Workflow: "conversation-archive", Domains: []Domain{DomainConversation}}, nil
	case strings.HasPrefix(eventType, "digest."):
		return Route{
			Workflow: "digest",
			Domains:  []Domain{DomainDigest},
		}, nil
	case strings.HasPrefix(eventType, "ningo."):
		return Route{
			Workflow: "ningo",
			Domains:  []Domain{DomainSystem},
		}, nil
	case eventType == "message.created" && event.Source == SourceDesktop:
		return Route{Workflow: "desktop-conversation", Domains: []Domain{DomainChat}}, nil
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
