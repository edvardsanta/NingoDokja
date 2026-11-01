package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	knowledge "read_books/dokja_domain/dokja_knowledge"
	"read_books/internal/core"
	"strings"
	"sync/atomic"
	"testing"
)

type adaptiveStore struct {
	documents map[string]string
	searches  []string
	hits      map[string][]string
}

func (s *adaptiveStore) Dispatch(_ context.Context, event string, payload map[string]any) (map[string]any, error) {
	if event == "knowledge.search" {
		query, _ := payload["query"].(string)
		s.searches = append(s.searches, query)
		hits := []any{}
		for key, ids := range s.hits {
			if strings.Contains(query, key) {
				for _, id := range ids {
					hits = append(hits, map[string]any{"source_id": id, "kind": "note", "relevant": true})
				}
			}
		}
		return map[string]any{"hits": hits}, nil
	}
	if event != "knowledge.list" {
		return nil, fmt.Errorf("unexpected port operation %s", event)
	}
	id, _ := payload["source_id"].(string)
	return map[string]any{"documents": []any{map[string]any{"source_id": id, "source_ref": "file:" + id, "title": id, "text": s.documents[id]}}}, nil
}

func adaptiveEvent() core.Event {
	return core.Event{EventID: "adaptive", Source: core.SourceCLI, Type: "knowledge.application.propose.adaptive", User: core.EventUser{ID: "test"}, Channel: core.EventChannel{ID: "test"}, Payload: map[string]any{"contract": knowledge.AdaptiveContract, "goal": "Retry the guide"}}
}

func newAdaptiveStore() *adaptiveStore {
	return &adaptiveStore{
		documents: map[string]string{"guide": "Use bounded backoff.", "api": "Reuse a stable key."},
		hits:      map[string][]string{"guide": {"guide"}, "key": {"api"}},
	}
}

type adaptivePrompt struct {
	TaskID      string `json:"task_id"`
	Revision    int    `json:"revision"`
	Application struct {
		Sources []map[string]any `json:"sources"`
	} `json:"application"`
}

func adaptiveReply(t *testing.T, prompt knowledge.ProposalPrompt, proposal *knowledge.QuotedProposal, disposition, summary string) knowledge.ProposalResponse {
	t.Helper()
	var input adaptivePrompt
	if err := json.Unmarshal([]byte(prompt.Data), &input); err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(knowledge.ProviderProposal{Contract: knowledge.ProposalContract, TaskID: input.TaskID, Revision: input.Revision, Disposition: disposition, Summary: summary, Proposal: proposal})
	if err != nil {
		t.Fatal(err)
	}
	in, out := int64(100), int64(10)
	return knowledge.ProposalResponse{Body: string(raw), Provider: "fixture", RequestedModel: "fixture-model", InputTokens: &in, OutputTokens: &out, BudgetMode: "post_generation", BudgetStatus: "ok"}
}

func guideProposal(t *testing.T, prompt knowledge.ProposalPrompt, uncited bool) *knowledge.QuotedProposal {
	t.Helper()
	var input adaptivePrompt
	if err := json.Unmarshal([]byte(prompt.Data), &input); err != nil {
		t.Fatal(err)
	}
	source := input.Application.Sources[0]
	quote := knowledge.QuotedCitation{SourceID: source["source_id"].(string), TextVersion: source["text_version"].(string), Quote: "Use bounded backoff.", Relation: "supports"}
	claim := knowledge.QuotedClaim{ID: "claim", Statement: "The stable key exists", Citations: []knowledge.QuotedCitation{quote}}
	if uncited {
		claim.Citations = []knowledge.QuotedCitation{}
	}
	return &knowledge.QuotedProposal{Claims: []knowledge.QuotedClaim{claim}, Method: knowledge.QuotedMethod{ID: "method", Description: "Back off", Prerequisites: []string{"claim"}, Citations: []knowledge.QuotedCitation{quote}}}
}

func runAdaptive(t *testing.T, ctx context.Context, store *adaptiveStore, proposer applicationProposerFunc) map[string]any {
	t.Helper()
	result, err := core.DefaultService(NewKnowledgeDomainHandler(store).WithMeasuredApplicationProposer(proposer)).ProcessWithResult(ctx, adaptiveEvent())
	if err != nil {
		t.Fatal(err)
	}
	return result.Result["knowledge"].(map[string]any)
}

func TestAdaptiveRetrievesOnceForAGapThenRevises(t *testing.T) {
	store := newAdaptiveStore()
	var calls atomic.Int32
	output := runAdaptive(t, t.Context(), store, func(_ context.Context, prompt knowledge.ProposalPrompt) (knowledge.ProposalResponse, error) {
		if calls.Add(1) == 1 {
			if strings.Contains(prompt.Data, "stable key.") {
				t.Error("the second document arrived before it was retrieved")
			}
			return adaptiveReply(t, prompt, guideProposal(t, prompt, true), "propose", "Needs the key"), nil
		}
		if !strings.Contains(prompt.Data, "Reuse a stable key.") || !strings.Contains(prompt.Data, `"revision":2`) {
			t.Error("revision lost the retrieved document or its binding")
		}
		return adaptiveReply(t, prompt, guideProposal(t, prompt, false), "propose", "Supported"), nil
	})
	proposal := output["proposal"].(map[string]any)
	if output["status"] != "proposed" || calls.Load() != 2 || proposal["revision"] != 2 || proposal["stop_reason"] != "revised" || proposal["strategy"] != "adaptive" {
		t.Fatal(output)
	}
	if strings.Join(store.searches, "|") != "Retry the guide|The stable key exists" {
		t.Fatal(store.searches)
	}
	inference := proposal["inference"].(map[string]any)
	if inference["calls"] != 2 || inference["input_tokens"] != int64(200) || inference["provider"] != "fixture" {
		t.Fatal(inference)
	}
	if assessment := proposal["assessment"].(map[string]any); assessment["applicability"] != "conditional" {
		t.Fatal(assessment)
	}
	if task := output["task"].(core.TaskResult); len(task.Evidence) != 2 {
		t.Fatal(task.Evidence)
	}
}

