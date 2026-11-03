//go:build integration

package applicationjournal

import (
	"context"
	"crypto/sha256"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	knowledge "read_books/dokja_domain/dokja_knowledge"
	"read_books/internal/core"
	"read_books/internal/handlers"
)

type steeringIdentityKey struct{}
type nativeSteeringFixture struct {
	started chan struct{}
	release <-chan struct{}
}

func (s nativeSteeringFixture) Dispatch(ctx context.Context, event string, payload map[string]any) (map[string]any, error) {
	if payload["source_id"] == "source:old" {
		close(s.started)
		<-s.release
	}
	return (nativeSourceFixture{}).Dispatch(ctx, event, payload)
}
func TestPostgresSupervisedRedirectRetainsOriginalAttempt(t *testing.T) {
	ctx := context.WithValue(context.Background(), steeringIdentityKey{}, "fixture-operator")
	db := database(t)
	store, err := Initialize(ctx, db)
	if err != nil {
		t.Fatal(err)
	}
	scope := "steering-fixture:" + uuid.NewString()
	release := make(chan struct{})
	var once sync.Once
	unblock := func() { once.Do(func() { close(release) }) }
	defer unblock()
	started := make(chan struct{})
	host, err := handlers.NewSourceSupervisor(ctx, handlers.NewKnowledgeDomainHandler(nativeSteeringFixture{started: started, release: release}), handlers.SourceSupervisorOptions{
		Scope: scope, Store: store, Clock: time.Now,
		Authorize: func(ctx context.Context, p handlers.SourceSupervisorPermission) (handlers.SourceSupervisorActor, error) {
			if ctx.Value(steeringIdentityKey{}) != "fixture-operator" || p.Scope != scope {
				return handlers.SourceSupervisorActor{}, handlers.ErrSourceControlDenied
			}
			return handlers.SourceSupervisorActor{Ref: "actor:fixture", DecisionRef: "decision:fixture"}, nil
		},
		AuthorizeSource: func(ctx context.Context, p handlers.SourceReadPermission) (string, error) {
			if ctx.Value(steeringIdentityKey{}) != "fixture-operator" || p.Scope != scope || p.PlanRevision == 0 {
				return "", handlers.ErrSourceControlDenied
			}
			return "decision:source", nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	pin := fmt.Sprintf("sha256:%x", sha256.Sum256([]byte("Synthetic source validation fixture.")))
	event := core.Event{Source: core.SourceCLI, Type: "knowledge.application.prepare", Payload: map[string]any{"contract": knowledge.ApplicationContract, "goal": "Validate the original source", "sources": []any{map[string]any{"source_id": "source:old", "text_version": pin}}}}
	if _, err := host.Start(ctx, event); err != nil {
		t.Fatal(err)
	}
	select {
	case <-started:
	case <-time.After(3 * time.Second):
		t.Fatal("source read did not start")
	}
	replacement := knowledge.ApplicationRequest{Contract: knowledge.ApplicationContract, Goal: "Validate the corrected source", Sources: []knowledge.ApplicationSelection{{SourceID: "source:new", TextVersion: pin}}}
	ack, err := host.Control(ctx, knowledge.SourceControl{Action: "redirect", Reason: "The original source is the wrong version", Application: &replacement, OperationRef: "control:redirect", ExpectedRevision: 1})
	if err != nil || ack.State != "reassessing" || ack.PlanRevision != 2 {
		t.Fatalf("unexpected acknowledgment: %+v %v", ack, err)
	}
	unblock()
	var snapshot handlers.SourceSupervisorSnapshot
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		snapshot, err = host.Status(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if snapshot.State == "prepared" {
			break
		}
		time.Sleep(time.Millisecond)
	}
	if snapshot.State != "prepared" || len(snapshot.Settlements) != 2 || snapshot.Prepared["goal"] != replacement.Goal || snapshot.Controls[0].ActorRef != "actor:fixture" {
		t.Fatalf("supervision did not prepare the corrected plan: %+v", snapshot)
	}
	db.Close()
	db = database(t)
	reopened, err := Open(ctx, db)
	if err != nil {
		t.Fatal(err)
	}
	for index, receipt := range snapshot.Settlements {
		episode, found, err := reopened.Read(ctx, scope, receipt.EpisodeRef)
		if err != nil || !found || episode.Attempt.StateRevision != uint64(index+1) || len(episode.Observations) != 1 || episode.Observations[0].Outcome != "succeeded" || episode.Attempt.WorkspaceAttemptRef != receipt.WorkspaceAttemptRef {
			t.Fatalf("original native receipt did not survive reopen: %+v %v", episode, err)
		}
		if receipt.PlanRevision != uint64(index+1) || !receipt.Confirmed {
			t.Fatalf("receipt was relabelled: %+v", receipt)
		}
	}
	if snapshot.Settlements[0].WorkspaceAttemptRef == snapshot.Settlements[1].WorkspaceAttemptRef {
		t.Fatal("redirect reused the old workspace attempt")
	}
}
