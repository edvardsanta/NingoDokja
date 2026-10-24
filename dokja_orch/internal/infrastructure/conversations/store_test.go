package conversations

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"path/filepath"
	"sync"
	"testing"
	"time"

	conversation "read_books/dokja_domain/dokja_conversation"
)

var ctx = context.Background()

func open(t *testing.T, path string) (*Store, *conversation.Archive) {
	t.Helper()
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s, conversation.New(s)
}

func TestAConversationSurvivesARestartWithItsIDsAndOrder(t *testing.T) {
	path := filepath.Join(t.TempDir(), FileName)
	s, a := open(t, path)
	c, err := a.Start(ctx, conversation.OwnerScope, conversation.KindGeneral, "", "On reading")
	if err != nil {
		t.Fatal(err)
	}
	first, _, _ := a.Send(ctx, conversation.OwnerScope, c.ID, "send-0001", "What is a reader?", conversation.Provenance{SelectedProvider: "ollama", SelectedModel: "qwen3:8b"})
	a.Complete(ctx, conversation.OwnerScope, first, "One who judges.", "ollama", "qwen3:8b")
	second, _, _ := a.Send(ctx, conversation.OwnerScope, c.ID, "send-0002", "And a writer?", conversation.Provenance{})
	a.Fail(ctx, conversation.OwnerScope, second, "chat inference is unavailable")
	s.Close()

	_, again := open(t, path)
	stored, err := again.Get(ctx, conversation.OwnerScope, c.ID)
	if err != nil || stored.Title != "On reading" || stored.Turns != 2 || stored.Revision != 3 {
		t.Fatal(stored, err)
	}
	turns, err := again.Turns(ctx, conversation.OwnerScope, c.ID, 0, 10)
	if err != nil || len(turns) != 2 || turns[0].ID != first.ID || turns[0].Seq != 1 || turns[0].Reply != "One who judges." || turns[0].Provenance.ObservedModel != "qwen3:8b" {
		t.Fatal(turns, err)
	}
	if turns[1].State != conversation.TurnFailed || turns[1].Reason != "chat inference is unavailable" || turns[1].Reply != "" {
		t.Fatal("a failed send stays visibly failed", turns[1])
	}
}

func TestARetriedSendReturnsItsTurnAndNeverMakesAnother(t *testing.T) {
	_, a := open(t, filepath.Join(t.TempDir(), FileName))
	c, _ := a.Start(ctx, conversation.OwnerScope, conversation.KindGeneral, "", "")
	turn, replayed, _ := a.Send(ctx, conversation.OwnerScope, c.ID, "send-0001", "q", conversation.Provenance{})
	if replayed {
		t.Fatal("the first send is no replay")
	}
	a.Complete(ctx, conversation.OwnerScope, turn, "the answer", "", "")
	again, replayed, err := a.Send(ctx, conversation.OwnerScope, c.ID, "send-0001", "q", conversation.Provenance{})
	if err != nil || !replayed || again.ID != turn.ID || again.State != conversation.TurnCompleted || again.Reply != "the answer" {
		t.Fatal(again, replayed, err)
	}
	if _, err := a.Complete(ctx, conversation.OwnerScope, turn, "a second answer", "", ""); !errors.Is(err, conversation.ErrConflict) {
		t.Fatal("a completed turn keeps its answer", err)
	}
	if _, err := a.Fail(ctx, conversation.OwnerScope, turn, "late failure"); !errors.Is(err, conversation.ErrConflict) {
		t.Fatal("a completed turn is never overwritten by a failure", err)
	}
	stored, _ := a.Get(ctx, conversation.OwnerScope, c.ID)
	turns, _ := a.Turns(ctx, conversation.OwnerScope, c.ID, 0, 10)
	if stored.Turns != 1 || len(turns) != 1 || turns[0].Reply != "the answer" {
		t.Fatal(stored, turns)
	}
}

