package core

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"
)

func taskEvent() Event {
	return Event{
		EventID: "evt-1", Source: SourceDiscord, Type: "meme.hashtag.suggest",
		User: EventUser{ID: "u1"}, Channel: EventChannel{ID: "c1"},
		Payload: map[string]any{"text": "a joke", "nested": map[string]any{"k": "v"}},
	}
}

func taskWorkflow(steps ...TaskStep) TypedWorkflow {
	return TypedWorkflow{Name: "test", Version: "1", Goal: "suggest", Deadline: time.Second, Steps: steps}
}

func continueWith(output map[string]any) TaskStepFunc {
	return func(context.Context, StepInput) (StepResult, error) {
		return StepResult{Disposition: StepContinue, Output: output}, nil
	}
}

func disposing(disposition StepDisposition, reason string) TaskStepFunc {
	return func(context.Context, StepInput) (StepResult, error) {
		return StepResult{Disposition: disposition, Reason: reason}, nil
	}
}

type ranSteps struct {
	mu    sync.Mutex
	names []string
}

func (r *ranSteps) step(name string, run TaskStepFunc) TaskStep {
	return TaskStep{Name: name, Domain: DomainMeme, Action: name, Run: func(ctx context.Context, in StepInput) (StepResult, error) {
		r.mu.Lock()
		r.names = append(r.names, name)
		r.mu.Unlock()
		return run(ctx, in)
	}}
}

func run(t *testing.T, workflow TypedWorkflow) TaskResult {
	t.Helper()
	result, err := RunTask(context.Background(), taskEvent(), workflow, PrototypeTaskLimits())
	if err != nil {
		t.Fatal(err)
	}
	return result
}

func TestTaskIdentityAndScopeComeFromTheEvent(t *testing.T) {
	result := run(t, taskWorkflow(TaskStep{Name: "a", Run: continueWith(nil)}))

	if result.Status != TaskCompleted || result.EventID != "evt-1" || result.TaskID == "" ||
		result.AttemptID == "" || result.TaskID == result.AttemptID {
		t.Fatalf("unexpected identity: %#v", result)
	}
	if result.Scope != (TaskScope{Source: SourceDiscord, ActorID: "u1", ChannelID: "c1"}) {
		t.Fatalf("scope must be copied from the event: %#v", result.Scope)
	}
	again := run(t, taskWorkflow(TaskStep{Name: "a", Run: continueWith(nil)}))
	if again.AttemptID == result.AttemptID || again.TaskID == result.TaskID {
		t.Fatal("each current workspace needs fresh task/attempt IDs even for the same event; logical task identity must be separate")
	}
}

func TestEachDispositionEndsTheTaskAsDeclared(t *testing.T) {
	cases := []struct {
		name   string
		step   TaskStep
		status TaskStatus
		reason string
		ran    []string
	}{
		{"continue runs every step", TaskStep{Run: disposing(StepContinue, "")}, TaskCompleted, "", []string{"first", "second"}},
		{"stop ends the task", TaskStep{Run: disposing(StepStop, "blocked")}, TaskStopped, "blocked", []string{"first"}},
		{"abstain finishes by default", TaskStep{Run: disposing(StepAbstain, "no evidence")}, TaskCompleted, ReasonAbstained, []string{"first"}},
		{"abstain can fall through", TaskStep{Run: disposing(StepAbstain, "no evidence"), ContinueOnAbstain: true}, TaskCompleted, "", []string{"first", "second"}},
		{"a required failure fails the task", TaskStep{Run: disposing(StepFail, "service_down")}, TaskFailed, "service_down", []string{"first"}},
		{"an optional failure is degraded", TaskStep{Run: disposing(StepFail, "memory_down"), Optional: true}, TaskCompleted, "", []string{"first", "second"}},
		{"an error is a failure", TaskStep{Run: func(context.Context, StepInput) (StepResult, error) {
			return StepResult{Disposition: StepContinue}, errors.New("boom")
		}}, TaskFailed, ReasonStepError, []string{"first"}},
		{"an unknown disposition is a failure", TaskStep{Run: disposing("maybe", "")}, TaskFailed, ReasonInvalidResult, []string{"first"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ran := &ranSteps{}
			first := ran.step("first", tc.step.Run)
			first.Optional, first.ContinueOnAbstain = tc.step.Optional, tc.step.ContinueOnAbstain
			result := run(t, taskWorkflow(first, ran.step("second", continueWith(nil))))

			if result.Status != tc.status || result.Reason != tc.reason {
				t.Fatalf("expected %s/%q, got %s/%q", tc.status, tc.reason, result.Status, result.Reason)
			}
			if strings.Join(ran.names, ",") != strings.Join(tc.ran, ",") {
				t.Fatalf("expected steps %v to run, got %v", tc.ran, ran.names)
			}
			if tc.step.Optional && !result.Steps[0].Degraded {
				t.Fatalf("an optional failure must be recorded as degraded: %#v", result.Steps[0])
			}
		})
	}
}

