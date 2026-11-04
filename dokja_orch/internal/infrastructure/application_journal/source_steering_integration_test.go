//go:build integration

package applicationjournal

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"strings"
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

func TestPostgresDesktopRPCSteeringAndCredentialBoundary(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	db := database(t)
	store, err := Initialize(ctx, db)
	if err != nil {
		t.Fatal(err)
	}
	release := make(chan struct{})
	var once sync.Once
	unblock := func() { once.Do(func() { close(release) }) }
	defer unblock()
	started := make(chan struct{})
	handler := handlers.NewKnowledgeDomainHandler(nativeSteeringFixture{started: started, release: release})
	host := handlers.NewDesktopSourceApplications(ctx, handler, store, func() bool { return true })
	handler.WithSourceApplications(host)
	token := strings.Repeat("t", 40)
	service := core.DefaultService(handler).WithSourceSupervisionToken(token)
	request := func(kind string, payload map[string]any) map[string]any {
		t.Helper()
		result, err := service.ProcessWithResult(ctx, core.Event{Source: core.SourceDesktop, Type: "knowledge.application.supervision." + kind, Payload: payload, Context: map[string]any{"source_supervision_token": token, "is_owner": true}, User: core.EventUser{ID: "untrusted-identity"}})
		if err != nil {
			t.Fatal(err)
		}
		raw, _ := json.Marshal(result)
		if strings.Contains(string(raw), token) {
			t.Fatal("credential leaked through RPC result")
		}
		return result.Result["knowledge"].(map[string]any)
	}
	caps := request("status", map[string]any{})
	pin := fmt.Sprintf("sha256:%x", sha256.Sum256([]byte("Synthetic source validation fixture.")))
	application := map[string]any{"contract": knowledge.ApplicationContract, "goal": "Validate the selected source", "constraints": []string{}, "sources": []any{map[string]any{"source_id": "source:old", "text_version": pin}}}
	first := request("start", map[string]any{"operation_ref": "start:rpc", "host_ref": caps["host_ref"], "application": application})
	ref := first["application_ref"].(string)
	select {
	case <-started:
	case <-time.After(3 * time.Second):
		t.Fatal("native read did not begin")
	}
	pause := map[string]any{"application_ref": ref, "operation_ref": "control:pause", "expected_revision": 1, "action": "pause", "reason": "Inspect the current approach"}
	snapshot := request("control", pause)
	if snapshot["state"] != "paused" {
		t.Fatalf("pause did not reach the real supervisor: %v", snapshot)
	}
	replay := request("control", pause)
	if replay["control_revision"] != snapshot["control_revision"] {
		t.Fatal("duplicate control advanced the revision")
	}
	unblock()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		snapshot = request("status", map[string]any{"application_ref": ref})
		if snapshot["pending"].(int) == 0 {
			break
		}
		time.Sleep(time.Millisecond)
	}
	candidate := map[string]any{"contract": knowledge.ApplicationContract, "goal": "Check a corrected source", "constraints": []string{}, "sources": []any{map[string]any{"source_id": "source:new"}}}
	checked := request("preview", map[string]any{"application_ref": ref, "expected_revision": 2, "application": candidate})
	if checked["status"] != "prepared" {
		t.Fatal("paused preview did not use the real source port")
	}
	snapshot = request("status", map[string]any{"application_ref": ref})
	if snapshot["read_attempts"] != 2 || snapshot["control_revision"] != uint64(2) {
		t.Fatalf("preview escaped application budget or advanced control: %v", snapshot)
	}
	receipts := snapshot["receipts"].([]any)
	if len(receipts) != 1 || receipts[0].(map[string]any)["confirmed"] != true || receipts[0].(map[string]any)["source_id"] != "source:old" {
		t.Fatalf("Desktop projection lost actual native receipt: %v", snapshot)
	}
	var episode string
	if err := db.QueryRow(`SELECT episode_ref FROM dokja_application_journal.episodes WHERE attempt_ref=$1`, receipts[0].(map[string]any)["attempt_ref"]).Scan(&episode); err != nil {
		t.Fatal(err)
	}
	recorded, found, err := store.Read(ctx, "desktop:operator", episode)
	if err != nil || !found || recorded.Attempt.AuthorizationRef != "desktop:source-read" {
		t.Fatalf("missing private operator receipt: %+v %v", recorded, err)
	}
	request("control", map[string]any{"application_ref": ref, "operation_ref": "control:cancel", "expected_revision": 2, "action": "cancel"})
}
