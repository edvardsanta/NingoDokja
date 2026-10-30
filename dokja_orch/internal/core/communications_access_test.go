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
	for eventType, domain := range map[string]Domain{
		"communications.channels":    DomainCommunications,
		"communications.history":     DomainCommunications,
		"communications.send":        DomainSystem,
		"communications.link.status": DomainSystem,
		"communications.link.start":  DomainSystem,
		"communications.link.reset":  DomainSystem,
		"music.play":                 DomainCommunications,
		"music.skip":                 DomainCommunications,
		"music.pause":                DomainCommunications,
		"music.resume":               DomainCommunications,
		"music.stop":                 DomainCommunications,
		"music.queue":                DomainCommunications,
		"music.volume":               DomainCommunications,
		"music.ended":                DomainCommunications,
		"music.comment":              DomainCommunications,
	} {
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

type systemRecorder struct {
	events []Event
}

func (h *systemRecorder) Domain() Domain { return DomainSystem }
func (h *systemRecorder) Handle(_ context.Context, event Event, step WorkflowStep) (map[string]any, error) {
	h.events = append(h.events, event)
	return map[string]any{"action": step.Action}, nil
}

func TestLinkActionsNeedTheCommunicationsCredentialAndGetTheirOwnAction(t *testing.T) {
	token := strings.Repeat("x", 32)
	for eventType, action := range map[string]string{
		"communications.link.status": "read-interface-link",
		"communications.link.start":  "start-interface-link",
		"communications.link.reset":  "reset-interface-link",
	} {
		denied := &systemRecorder{}
		service := DefaultService(denied).WithCommunicationsToken(token)
		_, err := service.ProcessWithResult(context.Background(), Event{
			Source: SourceCLI, Type: eventType, Payload: map[string]any{"interface": "whatsapp"},
			Context: map[string]any{"communications_token": "wrong"},
		})
		if err == nil || len(denied.events) != 0 {
			t.Fatalf("%s must be refused without the credential, err=%v", eventType, err)
		}

		allowed := &systemRecorder{}
		service = DefaultService(allowed).WithCommunicationsToken(token)
		result, err := service.ProcessWithResult(context.Background(), Event{
			Source: EventSource("desktop"), Type: eventType, Payload: map[string]any{"interface": "whatsapp"},
			Context: map[string]any{"communications_token": token},
		})
		if err != nil || len(allowed.events) != 1 {
			t.Fatalf("%s must reach the system handler, err=%v", eventType, err)
		}
		raw, _ := json.Marshal(result)
		if strings.Contains(string(raw), token) || allowed.events[0].Context["communications_token"] != nil {
			t.Fatal("credential leaked")
		}
		if got := result.Result["system"].(map[string]any)["action"]; got != action {
			t.Fatalf("%s got action %v, want %s", eventType, got, action)
		}
	}
}