func TestAModerationBlockPreventsLaterSideEffects(t *testing.T) {
	delivered := false
	result := run(t, taskWorkflow(
		TaskStep{Name: "screen", Domain: DomainModeration, Run: disposing(StepStop, "moderation_blocked")},
		TaskStep{Name: "deliver", Domain: DomainCommunications, Run: func(context.Context, StepInput) (StepResult, error) {
			delivered = true
			return StepResult{Disposition: StepContinue, Receipts: []Receipt{{ActionID: "send", Status: "sent"}}}, nil
		}},
	))

	if delivered || result.Status != TaskStopped || result.Reason != "moderation_blocked" || len(result.Receipts) != 0 {
		t.Fatalf("a block must stop the task before delivery: delivered=%v %#v", delivered, result)
	}
}

func TestStepsSeeCopiesAndEarlierOutputs(t *testing.T) {
	event := taskEvent()
	shared := map[string]any{"list": []any{"x"}}
	var seen StepInput
	workflow := taskWorkflow(
		TaskStep{Name: "produce", Run: continueWith(shared)},
		TaskStep{Name: "tamper", Run: func(_ context.Context, in StepInput) (StepResult, error) {
			in.Payload["text"] = "changed"
			in.Payload["nested"].(map[string]any)["k"] = "changed"
			in.Outputs["produce"]["list"].([]any)[0] = "changed"
			return StepResult{Disposition: StepContinue}, nil
		}},
		TaskStep{Name: "read", Run: func(_ context.Context, in StepInput) (StepResult, error) {
			seen = in
			return StepResult{Disposition: StepContinue}, nil
		}},
	)
	result, err := RunTask(context.Background(), event, workflow, PrototypeTaskLimits())
	if err != nil {
		t.Fatal(err)
	}
	shared["list"].([]any)[0] = "changed after return"

	if seen.Payload["text"] != "a joke" || seen.Payload["nested"].(map[string]any)["k"] != "v" ||
		seen.Outputs["produce"]["list"].([]any)[0] != "x" {
		t.Fatalf("a step changed what later steps see: %#v", seen)
	}
	if result.Outputs["produce"]["list"].([]any)[0] != "x" || event.Payload["text"] != "a joke" {
		t.Fatalf("outputs and the event must be copied: %#v / %#v", result.Outputs, event.Payload)
	}
}

func TestEvidenceFromAnotherScopeIsDropped(t *testing.T) {
	own := TaskScope{Source: SourceDiscord, ActorID: "u1", ChannelID: "c1"}
	foreign := TaskScope{Source: SourceDiscord, ActorID: "u2", ChannelID: "c1"}
	result := run(t, taskWorkflow(TaskStep{Name: "retrieve", Run: func(context.Context, StepInput) (StepResult, error) {
		return StepResult{Disposition: StepContinue, Evidence: []Evidence{
			{Ref: "kb:1", Capability: "knowledge.search", Scope: own},
			{Ref: "kb:2", Capability: "knowledge.search", Scope: foreign},
		}}, nil
	}}))

	if len(result.Evidence) != 1 || result.Evidence[0].Ref != "kb:1" || result.Steps[0].Dropped != 1 {
		t.Fatalf("only own-scope evidence may enter the task: %#v", result)
	}
}

func TestUnscopedEvidenceIsKeptAndCounted(t *testing.T) {
	result := run(t, taskWorkflow(TaskStep{Name: "retrieve", Run: func(context.Context, StepInput) (StepResult, error) {
		return StepResult{Disposition: StepContinue, Evidence: []Evidence{{Ref: "kb:1", Capability: "knowledge.search"}}}, nil
	}}))

	if len(result.Evidence) != 1 || result.Evidence[0].Scope != (TaskScope{}) ||
		result.Steps[0].Unscoped != 1 || result.Steps[0].Dropped != 0 {
		t.Fatalf("evidence from a store without scopes is kept, flagged and never stamped: %#v", result)
	}
}

func TestAProposalOutsideTheWorkflowIsRefused(t *testing.T) {
	result := run(t, taskWorkflow(TaskStep{Name: "suggest", Domain: DomainMeme, Run: func(context.Context, StepInput) (StepResult, error) {
		return StepResult{Disposition: StepContinue, Proposals: []Proposal{{ActionID: "send", Domain: DomainCommunications}}}, nil
	}}))

	if result.Status != TaskFailed || result.Reason != ReasonInvalidResult || len(result.Proposals) != 0 {
		t.Fatalf("a proposal for a domain the workflow does not run must be refused: %#v", result)
	}
}

