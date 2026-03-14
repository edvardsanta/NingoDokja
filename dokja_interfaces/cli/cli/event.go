package cli

import (
	"strings"
	"time"

	"github.com/google/uuid"
)

const (
	DefaultEndpoint = "tcp://127.0.0.1:5555"
	DefaultTopic    = "events"
)

type Event struct {
	EventID   string         `json:"event_id"`
	Timestamp time.Time      `json:"timestamp"`
	Source    string         `json:"source"`
	Type      string         `json:"type"`
	User      EventUser      `json:"user"`
	Channel   EventChannel   `json:"channel"`
	Payload   map[string]any `json:"payload"`
	Context   map[string]any `json:"context"`
}

type EventUser struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type EventChannel struct {
	ID string `json:"id"`
}

type EmitOptions struct {
	Type      string
	UserID    string
	UserName  string
	ChannelID string
	Payload   map[string]any
	Context   map[string]any
	Source    string
}

func BuildEvent(opts EmitOptions) Event {
	payload := opts.Payload
	if payload == nil {
		payload = map[string]any{}
	}

	contextMap := opts.Context
	if contextMap == nil {
		contextMap = map[string]any{}
	}

	source := strings.TrimSpace(opts.Source)
	if source == "" {
		source = "cli"
	}

	return Event{
		EventID:   uuid.NewString(),
		Timestamp: time.Now().UTC(),
		Source:    source,
		Type:      strings.TrimSpace(opts.Type),
		User: EventUser{
			ID:   strings.TrimSpace(opts.UserID),
			Name: strings.TrimSpace(opts.UserName),
		},
		Channel: EventChannel{
			ID: strings.TrimSpace(opts.ChannelID),
		},
		Payload: payload,
		Context: contextMap,
	}
}
