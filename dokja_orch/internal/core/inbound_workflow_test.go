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
