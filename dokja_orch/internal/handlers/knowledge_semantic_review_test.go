package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	knowledge "read_books/dokja_domain/dokja_knowledge"
	"read_books/internal/core"
	"strings"
	"testing"
	"time"
)

type applicationReviewerFunc func(context.Context, knowledge.ProposalPrompt) (knowledge.ProposalResponse, error)

func (f applicationReviewerFunc) Review(ctx context.Context, prompt knowledge.ProposalPrompt) (knowledge.ProposalResponse, error) {
	return f(ctx, prompt)
}

func reviewEvent(t *testing.T, store *applicationStore) core.Event {
	t.Helper()
	event := assessmentEvent(t, store)
	event.Type = "knowledge.application.review"
	event.Payload = map[string]any{"contract": knowledge.ReviewRequestContract, "assessment": event.Payload, "proposed_by": map[string]any{"provider": "fixture", "requested_model": "fixture-model"}}
	return event
}

func fixtureReviewReply(t *testing.T, prompt knowledge.ProposalPrompt, verdict string) knowledge.ProposalResponse {
	t.Helper()
	var input struct {
		TaskID string `json:"task_id"`
	}
	if err := json.Unmarshal([]byte(prompt.Data), &input); err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(knowledge.ProviderReview{
		Contract: knowledge.ReviewContract, TaskID: input.TaskID, Revision: 1,
		RequiredConditions: []knowledge.ReviewCondition{{ID: "key", Statement: "The key is stable", CoveredBy: []string{"idempotency"}}},
		Claims:             []knowledge.ReviewFinding{{ID: "idempotency", Verdict: "supported", Reason: "stated"}},
		Method:             knowledge.ReviewFinding{ID: "retry", Verdict: "supported", Reason: "stated"},
		Conflicts:          []knowledge.ReviewConflict{}, Verdict: verdict, Reason: "fixture",
	})
	if err != nil {
		t.Fatal(err)
	}
	inputTokens, outputTokens := int64(100), int64(10)
	return knowledge.ProposalResponse{Body: string(raw), Provider: "fixture", RequestedModel: "fixture-model", InputTokens: &inputTokens, OutputTokens: &outputTokens, BudgetMode: "post_generation", BudgetStatus: "ok"}
}

func processReview(t *testing.T, ctx context.Context, store *applicationStore, reviewer knowledge.ApplicationReviewer, event core.Event) map[string]any {
	t.Helper()
	result, err := core.DefaultService(NewKnowledgeDomainHandler(store).WithApplicationReviewer(reviewer)).ProcessWithResult(ctx, event)
	if err != nil {
		t.Fatal(err)
	}
	return result.Result["knowledge"].(map[string]any)
}

func TestReviewRunsOneMeasuredCallThroughApplication(t *testing.T) {
	store := assessmentStore()
	calls := 0
	output := processReview(t, t.Context(), store, applicationReviewerFunc(func(_ context.Context, prompt knowledge.ProposalPrompt) (knowledge.ProposalResponse, error) {
		calls++
		if !strings.Contains(prompt.Data, "Reuse a stable key.") || !strings.Contains(prompt.Data, `"candidate"`) {
			t.Error("review lost its sources or candidate")
		}
		return fixtureReviewReply(t, prompt, "accepted"), nil
	}), reviewEvent(t, store))
	if output["status"] != "reviewed" || calls != 1 {
		t.Fatal(output, calls)
	}
	review := output["review"].(map[string]any)
	inference := review["inference"].(map[string]any)
	if review["outcome"] != "accepted" || review["independence"] != "same_model" || review["can_execute"] != false || inference["calls"] != 1 || inference["input_tokens"] != int64(100) {
		t.Fatal(review)
	}
}

