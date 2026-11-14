package handlers

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	knowledge "read_books/dokja_domain/dokja_knowledge"
	"read_books/internal/core"
)

// prepareApplication runs a read-only, opt-in task. The orchestrator owns the
// sequence/deadline/workspace; Knowledge validates selection and evidence.
func (h *KnowledgeDomainHandler) prepareApplication(ctx context.Context, event core.Event) (map[string]any, error) {
	request, err := knowledge.ParseApplicationRequest(event.Payload)
	if err != nil {
		return nil, err
	}
	if _, enabled := ctx.Value(sourceReadJournalKey{}).(SourceReadJournal); enabled {
		for _, source := range request.Sources {
			if source.TextVersion == "" {
				return nil, fmt.Errorf("journaled source reads require every text version pin")
			}
		}
	}
	return h.runApplicationTask(ctx, event, h.applicationPreparationWorkflow(request), "prepared", "application", "context")
}

// PreparedKnowledgeSource is the validated whole-source read a focused Talk turn asked for. The
// chat handler uses the returned text_version to pin every passage it cites from that source.
type PreparedKnowledgeSource struct {
	Source map[string]any
	Task   core.TaskResult
}

// PrepareFocusedSource runs the existing one-source preparation workflow for a Talk focus. It is
// intentionally narrow: no caller authority, outcome, applicability or conclusion enters here.
func (h *KnowledgeDomainHandler) PrepareFocusedSource(ctx context.Context, sourceID, goal string) (PreparedKnowledgeSource, error) {
	goal = strings.TrimSpace(goal)
	if h == nil || h.domain == nil || sourceID == "" || goal == "" {
		return PreparedKnowledgeSource{}, fmt.Errorf("focused source preparation is incomplete")
	}
	event := core.Event{
		EventID: uuid.NewString(), Source: core.SourceOrchestrator, Type: knowledge.ActionPrepareApplication,
		Payload: map[string]any{
			"contract": knowledge.ApplicationContract,
			"goal":     goal,
			"sources":  []any{map[string]any{"source_id": sourceID}},
		},
	}
	output, err := h.prepareApplication(ctx, event)
	if err != nil {
		return PreparedKnowledgeSource{}, err
	}
	if output["status"] != "prepared" {
		return PreparedKnowledgeSource{}, fmt.Errorf("focused source was not prepared")
	}
	application, _ := output["application"].(map[string]any)
	observations, _ := application["sources"].([]any)
	if len(observations) != 1 {
		return PreparedKnowledgeSource{}, fmt.Errorf("focused source preparation returned no unique source")
	}
	source, _ := observations[0].(map[string]any)
	version, _ := source["text_version"].(string)
	if version == "" {
		return PreparedKnowledgeSource{}, fmt.Errorf("focused source preparation returned no text version")
	}
	task, _ := output["task"].(core.TaskResult)
	return PreparedKnowledgeSource{Source: source, Task: task}, nil
}

