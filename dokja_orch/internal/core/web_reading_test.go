package core

import (
	"context"
	chat "read_books/dokja_domain/dokja_chat"
	"testing"
)

func TestDiscordLinksUseConversationWorkflowAfterAuthorization(t *testing.T) {
	event := Event{Source: SourceDiscord, Type: "message.created", Payload: map[string]any{"content": "Read https://example.com/news and https://example.com/law#art140"}, Context: map[string]any{"gateway_inbound": true}}
	route, err := NewRuleBasedEventRouter().Route(context.Background(), event)
	if err != nil || route.Workflow != "link-conversation" {
		t.Fatalf("%+v %v", route, err)
	}
	workflow, err := NewDefaultWorkflowEngine().Plan(context.Background(), event, route)
	if err != nil || workflow.Steps[0].Action != "authorize-inbound" || workflow.Steps[len(workflow.Steps)-1].Action != chat.ActionConverseLinks {
		t.Fatalf("%+v %v", workflow, err)
	}
	event.Source = SourceWhatsApp
	route, err = NewRuleBasedEventRouter().Route(context.Background(), event)
	if err != nil || route.Workflow == "link-conversation" {
		t.Fatal("changed WhatsApp behavior")
	}
}
