package conversation

import (
	"context"
	"strings"
	"testing"
	"time"
)

// recorder is a Store that keeps what reached it, to show what the rules let through.
type recorder struct {
	created  []Conversation
	begun    []Turn
	finished []Turn
	listed   []int
	renamed  []string
	retried  []string
}

func (r *recorder) Create(_ context.Context, _ Scope, c Conversation) (Conversation, error) {
	r.created = append(r.created, c)
	return c, nil
}
func (r *recorder) Get(context.Context, Scope, string) (Conversation, error) {
	return Conversation{}, nil
}
func (r *recorder) List(_ context.Context, _ Scope, _ string, limit int) ([]Conversation, string, error) {
	r.listed = append(r.listed, limit)
	return nil, "", nil
}
func (r *recorder) Rename(_ context.Context, _ Scope, _ string, _ uint64, title string, _ time.Time) (Conversation, error) {
	r.renamed = append(r.renamed, title)
	return Conversation{Title: title}, nil
}
func (r *recorder) Delete(context.Context, Scope, string, uint64) error { return nil }
func (r *recorder) BeginTurn(_ context.Context, _ Scope, t Turn) (Turn, bool, error) {
	r.begun = append(r.begun, t)
	return t, false, nil
}
func (r *recorder) RetryTurn(_ context.Context, _ Scope, id string, _ time.Time) (Turn, error) {
	r.retried = append(r.retried, id)
	return Turn{ID: id, State: TurnPending}, nil
}
func (r *recorder) FinishTurn(_ context.Context, _ Scope, t Turn) (Turn, error) {
	r.finished = append(r.finished, t)
	return t, nil
}
func (r *recorder) Turns(context.Context, Scope, string, int, int) ([]Turn, error) { return nil, nil }
func (r *recorder) InterruptPending(context.Context, time.Time) (int, error)       { return 0, nil }

const someID = "0123456789abcdef0123456789abcdef"

func TestOnlyTheOwnersInterfacesHaveAScope(t *testing.T) {
	for _, source := range []string{"desktop", "cli", "tui"} {
		if scope, ok := ScopeFor(source); !ok || scope != OwnerScope {
			t.Fatal(source)
		}
	}
	for _, source := range []string{"discord", "whatsapp", "teams", "", "Desktop"} {
		if _, ok := ScopeFor(source); ok {
			t.Fatalf("%q must have no scope", source)
		}
	}
}

func TestStartKeepsReadingDiscussionsApartAndBoundsTheTitle(t *testing.T) {
	store := &recorder{}
	a := New(store)
	c, err := a.Start(context.Background(), OwnerScope, KindReading, "reading-1", "  On   reading  ")
	if err != nil || c.Kind != KindReading || c.ReadingID != "reading-1" || c.Title != "On reading" || len(c.ID) != 32 || c.Revision != 1 {
		t.Fatal(c, err)
	}
	refused := []struct {
		scope   Scope
		kind    Kind
		reading string
		title   string
	}{
		{"", KindGeneral, "", ""},
		{OwnerScope, "other", "", ""},
		{OwnerScope, KindReading, "", ""},
		{OwnerScope, KindGeneral, "reading-1", ""},
		{OwnerScope, KindReading, strings.Repeat("r", MaxReadingID+1), ""},
		{OwnerScope, KindGeneral, "", strings.Repeat("t", MaxTitleRunes+1)},
	}
	for _, r := range refused {
		if _, err := a.Start(context.Background(), r.scope, r.kind, r.reading, r.title); err != ErrInvalid {
			t.Fatalf("%+v must be refused, got %v", r, err)
		}
	}
	if len(store.created) != 1 {
		t.Fatal(store.created)
	}
}

