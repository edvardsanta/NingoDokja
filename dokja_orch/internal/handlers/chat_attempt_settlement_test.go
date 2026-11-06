package handlers

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	conversation "read_books/dokja_domain/dokja_conversation"
)

type heldAttemptModel struct {
	calls   atomic.Int32
	started [2]chan struct{}
	release [2]chan struct{}
}

func (m *heldAttemptModel) Generate(ctx context.Context, text string) (map[string]any, error) {
	return m.GenerateMessages(ctx, []map[string]string{{"role": "user", "content": text}})
}

func (m *heldAttemptModel) GenerateMessages(ctx context.Context, _ []map[string]string) (map[string]any, error) {
	index := m.calls.Add(1) - 1
	if index > 1 {
		return nil, errors.New("unexpected additional generation")
	}
	close(m.started[index])
	select {
	case <-m.release[index]:
		if index == 0 {
			return map[string]any{"reply": "Old answer", "provider": "ollama", "model": "fixture/old"}, nil
		}
		return map[string]any{"reply": "Current answer", "provider": "ollama", "model": "fixture/current"}, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func TestLateArchivedReplyCannotCompleteTheCurrentRetry(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	archive := archiveFor(t)
	model := &heldAttemptModel{
		started: [2]chan struct{}{make(chan struct{}), make(chan struct{})},
		release: [2]chan struct{}{make(chan struct{}), make(chan struct{})},
	}
	handler := NewChatDomainHandler(model).WithArchive(archive)
	held, err := archive.Start(ctx, conversation.OwnerScope, conversation.KindGeneral, "", "")
	if err != nil {
		t.Fatal(err)
	}
	type response struct {
		result map[string]any
		err    error
	}
	firstResult, secondResult := make(chan response, 1), make(chan response, 1)
	event := ownerTurn(map[string]any{"content": "Question", "conversation_id": held.ID, "send_id": "send-0001"})
	go func() {
		result, err := handler.Handle(ctx, event, archiveStep)
		firstResult <- response{result, err}
	}()
	awaitAttempt := func(started <-chan struct{}) {
		t.Helper()
		select {
		case <-started:
		case <-ctx.Done():
			t.Fatal("generation did not reach its controlled boundary", ctx.Err())
		}
	}
	awaitResult := func(done <-chan response) response {
		t.Helper()
		select {
		case result := <-done:
			return result
		case <-ctx.Done():
			t.Fatal("handler did not settle", ctx.Err())
			return response{}
		}
	}
	awaitAttempt(model.started[0])
	turns, err := archive.Turns(ctx, conversation.OwnerScope, held.ID, 0, 10)
	if err != nil || len(turns) != 1 {
		t.Fatal(turns, err)
	}
	first := turns[0]
	// The old executor remains alive even though the current authority recorded
	// its attempt as failed; an explicit retry opens a new settlement boundary.
	if _, err := archive.Fail(ctx, conversation.OwnerScope, first, "First attempt unavailable"); err != nil {
		t.Fatal(err)
	}
	go func() {
		result, err := handler.Handle(ctx, ownerTurn(map[string]any{"conversation_id": held.ID, "retry_turn_id": first.ID}), archiveStep)
		secondResult <- response{result, err}
	}()
	awaitAttempt(model.started[1])
	close(model.release[0])
	stale := awaitResult(firstResult)
	if stale.err == nil || stale.result != nil {
		t.Fatalf("stale generation must not publish its reply: %+v", stale)
	}
	turns, err = archive.Turns(ctx, conversation.OwnerScope, held.ID, 0, 10)
	if err != nil || len(turns) != 1 || turns[0].State != conversation.TurnPending || turns[0].Attempts != 2 || turns[0].Reply != "" {
		t.Fatal("old handler altered current retry", turns, err)
	}
	close(model.release[1])
	current := awaitResult(secondResult)
	if current.err != nil || current.result["reply"] != "Current answer" {
		t.Fatal(current.result, current.err)
	}
	replayed, err := handler.Handle(ctx, event, archiveStep)
	if err != nil || replayed["reply"] != "Current answer" || replayed["replayed"] != true || model.calls.Load() != 2 {
		t.Fatal("replay must use the current attempt's stored result", replayed, err, model.calls.Load())
	}
}

func TestArchivedRetryCannotChangeAnotherConversation(t *testing.T) {
	for _, interrupted := range []bool{false, true} {
		name := "failed"
		if interrupted {
			name = "interrupted"
		}
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			archive := archiveFor(t)
			stated, err := archive.Start(ctx, conversation.OwnerScope, conversation.KindGeneral, "", "Stated conversation")
			if err != nil {
				t.Fatal(err)
			}
			actual, err := archive.Start(ctx, conversation.OwnerScope, conversation.KindGeneral, "", "Actual conversation")
			if err != nil {
				t.Fatal(err)
			}
			turn, _, err := archive.Send(ctx, conversation.OwnerScope, actual.ID, "send-0001", "Question", conversation.Provenance{})
			if err != nil {
				t.Fatal(err)
			}
			if interrupted {
				if n, err := archive.Recover(ctx, time.Now().UTC()); err != nil || n != 1 {
					t.Fatal(n, err)
				}
			} else if _, err := archive.Fail(ctx, conversation.OwnerScope, turn, "Unavailable"); err != nil {
				t.Fatal(err)
			}
			before, err := archive.Turns(ctx, conversation.OwnerScope, actual.ID, 0, 10)
			if err != nil || len(before) != 1 {
				t.Fatal(before, err)
			}
			beforeConversation, err := archive.Get(ctx, conversation.OwnerScope, actual.ID)
			if err != nil {
				t.Fatal(err)
			}
			model := &scriptedModel{answers: []map[string]any{{"reply": "Correct retry"}}}
			handler := NewChatDomainHandler(model).WithArchive(archive)
			result, err := handler.Handle(ctx, ownerTurn(map[string]any{"conversation_id": stated.ID, "retry_turn_id": turn.ID}), archiveStep)
			if !errors.Is(err, conversation.ErrNotFound) || result != nil || len(model.asked) != 0 {
				t.Fatal("mismatched conversation must refuse before generation", result, err, len(model.asked))
			}
			after, err := archive.Turns(ctx, conversation.OwnerScope, actual.ID, 0, 10)
			if err != nil || len(after) != 1 || !reflect.DeepEqual(after[0], before[0]) {
				t.Fatal("refused retry mutated another conversation's turn", before, after, err)
			}
			afterConversation, err := archive.Get(ctx, conversation.OwnerScope, actual.ID)
			if err != nil || afterConversation != beforeConversation {
				t.Fatal("refused retry changed another conversation's revision", beforeConversation, afterConversation, err)
			}
			unchangedStated, err := archive.Get(ctx, conversation.OwnerScope, stated.ID)
			if err != nil || unchangedStated != stated {
				t.Fatal("refused retry changed the stated conversation", stated, unchangedStated, err)
			}
			result, err = handler.Handle(ctx, ownerTurn(map[string]any{"conversation_id": actual.ID, "retry_turn_id": turn.ID}), archiveStep)
			if err != nil || result["reply"] != "Correct retry" || len(model.asked) != 1 {
				t.Fatal("valid matching retry must remain available", result, err, len(model.asked))
			}
		})
	}
}

