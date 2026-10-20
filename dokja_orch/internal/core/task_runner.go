package core

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
)

const maxTaskSummary = 200

// reasonTerminal is what apply reports if anything tries to change an ended task.
const reasonTerminal = "task_terminal"

// workspace is the volatile state of one attempt. Only RunTask touches it.
type workspace struct {
	result       TaskResult
	goal         string
	payload      map[string]any
	payloadBytes int
	domains      map[Domain]bool
	limits       TaskLimits
	terminal     bool
}

func validateTask(workflow TypedWorkflow, limits TaskLimits) error {
	if workflow.Name == "" || workflow.Version == "" {
		return fmt.Errorf("typed workflow needs a name and a version")
	}
	if workflow.Deadline <= 0 {
		return fmt.Errorf("typed workflow %s needs a positive deadline", workflow.Name)
	}
	if limits.MaxSteps <= 0 || limits.MaxEvidence <= 0 || limits.MaxProposals <= 0 || limits.MaxBytes <= 0 {
		return fmt.Errorf("task limits must all be positive, got %+v", limits)
	}
	names := map[string]bool{}
	for _, step := range workflow.Steps {
		if step.Name == "" || step.Run == nil {
			return fmt.Errorf("typed workflow %s has a step without a name or a function", workflow.Name)
		}
		if names[step.Name] {
			return fmt.Errorf("typed workflow %s repeats the step name %q", workflow.Name, step.Name)
		}
		names[step.Name] = true
	}
	return nil
}

func scopeOf(event Event) TaskScope {
	return TaskScope{Source: event.Source, ActorID: event.User.ID, ChannelID: event.Channel.ID}
}

// RunTask executes one attempt of a typed workflow for an event. It returns an error
// only when the workflow or limits are invalid, before any step runs; every other
// ending is a TaskResult status. Cancellation reaches each step through its context,
// but a step that ignores it keeps running after the task ends.
func RunTask(ctx context.Context, event Event, workflow TypedWorkflow, limits TaskLimits) (TaskResult, error) {
	if err := validateTask(workflow, limits); err != nil {
		return TaskResult{}, err
	}
	// The effective deadline is the earlier of the workflow's and the caller's.
	ctx, cancel := context.WithTimeout(ctx, workflow.Deadline)
	defer cancel()

	deadline, _ := ctx.Deadline()
	w := &workspace{
		goal: workflow.Goal, payload: copyMap(event.Payload), domains: map[Domain]bool{}, limits: limits,
		result: TaskResult{
			TaskID: uuid.NewString(), AttemptID: uuid.NewString(), EventID: event.EventID,
			Workflow: workflow.Name, Version: workflow.Version, Scope: scopeOf(event), Deadline: deadline,
			Status: TaskCreated, Outputs: map[string]map[string]any{},
		},
	}
	for _, step := range workflow.Steps {
		w.domains[step.Domain] = true
	}
	w.payloadBytes = encodedSize(w.payload)
	if w.payloadBytes > limits.MaxBytes {
		// Large content belongs outside the workspace, referenced as evidence.
		return w.end(TaskStopped, ReasonSizeLimit, "the event payload is larger than the task budget"), nil
	}
	w.result.Status = TaskRunning
	for index, step := range workflow.Steps {
		if done, status, reason := interrupted(ctx); done {
			return w.end(status, reason, ""), nil
		}
		if index >= limits.MaxSteps {
			return w.end(TaskStopped, ReasonStepLimit, fmt.Sprintf("more than %d steps", limits.MaxSteps)), nil
		}
		started := time.Now()
		outcome, err := runStep(ctx, step, w.input())
		record := StepRecord{Name: step.Name, Domain: step.Domain, Action: step.Action, Duration: time.Since(started)}
		if done, status, reason := interrupted(ctx); done {
			// A result that arrives after the deadline or cancellation is discarded, and
			// whatever the step did meanwhile is unconfirmed.
			record.Reason = ReasonOutcomeUnknown
			w.result.Steps = append(w.result.Steps, record)
			return w.end(status, reason, fmt.Sprintf("step %s did not finish", step.Name)), nil
		}
		if err != nil {
			outcome = StepResult{Disposition: StepFail, Reason: ReasonStepError}
		}
		if !validDisposition(outcome.Disposition) || !w.allowed(outcome.Proposals) {
			outcome = StepResult{Disposition: StepFail, Reason: ReasonInvalidResult}
		}
		record.Disposition, record.Reason = outcome.Disposition, outcome.Reason

		if outcome.Disposition == StepFail {
			if step.Optional {
				record.Degraded = true
				w.result.Steps = append(w.result.Steps, record)
				continue
			}
			w.result.Steps = append(w.result.Steps, record)
			return w.end(TaskFailed, outcome.Reason, failureSummary(step, err)), nil
		}
		dropped, unscoped, limitReason := w.apply(step, outcome)
		record.Dropped, record.Unscoped = dropped, unscoped
		w.result.Steps = append(w.result.Steps, record)
		if limitReason != "" {
			return w.end(TaskStopped, limitReason, fmt.Sprintf("step %s exceeded a task limit", step.Name)), nil
		}
		switch outcome.Disposition {
		case StepStop:
			return w.end(TaskStopped, outcome.Reason, fmt.Sprintf("step %s stopped the task", step.Name)), nil
		case StepAbstain:
			if !step.ContinueOnAbstain {
				return w.end(TaskCompleted, ReasonAbstained, fmt.Sprintf("step %s abstained: %s", step.Name, outcome.Reason)), nil
			}
		}
	}
	return w.end(TaskCompleted, "", ""), nil
}

