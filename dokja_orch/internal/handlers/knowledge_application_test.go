package handlers

import (
	"context"
	"fmt"
	knowledge "read_books/dokja_domain/dokja_knowledge"
	"read_books/internal/core"
	"strings"
	"testing"
	"time"
)

type applicationStore struct {
	documents map[string]string
	calls     []string
	fail      string
	block     <-chan struct{}
	started   chan struct{}
	finished  chan struct{}
}

func (s *applicationStore) Dispatch(_ context.Context, event string, payload map[string]any) (map[string]any, error) {
	if s.block != nil {
		close(s.started)
		<-s.block
		defer close(s.finished)
	}
	id, _ := payload["source_id"].(string)
	s.calls = append(s.calls, id)
	if event != "knowledge.list" || payload["include_text"] != true || payload["limit"] != 1 {
		return nil, fmt.Errorf("unexpected port operation")
	}
	if id == s.fail {
		return nil, fmt.Errorf("private failure diagnostic")
	}
	if text, ok := s.documents[id]; ok {
		return map[string]any{"documents": []any{map[string]any{"source_id": id, "source_ref": "file:" + id, "title": id, "text": text}}}, nil
	}
	return map[string]any{"documents": []any{}}, nil
}
func applicationEvent() core.Event {
	return core.Event{EventID: "application-event", Source: core.SourceCLI, Type: "knowledge.application.prepare", User: core.EventUser{ID: "test-actor"}, Channel: core.EventChannel{ID: "test-channel"}, Payload: map[string]any{
		"contract": knowledge.ApplicationContract, "goal": "Determine when the retry method is applicable to this API", "constraints": []string{"No external calls or writes"},
		"sources": []any{map[string]any{"source_id": "guide"}, map[string]any{"source_id": "api"}},
	}}
}
func prepareThroughOrchestrator(ctx context.Context, store *applicationStore, event core.Event) (map[string]any, error) {
	result, err := core.DefaultService(NewKnowledgeDomainHandler(store)).ProcessWithResult(ctx, event)
	if err != nil {
		return nil, err
	}
	output, ok := result.Result["knowledge"].(map[string]any)
	if !ok {
		return nil, fmt.Errorf("missing Knowledge result: %v", result)
	}
	return output, nil
}
func TestPrepareApplicationReadsBothDocumentsThroughApplicationPorts(t *testing.T) {
	store := &applicationStore{documents: map[string]string{"guide": "Retry idempotent operations with backoff.", "api": "POST is idempotent only with a stable request key."}}
	output, err := prepareThroughOrchestrator(context.Background(), store, applicationEvent())
	if err != nil {
		t.Fatal(err)
	}
	if output["status"] != "prepared" || strings.Join(store.calls, ",") != "guide,api" {
		t.Fatalf("unexpected preparation: %v calls=%v", output, store.calls)
	}
	app := output["application"].(map[string]any)
	if app["applicability"] != "unassessed" || app["experience_status"] != "not_consulted" {
		t.Fatalf("invented assessment: %v", app)
	}
	sources := app["sources"].([]any)
	if len(sources) != 2 || sources[0].(map[string]any)["text"] != store.documents["guide"] {
		t.Fatalf("lost source evidence: %v", sources)
	}
	task := output["task"].(core.TaskResult)
	if task.Status != core.TaskCompleted || len(task.Steps) != 3 || len(task.Evidence) != 2 || task.Outputs != nil || len(task.Receipts) != 0 || len(task.Proposals) != 0 {
		t.Fatalf("unexpected task: %+v", task)
	}
	if task.Scope.Source != core.SourceCLI || task.Scope.ActorID != "test-actor" || task.Scope.ChannelID != "test-channel" || task.EventID != "application-event" {
		t.Fatalf("lost ingress identity: %+v", task)
	}
	for _, evidence := range task.Evidence {
		if evidence.Scope != (core.TaskScope{}) || !strings.HasPrefix(evidence.Version, "sha256:") {
			t.Fatalf("invented ownership or missing version: %+v", evidence)
		}
	}
	// A preparation can explicitly pin the versions it just observed.
	event := applicationEvent()
	selections := event.Payload["sources"].([]any)
	for i, source := range sources {
		selections[i].(map[string]any)["text_version"] = source.(map[string]any)["text_version"]
	}
	store.documents["api"] = "A revised API contract."
	output, err = prepareThroughOrchestrator(context.Background(), store, event)
	if err != nil {
		t.Fatal(err)
	}
	if output["status"] != "not_prepared" || output["application"] != nil || output["task"].(core.TaskResult).Outputs != nil {
		t.Fatalf("published stale/partial context: %v", output)
	}
}
func TestPrepareApplicationStopsOnMissingSourcesAndStoreFailure(t *testing.T) {
	for _, fail := range []string{"", "api"} {
		store := &applicationStore{documents: map[string]string{"guide": "Guide text"}, fail: fail}
		output, err := prepareThroughOrchestrator(context.Background(), store, applicationEvent())
		if err != nil {
			t.Fatal(err)
		}
		task := output["task"].(core.TaskResult)
		if output["status"] != "not_prepared" || output["application"] != nil || task.Status != core.TaskFailed || task.Outputs != nil || strings.Contains(task.Summary, "private") {
			t.Fatalf("unsafe partial context: %v", output)
		}
	}
}
func TestPrepareApplicationRejectsPayloadBeforeReading(t *testing.T) {
	store := &applicationStore{}
	event := applicationEvent()
	event.Payload["scope"] = "foreign"
	if _, err := prepareThroughOrchestrator(context.Background(), store, event); err == nil || len(store.calls) != 0 {
		t.Fatalf("invalid payload reached storage: %v %v", err, store.calls)
	}
}
func TestPrepareApplicationAggregateBudget(t *testing.T) {
	store := &applicationStore{documents: map[string]string{}}
	event := applicationEvent()
	selections := []any{}
	for i := 0; i < 5; i++ {
		id := fmt.Sprintf("source-%d", i)
		selections = append(selections, map[string]any{"source_id": id})
		store.documents[id] = strings.Repeat("x", knowledge.MaxApplicationSourceBytes)
	}
	event.Payload["sources"] = selections
	output, err := prepareThroughOrchestrator(context.Background(), store, event)
	if err != nil {
		t.Fatal(err)
	}
	if output["status"] != "not_prepared" || output["application"] != nil {
		t.Fatalf("oversized context published: %v", output)
	}
}
func TestPrepareApplicationDiscardsLateStoreResponse(t *testing.T) {
	release := make(chan struct{})
	store := &applicationStore{documents: map[string]string{"guide": "Late text"}, block: release, started: make(chan struct{}), finished: make(chan struct{})}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	type answer struct {
		result map[string]any
		err    error
	}
	done := make(chan answer, 1)
	go func() {
		output, err := prepareThroughOrchestrator(ctx, store, applicationEvent())
		done <- answer{output, err}
	}()
	select {
	case <-store.started:
	case <-time.After(2 * time.Second):
		t.Fatal("store did not start")
	}
	cancel()
	select {
	case result := <-done:
		if result.err != nil {
			t.Fatal(result.err)
		}
		task := result.result["task"].(core.TaskResult)
		if task.Status != core.TaskCancelled || result.result["application"] != nil || task.Outputs != nil {
			t.Fatalf("accepted cancelled context: %v", result.result)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("task waited for noncooperative store")
	}
	close(release)
	select {
	case <-store.finished:
	case <-time.After(2 * time.Second):
		t.Fatal("late read did not finish")
	}
}
func TestPrepareApplicationHonorsKnowledgeServiceControl(t *testing.T) {
	store := &applicationStore{}
	controls, err := core.NewControls("")
	if err != nil {
		t.Fatal(err)
	}
	if err = controls.SetService("knowledge", false); err != nil {
		t.Fatal(err)
	}
	result, err := core.DefaultService(NewKnowledgeDomainHandler(store)).WithControls(controls).ProcessWithResult(context.Background(), applicationEvent())
	if err != nil {
		t.Fatal(err)
	}
	if result.Result["skipped"] != true || len(store.calls) != 0 {
		t.Fatalf("disabled service was read: %v", result)
	}
}
