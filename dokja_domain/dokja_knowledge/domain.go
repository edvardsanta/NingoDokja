package knowledge

import (
	"context"
	"fmt"
	"strings"
)

const (
	ActionIngestKnowledge  = "ingest-knowledge"
	ActionSearchKnowledge  = "search-knowledge"
	ActionListKnowledge    = "list-knowledge"
	ActionDeleteKnowledge  = "delete-knowledge"
	ActionInspectKnowledge = "inspect-knowledge"
	ActionReindexKnowledge = "reindex-knowledge"
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

// Service is the research knowledge base. Dispatch sends one knowledge.* event to it.
type Service interface {
	Dispatch(ctx context.Context, eventType string, payload map[string]any) (map[string]any, error)
}

type Domain struct {
	service Service
}

func New(service Service) *Domain {
	return &Domain{service: service}
}

// requiredFields are what each action cannot do without. Checking them here keeps a
// malformed request from ever reaching the service.
var requiredFields = map[string][]string{
	ActionIngestKnowledge: {"title", "body"},
	ActionSearchKnowledge: {"query"},
	ActionDeleteKnowledge: {"source_id"},
}

var eventTypes = map[string]string{
	ActionIngestKnowledge:  "knowledge.ingest",
	ActionSearchKnowledge:  "knowledge.search",
	ActionListKnowledge:    "knowledge.list",
	ActionDeleteKnowledge:  "knowledge.delete",
	ActionInspectKnowledge: "knowledge.status",
	ActionReindexKnowledge: "knowledge.reindex",
}

func (d *Domain) Handle(ctx context.Context, request Request) (map[string]any, error) {
	if d == nil {
		return nil, fmt.Errorf("knowledge domain is nil")
	}
	if d.service == nil {
		return nil, fmt.Errorf("knowledge service is not configured")
	}

	eventType, ok := eventTypes[request.Action]
	if !ok {
		return nil, fmt.Errorf("unsupported knowledge action %q for event type %q", request.Action, request.Event.Type)
	}
	payload := request.Event.Payload
	for _, field := range requiredFields[request.Action] {
		if value, _ := payload[field].(string); strings.TrimSpace(value) == "" {
			return nil, fmt.Errorf("%s is required", field)
		}
	}
	return d.service.Dispatch(ctx, eventType, payload)
}