func TestReviewFailuresStayCountedAndPublishNothing(t *testing.T) {
	cases := []struct {
		name, status, reason string
		reply                func(*testing.T, knowledge.ProposalPrompt) (knowledge.ProposalResponse, error)
	}{
		{"provider", "provider_error", "provider_unavailable", func(*testing.T, knowledge.ProposalPrompt) (knowledge.ProposalResponse, error) {
			return knowledge.ProposalResponse{Provider: "fixture", RequestedModel: "fixture-model", BudgetStatus: "provider_error"}, fmt.Errorf("private diagnostic")
		}},
		{"over budget", "over_budget", "token_budget_exceeded", func(*testing.T, knowledge.ProposalPrompt) (knowledge.ProposalResponse, error) {
			input := int64(12001)
			return knowledge.ProposalResponse{Provider: "fixture", RequestedModel: "fixture-model", InputTokens: &input, BudgetStatus: "over_budget"}, fmt.Errorf("private diagnostic")
		}},
		{"invalid reply", "ok", "invalid_provider_review", func(t *testing.T, prompt knowledge.ProposalPrompt) (knowledge.ProposalResponse, error) {
			reply := fixtureReviewReply(t, prompt, "certainly")
			return reply, nil
		}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			store := assessmentStore()
			calls := 0
			output := processReview(t, t.Context(), store, applicationReviewerFunc(func(_ context.Context, prompt knowledge.ProposalPrompt) (knowledge.ProposalResponse, error) {
				calls++
				return c.reply(t, prompt)
			}), reviewEvent(t, store))
			task := output["task"].(core.TaskResult)
			if output["status"] != "not_reviewed" || output["review"] != nil || calls != 1 || task.Reason != c.reason {
				t.Fatal(output, calls)
			}
			if output["inference"].(map[string]any)["calls"] != 1 || strings.Contains(fmt.Sprint(output), "private diagnostic") {
				t.Fatal(output)
			}
		})
	}
}

func TestReviewIsNotCalledWhenTheCandidateFailsIntegrity(t *testing.T) {
	store := assessmentStore()
	event := reviewEvent(t, store)
	store.documents["api"] = "API guarantee changed."
	output := processReview(t, t.Context(), store, applicationReviewerFunc(func(context.Context, knowledge.ProposalPrompt) (knowledge.ProposalResponse, error) {
		t.Error("reviewer called for a changed source")
		return knowledge.ProposalResponse{}, nil
	}), event)
	if output["status"] != "not_reviewed" {
		t.Fatal(output)
	}
}

func TestReviewNeedsAConfiguredReviewer(t *testing.T) {
	store := assessmentStore()
	if _, err := core.DefaultService(NewKnowledgeDomainHandler(store)).ProcessWithResult(t.Context(), reviewEvent(t, store)); err == nil {
		t.Fatal("review ran without a reviewer")
	}
}

func TestReviewLateReplyCannotPublish(t *testing.T) {
	store := assessmentStore()
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Millisecond)
	defer cancel()
	output := processReview(t, ctx, store, applicationReviewerFunc(func(ctx context.Context, prompt knowledge.ProposalPrompt) (knowledge.ProposalResponse, error) {
		<-ctx.Done()
		return fixtureReviewReply(t, prompt, "accepted"), nil
	}), reviewEvent(t, store))
	if output["status"] != "not_reviewed" || output["review"] != nil {
		t.Fatal(output)
	}
}

func TestReviewStillHonorsKnowledgeControl(t *testing.T) {
	calls := 0
	reviewer := applicationReviewerFunc(func(context.Context, knowledge.ProposalPrompt) (knowledge.ProposalResponse, error) {
		calls++
		return knowledge.ProposalResponse{}, nil
	})
	controls, err := core.NewControls("")
	if err != nil {
		t.Fatal(err)
	}
	if err = controls.SetService("knowledge", false); err != nil {
		t.Fatal(err)
	}
	store := assessmentStore()
	event := reviewEvent(t, store)
	result, err := core.DefaultService(NewKnowledgeDomainHandler(store).WithApplicationReviewer(reviewer)).WithControls(controls).ProcessWithResult(t.Context(), event)
	if err != nil || result.Result["skipped"] != true || calls != 0 || len(store.calls) != 0 {
		t.Fatalf("disabled capability ran: %v %v", result, err)
	}
}