func interrupted(ctx context.Context) (bool, TaskStatus, string) {
	switch {
	case errors.Is(ctx.Err(), context.DeadlineExceeded):
		return true, TaskStopped, ReasonDeadlineExceeded
	case ctx.Err() != nil:
		return true, TaskCancelled, ReasonCancelled
	}
	return false, "", ""
}

// runStep returns as soon as the context ends, even if the step ignores it.
func runStep(ctx context.Context, step TaskStep, input StepInput) (StepResult, error) {
	type answer struct {
		result StepResult
		err    error
	}
	done := make(chan answer, 1)
	go func() {
		result, err := step.Run(ctx, input)
		done <- answer{result, err}
	}()
	select {
	case <-ctx.Done():
		return StepResult{}, ctx.Err()
	case got := <-done:
		return got.result, got.err
	}
}

// allowed refuses a proposal for a domain the workflow does not run.
func (w *workspace) allowed(proposals []Proposal) bool {
	for _, proposal := range proposals {
		if !w.domains[proposal.Domain] {
			return false
		}
	}
	return true
}

func validDisposition(disposition StepDisposition) bool {
	switch disposition {
	case StepContinue, StepStop, StepAbstain, StepFail:
		return true
	}
	return false
}

func failureSummary(step TaskStep, err error) string {
	if err == nil {
		return fmt.Sprintf("step %s failed", step.Name)
	}
	return bounded(fmt.Sprintf("step %s failed: %v", step.Name, err))
}

func bounded(text string) string {
	runes := []rune(text)
	if len(runes) <= maxTaskSummary {
		return text
	}
	return fmt.Sprintf("%s…", string(runes[:maxTaskSummary]))
}

func (w *workspace) input() StepInput {
	outputs := make(map[string]map[string]any, len(w.result.Outputs))
	for name, output := range w.result.Outputs {
		outputs[name] = copyMap(output)
	}
	return StepInput{
		TaskID: w.result.TaskID, Goal: w.goal, Scope: w.result.Scope, Payload: copyMap(w.payload),
		Evidence: copyEvidence(w.result.Evidence), Proposals: copyProposals(w.result.Proposals), Outputs: outputs,
	}
}