func TestAPayloadOverTheBudgetStopsBeforeAnyStep(t *testing.T) {
	ran := false
	event := taskEvent()
	event.Payload["upload"] = strings.Repeat("A", 2048)
	workflow := taskWorkflow(TaskStep{Name: "a", Run: func(context.Context, StepInput) (StepResult, error) {
		ran = true
		return StepResult{Disposition: StepContinue}, nil
	}})

	result, err := RunTask(context.Background(), event, workflow, TaskLimits{MaxSteps: 4, MaxEvidence: 4, MaxProposals: 4, MaxBytes: 1024})
	if err != nil {
		t.Fatal(err)
	}
	if ran || result.Status != TaskStopped || result.Reason != ReasonSizeLimit {
		t.Fatalf("large content belongs outside the workspace: ran=%v %#v", ran, result)
	}
}

func TestConcurrentTasksShareNothing(t *testing.T) {
	var wg sync.WaitGroup
	results := make([]TaskResult, 8)
	for i := range results {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			event := taskEvent()
			event.User.ID = fmt.Sprintf("u%d", i)
			workflow := taskWorkflow(
				TaskStep{Name: "write", Run: func(_ context.Context, in StepInput) (StepResult, error) {
					return StepResult{Disposition: StepContinue, Output: map[string]any{"actor": in.Scope.ActorID},
						Evidence: []Evidence{{Ref: fmt.Sprintf("ref-%d", i), Scope: in.Scope}}}, nil
				}},
				TaskStep{Name: "check", Run: func(_ context.Context, in StepInput) (StepResult, error) {
					if len(in.Evidence) != 1 || in.Outputs["write"]["actor"] != in.Scope.ActorID {
						return StepResult{Disposition: StepFail, Reason: "leak"}, nil
					}
					return StepResult{Disposition: StepContinue}, nil
				}},
			)
			results[i], _ = RunTask(context.Background(), event, workflow, PrototypeTaskLimits())
		}(i)
	}
	wg.Wait()
	for i, result := range results {
		if result.Status != TaskCompleted || result.Evidence[0].Ref != fmt.Sprintf("ref-%d", i) {
			t.Fatalf("task %d saw another task's state: %#v", i, result)
		}
	}
}

func TestTheDeadlineStopsAStepThatIgnoresIt(t *testing.T) {
	workflow := taskWorkflow(TaskStep{Name: "slow", Run: func(context.Context, StepInput) (StepResult, error) {
		time.Sleep(200 * time.Millisecond)
		return StepResult{Disposition: StepContinue, Output: map[string]any{"late": true}}, nil
	}})
	workflow.Deadline = 20 * time.Millisecond

	started := time.Now()
	result, err := RunTask(context.Background(), taskEvent(), workflow, PrototypeTaskLimits())
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != TaskStopped || result.Reason != ReasonDeadlineExceeded || len(result.Outputs) != 0 {
		t.Fatalf("a late result must be discarded: %#v", result)
	}
	if result.Steps[0].Reason != ReasonOutcomeUnknown || result.Steps[0].Disposition != "" {
		t.Fatalf("a step cut off by the deadline is unconfirmed, not failed: %#v", result.Steps[0])
	}
	if result.Deadline.IsZero() {
		t.Fatal("the effective deadline belongs in the result")
	}
	if time.Since(started) > 150*time.Millisecond {
		t.Fatalf("the task waited for the slow step: %s", time.Since(started))
	}
}

func TestTheCallerDeadlineWinsWhenItIsEarlier(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	workflow := taskWorkflow(TaskStep{Name: "wait", Run: func(ctx context.Context, _ StepInput) (StepResult, error) {
		<-ctx.Done()
		return StepResult{}, ctx.Err()
	}})
	workflow.Deadline = time.Minute

	result, _ := RunTask(ctx, taskEvent(), workflow, PrototypeTaskLimits())
	if result.Status != TaskStopped || result.Reason != ReasonDeadlineExceeded {
		t.Fatalf("expected the caller's deadline to stop the task: %#v", result)
	}
}

func TestCancellationReachesTheActiveStep(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	sawCancel := make(chan struct{})
	workflow := taskWorkflow(TaskStep{Name: "wait", Run: func(ctx context.Context, _ StepInput) (StepResult, error) {
		cancel()
		<-ctx.Done()
		close(sawCancel)
		return StepResult{}, ctx.Err()
	}})

	result, _ := RunTask(ctx, taskEvent(), workflow, PrototypeTaskLimits())
	select {
	case <-sawCancel:
	case <-time.After(time.Second):
		t.Fatal("the step never saw the cancellation")
	}
	if result.Status != TaskCancelled || result.Reason != ReasonCancelled {
		t.Fatalf("expected a cancelled task: %#v", result)
	}
}

