package core

import (
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
)

type EventSource string

const (
	SourceDiscord      EventSource = "discord"
	SourceWhatsApp     EventSource = "whatsapp"
	SourceCLI          EventSource = "cli"
	SourceWeb          EventSource = "web"
	SourceAPI          EventSource = "api"
	SourceOrchestrator EventSource = "orchestrator"
)

type Domain string

const (
	DomainSystem     Domain = "system"
	DomainBook       Domain = "book"
	DomainChat       Domain = "chat"
	DomainMemory     Domain = "memory"
	DomainMeme       Domain = "meme"
	DomainAutomation Domain = "automation"
	DomainModeration Domain = "moderation"
)

type Event struct {
	EventID   string         `json:"event_id"`
	Timestamp time.Time      `json:"timestamp"`
	Source    EventSource    `json:"source"`
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

type Route struct {
	Workflow string
	Domains  []Domain
}

func (e *Event) Normalize() error {
	if e == nil {
		return fmt.Errorf("event is nil")
	}

	e.EventID = strings.TrimSpace(e.EventID)
	if e.EventID == "" {
		e.EventID = uuid.NewString()
	}

	if e.Timestamp.IsZero() {
		e.Timestamp = time.Now().UTC()
	} else {
		e.Timestamp = e.Timestamp.UTC()
	}

	e.Type = strings.TrimSpace(e.Type)
	if e.Type == "" {
		return fmt.Errorf("event type is required")
	}

	e.Source = EventSource(strings.TrimSpace(string(e.Source)))
	if e.Source == "" {
		return fmt.Errorf("event source is required")
	}

	e.User.ID = strings.TrimSpace(e.User.ID)
	e.User.Name = strings.TrimSpace(e.User.Name)
	e.Channel.ID = strings.TrimSpace(e.Channel.ID)

	if e.Payload == nil {
		e.Payload = map[string]any{}
	}
	if e.Context == nil {
		e.Context = map[string]any{}
	}

	return nil
}
