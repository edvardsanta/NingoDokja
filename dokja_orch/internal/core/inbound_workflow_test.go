package core

import (
	"context"
	"testing"
)

type inboundStepHandler struct {
	domain Domain
	calls  int
	deny   bool
}

func (h *inboundStepHandler) Domain() Domain { return h.domain }
func (h *inboundStepHandler) Handle(_ context.Context, _ Event, step WorkflowStep) (map[string]any, error) {
	h.calls++
	if h.domain == DomainCommunications && step.Action != "authorize-inbound" {
		return nil, ErrInboundDenied
	}
	if h.deny {
		return nil, ErrInboundDenied
	}
	return nil, nil
}

func TestBridgeInboundDenialStopsBeforeModerationAndChat(t *testing.T) {
	communications := &inboundStepHandler{domain: DomainCommunications, deny: true}
	moderation := &inboundStepHandler{domain: DomainModeration}
	chat := &inboundStepHandler{domain: DomainChat}
	service := DefaultService(communications, moderation, chat)
	result, err := service.ProcessWithResult(context.Background(), Event{
		Source: SourceDiscord, Type: "message.created", Context: map[string]any{"gateway_inbound": true},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Result["skipped"] != true || result.Result["silent"] != true || communications.calls != 1 || moderation.calls != 0 || chat.calls != 0 {
		t.Fatalf("denied event reached another domain: result=%#v calls=%d,%d,%d", result.Result, communications.calls, moderation.calls, chat.calls)
	}
}

func TestBridgeInboundApprovalRunsTheExistingWorkflow(t *testing.T) {
	communications := &inboundStepHandler{domain: DomainCommunications}
	moderation := &inboundStepHandler{domain: DomainModeration}
	chat := &inboundStepHandler{domain: DomainChat}
	service := DefaultService(communications, moderation, chat)
	_, err := service.ProcessWithResult(context.Background(), Event{
		Source: SourceWhatsApp, Type: "message.created", Context: map[string]any{"gateway_inbound": true},
	})
	if err != nil || communications.calls != 1 || moderation.calls != 1 || chat.calls != 1 {
		t.Fatalf("approved workflow did not run: err=%v calls=%d,%d,%d", err, communications.calls, moderation.calls, chat.calls)
	}
}

func TestBridgeCommandRoutesThroughCommunicationsFirst(t *testing.T) {
	route, err := NewRuleBasedEventRouter().Route(context.Background(), Event{
		Source: SourceDiscord, Type: "meme.fetch", Context: map[string]any{"gateway_inbound": true},
	})
	if err != nil || len(route.Domains) != 2 || route.Domains[0] != DomainCommunications || route.Domains[1] != DomainMeme {
		t.Fatalf("command bypassed inbound authorization: route=%#v err=%v", route, err)
	}
}

func TestOnlyTheInboundAuthorizationStepIsRewritten(t *testing.T) {
	plan := func(eventType string) []string {
		event := Event{Source: SourceDiscord, Type: eventType, Context: map[string]any{"gateway_inbound": true}}
		route, err := NewRuleBasedEventRouter().Route(context.Background(), event)
		if err != nil {
			t.Fatal(err)
		}
		workflow, err := NewDefaultWorkflowEngine().Plan(context.Background(), event, route)
		if err != nil {
			t.Fatal(err)
		}
		actions := make([]string, len(workflow.Steps))
		for i, step := range workflow.Steps {
			actions[i] = string(step.Domain) + ":" + step.Action
		}
		return actions
	}
	if got := plan("music.play"); len(got) != 2 || got[0] != "communications:authorize-inbound" || got[1] != "communications:play-music" {
		t.Fatalf("a music command must be authorized, then played: %v", got)
	}
	if got := plan("message.created"); len(got) != 3 || got[0] != "communications:authorize-inbound" || got[1] != "moderation:screen-event" {
		t.Fatalf("a message keeps its authorization first: %v", got)
	}
	// Without the bridge's mark there is nothing to authorize: the action runs as planned.
	route, _ := NewRuleBasedEventRouter().Route(context.Background(), Event{Type: "music.skip"})
	workflow, _ := NewDefaultWorkflowEngine().Plan(context.Background(), Event{Type: "music.skip"}, route)
	if len(workflow.Steps) != 1 || workflow.Steps[0].Action != "skip-music" {
		t.Fatalf("%v", workflow.Steps)
	}
}

func TestADeniedMusicCommandNeverReachesTheQueue(t *testing.T) {
	communications := &inboundStepHandler{domain: DomainCommunications, deny: true}
	service := DefaultService(communications)
	result, err := service.ProcessWithResult(context.Background(), Event{
		Source: SourceDiscord, Type: "music.play", Context: map[string]any{"gateway_inbound": true},
	})
	if err != nil || result.Result["silent"] != true || communications.calls != 1 {
		t.Fatalf("denied music command ran: result=%#v calls=%d err=%v", result.Result, communications.calls, err)
	}
}
