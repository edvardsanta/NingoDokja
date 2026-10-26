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

type applicationProposerFunc func(context.Context, knowledge.ProposalPrompt) (knowledge.ProposalResponse, error)

func (f applicationProposerFunc) Propose(ctx context.Context, prompt knowledge.ProposalPrompt) (knowledge.ProposalResponse, error) {
	return f(ctx, prompt)
}
func fixtureProviderReply(t *testing.T, prompt knowledge.ProposalPrompt) knowledge.ProposalResponse {
	t.Helper()
	var input struct {
		TaskID      string         `json:"task_id"`
		Application map[string]any `json:"application"`
	}
	if err := json.Unmarshal([]byte(prompt.Data), &input); err != nil {
		t.Fatal(err)
	}
	sources := input.Application["sources"].([]any)
	cite := func(i int, quote string) knowledge.QuotedCitation {
		s := sources[i].(map[string]any)
		return knowledge.QuotedCitation{SourceID: s["source_id"].(string), TextVersion: s["text_version"].(string), Quote: quote, Relation: "supports"}
	}
	reply := knowledge.ProviderProposal{Contract: knowledge.ProposalContract, TaskID: input.TaskID, Revision: 1, Disposition: "propose", Summary: "Apply only if the API prerequisite is satisfied", Proposal: &knowledge.QuotedProposal{
		Claims: []knowledge.QuotedClaim{{ID: "idempotent", Statement: "The request satisfies the API prerequisite", Citations: []knowledge.QuotedCitation{cite(1, "Reuse a stable key.")}}},
		Method: knowledge.QuotedMethod{ID: "retry", Description: "Use bounded backoff", Prerequisites: []string{"idempotent"}, Citations: []knowledge.QuotedCitation{cite(0, "Use bounded backoff.")}},
	}}
	raw, err := json.Marshal(reply)
	if err != nil {
		t.Fatal(err)
	}
	return knowledge.ProposalResponse{Body: string(raw), Provider: "fixture", RequestedModel: "fixture-model"}
}
func processProposal(t *testing.T, ctx context.Context, store *applicationStore, proposer knowledge.ApplicationProposer, event core.Event) map[string]any {
	t.Helper()
	result, err := core.DefaultService(NewKnowledgeDomainHandler(store).WithApplicationProposer(proposer)).ProcessWithResult(ctx, event)
	if err != nil {
		t.Fatal(err)
	}
	return result.Result["knowledge"].(map[string]any)
}
func proposalEvent() core.Event {
	e := applicationEvent()
	e.Type = "knowledge.application.propose"
	return e
}
func TestProviderProposalRunsOneBoundedCallThroughApplication(t *testing.T) {
	store := assessmentStore()
	calls := 0
	provider := applicationProposerFunc(func(ctx context.Context, prompt knowledge.ProposalPrompt) (knowledge.ProposalResponse, error) {
		calls++
		if _, ok := ctx.Deadline(); !ok {
			t.Error("missing deadline")
		}
		return fixtureProviderReply(t, prompt), nil
	})
	output := processProposal(t, t.Context(), store, provider, proposalEvent())
	if output["status"] != "proposed" || calls != 1 || strings.Join(store.calls, ",") != "guide,api" {
		t.Fatalf("unexpected path: %v calls=%d", output, calls)
	}
	proposal := output["proposal"].(map[string]any)
	assessment := proposal["assessment"].(map[string]any)
	if proposal["semantic_status"] != "unverified" || proposal["can_execute"] != false || assessment["applicability"] != "conditional" || proposal["inference"].(map[string]any)["input_tokens"] != nil {
		t.Fatalf("overstated result: %v", proposal)
	}
	task := output["task"].(core.TaskResult)
	if task.Outputs != nil || len(task.Steps) != 4 || len(task.Receipts) != 0 || len(task.Proposals) != 0 || task.Scope.ActorID != "test-actor" {
		t.Fatalf("unexpected task: %+v", task)
	}
}
func TestProviderFailureIsCountedWithoutPublishingInvalidOutput(t *testing.T) {
	for _, kind := range []string{"invalid", "unavailable", "clarify"} {
		t.Run(kind, func(t *testing.T) {
			calls := 0
			provider := applicationProposerFunc(func(ctx context.Context, prompt knowledge.ProposalPrompt) (knowledge.ProposalResponse, error) {
				calls++
				reply := fixtureProviderReply(t, prompt)
				switch kind {
				case "invalid":
					reply.Body = `{"private":"invalid output"}`
				case "unavailable":
					return knowledge.ProposalResponse{}, fmt.Errorf("private transport diagnostic")
				case "clarify":
					var p knowledge.ProviderProposal
					json.Unmarshal([]byte(reply.Body), &p)
					p.Disposition = "clarify"
					p.Proposal = nil
					p.Summary = "What is the key age?"
					raw, _ := json.Marshal(p)
					reply.Body = string(raw)
				}
				return reply, nil
			})
			output := processProposal(t, t.Context(), assessmentStore(), provider, proposalEvent())
			if calls != 1 {
				t.Fatal("provider retried")
			}
			if kind == "clarify" {
				if output["status"] != "proposed" || output["proposal"].(map[string]any)["disposition"] != "clarify" {
					t.Fatal(output)
				}
				return
			}
			if output["status"] != "not_proposed" || output["proposal"] != nil || output["inference"].(map[string]any)["calls"] != 1 {
				t.Fatalf("invalid output leaked or attempt lost: %v", output)
			}
			if strings.Contains(fmt.Sprint(output), "private") {
				t.Fatal("provider diagnostics exposed")
			}
		})
	}
}
func TestProviderIsNotCalledForMissingOrOversizedEvidence(t *testing.T) {
	for _, kind := range []string{"missing", "oversized"} {
		t.Run(kind, func(t *testing.T) {
			store := assessmentStore()
			if kind == "missing" {
				delete(store.documents, "api")
			} else {
				store.documents["guide"] = strings.Repeat("x", 16000)
				store.documents["api"] = strings.Repeat("y", 16000)
			}
			calls := 0
			provider := applicationProposerFunc(func(context.Context, knowledge.ProposalPrompt) (knowledge.ProposalResponse, error) {
				calls++
				return knowledge.ProposalResponse{}, nil
			})
			output := processProposal(t, t.Context(), store, provider, proposalEvent())
			if calls != 0 || output["status"] != "not_proposed" || output["proposal"] != nil {
				t.Fatalf("invalid evidence reached provider: %v", output)
			}
		})
	}
}
func TestProviderLateReplyCannotPublishOrRetry(t *testing.T) {
	entered, release, finished := make(chan struct{}), make(chan struct{}), make(chan struct{})
	provider := applicationProposerFunc(func(ctx context.Context, prompt knowledge.ProposalPrompt) (knowledge.ProposalResponse, error) {
		reply := fixtureProviderReply(t, prompt)
		close(entered)
		<-release
		defer close(finished)
		return reply, nil
	})
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	done := make(chan map[string]any, 1)
	go func() { done <- processProposal(t, ctx, assessmentStore(), provider, proposalEvent()) }()
	select {
	case <-entered:
	case <-time.After(2 * time.Second):
		t.Fatal("provider did not start")
	}
	cancel()
	select {
	case output := <-done:
		if output["proposal"] != nil || output["task"].(core.TaskResult).Status != core.TaskCancelled {
			t.Fatal(output)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("task waited for provider")
	}
	close(release)
	select {
	case <-finished:
	case <-time.After(2 * time.Second):
		t.Fatal("provider did not finish")
	}
}
func TestProposalStillHonorsKnowledgeControl(t *testing.T) {
	calls := 0
	provider := applicationProposerFunc(func(context.Context, knowledge.ProposalPrompt) (knowledge.ProposalResponse, error) {
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
	result, err := core.DefaultService(NewKnowledgeDomainHandler(store).WithApplicationProposer(provider)).WithControls(controls).ProcessWithResult(t.Context(), proposalEvent())
	if err != nil || result.Result["skipped"] != true || calls != 0 || len(store.calls) != 0 {
		t.Fatalf("disabled capability ran: %v %v", result, err)
	}
}