func TestConcurrentSendsTakeDistinctPlaces(t *testing.T) {
	_, a := open(t, filepath.Join(t.TempDir(), FileName))
	c, _ := a.Start(ctx, conversation.OwnerScope, conversation.KindGeneral, "", "")
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			if _, _, err := a.Send(ctx, conversation.OwnerScope, c.ID, fmt.Sprintf("send-%04d", i), "q", conversation.Provenance{}); err != nil {
				t.Error(err)
			}
		}(i)
	}
	wg.Wait()
	turns, _ := a.Turns(ctx, conversation.OwnerScope, c.ID, 0, 100)
	if len(turns) != 20 {
		t.Fatal(len(turns))
	}
	for i, turn := range turns {
		if turn.Seq != i+1 {
			t.Fatal("places must run 1..20 without gaps or repeats", i, turn.Seq)
		}
	}
}

func TestAnotherScopeSeesNothingAndChangesNothing(t *testing.T) {
	s, a := open(t, filepath.Join(t.TempDir(), FileName))
	c, _ := a.Start(ctx, conversation.OwnerScope, conversation.KindGeneral, "", "private")
	turn, _, _ := a.Send(ctx, conversation.OwnerScope, c.ID, "send-0001", "q", conversation.Provenance{})
	// The domain refuses any other scope itself; the store must hold too, should a caller skip it.
	other := conversation.Scope("someone-else")
	if _, err := s.Get(ctx, other, c.ID); !errors.Is(err, conversation.ErrNotFound) {
		t.Fatal(err)
	}
	if items, _, _ := s.List(ctx, other, "", 10); len(items) != 0 {
		t.Fatal(items)
	}
	if _, err := s.Turns(ctx, other, c.ID, 0, 10); !errors.Is(err, conversation.ErrNotFound) {
		t.Fatal(err)
	}
	if _, _, err := s.BeginTurn(ctx, other, conversation.Turn{ID: "0123456789abcdef0123456789abcdef", ConversationID: c.ID, SendID: "send-9999", Content: "x", State: conversation.TurnPending, CreatedAt: time.Now()}); !errors.Is(err, conversation.ErrNotFound) {
		t.Fatal(err)
	}
	if _, err := s.RetryTurn(ctx, other, turn.ID, time.Now()); !errors.Is(err, conversation.ErrNotFound) {
		t.Fatal(err)
	}
	if _, err := s.FinishTurn(ctx, other, conversation.Turn{ID: turn.ID, State: conversation.TurnCompleted, Reply: "stolen", FinishedAt: time.Now()}); !errors.Is(err, conversation.ErrNotFound) {
		t.Fatal(err)
	}
	if _, err := s.Rename(ctx, other, c.ID, c.Revision, "x", time.Now()); !errors.Is(err, conversation.ErrNotFound) {
		t.Fatal(err)
	}
	if err := s.Delete(ctx, other, c.ID, 2); !errors.Is(err, conversation.ErrNotFound) {
		t.Fatal(err)
	}
	turns, _ := a.Turns(ctx, conversation.OwnerScope, c.ID, 0, 10)
	if len(turns) != 1 || turns[0].State != conversation.TurnPending {
		t.Fatal(turns)
	}
}

func TestTheListPagesNewestFirstWithoutRepeatsOrGaps(t *testing.T) {
	_, a := open(t, filepath.Join(t.TempDir(), FileName))
	made := map[string]bool{}
	for i := 0; i < 7; i++ {
		c, _ := a.Start(ctx, conversation.OwnerScope, conversation.KindGeneral, "", fmt.Sprintf("c%d", i))
		made[c.ID] = true
	}
	seen := []string{}
	cursor := ""
	for pages := 0; pages < 10; pages++ {
		items, next, err := a.List(ctx, conversation.OwnerScope, cursor, 3)
		if err != nil {
			t.Fatal(err)
		}
		for _, c := range items {
			seen = append(seen, c.Title)
			delete(made, c.ID)
		}
		if next == "" {
			break
		}
		cursor = next
	}
	if len(seen) != 7 || len(made) != 0 || seen[0] != "c6" || seen[6] != "c0" {
		t.Fatal(seen)
	}
}

