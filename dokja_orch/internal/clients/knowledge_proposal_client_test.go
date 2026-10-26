package clients

import (
	"context"
	"encoding/json"
	"net/http"
	knowledge "read_books/dokja_domain/dokja_knowledge"
	"strings"
	"testing"
)

func TestKnowledgeProposalUsesRuntimeSocketWithOneRequest(t *testing.T) {
	calls := 0
	chat := inferenceSocket(t, func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.URL.Path != "/chat" {
			t.Error(r.URL.Path)
		}
		var request struct {
			Messages []map[string]string `json:"messages"`
		}
		if json.NewDecoder(r.Body).Decode(&request) != nil || len(request.Messages) != 2 {
			t.Error("invalid message roles")
		}
		if request.Messages[0]["role"] != "system" || request.Messages[1]["role"] != "user" || request.Messages[1]["content"] != "untrusted data" {
			t.Error("data boundary lost")
		}
		json.NewEncoder(w).Encode(map[string]any{"reply": "{}", "provider": "fixture", "requested_model": "fixture-model"})
	})
	response, err := NewKnowledgeProposalClient(chat).Propose(t.Context(), knowledge.ProposalPrompt{Instructions: "domain instructions", Data: "untrusted data"})
	if err != nil || response.Body != "{}" || response.RequestedModel != "fixture-model" || calls != 1 {
		t.Fatalf("unexpected response: %+v %v calls=%d", response, err, calls)
	}
}
func TestKnowledgeProposalRejectsOversizedMissingAndFailedReplies(t *testing.T) {
	for _, body := range []map[string]any{
		{"reply": strings.Repeat("x", knowledge.MaxProposalReplyBytes+1), "provider": "fixture", "requested_model": "fixture-model"},
		{"reply": "{}", "provider": "fixture"},
	} {
		chat := inferenceSocket(t, func(w http.ResponseWriter, r *http.Request) { json.NewEncoder(w).Encode(body) })
		if _, err := NewKnowledgeProposalClient(chat).Propose(t.Context(), knowledge.ProposalPrompt{Instructions: "instructions", Data: "data"}); err == nil {
			t.Fatal("accepted invalid transport reply")
		}
	}
	chat := inferenceSocket(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(500)
		w.Write([]byte(`{"error":"private diagnostic"}`))
	})
	_, err := NewKnowledgeProposalClient(chat).Propose(t.Context(), knowledge.ProposalPrompt{Instructions: "instructions", Data: "data"})
	if err == nil || strings.Contains(err.Error(), "private") {
		t.Fatalf("leaked provider diagnostic: %v", err)
	}
}
func TestKnowledgeProposalRejectsCancelledOrOversizedRequestBeforeCall(t *testing.T) {
	calls := 0
	chat := inferenceSocket(t, func(w http.ResponseWriter, r *http.Request) { calls++ })
	client := NewKnowledgeProposalClient(chat)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := client.Propose(ctx, knowledge.ProposalPrompt{Instructions: "instructions", Data: "data"}); err == nil {
		t.Fatal("accepted cancelled request")
	}
	if _, err := client.Propose(t.Context(), knowledge.ProposalPrompt{Instructions: "instructions", Data: strings.Repeat("x", knowledge.MaxProposalPromptBytes)}); err == nil {
		t.Fatal("accepted oversized request")
	}
	if calls != 0 {
		t.Fatal("invalid request reached Runtime")
	}
}
