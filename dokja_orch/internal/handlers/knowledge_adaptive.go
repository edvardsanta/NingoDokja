package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	knowledge "read_books/dokja_domain/dokja_knowledge"
	"read_books/internal/core"
	"time"
)

const adaptiveDeadline = 90 * time.Second

// adaptiveRevisionFloor is the time a second proposal needs: real ones took up to 34
// seconds. With less left, the run keeps the first proposal instead of losing it.
const adaptiveRevisionFloor = 45 * time.Second
const adaptiveCallMargin = 2 * time.Second

// proposeAdaptively finds its own evidence from the goal, proposes, and retrieves once
// more only when the assessment shows a gap. The orchestrator owns this sequence and
// its budgets; Knowledge owns each decision. Every search is a step with evidence, and
// every provider call is counted in rounds, so nothing about it is hidden.
func (h *KnowledgeDomainHandler) proposeAdaptively(ctx context.Context, event core.Event) (map[string]any, error) {
	request, err := knowledge.ParseAdaptiveRequest(event.Payload)
	if err != nil {
		return nil, err
	}
	if h.measuredProposer == nil {
		return nil, fmt.Errorf("application inference is not configured")
	}
	started := time.Now()
	remaining := func() time.Duration {
		left := adaptiveDeadline - time.Since(started)
		if deadline, ok := ctx.Deadline(); ok && time.Until(deadline) < left {
			return time.Until(deadline)
		}
		return left
	}
	workflow := core.TypedWorkflow{Name: "knowledge-application-propose-adaptive", Version: "1", Goal: request.Goal, Deadline: adaptiveDeadline}
	workflow.Steps = []core.TaskStep{
		{Name: "retrieve-1", Domain: core.DomainKnowledge, Action: "find-application-sources", Run: func(ctx context.Context, input core.StepInput) (core.StepResult, error) {
			step, err := h.retrieveAdaptively(ctx, input, request.Goal, nil, knowledge.AdaptiveInitialDocuments, knowledge.MaxAdaptiveTextBytes)
			if err == nil && len(adaptiveSources(step.Output)) == 0 {
				step.Disposition, step.Reason = core.StepStop, "no_relevant_sources"
			}
			return step, err
		}},
		{Name: "proposal-1", Domain: core.DomainKnowledge, Action: knowledge.ActionProposeAdaptiveApplication, Run: func(ctx context.Context, input core.StepInput) (core.StepResult, error) {
			sources := adaptiveSources(input.Outputs["retrieve-1"])
			return h.adaptiveProposal(ctx, input.TaskID, 1, request, sources, 60*time.Second)
		}},
		{Name: "retrieve-2", Domain: core.DomainKnowledge, Action: "find-application-sources", Run: func(ctx context.Context, input core.StepInput) (core.StepResult, error) {
			query, gap := knowledge.AdaptiveGap(input.Outputs["proposal-1"])
			if !gap {
				return adaptiveStop("resolved"), nil
			}
			if remaining() < adaptiveRevisionFloor {
				return adaptiveStop("time_exhausted"), nil
			}
			first := adaptiveSources(input.Outputs["retrieve-1"])
			loaded, used := map[string]bool{}, 0
			for _, source := range first {
				id, _ := source["source_id"].(string)
				text, _ := source["text"].(string)
				loaded[id] = true
				used += len(text)
			}
			step, err := h.retrieveAdaptively(ctx, input, query, loaded, knowledge.AdaptiveExtraDocuments, knowledge.MaxAdaptiveTextBytes-used)
			if err != nil {
				return step, err
			}
			if len(adaptiveSources(step.Output)) == 0 {
				step.Output["stop_reason"] = "no_new_evidence"
			}
			return step, nil
		}},
		{Name: "proposal-2", Domain: core.DomainKnowledge, Action: knowledge.ActionProposeAdaptiveApplication, Run: func(ctx context.Context, input core.StepInput) (core.StepResult, error) {
			extra := adaptiveSources(input.Outputs["retrieve-2"])
			if len(extra) == 0 {
				return core.StepResult{Disposition: core.StepContinue}, nil
			}
			sources := append(adaptiveSources(input.Outputs["retrieve-1"]), extra...)
			limit := min(60*time.Second, remaining()-adaptiveCallMargin)
			step, err := h.adaptiveProposal(ctx, input.TaskID, 2, request, sources, limit)
			if err != nil || step.Disposition == core.StepStop {
				// The first proposal is still complete, so a failed second attempt only
				// ends the retrieval. Its attempt stays counted in the rounds.
				failure := step.Output
				if failure == nil {
					failure = map[string]any{"inference": map[string]any{"calls": 1, "provider": "", "requested_model": "", "input_tokens": nil, "output_tokens": nil, "outcome": "unknown"}}
				}
				failure["failed"] = true
				return core.StepResult{Disposition: core.StepContinue, Output: failure}, nil
			}
			return step, nil
		}},
		{Name: "result", Domain: core.DomainKnowledge, Action: "settle-adaptive-proposal", Run: func(_ context.Context, input core.StepInput) (core.StepResult, error) {
			return core.StepResult{Disposition: core.StepContinue, Output: settleAdaptive(input.Outputs)}, nil
		}},
	}
	task, err := core.RunTask(ctx, event, workflow, core.TaskLimits{MaxSteps: len(workflow.Steps), MaxEvidence: knowledge.MaxApplicationSources, MaxProposals: 1, MaxBytes: 512 * 1024})
	if err != nil {
		return nil, err
	}
	result := map[string]any{"status": "not_proposed"}
	if task.Status == core.TaskCompleted && task.Reason == "" {
		result["status"] = "proposed"
		result["proposal"] = task.Outputs["result"]
	} else if first := task.Outputs["proposal-1"]; first != nil {
		result["inference"] = first["inference"]
	}
	task.Outputs = nil
	result["task"] = task
	return result, nil
}

