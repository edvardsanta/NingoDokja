package handlers

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	conversation "read_books/dokja_domain/dokja_conversation"
	"read_books/internal/core"
)

// stopFixture is the two handlers as main wires them: they share only the running sends.
type stopFixture struct {
	archive *conversation.Archive
	chat    *ChatDomainHandler
	talk    *ConversationDomainHandler
	model   *gatedModel
}

func newStopFixture(t *testing.T, questions ...string) stopFixture {
	t.Helper()
	archive := archiveFor(t)
	running := core.NewRunningSends()
	model := newGatedModel(questions...)
	return stopFixture{
		archive: archive,
		chat:    NewChatDomainHandler(model).WithArchive(archive).WithRunningSends(running),
		talk:    NewConversationDomainHandler(archive).WithRunningSends(running),
		model:   model,
	}
}

func (f stopFixture) stop(t *testing.T, conversationID, sendID string) (map[string]any, error) {
	t.Helper()
	return ask(t, f.talk, core.SourceDesktop, "conversation.stop", map[string]any{"conversation_id": conversationID, "send_id": sendID})
}

// stopWhenRunning stops a send that was recorded a moment ago: it becomes stoppable just after.
func (f stopFixture) stopWhenRunning(t *testing.T, ctx context.Context, conversationID, sendID string) {
	t.Helper()
	for {
		_, err := f.stop(t, conversationID, sendID)
		if err == nil {
			return
		}
		if !errors.Is(err, conversation.ErrConflict) {
			t.Fatal(err)
		}
		select {
		case <-ctx.Done():
			t.Fatal("the send never became stoppable", ctx.Err())
		case <-time.After(5 * time.Millisecond):
		}
	}
}

func TestStoppingASendBeingAnsweredRecordsThatTheOwnerStoppedIt(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	f := newStopFixture(t, "First", "Next")
	held, _ := f.archive.Start(ctx, conversation.OwnerScope, conversation.KindGeneral, "", "")

	first := sendAsync(ctx, f.chat, held.ID, "send-0001", "First")
	awaitSignal(t, ctx, f.model.started["First"], "the send was not asked")
	answer, err := f.stop(t, held.ID, "send-0001")
	if err != nil || answer["stopping"] != true {
		t.Fatal("a running send must accept the stop", answer, err)
	}
	got := awaitHandled(t, ctx, first)
	if got.err == nil || got.err.Error() != reasonOwnerStopped {
		t.Fatal("the requester must learn the owner stopped it", got.result, got.err)
	}
	turns := awaitTurns(t, ctx, f.archive, held.ID, 1)
	if turns[0].State != conversation.TurnFailed || turns[0].Reason != reasonOwnerStopped {
		t.Fatal("the stopped turn must say why", turns[0])
	}

	// The conversation goes on: nothing is left holding its queue.
	close(f.model.release["Next"])
	if got := awaitHandled(t, ctx, sendAsync(ctx, f.chat, held.ID, "send-0002", "Next")); got.err != nil {
		t.Fatal("the next send must be answered", got.err)
	}
}

func TestAStoppedSendCanBeSentAgain(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	f := newStopFixture(t, "First")
	held, _ := f.archive.Start(ctx, conversation.OwnerScope, conversation.KindGeneral, "", "")

	first := sendAsync(ctx, f.chat, held.ID, "send-0001", "First")
	awaitSignal(t, ctx, f.model.started["First"], "the send was not asked")
	if _, err := f.stop(t, held.ID, "send-0001"); err != nil {
		t.Fatal(err)
	}
	awaitHandled(t, ctx, first)

	// The gate closes once per generation, so the retry gets a fresh one and is answered at once.
	f.model.mu.Lock()
	f.model.started["First"], f.model.release["First"] = make(chan struct{}), make(chan struct{})
	close(f.model.release["First"])
	f.model.mu.Unlock()
	turns := awaitTurns(t, ctx, f.archive, held.ID, 1)
	result, err := f.chat.Handle(ctx, ownerTurn(map[string]any{"conversation_id": held.ID, "retry_turn_id": turns[0].ID}), archiveStep)
	if err != nil || !strings.Contains(chatReplyFromResult(result), "Answer to First") {
		t.Fatal("a stopped send must retry like any failed one", result, err)
	}
}

func TestStoppingAQueuedSendNeverAsksTheModelAndLeavesTheEarlierReply(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	f := newStopFixture(t, "First", "Second")
	held, _ := f.archive.Start(ctx, conversation.OwnerScope, conversation.KindGeneral, "", "")

	first := sendAsync(ctx, f.chat, held.ID, "send-0001", "First")
	awaitSignal(t, ctx, f.model.started["First"], "the first send was not asked")
	second := sendAsync(ctx, f.chat, held.ID, "send-0002", "Second")
	awaitTurns(t, ctx, f.archive, held.ID, 2)
	f.stopWhenRunning(t, ctx, held.ID, "send-0002")

	got := awaitHandled(t, ctx, second)
	if got.err == nil || got.err.Error() != reasonOwnerStopped {
		t.Fatal("a queued send the owner stopped must say so", got.result, got.err)
	}
	turns := awaitTurns(t, ctx, f.archive, held.ID, 2)
	if turns[1].State != conversation.TurnFailed || turns[1].Reason != reasonOwnerStopped || f.model.asked("Second") {
		t.Fatal("the stopped send must be failed and never asked", turns[1], f.model.asked("Second"))
	}
	close(f.model.release["First"])
	if got := awaitHandled(t, ctx, first); got.err != nil {
		t.Fatal("stopping a later send must not touch the earlier one", got.err)
	}
}

