package handlers

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	knowledge "read_books/dokja_domain/dokja_knowledge"
	"read_books/internal/core"
	"strings"
	"testing"
	"time"
)

func assessmentEvent(t *testing.T, store *applicationStore) core.Event {
	t.Helper()
	event := applicationEvent()
	event.Type = "knowledge.application.assess"
	request := knowledge.AssessmentRequest{Contract: knowledge.AssessmentContract, Application: knowledge.ApplicationRequest{Contract: knowledge.ApplicationContract, Goal: "Apply the method within the API contract", Constraints: []string{"Do not execute actions"}}}
	cite := func(id, quote string) knowledge.SourceCitation {
		text := store.documents[id]
		sum := sha256.Sum256([]byte(text))
		version := "sha256:" + hex.EncodeToString(sum[:])
		request.Application.Sources = append(request.Application.Sources, knowledge.ApplicationSelection{SourceID: id, TextVersion: version})
		start := strings.Index(text, quote)
		if start < 0 {
			t.Fatal("missing quotation")
		}
		return knowledge.SourceCitation{SourceID: id, TextVersion: version, Start: start, End: start + len(quote), Quote: quote, Relation: "supports"}
	}
	methodCitation := cite("guide", "Use bounded backoff.")
	claimCitation := cite("api", "Reuse a stable key.")
	request.Proposal = knowledge.ApplicationProposal{
		Claims: []knowledge.CandidateClaim{{ID: "idempotency", Statement: "The API supports idempotent retries for the request", Citations: []knowledge.SourceCitation{claimCitation}}},
		Method: knowledge.CandidateMethod{ID: "retry", Description: "Retry using bounded backoff", Prerequisites: []string{"idempotency"}, Citations: []knowledge.SourceCitation{methodCitation}},
	}
	raw, err := json.Marshal(request)
	if err != nil {
		t.Fatal(err)
	}
	event.Payload = nil
	if err = json.Unmarshal(raw, &event.Payload); err != nil {
		t.Fatal(err)
	}
	return event
}
func assessmentStore() *applicationStore {
	return &applicationStore{documents: map[string]string{"guide": "Notation α: Use bounded backoff.", "api": "API guarantee: Reuse a stable key."}}
}
func firstAssessmentCitation(event core.Event) map[string]any {
	return event.Payload["proposal"].(map[string]any)["claims"].([]any)[0].(map[string]any)["citations"].([]any)[0].(map[string]any)
}
func TestAssessmentFlowsThroughOrchestratorAndKeepsPreparationCompatible(t *testing.T) {
	store := assessmentStore()
	event := assessmentEvent(t, store)
	output, err := prepareThroughOrchestrator(context.Background(), store, event)
	if err != nil {
		t.Fatal(err)
	}
	assessment := output["assessment"].(map[string]any)
	if output["status"] != "assessed" || assessment["applicability"] != "conditional" || assessment["can_execute"] != false || assessment["semantic_status"] != "unverified" || assessment["requires_semantic_review"] != true {
		t.Fatalf("unexpected assessment: %v", output)
	}
	task := output["task"].(core.TaskResult)
	if len(task.Steps) != 4 || len(task.Evidence) != 2 || len(task.Proposals) != 0 || len(task.Receipts) != 0 || task.Outputs != nil || task.Scope.ActorID != "test-actor" || strings.Join(store.calls, ",") != "guide,api" {
		t.Fatalf("unexpected task: %+v calls=%v", task, store.calls)
	}
	for _, e := range task.Evidence {
		if e.Scope != (core.TaskScope{}) {
			t.Fatalf("invented source ownership: %+v", e)
		}
	}
	store.calls = nil
	prepared, err := prepareThroughOrchestrator(context.Background(), store, applicationEvent())
	if err != nil {
		t.Fatal(err)
	}
	if prepared["status"] != "prepared" || prepared["application"].(map[string]any)["applicability"] != "unassessed" || prepared["assessment"] != nil {
		t.Fatalf("preparation behavior changed: %v", prepared)
	}
}
func TestAssessmentPublishesNoResultForChangedOrInventedEvidence(t *testing.T) {
	for _, change := range []string{"stale", "invented"} {
		t.Run(change, func(t *testing.T) {
			store := assessmentStore()
			event := assessmentEvent(t, store)
			if change == "stale" {
				store.documents["api"] = "Changed API contract"
			} else {
				c := firstAssessmentCitation(event)
				c["quote"] = strings.Repeat("x", len(c["quote"].(string)))
			}
			output, err := prepareThroughOrchestrator(context.Background(), store, event)
			if err != nil {
				t.Fatal(err)
			}
			task := output["task"].(core.TaskResult)
			if output["status"] != "not_assessed" || output["assessment"] != nil || output["application"] != nil || task.Outputs != nil || task.Status != core.TaskFailed {
				t.Fatalf("published invalid/partial assessment: %v", output)
			}
		})
	}
}
func TestAssessmentRejectsForeignReferenceBeforeStoreRead(t *testing.T) {
	store := assessmentStore()
	event := assessmentEvent(t, store)
	firstAssessmentCitation(event)["source_id"] = "foreign"
	if _, err := prepareThroughOrchestrator(context.Background(), store, event); err == nil || len(store.calls) != 0 {
		t.Fatalf("invalid reference reached storage: %v %v", err, store.calls)
	}
}
func TestAssessmentDoesNotExposePreparationAfterCancellation(t *testing.T) {
	store := assessmentStore()
	event := assessmentEvent(t, store)
	release := make(chan struct{})
	store.block = release
	store.started = make(chan struct{})
	store.finished = make(chan struct{})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan map[string]any, 1)
	errors := make(chan error, 1)
	go func() {
		output, err := prepareThroughOrchestrator(ctx, store, event)
		if err != nil {
			errors <- err
			return
		}
		done <- output
	}()
	select {
	case <-store.started:
	case <-time.After(2 * time.Second):
		t.Fatal("store did not start")
	}
	cancel()
	select {
	case err := <-errors:
		t.Fatal(err)
	case output := <-done:
		if output["status"] != "not_assessed" || output["assessment"] != nil || output["task"].(core.TaskResult).Status != core.TaskCancelled {
			t.Fatalf("published late assessment: %v", output)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("task waited for noncooperative store")
	}
	close(release)
	select {
	case <-store.finished:
	case <-time.After(2 * time.Second):
		t.Fatal("store did not finish")
	}
}
func TestAssessmentHonorsKnowledgeControl(t *testing.T) {
	store := assessmentStore()
	event := assessmentEvent(t, store)
	controls, err := core.NewControls("")
	if err != nil {
		t.Fatal(err)
	}
	if err = controls.SetService("knowledge", false); err != nil {
		t.Fatal(err)
	}
	result, err := core.DefaultService(NewKnowledgeDomainHandler(store)).WithControls(controls).ProcessWithResult(context.Background(), event)
	if err != nil {
		t.Fatal(err)
	}
	if result.Result["skipped"] != true || len(store.calls) != 0 {
		t.Fatalf("disabled service was read: %v", result)
	}
}
