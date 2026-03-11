package core

import (
	"context"
	"testing"
)

func TestRuleBasedEventRouterRoutesScheduledMemeDispatch(t *testing.T) {
	router := NewRuleBasedEventRouter()

	route, err := router.Route(context.Background(), Event{
		Type: "meme.dispatch.scheduled",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if route.Workflow != "meme-dispatch" {
		t.Fatalf("expected workflow meme-dispatch, got %q", route.Workflow)
	}
	if len(route.Domains) != 1 || route.Domains[0] != DomainSystem {
		t.Fatalf("expected system domain route, got %#v", route.Domains)
	}
}

func TestDefaultWorkflowEnginePlansScheduledMemeDispatch(t *testing.T) {
	engine := NewDefaultWorkflowEngine()

	workflow, err := engine.Plan(context.Background(), Event{
		Type: "meme.dispatch.scheduled",
	}, Route{
		Workflow: "meme-dispatch",
		Domains:  []Domain{DomainSystem},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if workflow.Name != "meme-dispatch" {
		t.Fatalf("expected workflow name meme-dispatch, got %q", workflow.Name)
	}
	if len(workflow.Steps) != 1 {
		t.Fatalf("expected one workflow step, got %d", len(workflow.Steps))
	}
	if workflow.Steps[0].Action != "deliver-scheduled-memes" {
		t.Fatalf("expected deliver-scheduled-memes action, got %q", workflow.Steps[0].Action)
	}
}