func TestASendIsPendingUntilItEndsAndCarriesOnlyWhatWasChosen(t *testing.T) {
	store := &recorder{}
	a := New(store)
	turn, replayed, err := a.Send(context.Background(), OwnerScope, someID, "send-0001", "  What is a reader?  ", Provenance{SelectedProvider: "ollama", SelectedModel: "qwen3:8b", ObservedProvider: "codex", ObservedModel: "x"})
	if err != nil || replayed || turn.State != TurnPending || turn.Attempts != 1 || turn.Content != "What is a reader?" || turn.Provenance.ObservedProvider != "" || turn.Provenance.SelectedModel != "qwen3:8b" {
		t.Fatal(turn, err)
	}
	for _, bad := range []struct{ conversation, send, content string }{
		{"not-an-id", "send-0001", "q"},
		{someID, "short", "q"},
		{someID, "send 0001!", "q"},
		{someID, "send-0001", "   "},
		{someID, "send-0001", strings.Repeat("q", MaxContentRunes+1)},
	} {
		if _, _, err := a.Send(context.Background(), OwnerScope, bad.conversation, bad.send, bad.content, Provenance{}); err != ErrInvalid {
			t.Fatalf("%+v must be refused", bad)
		}
	}
	if _, _, err := a.Send(context.Background(), OwnerScope, someID, "send-0002", "q", Provenance{SelectedProvider: "ollama"}); err != ErrInvalid {
		t.Fatal("a provider without a model is not a choice")
	}
	if _, _, err := a.Send(context.Background(), OwnerScope, someID, "send-0002", "q", Provenance{SelectedProvider: "elsewhere", SelectedModel: "m"}); err != ErrInvalid {
		t.Fatal("an unknown provider is refused")
	}
	if len(store.begun) != 1 {
		t.Fatal(store.begun)
	}
}

func TestATurnEndsCompletedWithItsReplyOrFailedWithAReason(t *testing.T) {
	store := &recorder{}
	a := New(store)
	turn, _, _ := a.Send(context.Background(), OwnerScope, someID, "send-0001", "q", Provenance{})
	done, err := a.Complete(context.Background(), OwnerScope, turn, strings.Repeat("a", MaxReplyRunes+10), "ollama", "qwen3:8b")
	if err != nil || done.State != TurnCompleted || len([]rune(done.Reply)) != MaxReplyRunes || done.Provenance.ObservedModel != "qwen3:8b" || done.FinishedAt.IsZero() {
		t.Fatal(done.State, err)
	}
	if _, err := a.Complete(context.Background(), OwnerScope, turn, "  ", "", ""); err != ErrInvalid {
		t.Fatal("an empty reply is no reply")
	}
	failed, err := a.Fail(context.Background(), OwnerScope, turn, strings.Repeat("r", 400))
	if err != nil || failed.State != TurnFailed || failed.Reply != "" || len([]rune(failed.Reason)) != MaxReasonRunes {
		t.Fatal(failed, err)
	}
}

func TestListAndTurnPagesAreBounded(t *testing.T) {
	store := &recorder{}
	a := New(store)
	for _, limit := range []int{0, -1, 10, 1000} {
		a.List(context.Background(), OwnerScope, "", limit)
	}
	if got := store.listed; got[0] != MaxListLimit || got[1] != MaxListLimit || got[2] != 10 || got[3] != MaxListLimit {
		t.Fatal(got)
	}
	for _, cursor := range []string{"x", "1.zz", "1." + someID + "'; DROP TABLE turns; --"} {
		if _, _, err := a.List(context.Background(), OwnerScope, cursor, 10); err != ErrInvalid {
			t.Fatalf("cursor %q must be refused", cursor)
		}
	}
	if _, _, err := a.List(context.Background(), OwnerScope, "1700000000000000000."+someID, 10); err != nil {
		t.Fatal(err)
	}
	if _, err := a.Turns(context.Background(), OwnerScope, someID, -1, 10); err != ErrInvalid {
		t.Fatal("a negative position is refused")
	}
}

func TestOnlyCompletedTurnsBecomeContextWithinBothBudgets(t *testing.T) {
	turns := []Turn{
		{Seq: 1, State: TurnCompleted, Content: "aaaa", Reply: "aaaa"},
		{Seq: 2, State: TurnFailed, Content: "bb"},
		{Seq: 3, State: TurnCompleted, Content: "cc", Reply: "cc"},
		{Seq: 4, State: TurnInterrupted, Content: "dd"},
		{Seq: 5, State: TurnCompleted, Content: "ee", Reply: "ee"},
		{Seq: 6, State: TurnPending, Content: "ff"},
	}
	seqs := func(picked []Turn) []int {
		out := []int{}
		for _, t := range picked {
			out = append(out, t.Seq)
		}
		return out
	}
	if got := seqs(ContextTurns(turns, 10, 100)); len(got) != 3 || got[0] != 1 || got[1] != 3 || got[2] != 5 {
		t.Fatal(got)
	}
	if got := seqs(ContextTurns(turns, 2, 100)); len(got) != 2 || got[0] != 3 || got[1] != 5 {
		t.Fatal("the newest win the turn budget", got)
	}
	if got := seqs(ContextTurns(turns, 10, 9)); len(got) != 2 || got[0] != 3 || got[1] != 5 {
		t.Fatal("the text budget stops at the first turn that does not fit", got)
	}
}

