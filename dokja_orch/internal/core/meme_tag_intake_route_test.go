package core

import (
	"context"
	"testing"
)

func TestAMarkedMemeTagPostGoesToIntakeAndNeverToChat(t *testing.T) {
	event := Event{Source: SourceDiscord, Type: "message.created", Context: map[string]any{"gateway_inbound": true, "meme_tag_post": true}}
	route, err := NewRuleBasedEventRouter().Route(context.Background(), event)
	if err != nil || route.Workflow != "meme-tag-intake" || len(route.Domains) != 2 ||
		route.Domains[0] != DomainCommunications || route.Domains[1] != DomainKnowledge {
		t.Fatalf("a tag post is authorized first, then read for the queue: %#v %v", route, err)
	}
	workflow, err := NewDefaultWorkflowEngine().Plan(context.Background(), event, route)
	if err != nil || len(workflow.Steps) != 2 || workflow.Steps[0].Action != "authorize-inbound" || workflow.Steps[1].Action != "propose-meme-tag" {
		t.Fatalf("the workflow: %#v %v", workflow, err)
	}
	for _, step := range workflow.Steps {
		if step.Domain == DomainChat || step.Domain == DomainModeration {
			t.Fatal("a tag post must not reach chat or moderation")
		}
	}
}

func TestAnUnmarkedMessageKeepsItsRoute(t *testing.T) {
	for name, event := range map[string]Event{
		"an ordinary Discord message":  {Source: SourceDiscord, Type: "message.created", Context: map[string]any{"gateway_inbound": true}},
		"the flag set false":           {Source: SourceDiscord, Type: "message.created", Context: map[string]any{"gateway_inbound": true, "meme_tag_post": false}},
		"the flag on another platform": {Source: SourceWhatsApp, Type: "message.created", Context: map[string]any{"gateway_inbound": true, "meme_tag_post": true}},
	} {
		t.Run(name, func(t *testing.T) {
			route, err := NewRuleBasedEventRouter().Route(context.Background(), event)
			if err != nil || route.Workflow == "meme-tag-intake" {
				t.Fatalf("%#v %v", route, err)
			}
		})
	}
}