func adaptiveStop(reason string) core.StepResult {
	return core.StepResult{Disposition: core.StepContinue, Output: map[string]any{"stop_reason": reason, "sources": []any{}}}
}

func (h *KnowledgeDomainHandler) retrieveAdaptively(ctx context.Context, input core.StepInput, query string, loaded map[string]bool, limit, budget int) (core.StepResult, error) {
	found, err := h.domain.FindApplicationSources(ctx, query, loaded, limit, budget)
	if err != nil {
		return core.StepResult{}, err
	}
	sources, evidence, ids := []any{}, []core.Evidence{}, []any{}
	for _, source := range found.Sources {
		id, _ := source["source_id"].(string)
		version, _ := source["text_version"].(string)
		sources = append(sources, source)
		ids = append(ids, id)
		evidence = append(evidence, core.Evidence{Ref: id, Version: version, Capability: "knowledge.search", ObservedAt: time.Now().UTC()})
	}
	skipped := []any{}
	for _, id := range found.Skipped {
		skipped = append(skipped, id)
	}
	return core.StepResult{Disposition: core.StepContinue, Evidence: evidence, Output: map[string]any{"query": query, "sources": sources, "retrieved": ids, "skipped": skipped}}, nil
}

func adaptiveSources(output map[string]any) []map[string]any {
	listed, _ := output["sources"].([]any)
	sources := make([]map[string]any, 0, len(listed))
	for _, source := range listed {
		if item, ok := source.(map[string]any); ok {
			sources = append(sources, item)
		}
	}
	return sources
}

