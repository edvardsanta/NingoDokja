package handlers

import (
	"context"
	"fmt"
	knowledge "read_books/dokja_domain/dokja_knowledge"
	"read_books/internal/core"
	"testing"
)

func TestMeasuredProposalKeepsUsageAndNeverFallsBack(t *testing.T) {
	for _, status := range []string{"ok", "over_budget", "usage_unavailable"} {
		t.Run(status, func(t *testing.T) {
			calls := 0
			measured := applicationProposerFunc(func(ctx context.Context, prompt knowledge.ProposalPrompt) (knowledge.ProposalResponse, error) {
				calls++
				reply := fixtureProviderReply(t, prompt)
				input, output := int64(100), int64(10)
				reply.InputTokens = &input
				reply.OutputTokens = &output
				reply.BudgetMode = "post_generation"
				reply.BudgetStatus = status
				if status != "ok" {
					reply.Body = ""
					return reply, fmt.Errorf("private rejection diagnostic")
				}
				return reply, nil
			})
			legacy := applicationProposerFunc(func(context.Context, knowledge.ProposalPrompt) (knowledge.ProposalResponse, error) {
				t.Error("unmeasured fallback invoked")
				return knowledge.ProposalResponse{}, nil
			})
			handler := NewKnowledgeDomainHandler(assessmentStore()).WithApplicationProposer(legacy).WithMeasuredApplicationProposer(measured)
			event := proposalEvent()
			event.Type = "knowledge.application.propose.measured"
			result, err := core.DefaultService(handler).ProcessWithResult(t.Context(), event)
			if err != nil {
				t.Fatal(err)
			}
			output := result.Result["knowledge"].(map[string]any)
			if calls != 1 {
				t.Fatal("measured call count", calls)
			}
			if status == "ok" {
				metadata := output["proposal"].(map[string]any)["inference"].(map[string]any)
				if metadata["input_tokens"] != int64(100) || metadata["budget_mode"] != "post_generation" {
					t.Fatal("usage lost", metadata)
				}
				return
			}
			if output["status"] != "not_proposed" || output["proposal"] != nil || output["inference"].(map[string]any)["input_tokens"] != int64(100) {
				t.Fatal("rejected result leaked or usage lost", output)
			}
			task := output["task"].(core.TaskResult)
			want := "token_budget_exceeded"
			if status == "usage_unavailable" {
				want = "token_usage_unavailable"
			}
			if task.Reason != want || task.Outputs != nil {
				t.Fatal(task)
			}
		})
	}
}
