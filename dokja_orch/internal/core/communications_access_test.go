package core

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

type communicationsHandler struct {
	called  bool
	context map[string]any
}

func (h *communicationsHandler) Domain() Domain { return DomainCommunications }
func (h *communicationsHandler) Handle(_ context.Context, event Event, _ WorkflowStep) (map[string]any, error) {
	h.called = true
	h.context = event.Context
	return map[string]any{"channels": []any{}}, nil
}
func TestCommunicationsAccessAndCredentialRedaction(t *testing.T) {
	token := strings.Repeat("x", 32)
	for _, configured := range []string{"", "short", token} {
		for _, supplied := range []string{"", "wrong", token} {
			h := &communicationsHandler{}
			s := DefaultService(h).WithCommunicationsToken(configured)
			result, err := s.ProcessWithResult(context.Background(), Event{Source: SourceCLI, Type: " communications.channels ", Context: map[string]any{"communications_token": supplied}})
			valid := configured == token && supplied == token
			if (err == nil) != valid || h.called != valid {
				t.Fatalf("unexpected access result configured=%t valid=%t", configured == token, valid)
			}
			raw, _ := json.Marshal(result)
			if strings.Contains(string(raw), token) || h.context["communications_token"] != nil {
				t.Fatal("credential leaked")
			}
		}
	}
}
func TestCommunicationsRoutes(t *testing.T) {
	for eventType, domain := range map[string]Domain{"communications.channels": DomainCommunications, "communications.history": DomainCommunications, "communications.send": DomainSystem} {
		event := Event{Type: eventType}
		route, err := NewRuleBasedEventRouter().Route(context.Background(), event)
		if err != nil || len(route.Domains) != 1 || route.Domains[0] != domain {
			t.Fatal("bad route", eventType)
		}
		workflow, _ := NewDefaultWorkflowEngine().Plan(context.Background(), event, route)
		if workflow.Steps[0].Action == "handle-event" {
			t.Fatal("missing action")
		}
	}
}