func TestAdaptiveStopsWithoutASecondCallWhenNoGapRemains(t *testing.T) {
	cases := map[string]struct {
		disposition, summary, reason string
		uncited                      bool
	}{
		"resolved":    {"propose", "Supported", "resolved", false},
		"abstained":   {"abstain", "No", "resolved", false},
		"exhausted":   {"clarify", "Nothing matches this", "no_new_evidence", false},
		"no evidence": {"propose", "The guide again", "no_new_evidence", true},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			var calls atomic.Int32
			output := runAdaptive(t, t.Context(), newAdaptiveStore(), func(_ context.Context, prompt knowledge.ProposalPrompt) (knowledge.ProposalResponse, error) {
				calls.Add(1)
				var proposal *knowledge.QuotedProposal
				if c.disposition == "propose" {
					proposal = guideProposal(t, prompt, c.uncited)
					if c.uncited {
						proposal.Claims[0].Statement = "The guide again"
					}
				}
				return adaptiveReply(t, prompt, proposal, c.disposition, c.summary), nil
			})
			proposal := output["proposal"].(map[string]any)
			if output["status"] != "proposed" || calls.Load() != 1 || proposal["stop_reason"] != c.reason || proposal["revision"] != 1 {
				t.Fatal(calls.Load(), output)
			}
		})
	}
}

func TestAdaptiveNeverCallsAProviderWithoutEvidence(t *testing.T) {
	store := newAdaptiveStore()
	store.hits = nil
	output := runAdaptive(t, t.Context(), store, func(context.Context, knowledge.ProposalPrompt) (knowledge.ProposalResponse, error) {
		t.Error("provider called without evidence")
		return knowledge.ProposalResponse{}, nil
	})
	if output["status"] != "not_proposed" || output["task"].(core.TaskResult).Reason != "no_relevant_sources" {
		t.Fatal(output)
	}
}

func TestAdaptiveKeepsTheFirstProposalWhenTheRevisionFails(t *testing.T) {
	var calls atomic.Int32
	output := runAdaptive(t, t.Context(), newAdaptiveStore(), func(_ context.Context, prompt knowledge.ProposalPrompt) (knowledge.ProposalResponse, error) {
		if calls.Add(1) == 1 {
			return adaptiveReply(t, prompt, guideProposal(t, prompt, true), "propose", "Needs the key"), nil
		}
		return knowledge.ProposalResponse{Provider: "fixture", RequestedModel: "fixture-model", BudgetStatus: "provider_error"}, fmt.Errorf("private diagnostic")
	})
	proposal := output["proposal"].(map[string]any)
	inference := proposal["inference"].(map[string]any)
	if output["status"] != "proposed" || proposal["revision"] != 1 || proposal["stop_reason"] != "revision_failed" || inference["calls"] != 2 || inference["input_tokens"] != nil {
		t.Fatal(output)
	}
	second := proposal["rounds"].([]any)[1].(map[string]any)
	if second["failed"] != true || strings.Contains(fmt.Sprint(output), "private diagnostic") {
		t.Fatal(second)
	}
}

func TestAdaptiveDoesNotStartARevisionItCannotFinish(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), adaptiveRevisionFloor-1e9)
	defer cancel()
	var calls atomic.Int32
	output := runAdaptive(t, ctx, newAdaptiveStore(), func(_ context.Context, prompt knowledge.ProposalPrompt) (knowledge.ProposalResponse, error) {
		calls.Add(1)
		return adaptiveReply(t, prompt, guideProposal(t, prompt, true), "propose", "Needs the key"), nil
	})
	proposal := output["proposal"].(map[string]any)
	if calls.Load() != 1 || proposal["stop_reason"] != "time_exhausted" {
		t.Fatal(calls.Load(), output)
	}
}

func TestAdaptiveFirstFailureStopsAndStaysCounted(t *testing.T) {
	output := runAdaptive(t, t.Context(), newAdaptiveStore(), func(context.Context, knowledge.ProposalPrompt) (knowledge.ProposalResponse, error) {
		return knowledge.ProposalResponse{Provider: "fixture", RequestedModel: "fixture-model", BudgetStatus: "over_budget"}, fmt.Errorf("private diagnostic")
	})
	if output["status"] != "not_proposed" || output["proposal"] != nil || output["inference"].(map[string]any)["calls"] != 1 || output["task"].(core.TaskResult).Reason != "token_budget_exceeded" {
		t.Fatal(output)
	}
}

func TestAdaptiveHasNoUnmeasuredFallback(t *testing.T) {
	legacy := applicationProposerFunc(func(context.Context, knowledge.ProposalPrompt) (knowledge.ProposalResponse, error) {
		t.Error("unmeasured proposer used")
		return knowledge.ProposalResponse{}, nil
	})
	if _, err := core.DefaultService(NewKnowledgeDomainHandler(newAdaptiveStore()).WithApplicationProposer(legacy)).ProcessWithResult(t.Context(), adaptiveEvent()); err == nil {
		t.Fatal("ran without a measured proposer")
	}
}