// apply merges a step's result into the workspace. Evidence from another explicit scope
// is dropped; unscoped evidence is kept and counted. A result that breaks a limit is not
// applied at all.
func (w *workspace) apply(step TaskStep, outcome StepResult) (int, int, string) {
	if w.terminal {
		return 0, 0, reasonTerminal
	}
	kept := make([]Evidence, 0, len(outcome.Evidence))
	unscoped := 0
	for _, evidence := range copyEvidence(outcome.Evidence) {
		switch evidence.Scope {
		case w.result.Scope:
		case TaskScope{}:
			unscoped++
		default:
			continue
		}
		kept = append(kept, evidence)
	}
	dropped := len(outcome.Evidence) - len(kept)
	if len(w.result.Evidence)+len(kept) > w.limits.MaxEvidence {
		return dropped, unscoped, ReasonEvidenceLimit
	}
	if len(w.result.Proposals)+len(outcome.Proposals) > w.limits.MaxProposals {
		return dropped, unscoped, ReasonProposalLimit
	}
	next := w.result
	next.Evidence = append(copyEvidence(w.result.Evidence), kept...)
	next.Proposals = append(copyProposals(w.result.Proposals), copyProposals(outcome.Proposals)...)
	next.Receipts = append(append([]Receipt{}, w.result.Receipts...), outcome.Receipts...)
	next.Outputs = make(map[string]map[string]any, len(w.result.Outputs)+1)
	for name, output := range w.result.Outputs {
		next.Outputs[name] = output
	}
	if outcome.Output != nil {
		next.Outputs[step.Name] = copyMap(outcome.Output)
	}
	if w.payloadBytes+serializedSize(next) > w.limits.MaxBytes {
		return dropped, unscoped, ReasonSizeLimit
	}
	w.result = next
	return dropped, unscoped, ""
}

func serializedSize(result TaskResult) int {
	return encodedSize(struct {
		Evidence  []Evidence                `json:"evidence"`
		Proposals []Proposal                `json:"proposals"`
		Receipts  []Receipt                 `json:"receipts"`
		Outputs   map[string]map[string]any `json:"outputs"`
	}{result.Evidence, result.Proposals, result.Receipts, result.Outputs})
}

func encodedSize(value any) int {
	encoded, err := json.Marshal(value)
	if err != nil {
		return int(^uint(0) >> 1)
	}
	return len(encoded)
}

func (w *workspace) end(status TaskStatus, reason, summary string) TaskResult {
	w.terminal = true
	w.result.Status, w.result.Reason, w.result.Summary = status, reason, bounded(summary)
	result := w.result
	w.payload = nil
	return result
}

func copyEvidence(values []Evidence) []Evidence {
	copied := make([]Evidence, len(values))
	copy(copied, values)
	return copied
}

func copyProposals(values []Proposal) []Proposal {
	copied := make([]Proposal, len(values))
	for i, proposal := range values {
		proposal.EvidenceRefs = append([]string(nil), proposal.EvidenceRefs...)
		proposal.Preconditions = append([]string(nil), proposal.Preconditions...)
		copied[i] = proposal
	}
	return copied
}

func copyMap(values map[string]any) map[string]any {
	if values == nil {
		return map[string]any{}
	}
	copied := make(map[string]any, len(values))
	for key, value := range values {
		copied[key] = copyValue(value)
	}
	return copied
}

func copyValue(value any) any {
	switch typed := value.(type) {
	case map[string]any:
		return copyMap(typed)
	case []any:
		copied := make([]any, len(typed))
		for i, item := range typed {
			copied[i] = copyValue(item)
		}
		return copied
	case []string:
		return append([]string(nil), typed...)
	case map[string]string:
		copied := make(map[string]string, len(typed))
		for key, item := range typed {
			copied[key] = item
		}
		return copied
	default:
		return value
	}
}