func TestAStopThatFindsNothingRunningConflictsAndChangesNothing(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	f := newStopFixture(t, "First")
	held, _ := f.archive.Start(ctx, conversation.OwnerScope, conversation.KindGeneral, "", "")
	close(f.model.release["First"])
	if got := awaitHandled(t, ctx, sendAsync(ctx, f.chat, held.ID, "send-0001", "First")); got.err != nil {
		t.Fatal(got.err)
	}

	for _, sendID := range []string{"send-0001", "send-unknown"} {
		if _, err := f.stop(t, held.ID, sendID); !errors.Is(err, conversation.ErrConflict) {
			t.Fatal("nothing is running under", sendID, err)
		}
	}
	if turns := awaitTurns(t, ctx, f.archive, held.ID, 1); turns[0].State != conversation.TurnCompleted {
		t.Fatal("a settled turn must stay as it was", turns[0])
	}
}

func TestAStopIsRefusedOutsideTheCallersConversationsWithoutEffect(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	f := newStopFixture(t, "First")
	held, _ := f.archive.Start(ctx, conversation.OwnerScope, conversation.KindGeneral, "", "")
	other, _ := f.archive.Start(ctx, conversation.OwnerScope, conversation.KindGeneral, "", "")
	first := sendAsync(ctx, f.chat, held.ID, "send-0001", "First")
	awaitSignal(t, ctx, f.model.started["First"], "the send was not asked")

	if _, err := f.stop(t, "conv-unknown", "send-0001"); !errors.Is(err, conversation.ErrNotFound) {
		t.Fatal("an unknown conversation is not found", err)
	}
	if _, err := f.stop(t, other.ID, "send-0001"); !errors.Is(err, conversation.ErrConflict) {
		t.Fatal("a send belongs to its own conversation", err)
	}
	if _, err := f.stop(t, held.ID, ""); !errors.Is(err, conversation.ErrInvalid) {
		t.Fatal("a stop must name its send", err)
	}
	payload := map[string]any{"conversation_id": held.ID, "send_id": "send-0001"}
	if _, err := ask(t, f.talk, core.SourceDiscord, "conversation.stop", payload); err == nil {
		t.Fatal("a channel keeps no conversation to stop")
	}
	// Steering is a separate capability nothing offers: refused, and the send is not disturbed.
	if _, err := ask(t, f.talk, core.SourceDesktop, "conversation.steer", payload); err == nil || !strings.Contains(err.Error(), "unsupported") {
		t.Fatal("same-turn steering must be refused as unsupported", err)
	}

	close(f.model.release["First"])
	if got := awaitHandled(t, ctx, first); got.err != nil {
		t.Fatal("no refused request may reach the running send", got.err)
	}
}

func TestAStopIsRoutedToTheArchiveByTheService(t *testing.T) {
	f := newStopFixture(t)
	service := core.DefaultService(f.talk)
	held, _ := f.archive.Start(context.Background(), conversation.OwnerScope, conversation.KindGeneral, "", "")
	_, err := service.ProcessWithResult(context.Background(), core.Event{
		EventID: "e-1", Source: core.SourceDesktop, Type: "conversation.stop",
		Payload: map[string]any{"conversation_id": held.ID, "send_id": "send-0001"},
	})
	if !errors.Is(err, conversation.ErrConflict) {
		t.Fatal("the stop must reach the archive's handler", err)
	}
}

func TestAStopReachesASendRunningThroughTheService(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	f := newStopFixture(t, "First")
	service := core.DefaultService(f.chat, f.talk)
	held, _ := f.archive.Start(ctx, conversation.OwnerScope, conversation.KindGeneral, "", "")

	event := ownerTurn(map[string]any{"content": "First", "conversation_id": held.ID, "send_id": "send-0001"})
	event.EventID = "e-send"
	done := make(chan error, 1)
	go func() {
		_, err := service.ProcessWithResult(ctx, event)
		done <- err
	}()
	awaitSignal(t, ctx, f.model.started["First"], "the send was not asked")
	if _, err := service.ProcessWithResult(ctx, core.Event{
		EventID: "e-stop", Source: event.Source, Type: "conversation.stop",
		Payload: map[string]any{"conversation_id": held.ID, "send_id": "send-0001"},
	}); err != nil {
		t.Fatal("the stop must be served while the send is still open", err)
	}
	select {
	case err := <-done:
		if err == nil || !strings.Contains(err.Error(), reasonOwnerStopped) {
			t.Fatal("the send must end stopped", err)
		}
	case <-ctx.Done():
		t.Fatal("the send kept running after the stop", ctx.Err())
	}
	if turns := awaitTurns(t, ctx, f.archive, held.ID, 1); turns[0].Reason != reasonOwnerStopped {
		t.Fatal(turns[0])
	}
}

func TestAStopWithoutRunningSendsIsUnavailable(t *testing.T) {
	archive := archiveFor(t)
	held, _ := archive.Start(context.Background(), conversation.OwnerScope, conversation.KindGeneral, "", "")
	talk := NewConversationDomainHandler(archive)
	_, err := ask(t, talk, core.SourceDesktop, "conversation.stop", map[string]any{"conversation_id": held.ID, "send_id": "send-0001"})
	if !errors.Is(err, conversation.ErrUnavailable) {
		t.Fatal(err)
	}
}