func (h *KnowledgeDomainHandler) applicationPreparationWorkflow(request knowledge.ApplicationRequest) core.TypedWorkflow {
	workflow := core.TypedWorkflow{Name: "knowledge-application-prepare", Version: "1", Goal: request.Goal, Deadline: 15 * time.Second}
	for index, selected := range request.Sources {
		workflow.Steps = append(workflow.Steps, core.TaskStep{
			Name: fmt.Sprintf("source-%d", index), Domain: core.DomainKnowledge, Action: "read-application-source",
			Run: func(ctx context.Context, input core.StepInput) (core.StepResult, error) {
				source, receipts, err := h.loadApplicationSource(ctx, input, selected, index)
				if err != nil {
					return core.StepResult{}, err
				}
				return core.StepResult{Disposition: core.StepContinue, Output: source, Receipts: receipts, Evidence: []core.Evidence{{
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
	if output := task.Outputs[outputKey]; task.Status != core.TaskCompleted && output != nil {
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
			return runApplicationInference(ctx, 60*time.Second, "invalid_provider_proposal", "invalid_proposal",
				func(ctx context.Context) (knowledge.ProposalResponse, error) { return proposer.Propose(ctx, prompt) },
				func(response knowledge.ProposalResponse) (map[string]any, error) {
					return knowledge.DecodeProviderProposal(input.TaskID, request, application, response.Body)
				})
		},
	})
	return h.runApplicationTask(ctx, event, workflow, "proposed", "proposal", "proposal")
}

// runApplicationInference makes the single provider attempt of an application step.
// Usage and the attempt stay on record when the reply is withheld or refused.
func runApplicationInference(
	ctx context.Context, limit time.Duration, invalidReason, invalidOutcome string,
	call func(context.Context) (knowledge.ProposalResponse, error),
	decode func(knowledge.ProposalResponse) (map[string]any, error),
) (core.StepResult, error) {
	callCtx, cancel := context.WithTimeout(ctx, limit)
	defer cancel()
	started := time.Now()
	response, err := call(callCtx)
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
		switch response.BudgetStatus {
		case "over_budget":
			metadata["outcome"] = "over_budget"
			return stop("token_budget_exceeded")
		case "usage_unavailable":
			metadata["outcome"] = "usage_unavailable"
			return stop("token_usage_unavailable")
		}
		return stop("provider_unavailable")
	}
	output, err := decode(response)
	if err != nil {
		metadata["outcome"] = invalidOutcome
		return stop(invalidReason)
	}
	metadata["outcome"] = "accepted"
	output["inference"] = metadata
	return core.StepResult{Disposition: core.StepContinue, Output: output}, nil
}

// reviewApplication re-reads the pinned sources, repeats the deterministic assessment,
// then asks for one measured reviewer verdict in a task of its own. The reviewer never
// sees the proposer's summary or reasoning.
func (h *KnowledgeDomainHandler) reviewApplication(ctx context.Context, event core.Event) (map[string]any, error) {
	request, err := knowledge.ParseReviewRequest(event.Payload)
	if err != nil {
		return nil, err
	}
	if h.reviewer == nil {
		return nil, fmt.Errorf("application review inference is not configured")
	}
	workflow := h.applicationPreparationWorkflow(request.Assessment.Application)
	workflow.Name = "knowledge-application-review"
	workflow.Deadline = 90 * time.Second
	workflow.Steps = append(workflow.Steps, core.TaskStep{
		Name: "assessment", Domain: core.DomainKnowledge, Action: knowledge.ActionAssessApplication,
		Run: func(_ context.Context, input core.StepInput) (core.StepResult, error) {
			output, err := knowledge.AssessApplication(request.Assessment, input.Outputs["context"])
			return core.StepResult{Disposition: core.StepContinue, Output: output}, err
		},
	}, core.TaskStep{
		Name: "review", Domain: core.DomainKnowledge, Action: knowledge.ActionReviewApplication,
		Run: func(ctx context.Context, input core.StepInput) (core.StepResult, error) {
			application := input.Outputs["context"]
			prompt, err := knowledge.BuildReviewPrompt(input.TaskID, application, request.Assessment.Proposal)
			if err != nil {
				return core.StepResult{}, err
			}
			// A review took close to a minute against a real model, so it gets nearly all
			// of the 90-second task; the deterministic steps above take milliseconds.
			return runApplicationInference(ctx, 85*time.Second, "invalid_provider_review", "invalid_review",
				func(ctx context.Context) (knowledge.ProposalResponse, error) { return h.reviewer.Review(ctx, prompt) },
				func(response knowledge.ProposalResponse) (map[string]any, error) {
					return knowledge.DecodeProviderReview(input.TaskID, request, application, input.Outputs["assessment"], response)
				})
		},
	})
	return h.runApplicationTask(ctx, event, workflow, "reviewed", "review", "review")
}
