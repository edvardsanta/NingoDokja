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
	ActionSearchKnowledge: {"query"},
	ActionDeleteKnowledge: {"source_id"},
}

// ingestInputs are the ways to hand the service a document: its text, an uploaded file or
// a source it should read. Exactly one is needed; a title is only required with a body,
// since files and sources carry their own.
var ingestInputs = []string{"body", "content_b64", "source"}

func validateIngest(payload map[string]any) error {
	given := 0
	for _, field := range ingestInputs {
		if value, _ := payload[field].(string); strings.TrimSpace(value) != "" {
			given++
		}
	}
	if given != 1 {
		return fmt.Errorf("send exactly one of %s", strings.Join(ingestInputs, ", "))
	}
	if body, _ := payload["body"].(string); strings.TrimSpace(body) != "" {
		if title, _ := payload["title"].(string); strings.TrimSpace(title) == "" {
			return fmt.Errorf("title is required with body")
		}
	}
	if upload, _ := payload["content_b64"].(string); strings.TrimSpace(upload) != "" {
		if name, _ := payload["filename"].(string); strings.TrimSpace(name) == "" {
			return fmt.Errorf("filename is required with content_b64")
		}
	}
	return nil
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
	if request.Action == ActionIngestKnowledge {
		if err := validateIngest(payload); err != nil {
			return nil, err
		}
	}
	for _, field := range requiredFields[request.Action] {
		if value, _ := payload[field].(string); strings.TrimSpace(value) == "" {
			return nil, fmt.Errorf("%s is required", field)
		}
	}
	return d.service.Dispatch(ctx, eventType, payload)
}