func TestRenameAndDeleteNeedTheCurrentRevisionAndDeleteTakesTheTurns(t *testing.T) {
	s, a := open(t, filepath.Join(t.TempDir(), FileName))
	c, _ := a.Start(ctx, conversation.OwnerScope, conversation.KindReading, "reading-1", "")
	a.Send(ctx, conversation.OwnerScope, c.ID, "send-0001", "q", conversation.Provenance{})
	if _, err := a.Rename(ctx, conversation.OwnerScope, c.ID, c.Revision, "stale"); !errors.Is(err, conversation.ErrConflict) {
		t.Fatal("the send moved the revision", err)
	}
	current, _ := a.Get(ctx, conversation.OwnerScope, c.ID)
	renamed, err := a.Rename(ctx, conversation.OwnerScope, c.ID, current.Revision, "On one reading")
	if err != nil || renamed.Title != "On one reading" || renamed.Revision != current.Revision+1 || renamed.ReadingID != "reading-1" {
		t.Fatal(renamed, err)
	}
	if err := a.Delete(ctx, conversation.OwnerScope, c.ID, current.Revision); !errors.Is(err, conversation.ErrConflict) {
		t.Fatal(err)
	}
	if err := a.Delete(ctx, conversation.OwnerScope, c.ID, renamed.Revision); err != nil {
		t.Fatal(err)
	}
	var left int
	s.db.QueryRow(`SELECT COUNT(*) FROM turns`).Scan(&left)
	if left != 0 {
		t.Fatal("the turns go with their conversation", left)
	}
	if _, err := a.Get(ctx, conversation.OwnerScope, c.ID); !errors.Is(err, conversation.ErrNotFound) {
		t.Fatal(err)
	}
}

func TestATurnLeftPendingByAStopIsInterruptedAndNotSentAgain(t *testing.T) {
	path := filepath.Join(t.TempDir(), FileName)
	s, a := open(t, path)
	c, _ := a.Start(ctx, conversation.OwnerScope, conversation.KindGeneral, "", "")
	a.Send(ctx, conversation.OwnerScope, c.ID, "send-0001", "q", conversation.Provenance{})
	s.Close()

	_, again := open(t, path)
	n, err := again.Recover(ctx, time.Now().UTC())
	if err != nil || n != 1 {
		t.Fatal(n, err)
	}
	turns, _ := again.Turns(ctx, conversation.OwnerScope, c.ID, 0, 10)
	if turns[0].State != conversation.TurnInterrupted || turns[0].Reason == "" {
		t.Fatal(turns[0])
	}
	// The same send asked again finds the interrupted turn: it is not sent a second time.
	turn, replayed, _ := again.Send(ctx, conversation.OwnerScope, c.ID, "send-0001", "q", conversation.Provenance{})
	if !replayed || turn.State != conversation.TurnInterrupted {
		t.Fatal(turn, replayed)
	}
	if n, _ := again.Recover(ctx, time.Now().UTC()); n != 0 {
		t.Fatal("recovery touches only pending turns", n)
	}
}

func TestTurnPagesReadBackwardsFromTheLatest(t *testing.T) {
	_, a := open(t, filepath.Join(t.TempDir(), FileName))
	c, _ := a.Start(ctx, conversation.OwnerScope, conversation.KindGeneral, "", "")
	for i := 1; i <= 5; i++ {
		a.Send(ctx, conversation.OwnerScope, c.ID, fmt.Sprintf("send-%04d", i), fmt.Sprintf("q%d", i), conversation.Provenance{})
	}
	latest, _ := a.Turns(ctx, conversation.OwnerScope, c.ID, 0, 2)
	earlier, _ := a.Turns(ctx, conversation.OwnerScope, c.ID, latest[0].Seq, 2)
	if len(latest) != 2 || latest[0].Content != "q4" || latest[1].Content != "q5" || len(earlier) != 2 || earlier[0].Content != "q2" || earlier[1].Content != "q3" {
		t.Fatal(latest, earlier)
	}
}

func TestAnArchiveFromANewerBuildIsRefusedNotReadWrongly(t *testing.T) {
	path := filepath.Join(t.TempDir(), FileName)
	s, _ := open(t, path)
	s.Close()
	db, _ := sql.Open("sqlite", "file:"+path)
	db.Exec(fmt.Sprintf("PRAGMA user_version = %d", len(migrations)+1))
	db.Close()
	if _, err := Open(path); err == nil {
		t.Fatal("a newer archive must be refused")
	}
}

