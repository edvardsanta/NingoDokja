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
	task, err := core.RunTask(ctx, event, workflow, core.TaskLimits{
		MaxSteps: knowledge.MaxApplicationSources + 1, MaxEvidence: knowledge.MaxApplicationSources,
		MaxProposals: 1, MaxBytes: 512 * 1024,
	})
	if err != nil {
		return nil, err
	}
	result := map[string]any{"status": "not_prepared"}
	if task.Status == core.TaskCompleted && task.Reason == "" {
		result["status"] = "prepared"
		result["application"] = task.Outputs["context"]
	}
	// Partial source contents are never published as a usable context.
	task.Outputs = nil
	result["task"] = task
	return result, nil
}
