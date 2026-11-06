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

func TestConversationBoundRetryPreservesExistingConflictAndScopeRules(t *testing.T) {
	s, archive := open(t, filepath.Join(t.TempDir(), FileName))
	held, err := archive.Start(ctx, conversation.OwnerScope, conversation.KindGeneral, "", "")
	if err != nil {
		t.Fatal(err)
	}
	first, _, err := archive.Send(ctx, conversation.OwnerScope, held.ID, "send-0001", "First", conversation.Provenance{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := archive.Fail(ctx, conversation.OwnerScope, first, "Unavailable"); err != nil {
		t.Fatal(err)
	}
	second, _, err := archive.Send(ctx, conversation.OwnerScope, held.ID, "send-0002", "Second", conversation.Provenance{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := archive.Fail(ctx, conversation.OwnerScope, second, "Unavailable"); err != nil {
		t.Fatal(err)
	}
	if _, err := archive.RetryInConversation(ctx, conversation.OwnerScope, held.ID, first.ID); !errors.Is(err, conversation.ErrConflict) {
		t.Fatal("earlier turn in the right conversation must remain a conflict", err)
	}
	if _, err := archive.RetryInConversation(ctx, conversation.OwnerScope, held.ID, "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"); !errors.Is(err, conversation.ErrNotFound) {
		t.Fatal("unknown turn must not become a conflict", err)
	}
	if _, err := s.RetryTurnInConversation(ctx, "other", held.ID, second.ID, time.Now()); !errors.Is(err, conversation.ErrNotFound) {
		t.Fatal("another scope must see no turn", err)
	}
	if _, err := s.RetryTurnInConversation(ctx, conversation.OwnerScope, "", second.ID, time.Now()); !errors.Is(err, conversation.ErrNotFound) {
		t.Fatal("scoped storage operation must not accept an empty conversation", err)
	}
	retried, err := archive.RetryInConversation(ctx, conversation.OwnerScope, held.ID, second.ID)
	if err != nil || retried.ID != second.ID || retried.Attempts != 2 || retried.State != conversation.TurnPending {
		t.Fatal(retried, err)
	}
	if _, err := archive.RetryInConversation(ctx, conversation.OwnerScope, held.ID, second.ID); !errors.Is(err, conversation.ErrConflict) {
		t.Fatal("a second retry must not be admitted", err)
	}
}

// An executor that outlived recovery still holds the attempt it started. Its late result must not
// turn an interrupted turn into an answer nobody can trust, nor make the turn pending again.
func TestALateResultCannotSettleATurnRecoveryInterrupted(t *testing.T) {
	for _, outcome := range []string{"complete", "fail"} {
		t.Run(outcome, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), FileName)
			_, original := open(t, path)
			held, _ := original.Start(ctx, conversation.OwnerScope, conversation.KindGeneral, "", "")
			first, _, err := original.Send(ctx, conversation.OwnerScope, held.ID, "send-0001", "Question", conversation.Provenance{})
			if err != nil {
				t.Fatal(err)
			}
			_, recovered := open(t, path)
			if n, err := recovered.Recover(ctx, time.Now().UTC()); err != nil || n != 1 {
				t.Fatal(n, err)
			}
			if outcome == "complete" {
				_, err = original.Complete(ctx, conversation.OwnerScope, first, "Late answer", "ollama", "fixture/late")
			} else {
				_, err = original.Fail(ctx, conversation.OwnerScope, first, "Late failure")
			}
			if !errors.Is(err, conversation.ErrConflict) {
				t.Fatalf("a late %s after recovery must conflict: %v", outcome, err)
			}
			turns, _ := recovered.Turns(ctx, conversation.OwnerScope, held.ID, 0, 10)
			if got := turns[0]; got.State != conversation.TurnInterrupted || got.Reply != "" || got.Attempts != 1 {
				t.Fatalf("the interrupted turn changed: %+v", got)
			}
		})
	}
}

// A result reported twice for the same attempt is accepted once; the second report changes nothing,
// whether it repeats the answer, brings another one or says the attempt failed.
func TestTheSameAttemptIsSettledOnce(t *testing.T) {
	_, archive := open(t, filepath.Join(t.TempDir(), FileName))
	held, _ := archive.Start(ctx, conversation.OwnerScope, conversation.KindGeneral, "", "")
	turn, _, err := archive.Send(ctx, conversation.OwnerScope, held.ID, "send-0001", "Question", conversation.Provenance{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := archive.Complete(ctx, conversation.OwnerScope, turn, "First answer", "ollama", "fixture/first"); err != nil {
		t.Fatal(err)
	}
	for name, report := range map[string]func() error{
		"the same answer": func() error {
			_, err := archive.Complete(ctx, conversation.OwnerScope, turn, "First answer", "ollama", "fixture/first")
			return err
		},
		"another answer": func() error {
			_, err := archive.Complete(ctx, conversation.OwnerScope, turn, "Second answer", "ollama", "fixture/second")
			return err
		},
		"a failure": func() error {
			_, err := archive.Fail(ctx, conversation.OwnerScope, turn, "Failed after all")
			return err
		},
	} {
		if err := report(); !errors.Is(err, conversation.ErrConflict) {
			t.Fatalf("%s must conflict: %v", name, err)
		}
	}
	turns, _ := archive.Turns(ctx, conversation.OwnerScope, held.ID, 0, 10)
	if got := turns[0]; got.State != conversation.TurnCompleted || got.Reply != "First answer" || got.Provenance.ObservedModel != "fixture/first" {
		t.Fatalf("the accepted answer changed: %+v", got)
	}
}
