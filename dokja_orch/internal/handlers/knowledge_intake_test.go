package handlers

import (
	"context"
	"errors"
	"read_books/internal/core"
	"testing"
	"time"
)

type fakeFeeds struct {
	items   []any
	err     error
	calls   []string
	started chan struct{}
	release chan struct{}
}

func (f *fakeFeeds) Dispatch(_ context.Context, eventType string, _ map[string]any) (map[string]any, error) {
	f.calls = append(f.calls, eventType)
	if f.started != nil {
		f.started <- struct{}{}
		<-f.release
	}
	return map[string]any{"items": f.items}, f.err
}

func intakeFeedItem(id string) map[string]any {
	return map[string]any{
		"plugin": "example-blog", "id": id, "title": "A post", "body": "Its text",
		"published": time.Now().UTC().Add(-time.Hour).Format(time.RFC3339),
	}
}

func TestIntakeStoresChosenSourcesWithoutRefreshingThem(t *testing.T) {
	feeds := &fakeFeeds{items: []any{intakeFeedItem("kept"), map[string]any{"plugin": "other-source", "id": "no"}}}
	knowledge := &ingestRecorder{changed: true}
	task := NewIntakeTask(feeds, knowledge, []string{"example-blog"}, nil)

	result, run, err := task.run(context.Background(), core.Event{EventID: "event-1", Type: "knowledge.intake.run"})
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != core.TaskCompleted {
		t.Fatalf("status %s reason %s", result.Status, result.Reason)
	}
	if len(feeds.calls) != 1 || feeds.calls[0] != "feeds.items" {
		t.Fatalf("feeds calls: %v", feeds.calls)
	}
	if len(knowledge.payloads) != 1 || run.outcome.Stored != 1 || run.outcome.Changed != 1 {
		t.Fatalf("payloads=%d outcome=%+v", len(knowledge.payloads), run.outcome)
	}
}

func TestIntakeReportsFeedsBeingDown(t *testing.T) {
	task := NewIntakeTask(&fakeFeeds{err: errors.New("down")}, &ingestRecorder{}, []string{"example-blog"}, nil)
	result, _, err := task.run(context.Background(), core.Event{EventID: "event-1", Type: "knowledge.intake.run"})
	if err != nil {
		t.Fatal(err)
	}
	if result.Status == core.TaskCompleted || result.Reason != "feeds_unavailable" {
		t.Fatalf("status %s reason %s", result.Status, result.Reason)
	}
}

func TestIntakeDoesNothingUntilSourcesAreChosen(t *testing.T) {
	feeds := &fakeFeeds{}
	answer, err := NewIntakeTask(feeds, &ingestRecorder{}, nil, nil).Start(core.Event{})
	if err != nil || answer["reason"] != "no_sources" {
		t.Fatalf("answer=%v err=%v", answer, err)
	}
	if len(feeds.calls) != 0 {
		t.Fatalf("feeds was called: %v", feeds.calls)
	}
}

func TestIntakeRespectsFeedsBeingSwitchedOff(t *testing.T) {
	feeds := &fakeFeeds{}
	task := NewIntakeTask(feeds, &ingestRecorder{}, []string{"example-blog"}, func() bool { return false })
	answer, err := task.Start(core.Event{})
	if err != nil || answer["reason"] != "feeds_switched_off" || len(feeds.calls) != 0 {
		t.Fatalf("answer=%v err=%v calls=%v", answer, err, feeds.calls)
	}
}

func TestKnowledgeHandlerServesTheIntakeAction(t *testing.T) {
	handler := NewKnowledgeDomainHandler(&ingestRecorder{}).WithIntake(NewIntakeTask(&fakeFeeds{}, &ingestRecorder{}, nil, nil))
	answer, err := handler.Handle(context.Background(), core.Event{Type: "knowledge.intake.run"}, core.WorkflowStep{Action: "intake-knowledge"})
	if err != nil || answer["reason"] != "no_sources" {
		t.Fatalf("answer=%v err=%v", answer, err)
	}
}

func TestIntakeAnswersAtOnceAndRunsOneAtATime(t *testing.T) {
	feeds := &fakeFeeds{started: make(chan struct{}), release: make(chan struct{})}
	task := NewIntakeTask(feeds, &ingestRecorder{}, []string{"example-blog"}, nil)
	event := core.Event{EventID: "event-1", Type: "knowledge.intake.run"}

	answer, err := task.Start(event)
	if err != nil || answer["started"] != true {
		t.Fatalf("answer=%v err=%v", answer, err)
	}
	<-feeds.started // the first run is inside Feeds, and Start has already answered
	second, err := task.Start(event)
	if err != nil || second["reason"] != "already_running" {
		t.Fatalf("second=%v err=%v", second, err)
	}
	close(feeds.release)
	deadline := time.Now().Add(5 * time.Second)
	for task.running.Load() {
		if time.Now().After(deadline) {
			t.Fatal("the run did not end")
		}
		time.Sleep(time.Millisecond)
	}
}