func (h *KnowledgeDomainHandler) adaptiveProposal(ctx context.Context, taskID string, revision int, request knowledge.AdaptiveRequest, sources []map[string]any, limit time.Duration) (core.StepResult, error) {
	selection := request.Application(sources)
	application, err := knowledge.AssembleApplicationContext(selection, sources)
	if err != nil {
		return core.StepResult{}, err
	}
	normalized, err := normalizeApplication(application)
	if err != nil {
		return core.StepResult{}, err
	}
	prompt, err := knowledge.BuildProposalRevisionPrompt(taskID, revision, normalized)
	if err != nil {
		return core.StepResult{}, err
	}
	return runApplicationInference(ctx, limit, "invalid_provider_proposal", "invalid_proposal",
		func(ctx context.Context) (knowledge.ProposalResponse, error) {
			return h.measuredProposer.Propose(ctx, prompt)
		},
		func(response knowledge.ProposalResponse) (map[string]any, error) {
			return knowledge.DecodeProposalRevision(taskID, revision, selection, normalized, response.Body)
		})
}

// normalizeApplication gives an in-memory context the shape it has after the workspace,
// which is the shape the prompt and quotation anchoring read.
func normalizeApplication(application map[string]any) (map[string]any, error) {
	raw, err := json.Marshal(application)
	if err != nil {
		return nil, err
	}
	var normalized map[string]any
	return normalized, json.Unmarshal(raw, &normalized)
}

// settleAdaptive publishes the latest valid proposal with its whole account: which
// documents each round read, every call and why retrieval ended.
func settleAdaptive(outputs map[string]map[string]any) map[string]any {
	final := outputs["proposal-1"]
	if second := outputs["proposal-2"]; second != nil && second["failed"] != true && len(second) > 0 {
		final = second
	}
	rounds := []any{}
	inference := map[string]any{"calls": 0, "elapsed_ms": int64(0), "input_tokens": int64(0), "output_tokens": int64(0), "budget_mode": "post_generation"}
	for round := 1; round <= knowledge.AdaptiveMaxProposals; round++ {
		retrieval := outputs[fmt.Sprintf("retrieve-%d", round)]
		proposal := outputs[fmt.Sprintf("proposal-%d", round)]
		entry := map[string]any{"round": round, "query": retrieval["query"], "retrieved": retrieval["retrieved"], "skipped": retrieval["skipped"]}
		if metadata, ok := proposal["inference"].(map[string]any); ok {
			entry["inference"] = metadata
			entry["disposition"], entry["failed"] = proposal["disposition"], proposal["failed"] == true
			addAdaptiveUsage(inference, metadata)
		}
		if assessment, ok := proposal["assessment"].(map[string]any); ok {
			entry["applicability"] = assessment["applicability"]
		}
		rounds = append(rounds, entry)
	}
	settled := make(map[string]any, len(final)+3)
	for key, value := range final {
		settled[key] = value
	}
	settled["strategy"] = "adaptive"
	settled["rounds"] = rounds
	settled["inference"] = inference
	settled["stop_reason"] = adaptiveStopReason(outputs)
	return settled
}

// addAdaptiveUsage keeps a total only while every call's count is known: an unknown
// count turns the total null instead of being added as zero.
func addAdaptiveUsage(total, call map[string]any) {
	total["calls"] = total["calls"].(int) + 1
	if total["provider"] == nil || total["provider"] == "" {
		total["provider"], total["requested_model"] = call["provider"], call["requested_model"]
	}
	if elapsed, ok := call["elapsed_ms"].(int64); ok {
		total["elapsed_ms"] = total["elapsed_ms"].(int64) + elapsed
	}
	for _, key := range []string{"input_tokens", "output_tokens"} {
		sum, known := total[key].(int64)
		count, counted := call[key].(int64)
		if known && counted {
			total[key] = sum + count
		} else {
			total[key] = nil
		}
	}
}

func adaptiveStopReason(outputs map[string]map[string]any) string {
	if reason, ok := outputs["retrieve-2"]["stop_reason"].(string); ok {
		return reason
	}
	if outputs["proposal-2"]["failed"] == true {
		return "revision_failed"
	}
	return "revised"
}
