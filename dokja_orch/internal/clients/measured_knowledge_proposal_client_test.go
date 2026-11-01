package clients

import (
	"encoding/json"
	"net/http"
	knowledge "read_books/dokja_domain/dokja_knowledge"
	"strings"
	"testing"
)

func TestMeasuredKnowledgeUsesAcceptanceBudgetAndRetainsRejectedUsage(t *testing.T) {
	for _, status := range []string{"ok", "over_budget", "usage_unavailable"} {
		t.Run(status, func(t *testing.T) {
			calls := 0
			chat := inferenceSocket(t, func(w http.ResponseWriter, r *http.Request) {
				calls++
				if r.URL.Path != "/chat/measured" {
					t.Error("unmeasured path used", r.URL.Path)
				}
				var request struct {
					Budget   map[string]int64    `json:"budget"`
					Messages []map[string]string `json:"messages"`
				}
				if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
					t.Error(err)
				}
				if request.Budget["max_input_tokens"] != 12000 || request.Budget["max_output_tokens"] != knowledge.ProposalMaxOutputTokens || len(request.Messages) != 2 {
					t.Error("budget lost")
				}
				input := any(100)
				if status == "over_budget" {
					input = 12001
				}
				if status == "usage_unavailable" {
					input = nil
				}
				json.NewEncoder(w).Encode(map[string]any{"status": status, "reply": "private proposal", "provider": "fixture", "requested_model": "fixture-model", "budget_mode": "post_generation", "usage": map[string]any{"input_tokens": input, "output_tokens": 10}})
			})
			reply, err := NewMeasuredKnowledgeProposalClient(chat).Propose(t.Context(), knowledge.ProposalPrompt{Instructions: "instructions", Data: "data"})
			if calls != 1 || reply.BudgetMode != "post_generation" {
				t.Fatal(reply, err, calls)
			}
			if status == "ok" {
				if err != nil || reply.Body != "private proposal" || reply.InputTokens == nil || *reply.InputTokens != 100 {
					t.Fatal(reply, err)
				}
				return
			}
			if err == nil || reply.Body != "" || reply.BudgetStatus != status {
				t.Fatal("unaccepted body published", reply, err)
			}
			if status == "over_budget" && (reply.InputTokens == nil || *reply.InputTokens != 12001) {
				t.Fatal("rejected usage lost")
			}
		})
	}
}
func TestMeasuredKnowledgeIndependentlyRejectsOverBudgetOK(t *testing.T) {
	chat := inferenceSocket(t, func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{"status": "ok", "reply": "private proposal", "provider": "fixture", "requested_model": "fixture-model", "budget_mode": "post_generation", "usage": map[string]any{"input_tokens": 12001, "output_tokens": 10}})
	})
	reply, err := NewMeasuredKnowledgeProposalClient(chat).Propose(t.Context(), knowledge.ProposalPrompt{Instructions: "instructions", Data: "data"})
	if err == nil || reply.Body != "" || reply.BudgetStatus != "over_budget" {
		t.Fatal(reply, err)
	}
}

func TestMeasuredReviewWidensItsBoundsOnlyForReview(t *testing.T) {
	calls, inputBudget, budget, input, output := 0, int64(0), int64(0), int64(15000), int64(6000)
	chat := inferenceSocket(t, func(w http.ResponseWriter, r *http.Request) {
		calls++
		var request struct {
			Budget map[string]int64 `json:"budget"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Error(err)
		}
		inputBudget, budget = request.Budget["max_input_tokens"], request.Budget["max_output_tokens"]
		json.NewEncoder(w).Encode(map[string]any{"status": "ok", "reply": "private review", "provider": "fixture", "requested_model": "fixture-model", "budget_mode": "post_generation", "usage": map[string]any{"input_tokens": input, "output_tokens": output}})
	})
	client := NewMeasuredKnowledgeProposalClient(chat)
	prompt := knowledge.ProposalPrompt{Instructions: "instructions", Data: strings.Repeat("x", knowledge.MaxProposalPromptBytes)}
	if _, err := client.Propose(t.Context(), prompt); err == nil || calls != 0 {
		t.Fatal("proposal prompt bound widened", err, calls)
	}
	if reply, err := client.Review(t.Context(), prompt); err != nil || calls != 1 || inputBudget != knowledge.ReviewMaxInputTokens || budget != knowledge.ReviewMaxOutputTokens || reply.Body != "private review" {
		t.Fatal(reply, err, calls, budget)
	}
	if reply, err := client.Propose(t.Context(), knowledge.ProposalPrompt{Instructions: "instructions", Data: "data"}); err == nil || inputBudget != knowledge.ProposalMaxInputTokens || budget != knowledge.ProposalMaxOutputTokens || reply.Body != "" {
		t.Fatal("proposal token ceilings widened", reply, err, inputBudget, budget)
	}
	input, output = knowledge.ReviewMaxInputTokens+1, 10
	if reply, err := client.Review(t.Context(), prompt); err == nil || reply.BudgetStatus != "over_budget" {
		t.Fatal("review input ceiling ignored", reply, err)
	}
	input, output = 100, knowledge.ReviewMaxOutputTokens+1
	if reply, err := client.Review(t.Context(), prompt); err == nil || reply.BudgetStatus != "over_budget" {
		t.Fatal("review output ceiling ignored", reply, err)
	}
	prompt.Data = strings.Repeat("x", knowledge.MaxReviewPromptBytes)
	if _, err := client.Review(t.Context(), prompt); err == nil || calls != 4 {
		t.Fatal("review prompt bound ignored", err, calls)
	}
}
