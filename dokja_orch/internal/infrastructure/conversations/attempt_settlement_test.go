package conversations

import (
	"errors"
	"path/filepath"
	"testing"
	"time"

	conversation "read_books/dokja_domain/dokja_conversation"
)

func TestOldAttemptCannotSettleARetriedTurn(t *testing.T) {
	for _, recovery := range []bool{false, true} {
		for _, outcome := range []string{"complete", "fail"} {
			name := outcome
			if recovery {
				name += "_after_recovery"
			}
			t.Run(name, func(t *testing.T) {
				path := filepath.Join(t.TempDir(), FileName)
				_, original := open(t, path)
				held, err := original.Start(ctx, conversation.OwnerScope, conversation.KindGeneral, "", "Attempt identity")
				if err != nil {
					t.Fatal(err)
				}
				first, replayed, err := original.Send(ctx, conversation.OwnerScope, held.ID, "send-0001", "Question", conversation.Provenance{})
				if err != nil || replayed {
					t.Fatal(first, replayed, err)
				}
				// A second connection represents the authority processing recovery/retry;
				// the old executor still holds the first attempt's snapshot.
				_, current := open(t, path)
				if recovery {
					if n, err := current.Recover(ctx, time.Now().UTC()); err != nil || n != 1 {
						t.Fatal(n, err)
					}
				} else if _, err := original.Fail(ctx, conversation.OwnerScope, first, "First attempt failed"); err != nil {
					t.Fatal(err)
				}
				second, err := current.Retry(ctx, conversation.OwnerScope, first.ID)
				if err != nil || second.ID != first.ID || second.SendID != first.SendID || second.Attempts != 2 {
					t.Fatal(second, err)
				}
				var stale conversation.Turn
				if outcome == "complete" {
					stale, err = original.Complete(ctx, conversation.OwnerScope, first, "Old answer", "ollama", "fixture/old")
				} else {
					stale, err = original.Fail(ctx, conversation.OwnerScope, first, "Old failure")
				}
				if !errors.Is(err, conversation.ErrConflict) {
					t.Fatalf("old %s must conflict with retry: %+v, %v", outcome, stale, err)
				}
				turns, err := current.Turns(ctx, conversation.OwnerScope, held.ID, 0, 10)
				if err != nil || len(turns) != 1 {
					t.Fatal(turns, err)
				}
				pending := turns[0]
				if pending.State != conversation.TurnPending || pending.Attempts != 2 || pending.Reply != "" || pending.Reason != "" || pending.Provenance.ObservedProvider != "" || !pending.FinishedAt.IsZero() {
					t.Fatalf("stale outcome changed current attempt: %+v", pending)
				}
				done, err := current.Complete(ctx, conversation.OwnerScope, second, "Current answer", "ollama", "fixture/current")
				if err != nil || done.State != conversation.TurnCompleted || done.Attempts != 2 || done.Reply != "Current answer" {
					t.Fatal(done, err)
				}
				if _, err := original.Fail(ctx, conversation.OwnerScope, first, "Late failure after completion"); !errors.Is(err, conversation.ErrConflict) {
					t.Fatal("old failure must not replace current completion", err)
				}
				replayedTurn, replayed, err := current.Send(ctx, conversation.OwnerScope, held.ID, first.SendID, first.Content, conversation.Provenance{})
				if err != nil || !replayed || replayedTurn.ID != first.ID || replayedTurn.Reply != "Current answer" || replayedTurn.Provenance.ObservedModel != "fixture/current" {
					t.Fatalf("duplicate send must return current completion: %+v, %v, %v", replayedTurn, replayed, err)
				}
			})
		}
	}
}
