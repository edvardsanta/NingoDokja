//go:build integration

package applicationjournal

import (
	"context"
	"crypto/sha256"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"
	knowledge "read_books/dokja_domain/dokja_knowledge"
	"read_books/internal/core"
	"read_books/internal/handlers"
)

type nativeSourceFixture struct{}

func (nativeSourceFixture) Dispatch(_ context.Context, event string, payload map[string]any) (map[string]any, error) {
	if event != "knowledge.list" || payload["include_text"] != true || payload["limit"] != 1 {
		return nil, fmt.Errorf("unexpected native source operation")
	}
	id, _ := payload["source_id"].(string)
	return map[string]any{"documents": []any{map[string]any{"source_id": id, "text": "Synthetic source validation fixture."}}}, nil
}
func TestPostgresNativeSourceReadThroughOrchestrator(t *testing.T) {
	ctx := context.Background()
	db := database(t)
	store, err := Initialize(ctx, db)
	if err != nil {
		t.Fatal(err)
	}
	scope := "source-fixture:" + uuid.NewString()
	bound, err := handlers.WithSourceReadJournal(ctx, handlers.SourceReadJournal{
		Scope: scope, ApplicationRef: "application:fixture", StateRevision: 3, Store: store, Clock: time.Now,
		Authorize: func(ctx context.Context, p handlers.SourceReadPermission) (string, error) {
			if ctx.Err() != nil || p.Scope != scope || p.ApplicationRef != "application:fixture" || p.StateRevision != 3 || p.Capability != "knowledge.application.source.read" || p.Source.SourceID != "source:fixture" || (p.Purpose != "execute" && p.Purpose != "record") {
				return "", fmt.Errorf("fixture admission denied")
			}
			return "decision:fixture", nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	pin := fmt.Sprintf("sha256:%x", sha256.Sum256([]byte("Synthetic source validation fixture.")))
	event := core.Event{EventID: "event:fixture", Source: core.SourceCLI, Type: "knowledge.application.prepare", Payload: map[string]any{
		"contract": knowledge.ApplicationContract, "goal": "Validate the selected source", "sources": []any{map[string]any{"source_id": "source:fixture", "text_version": pin}},
	}}
	result, err := core.DefaultService(handlers.NewKnowledgeDomainHandler(nativeSourceFixture{})).ProcessWithResult(bound, event)
	if err != nil {
		t.Fatal(err)
	}
	output := result.Result["knowledge"].(map[string]any)
	if output["status"] != "prepared" {
		t.Fatalf("native preparation failed: %v", output)
	}
	task := output["task"].(core.TaskResult)
	if len(task.Receipts) != 1 {
		t.Fatalf("native receipt projection missing: %+v", task)
	}
	var episodeRef string
	if err := db.QueryRowContext(ctx, `SELECT episode_ref FROM dokja_application_journal.episodes WHERE scope_ref=$1 AND attempt_ref=$2`, scope, task.Receipts[0].ActionID).Scan(&episodeRef); err != nil {
		t.Fatal(err)
	}
	// Reopen through a new connection pool, without replaying the source action.
	db.Close()
	db = database(t)
	reopened, err := Open(ctx, db)
	if err != nil {
		t.Fatal(err)
	}
	episode, found, err := reopened.Read(ctx, scope, episodeRef)
	if err != nil || !found {
		t.Fatalf("missing durable source receipt: found=%v err=%v", found, err)
	}
	if episode.Revision != 3 || episode.Attempt.WorkspaceAttemptRef != task.AttemptID || episode.Attempt.WorkspaceTaskRef != task.TaskID || episode.Attempt.Phase != "settled" || episode.Attempt.Sources[0].Hash != pin || len(episode.Observations) != 1 || episode.Observations[0].Outcome != "succeeded" || episode.Observations[0].Observer != "knowledge.source-reader.v1" {
		t.Fatalf("incorrect durable native receipt: %+v", episode)
	}
	if episode.Attempt.Criterion.Ref != "knowledge.application.source.validated" {
		t.Fatal("source validation was promoted to method success")
	}
}
