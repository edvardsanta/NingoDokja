package moderation

import (
	"context"
	"fmt"
	"strings"
)

const (
	ActionScreenEvent = "screen-event"
	DecisionAllow     = "allow"
	DecisionBlock     = "block"
	StatusOK          = "ok"
	StatusSkipped     = "skipped"
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

type ScreenInput struct {
	EventID string
	Source  string
	Type    string
	Content string
	Payload map[string]any
	Context map[string]any
}

type Policy interface {
	Screen(ctx context.Context, input ScreenInput) (map[string]any, error)
}

type Domain struct {
	policy Policy
}

func New(policy Policy) *Domain {
	return &Domain{policy: policy}
}

func (d *Domain) Handle(ctx context.Context, request Request) (map[string]any, error) {
	if d == nil {
		return nil, fmt.Errorf("moderation domain is nil")
	}

	switch request.Action {
	case ActionScreenEvent:
		return d.screen(ctx, request.Event)
	default:
		return nil, fmt.Errorf("unsupported moderation action %q for event type %q", request.Action, request.Event.Type)
	}
}

func (d *Domain) screen(ctx context.Context, event Event) (map[string]any, error) {
	// TODO: Expand moderation input beyond text-only screening.
	// We should validate user/channel identity, interface source, and other
	// request metadata before downstream domains continue.
	content := extractContent(event.Payload)
	input := ScreenInput{
		EventID: event.ID,
		Source:  event.Source,
		Type:    event.Type,
		Content: content,
		Payload: event.Payload,
		Context: event.Context,
	}

	if d.policy != nil {
		return d.policy.Screen(ctx, input)
	}

	// TODO: Replace this permissive fallback with real policy behavior.
	// Moderation should eventually support configurable decisions such as
	// allow, redact, block, and flag, with explicit reason/category output.
	result := map[string]any{
		"status":   StatusOK,
		"decision": DecisionAllow,
		"blocked":  false,
		"source":   event.Source,
		"type":     event.Type,
	}
	if content == "" {
		result["status"] = StatusSkipped
		result["reason"] = "no textual content"
		return result, nil
	}

	// TODO: Add user-level and session-level validation here.
	// Examples: rate limits, blocked users, trusted interfaces, and content
	// class checks before chat generation or other workflows continue.
	result["message_chars"] = len(content)
	return result, nil
}

func extractContent(payload map[string]any) string {
	if payload == nil {
		return ""
	}

	keys := []string{"content", "message", "text", "prompt"}
	for _, key := range keys {
		value, _ := payload[key].(string)
		value = strings.TrimSpace(value)
		if value != "" {
			return value
		}
	}
	return ""
}