func TestRenameAndDeleteNameTheVersionTheySaw(t *testing.T) {
	store := &recorder{}
	a := New(store)
	if _, err := a.Rename(context.Background(), OwnerScope, someID, 0, "x"); err != ErrInvalid {
		t.Fatal("revision 0 names no version")
	}
	if _, err := a.Rename(context.Background(), OwnerScope, someID, 3, "  New   title "); err != nil || store.renamed[0] != "New title" {
		t.Fatal(err, store.renamed)
	}
	if err := a.Delete(context.Background(), OwnerScope, "not-an-id", 3); err != ErrInvalid {
		t.Fatal(err)
	}
	if _, err := a.Get(context.Background(), "", someID); err != ErrNotFound {
		t.Fatal("no scope reads nothing")
	}
}

func TestRetryNamesOneTurnOfTheOwner(t *testing.T) {
	a := New(&recorder{})
	if _, err := a.Retry(context.Background(), OwnerScope, "not-an-id"); err != ErrInvalid {
		t.Fatal(err)
	}
	if _, err := a.Retry(context.Background(), "", someID); err != ErrInvalid {
		t.Fatal(err)
	}
	if turn, err := a.Retry(context.Background(), OwnerScope, someID); err != nil || turn.State != TurnPending {
		t.Fatal(turn, err)
	}
}

type conversationRetryRecorder struct {
	recorder
	requests []struct {
		scope              Scope
		conversation, turn string
	}
}

func (r *conversationRetryRecorder) RetryTurnInConversation(_ context.Context, scope Scope, conversationID, turnID string, _ time.Time) (Turn, error) {
	r.requests = append(r.requests, struct {
		scope              Scope
		conversation, turn string
	}{scope, conversationID, turnID})
	return Turn{ID: turnID, ConversationID: conversationID, State: TurnPending, Attempts: 2}, nil
}

func TestConversationRetryRequiresAnAtomicScopedCapability(t *testing.T) {
	store := &recorder{}
	a := New(store)
	if _, err := a.RetryInConversation(context.Background(), OwnerScope, someID, someID); err != ErrUnavailable {
		t.Fatal("unsupported store must refuse scoped retry", err)
	}
	if len(store.retried) != 0 {
		t.Fatal("scoped retry must never fall back to a turn-only mutation", store.retried)
	}
}

func TestConversationRetryValidatesBeforeStorageAndPreservesBothIDs(t *testing.T) {
	store := &conversationRetryRecorder{}
	a := New(store)
	for _, invalid := range []struct {
		scope              Scope
		conversation, turn string
	}{
		{"", someID, someID},
		{OwnerScope, "", someID},
		{OwnerScope, "not-an-id", someID},
		{OwnerScope, someID, "not-an-id"},
	} {
		if _, err := a.RetryInConversation(context.Background(), invalid.scope, invalid.conversation, invalid.turn); err != ErrInvalid {
			t.Fatal(invalid, err)
		}
	}
	if len(store.requests) != 0 || len(store.retried) != 0 {
		t.Fatal("invalid retry reached storage", store.requests, store.retried)
	}
	turnID := strings.Repeat("a", 32)
	turn, err := a.RetryInConversation(context.Background(), OwnerScope, someID, turnID)
	if err != nil || turn.ID != turnID || turn.ConversationID != someID || turn.State != TurnPending {
		t.Fatal(turn, err)
	}
	if len(store.requests) != 1 || store.requests[0].scope != OwnerScope || store.requests[0].conversation != someID || store.requests[0].turn != turnID || len(store.retried) != 0 {
		t.Fatal("request identity was not passed intact to scoped storage", store.requests, store.retried)
	}
}