func TestLimitsStopTheTask(t *testing.T) {
	many := func(n int) []Evidence {
		evidence := make([]Evidence, n)
		for i := range evidence {
			evidence[i] = Evidence{Ref: fmt.Sprintf("r%d", i), Scope: TaskScope{Source: SourceDiscord, ActorID: "u1", ChannelID: "c1"}}
		}
		return evidence
	}
	limits := TaskLimits{MaxSteps: 2, MaxEvidence: 3, MaxProposals: 1, MaxBytes: 512}
	cases := []struct {
		name   string
		steps  []TaskStep
		reason string
	}{
		{"steps", []TaskStep{{Name: "a", Run: continueWith(nil)}, {Name: "b", Run: continueWith(nil)}, {Name: "c", Run: continueWith(nil)}}, ReasonStepLimit},
		{"evidence", []TaskStep{{Name: "a", Run: func(context.Context, StepInput) (StepResult, error) {
			return StepResult{Disposition: StepContinue, Evidence: many(4)}, nil
		}}}, ReasonEvidenceLimit},
		{"proposals", []TaskStep{{Name: "a", Run: func(context.Context, StepInput) (StepResult, error) {
			return StepResult{Disposition: StepContinue, Proposals: []Proposal{{ActionID: "1"}, {ActionID: "2"}}}, nil
		}}}, ReasonProposalLimit},
		{"size", []TaskStep{{Name: "a", Run: continueWith(map[string]any{"blob": strings.Repeat("x", 1024)})}}, ReasonSizeLimit},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			result, err := RunTask(context.Background(), taskEvent(), taskWorkflow(tc.steps...), limits)
			if err != nil {
				t.Fatal(err)
			}
			if result.Status != TaskStopped || result.Reason != tc.reason {
				t.Fatalf("expected %s, got %s/%q", tc.reason, result.Status, result.Reason)
			}
			if tc.reason != ReasonStepLimit && (len(result.Evidence) != 0 || len(result.Proposals) != 0 || len(result.Outputs) != 0) {
				t.Fatalf("a result over the limit must not be applied: %#v", result)
			}
		})
	}
}

func TestInvalidWorkflowsOrLimitsFailBeforeAnyStep(t *testing.T) {
	ran := false
	step := TaskStep{Name: "a", Run: func(context.Context, StepInput) (StepResult, error) {
		ran = true
		return StepResult{Disposition: StepContinue}, nil
	}}
	noDeadline := taskWorkflow(step)
	noDeadline.Deadline = 0
	cases := []struct {
		name     string
		workflow TypedWorkflow
		limits   TaskLimits
	}{
		{"no deadline", noDeadline, PrototypeTaskLimits()},
		{"no version", TypedWorkflow{Name: "x", Deadline: time.Second, Steps: []TaskStep{step}}, PrototypeTaskLimits()},
		{"zero limits", taskWorkflow(step), TaskLimits{}},
		{"step without a function", taskWorkflow(TaskStep{Name: "empty"}), PrototypeTaskLimits()},
		{"repeated step name", taskWorkflow(step, step), PrototypeTaskLimits()},
	}
	for _, tc := range cases {
		if _, err := RunTask(context.Background(), taskEvent(), tc.workflow, tc.limits); err == nil {
			t.Fatalf("%s: expected an error", tc.name)
		}
	}
	if ran {
		t.Fatal("no step may run for an invalid workflow")
	}
}

func TestAnEndedWorkspaceRejectsChanges(t *testing.T) {
	w := &workspace{limits: PrototypeTaskLimits(), result: TaskResult{Outputs: map[string]map[string]any{}}}
	ended := w.end(TaskCompleted, "", "")

	if _, _, reason := w.apply(TaskStep{Name: "late"}, StepResult{Output: map[string]any{"late": true}}); reason != reasonTerminal {
		t.Fatalf("expected the terminal refusal, got %q", reason)
	}
	if len(w.result.Outputs) != 0 || len(ended.Outputs) != 0 {
		t.Fatal("an ended task changed")
	}
}

func TestFailureSummariesAreBounded(t *testing.T) {
	result := run(t, taskWorkflow(TaskStep{Name: "fetch", Run: func(context.Context, StepInput) (StepResult, error) {
		return StepResult{}, errors.New(strings.Repeat("source text ", 100))
	}}))

	if result.Status != TaskFailed || !strings.HasPrefix(result.Summary, "step fetch failed") ||
		len([]rune(result.Summary)) > maxTaskSummary+1 {
		t.Fatalf("expected a bounded summary naming the step: %q", result.Summary)
	}
}
