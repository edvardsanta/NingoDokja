package handlers

import (
	"context"
	"fmt"
	knowledge "read_books/dokja_domain/dokja_knowledge"
	"read_books/internal/core"
	"time"
)

// prepareApplication runs a read-only, opt-in task. The orchestrator owns the
// sequence/deadline/workspace; Knowledge validates selection and evidence.
func (h *KnowledgeDomainHandler) prepareApplication(ctx context.Context, event core.Event) (map[string]any, error) {
	request, err := knowledge.ParseApplicationRequest(event.Payload)
	if err != nil {
		return nil, err
	}
	return h.runApplicationTask(ctx, event, h.applicationPreparationWorkflow(request), "prepared", "application", "context")
}

func (h *KnowledgeDomainHandler) applicationPreparationWorkflow(request knowledge.ApplicationRequest) core.TypedWorkflow {
	workflow := core.TypedWorkflow{Name: "knowledge-application-prepare", Version: "1", Goal: request.Goal, Deadline: 15 * time.Second}
	for index, selected := range request.Sources {
		workflow.Steps = append(workflow.Steps, core.TaskStep{
			Name: fmt.Sprintf("source-%d", index), Domain: core.DomainKnowledge, Action: "read-application-source",
			Run: func(ctx context.Context, _ core.StepInput) (core.StepResult, error) {
				source, err := h.domain.LoadApplicationSource(ctx, selected)
				if err != nil {
					return core.StepResult{}, err
				}
				return core.StepResult{Disposition: core.StepContinue, Output: source, Evidence: []core.Evidence{{
					Ref: selected.SourceID, Version: source["text_version"].(string), Capability: "knowledge.list", ObservedAt: time.Now().UTC(),
				}}}, nil
			},
		})
	}
	workflow.Steps = append(workflow.Steps, core.TaskStep{
		Name: "context", Domain: core.DomainKnowledge, Action: "assemble-application-context",
		Run: func(_ context.Context, input core.StepInput) (core.StepResult, error) {
			sources := make([]map[string]any, 0, len(request.Sources))
			for index := range request.Sources {
				sources = append(sources, input.Outputs[fmt.Sprintf("source-%d", index)])
			}
			output, err := knowledge.AssembleApplicationContext(request, sources)
			return core.StepResult{Disposition: core.StepContinue, Output: output}, err
		},
	})
	return workflow
}

func (h *KnowledgeDomainHandler) assessApplication(ctx context.Context, event core.Event) (map[string]any, error) {
	request, err := knowledge.ParseAssessmentRequest(event.Payload)
	if err != nil {
		return nil, err
	}
	workflow := h.applicationPreparationWorkflow(request.Application)
	workflow.Name = "knowledge-application-assess"
	workflow.Steps = append(workflow.Steps, core.TaskStep{
		Name: "assessment", Domain: core.DomainKnowledge, Action: knowledge.ActionAssessApplication,
		Run: func(_ context.Context, input core.StepInput) (core.StepResult, error) {
			output, err := knowledge.AssessApplication(request, input.Outputs["context"])
			return core.StepResult{Disposition: core.StepContinue, Output: output}, err
		},
	})
	return h.runApplicationTask(ctx, event, workflow, "assessed", "assessment", "assessment")
}

func (h *KnowledgeDomainHandler) runApplicationTask(ctx context.Context, event core.Event, workflow core.TypedWorkflow, status, resultKey, outputKey string) (map[string]any, error) {
	task, err := core.RunTask(ctx, event, workflow, core.TaskLimits{
		MaxSteps: len(workflow.Steps), MaxEvidence: knowledge.MaxApplicationSources,
		MaxProposals: 1, MaxBytes: 512 * 1024,
	})
	if err != nil {
		return nil, err
	}
	result := map[string]any{"status": "not_" + status}
	if task.Status == core.TaskCompleted && task.Reason == "" {
		result["status"] = status
		result[resultKey] = task.Outputs[outputKey]
	}
	if output := task.Outputs["proposal"]; task.Status != core.TaskCompleted && output != nil {
		if inference, ok := output["inference"]; ok {
			result["inference"] = inference
		}
	}
	// Partial source contents are never published as a usable context.
	task.Outputs = nil
	result["task"] = task
	return result, nil
}

func (h *KnowledgeDomainHandler) proposeApplication(ctx context.Context, event core.Event) (map[string]any, error) {
	return h.proposeApplicationWith(ctx, event, h.proposer)
}

func (h *KnowledgeDomainHandler) proposeApplicationWith(ctx context.Context, event core.Event, proposer knowledge.ApplicationProposer) (map[string]any, error) {
	request, err := knowledge.ParseApplicationRequest(event.Payload)
	if err != nil {
		return nil, err
	}
	if proposer == nil {
		return nil, fmt.Errorf("application inference is not configured")
	}
	workflow := h.applicationPreparationWorkflow(request)
	workflow.Name = "knowledge-application-propose"
	if event.Type == "knowledge.application.propose.measured" {
		workflow.Name += "-measured"
	}
	workflow.Deadline = 90 * time.Second
	workflow.Steps = append(workflow.Steps, core.TaskStep{
		Name: "proposal", Domain: core.DomainKnowledge, Action: knowledge.ActionProposeApplication,
		Run: func(ctx context.Context, input core.StepInput) (core.StepResult, error) {
			application := input.Outputs["context"]
			prompt, err := knowledge.BuildProposalPrompt(input.TaskID, application)
			if err != nil {
				return core.StepResult{}, err
			}
			callCtx, cancel := context.WithTimeout(ctx, 60*time.Second)
			defer cancel()
			started := time.Now()
			response, err := proposer.Propose(callCtx, prompt)
			if callCtx.Err() != nil {
				return core.StepResult{}, callCtx.Err()
			}
			metadata := map[string]any{"calls": 1, "provider": response.Provider, "requested_model": response.RequestedModel, "elapsed_ms": time.Since(started).Milliseconds(), "input_tokens": nil, "output_tokens": nil, "budget_mode": response.BudgetMode, "budget_status": response.BudgetStatus, "outcome": "unavailable"}
			if response.InputTokens != nil {
				metadata["input_tokens"] = *response.InputTokens
			}
			if response.OutputTokens != nil {
				metadata["output_tokens"] = *response.OutputTokens
			}
			stop := func(reason string) (core.StepResult, error) {
				return core.StepResult{Disposition: core.StepStop, Reason: reason, Output: map[string]any{"inference": metadata}}, nil
			}
			if err != nil {
				if response.BudgetStatus == "over_budget" {
					metadata["outcome"] = "over_budget"
					return stop("token_budget_exceeded")
				}
				if response.BudgetStatus == "usage_unavailable" {
					metadata["outcome"] = "usage_unavailable"
					return stop("token_usage_unavailable")
				}
				return stop("provider_unavailable")
			}
			output, err := knowledge.DecodeProviderProposal(input.TaskID, request, application, response.Body)
			if err != nil {
				metadata["outcome"] = "invalid_proposal"
				return stop("invalid_provider_proposal")
			}
			metadata["outcome"] = "accepted"
			output["inference"] = metadata
			return core.StepResult{Disposition: core.StepContinue, Output: output}, nil
		},
	})
	return h.runApplicationTask(ctx, event, workflow, "proposed", "proposal", "proposal")
}
