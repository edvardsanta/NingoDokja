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
	if err != nil || route.Workflow != "link-conversation" {
		t.Fatal("WhatsApp links did not reach the reading workflow")
	}
}

func TestWhatsAppLinkRoutingKeepsAuthorizationAndIncomingDocuments(t *testing.T) {
	for _, tc := range []struct {
		name, content, want string
		document            *chat.IncomingDocument
	}{
		{name: "news link", content: "Read https://example.com/news", want: "link-conversation"},
		{name: "multiple links", content: "https://example.com/a https://example.com/b", want: "link-conversation"},
		{name: "standalone link retains post delivery first", content: "https://example.com/post", want: "incoming-content"},
		{name: "attachment with link", content: "Read https://example.com/news", document: &chat.IncomingDocument{Filename: "memo.pdf"}, want: "incoming-content"},
		{name: "ordinary message", content: "Hello", want: "conversation"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			event := Event{Type: "message.created", Source: SourceWhatsApp, Payload: map[string]any{"content": tc.content}, Context: map[string]any{"gateway_inbound": true}}
			if tc.document != nil {
				event.Payload["document"] = tc.document
			}
			route, err := NewRuleBasedEventRouter().Route(t.Context(), event)
			if err != nil || route.Workflow != tc.want {
				t.Fatal(route, err)
			}
			workflow, err := NewDefaultWorkflowEngine().Plan(t.Context(), event, route)
			if err != nil || workflow.Steps[0].Action != "authorize-inbound" {
				t.Fatal(workflow, err)
			}
		})
	}
}
