package handlers

import (
	"context"
	"fmt"
	knowledgedomain "read_books/dokja_domain/dokja_knowledge"
	"read_books/internal/core"
)

// Reclassify explicitly asks the configured provider to revise automatic axes
// from stored content. No extractor, ingest or embedding call participates.
func (t *ReadingTask) Reclassify(ctx context.Context, event core.Event) (map[string]any, error) {
	if t.classifier == nil {
		return nil, fmt.Errorf("classifier_not_configured")
	}
	if t.enabled != nil && !t.enabled() {
		return nil, fmt.Errorf("reading is switched off (feeds)")
	}
	var snapshot *knowledgedomain.Reclassification
	var proposal knowledgedomain.ReadingProposal
	var id, version string
	var answer map[string]any
	var failure error
	skipped := false
	result, err := core.RunTask(ctx, event, core.TypedWorkflow{
		Name: "knowledge-reclassification", Version: "1", Goal: "revise an automatic document classification",
		Deadline: t.deadline,
		Steps: []core.TaskStep{
			{Name: "snapshot", Domain: core.DomainKnowledge, Action: "prepare-reclassification", Run: func(ctx context.Context, _ core.StepInput) (core.StepResult, error) {
				var err error
				snapshot, err = t.domain.PrepareReclassification(ctx, event.Payload)
				if err != nil {
					failure = err
					return core.StepResult{Disposition: core.StepFail, Reason: "knowledge_snapshot_failed"}, nil
				}
				skipped = !snapshot.Eligible()
				return core.StepResult{Disposition: core.StepContinue}, nil
			}},
			{Name: "classify", Domain: core.DomainKnowledge, Action: "classify-document", Run: func(ctx context.Context, _ core.StepInput) (core.StepResult, error) {
				if skipped {
					return core.StepResult{Disposition: core.StepContinue, Reason: "user_decision_preserved"}, nil
				}
				// Stored chunks are a text reconstruction; no original format is claimed.
				response, err := t.classifier.Classify(ctx, "", []map[string]any{snapshot.Document()})
				if err != nil {
					return core.StepResult{Disposition: core.StepFail, Reason: "classifier_failed"}, nil
				}
				proposals, ok := readProposals(response["classifications"])
				id, version = resultString(response, "plugin"), resultString(response, "version")
				if !ok || len(proposals) != 1 || id == "" || version == "" {
					return core.StepResult{Disposition: core.StepFail, Reason: "classifier_invalid_result"}, nil
				}
				proposal = proposals[0]
				return core.StepResult{Disposition: core.StepContinue}, nil
			}},
			{Name: "keep", Domain: core.DomainKnowledge, Action: "keep-reclassification", Run: func(ctx context.Context, _ core.StepInput) (core.StepResult, error) {
				if skipped {
					answer = map[string]any{"reclassified": false, "reason": "user_decision_preserved"}
					return core.StepResult{Disposition: core.StepContinue}, nil
				}
				var err error
				answer, err = t.domain.KeepReclassification(ctx, snapshot, proposal, id, version)
				if err != nil {
					failure = err
					return core.StepResult{Disposition: core.StepFail, Reason: "knowledge_reclassify_failed"}, nil
				}
				answer["reclassified"] = true
				return core.StepResult{Disposition: core.StepContinue}, nil
			}},
		},
	}, core.PrototypeTaskLimits())
	if err != nil {
		return nil, err
	}
	if result.Status != core.TaskCompleted {
		if failure != nil {
			return nil, fmt.Errorf("knowledge.reclassify %s: %w", result.Reason, failure)
		}
		return nil, fmt.Errorf("knowledge.reclassify: %s", result.Reason)
	}
	answer["task"] = result
	return answer, nil
}