func TestTheArchiveSitsBesideTheSharedDatabase(t *testing.T) {
	if got := PathBeside("/workspace/.dokja/dokja.db"); got != "/workspace/.dokja/dokja_conversations.db" {
		t.Fatal(got)
	}
}

func TestARetryReopensTheSameLatestTurnAndOnlyAfterItEnded(t *testing.T) {
	_, a := open(t, filepath.Join(t.TempDir(), FileName))
	c, _ := a.Start(ctx, conversation.OwnerScope, conversation.KindGeneral, "", "")
	turn, _, _ := a.Send(ctx, conversation.OwnerScope, c.ID, "send-0001", "q", conversation.Provenance{})
	if _, err := a.Retry(ctx, conversation.OwnerScope, turn.ID); !errors.Is(err, conversation.ErrConflict) {
		t.Fatal("a pending turn is still on its way", err)
	}
	a.Fail(ctx, conversation.OwnerScope, turn, "chat inference is unavailable")
	again, err := a.Retry(ctx, conversation.OwnerScope, turn.ID)
	if err != nil || again.ID != turn.ID || again.Seq != 1 || again.State != conversation.TurnPending || again.Attempts != 2 || again.Reason != "" || !again.FinishedAt.IsZero() {
		t.Fatal(again, err)
	}
	if _, err := a.Retry(ctx, conversation.OwnerScope, turn.ID); !errors.Is(err, conversation.ErrConflict) {
		t.Fatal("two retries cannot both win", err)
	}
	done, err := a.Complete(ctx, conversation.OwnerScope, again, "the answer", "", "")
	if err != nil || done.State != conversation.TurnCompleted {
		t.Fatal(done, err)
	}
	if _, err := a.Retry(ctx, conversation.OwnerScope, turn.ID); !errors.Is(err, conversation.ErrConflict) {
		t.Fatal("a completed turn is never sent again", err)
	}
	turns, _ := a.Turns(ctx, conversation.OwnerScope, c.ID, 0, 10)
	if len(turns) != 1 || turns[0].Content != "q" {
		t.Fatal("the owner's message is never doubled", turns)
	}
}

func TestOnlyTheLatestTurnMayBeRetried(t *testing.T) {
	_, a := open(t, filepath.Join(t.TempDir(), FileName))
	c, _ := a.Start(ctx, conversation.OwnerScope, conversation.KindGeneral, "", "")
	first, _, _ := a.Send(ctx, conversation.OwnerScope, c.ID, "send-0001", "q1", conversation.Provenance{})
	a.Fail(ctx, conversation.OwnerScope, first, "down")
	a.Send(ctx, conversation.OwnerScope, c.ID, "send-0002", "q2", conversation.Provenance{})
	if _, err := a.Retry(ctx, conversation.OwnerScope, first.ID); !errors.Is(err, conversation.ErrConflict) {
		t.Fatal("an earlier turn stays where it ended", err)
	}
}

func TestAnInterruptedTurnCanBeRetriedOnRequest(t *testing.T) {
	path := filepath.Join(t.TempDir(), FileName)
	s, a := open(t, path)
	c, _ := a.Start(ctx, conversation.OwnerScope, conversation.KindGeneral, "", "")
	turn, _, _ := a.Send(ctx, conversation.OwnerScope, c.ID, "send-0001", "q", conversation.Provenance{})
	s.Close()
	_, again := open(t, path)
	again.Recover(ctx, time.Now().UTC())
	retried, err := again.Retry(ctx, conversation.OwnerScope, turn.ID)
	if err != nil || retried.State != conversation.TurnPending || retried.Attempts != 2 {
		t.Fatal(retried, err)
	}
}

func TestOneSendIDNamesOneMessage(t *testing.T) {
	_, a := open(t, filepath.Join(t.TempDir(), FileName))
	c, _ := a.Start(ctx, conversation.OwnerScope, conversation.KindGeneral, "", "")
	a.Send(ctx, conversation.OwnerScope, c.ID, "send-0001", "the question", conversation.Provenance{})
	if _, _, err := a.Send(ctx, conversation.OwnerScope, c.ID, "send-0001", "another question", conversation.Provenance{}); !errors.Is(err, conversation.ErrConflict) {
		t.Fatal(err)
	}
}
