package core

import "testing"

func TestMacroContextOperationsAreInternal(t *testing.T) {
	router := NewRuleBasedEventRouter()
	for _, source := range []EventSource{SourceDiscord, SourceDesktop} {
		if _, err := router.Route(t.Context(), Event{Source: source, Type: "knowledge.context.recall"}); err == nil {
			t.Fatal("external context operation allowed", source)
		}
	}
	event := Event{Source: SourceOrchestrator, Type: "knowledge.context.recall"}
	route, err := router.Route(t.Context(), event)
	if err != nil {
		t.Fatal(err)
	}
	workflow, err := NewDefaultWorkflowEngine().Plan(t.Context(), event, route)
	if err != nil || len(workflow.Steps) != 1 || workflow.Steps[0].Action != "recall-context" {
		t.Fatal(workflow, err)
	}
}
