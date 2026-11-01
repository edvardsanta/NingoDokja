package handlers

import (
	"context"
	"fmt"
	knowledgedomain "read_books/dokja_domain/dokja_knowledge"
	"read_books/internal/core"
	"read_books/internal/logger"
	"sync/atomic"
	"time"
)

const (
	intakeTaskVersion = "1"
	// intakeTaskDeadline leaves room for every document of a run to take Knowledge's slowest
	// write of 110 seconds and a little over half of them to do so.
	intakeTaskDeadline = 20 * time.Minute
)

// IntakeTask keeps what the owner's chosen Feeds sources publish in Knowledge. Feeds never calls
// Knowledge: the orchestrator sequences the two, and the Knowledge domain decides what is taken.
//
// The events of the scheduler are handled one at a time, so Start returns at once and the run
// goes on in the background. Nothing is lost if it dies: every document has a stable identity,
// so the next run takes up what is missing.
type IntakeTask struct {
	feeds    knowledgedomain.Service
	domain   *knowledgedomain.Domain
	sources  []string
	enabled  func() bool
	deadline time.Duration
	now      func() time.Time
	running  atomic.Bool
}

func NewIntakeTask(feeds, knowledge knowledgedomain.Service, sources []string, feedsEnabled func() bool) *IntakeTask {
	return &IntakeTask{
		feeds: feeds, domain: knowledgedomain.New(knowledge), sources: sources,
		enabled: feedsEnabled, deadline: intakeTaskDeadline, now: time.Now,
	}
}

// Start begins a run unless there is nothing to do. Choosing no sources is the default, so it
// is an ordinary answer and not a failure on every tick.
func (t *IntakeTask) Start(event core.Event) (map[string]any, error) {
	if t == nil {
		return nil, fmt.Errorf("knowledge intake is not configured")
	}
	if len(t.sources) == 0 {
		return map[string]any{"skipped": true, "reason": "no_sources"}, nil
	}
	if t.enabled != nil && !t.enabled() {
		return map[string]any{"skipped": true, "reason": "feeds_switched_off"}, nil
	}
	if !t.running.CompareAndSwap(false, true) {
		return map[string]any{"skipped": true, "reason": "already_running"}, nil
	}
	go func() {
		defer t.running.Store(false)
		t.run(context.Background(), event)
	}()
	return map[string]any{"started": true, "sources": len(t.sources)}, nil
}

// intakeRun is what one run holds outside the workspace, which is bounded.
type intakeRun struct {
	selection knowledgedomain.IntakeSelection
	outcome   knowledgedomain.IntakeOutcome
}

func (t *IntakeTask) run(ctx context.Context, event core.Event) (core.TaskResult, *intakeRun, error) {
	run := &intakeRun{}
	result, err := core.RunTask(ctx, event, core.TypedWorkflow{
		Name: "knowledge-intake", Version: intakeTaskVersion, Goal: "keep what the chosen sources published",
		Deadline: t.deadline,
		Steps: []core.TaskStep{
			{Name: "collect", Domain: core.DomainKnowledge, Action: "read-feed-items", Run: t.collect(run)},
			{Name: "ingest", Domain: core.DomainKnowledge, Action: knowledgedomain.ActionIngestKnowledge, Run: t.store(run)},
		},
	}, core.PrototypeTaskLimits())
	if err != nil {
		logger.Error("knowledge intake was not started", err)
		return result, run, err
	}
	// Only the shape of what happened is logged, as for a reading.
	logger.Info(fmt.Sprintf(
		"knowledge intake attempt=%s status=%s reason=%s selected=%d stored=%d changed=%d failed=%d without_body=%d out_of_window=%d over_limit=%d",
		result.AttemptID, result.Status, result.Reason, len(run.selection.Documents), run.outcome.Stored, run.outcome.Changed,
		run.outcome.Failed, run.selection.WithoutBody, run.selection.OutOfWindow, run.selection.OverLimit,
	))
	return result, run, nil
}

// collect only reads what Feeds already holds. A refresh would rerun every plugin, whatever
// each of them costs, and they run on their own schedule anyway.
func (t *IntakeTask) collect(run *intakeRun) core.TaskStepFunc {
	return func(ctx context.Context, _ core.StepInput) (core.StepResult, error) {
		page, err := t.feeds.Dispatch(ctx, "feeds.items", nil)
		if err != nil {
			return core.StepResult{Disposition: core.StepFail, Reason: "feeds_unavailable"}, nil
		}
		run.selection = knowledgedomain.SelectIntake(page["items"], t.sources, t.now())
		return core.StepResult{Disposition: core.StepContinue, Output: map[string]any{
			"selected": len(run.selection.Documents), "without_body": run.selection.WithoutBody,
			"out_of_window": run.selection.OutOfWindow, "over_limit": run.selection.OverLimit,
		}}, nil
	}
}

func (t *IntakeTask) store(run *intakeRun) core.TaskStepFunc {
	return func(ctx context.Context, _ core.StepInput) (core.StepResult, error) {
		outcome, err := t.domain.StoreIntake(ctx, run.selection.Documents)
		run.outcome = outcome
		output := map[string]any{"stored": outcome.Stored, "changed": outcome.Changed, "failed": outcome.Failed}
		if err != nil {
			return core.StepResult{Disposition: core.StepFail, Reason: "knowledge_ingest_failed", Output: output}, nil
		}
		return core.StepResult{Disposition: core.StepContinue, Output: output}, nil
	}
}