// answeredThenLeft answers, but the requester has gone by the time the answer is back.
type answeredThenLeft struct{ cancel context.CancelFunc }

func (m answeredThenLeft) Generate(ctx context.Context, _ string) (map[string]any, error) {
	return m.GenerateMessages(ctx, nil)
}
func (m answeredThenLeft) GenerateMessages(context.Context, []map[string]string) (map[string]any, error) {
	m.cancel()
	return map[string]any{"reply": "An answer nobody waited for.", "provider": "ollama", "requested_model": "fixture/model"}, nil
}

// Delivering a reply and accepting it are different things: a requester that went away after the
// model answered loses the delivery, never the answer. The turn is completed and a read shows it.
func TestAnAnswerIsKeptWhenItsRequesterLeftAfterTheModelReplied(t *testing.T) {
	archive := archiveFor(t)
	ctx, cancel := context.WithCancel(context.Background())
	handler := NewChatDomainHandler(answeredThenLeft{cancel}).WithArchive(archive)
	held, _ := archive.Start(context.Background(), conversation.OwnerScope, conversation.KindGeneral, "", "")
	handler.Handle(ctx, ownerTurn(map[string]any{"content": "q", "conversation_id": held.ID, "send_id": "send-0001"}), archiveStep)
	turns, _ := archive.Turns(context.Background(), conversation.OwnerScope, held.ID, 0, 10)
	if len(turns) != 1 || turns[0].State != conversation.TurnCompleted || !strings.Contains(turns[0].Reply, "An answer nobody waited for.") {
		t.Fatalf("the accepted answer was lost with its delivery: %+v", turns)
	}
}

// waitsPastTheDeadline answers only after the request's time is up.
type waitsPastTheDeadline struct{}

func (waitsPastTheDeadline) Generate(ctx context.Context, _ string) (map[string]any, error) {
	return waitsPastTheDeadline{}.GenerateMessages(ctx, nil)
}
func (waitsPastTheDeadline) GenerateMessages(ctx context.Context, _ []map[string]string) (map[string]any, error) {
	<-ctx.Done()
	return nil, ctx.Err()
}

// A send's budget is its request deadline. Spent during generation, the turn fails once, in plain
// words, and nothing sends it again; the owner may retry it.
func TestASendWhoseDeadlinePassesFailsOnceAndIsNotSentAgain(t *testing.T) {
	archive := archiveFor(t)
	handler := NewChatDomainHandler(waitsPastTheDeadline{}).WithArchive(archive)
	held, _ := archive.Start(context.Background(), conversation.OwnerScope, conversation.KindGeneral, "", "")
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if _, err := handler.Handle(ctx, ownerTurn(map[string]any{"content": "q", "conversation_id": held.ID, "send_id": "send-0001"}), archiveStep); err == nil {
		t.Fatal("a send past its deadline has no reply")
	}
	turns, _ := archive.Turns(context.Background(), conversation.OwnerScope, held.ID, 0, 10)
	if len(turns) != 1 || turns[0].State != conversation.TurnFailed || turns[0].Reason != "the request stopped before the reply" || turns[0].Attempts != 1 {
		t.Fatalf("%+v", turns)
	}
}
