package handlers

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	conversation "read_books/dokja_domain/dokja_conversation"
	"read_books/internal/core"
)

// gatedModel answers each message only once the test releases it, and records the conversation it
// saw (the system prompt left out).
type gatedModel struct {
	mu      sync.Mutex
	started map[string]chan struct{}
	release map[string]chan struct{}
	seen    map[string][]map[string]string
}

func newGatedModel(questions ...string) *gatedModel {
	m := &gatedModel{started: map[string]chan struct{}{}, release: map[string]chan struct{}{}, seen: map[string][]map[string]string{}}
	for _, question := range questions {
		m.started[question], m.release[question] = make(chan struct{}), make(chan struct{})
	}
	return m
}

func (m *gatedModel) Generate(ctx context.Context, text string) (map[string]any, error) {
	return m.GenerateMessages(ctx, []map[string]string{{"role": "user", "content": text}})
}

func (m *gatedModel) GenerateMessages(ctx context.Context, messages []map[string]string) (map[string]any, error) {
	question := messages[len(messages)-1]["content"]
	m.mu.Lock()
	started, release, known := m.started[question], m.release[question], m.started[question] != nil
	if known {
		m.seen[question] = messages
	}
	m.mu.Unlock()
	if !known {
		return nil, fmt.Errorf("unexpected generation for %q", question)
	}
	close(started)
	select {
	case <-release:
		return map[string]any{"reply": "Answer to " + question}, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func (m *gatedModel) asked(question string) bool {
	select {
	case <-m.started[question]:
		return true
	default:
		return false
	}
}

func (m *gatedModel) history(question string) string {
	m.mu.Lock()
	defer m.mu.Unlock()
	parts := []string{}
	for _, message := range m.seen[question] {
		if message["role"] == "system" {
			continue
		}
		parts = append(parts, message["role"]+": "+message["content"])
	}
	return strings.Join(parts, " | ")
}

type handled struct {
	result map[string]any
	err    error
}

func sendAsync(ctx context.Context, handler *ChatDomainHandler, conversationID, sendID, content string) <-chan handled {
	done := make(chan handled, 1)
	go func() {
		result, err := handler.Handle(ctx, ownerTurn(map[string]any{"content": content, "conversation_id": conversationID, "send_id": sendID}), archiveStep)
		done <- handled{result, err}
	}()
	return done
}

func awaitSignal(t *testing.T, ctx context.Context, signal <-chan struct{}, what string) {
	t.Helper()
	select {
	case <-signal:
	case <-ctx.Done():
		t.Fatal(what, ctx.Err())
	}
}

func awaitHandled(t *testing.T, ctx context.Context, done <-chan handled) handled {
	t.Helper()
	select {
	case got := <-done:
		return got
	case <-ctx.Done():
		t.Fatal("the handler did not settle", ctx.Err())
		return handled{}
	}
}

// awaitTurns waits until the conversation holds n turns, so a send is known to be recorded.
func awaitTurns(t *testing.T, ctx context.Context, archive *conversation.Archive, conversationID string, n int) []conversation.Turn {
	t.Helper()
	for {
		turns, err := archive.Turns(ctx, conversation.OwnerScope, conversationID, 0, 10)
		if err != nil {
			t.Fatal(err)
		}
		if len(turns) >= n {
			return turns
		}
		select {
		case <-ctx.Done():
			t.Fatal("the send was not recorded", ctx.Err())
		case <-time.After(5 * time.Millisecond):
		}
	}
}

func TestASecondSendWaitsForTheEarlierReplyAndSeesIt(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	archive := archiveFor(t)
	model := newGatedModel("First", "Second")
	handler := NewChatDomainHandler(model).WithArchive(archive)
	held, _ := archive.Start(ctx, conversation.OwnerScope, conversation.KindGeneral, "", "")

	first := sendAsync(ctx, handler, held.ID, "send-0001", "First")
	awaitSignal(t, ctx, model.started["First"], "the first send was not asked")
	second := sendAsync(ctx, handler, held.ID, "send-0002", "Second")
	turns := awaitTurns(t, ctx, archive, held.ID, 2)
	if turns[0].Content != "First" || turns[1].Content != "Second" || turns[1].State != conversation.TurnPending {
		t.Fatal("both sends must be recorded in order, the second pending", turns)
	}
	time.Sleep(20 * time.Millisecond)
	if model.asked("Second") {
		t.Fatal("the second send was asked while the first was still being answered")
	}

	close(model.release["First"])
	if got := awaitHandled(t, ctx, first); got.err != nil || got.result["reply"] != "Answer to First" {
		t.Fatal(got.result, got.err)
	}
	awaitSignal(t, ctx, model.started["Second"], "the second send was not asked after the first")
	close(model.release["Second"])
	if got := awaitHandled(t, ctx, second); got.err != nil || got.result["reply"] != "Answer to Second" {
		t.Fatal(got.result, got.err)
	}
	if want := "user: First | assistant: Answer to First | user: Second"; model.history("Second") != want {
		t.Fatalf("the second reply must see the first:\n got %s\nwant %s", model.history("Second"), want)
	}
}

func TestSendsToDifferentConversationsAreAnsweredTogether(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	archive := archiveFor(t)
	model := newGatedModel("Here", "There")
	handler := NewChatDomainHandler(model).WithArchive(archive)
	one, _ := archive.Start(ctx, conversation.OwnerScope, conversation.KindGeneral, "", "")
	other, _ := archive.Start(ctx, conversation.OwnerScope, conversation.KindGeneral, "", "")

	here := sendAsync(ctx, handler, one.ID, "send-0001", "Here")
	there := sendAsync(ctx, handler, other.ID, "send-0001", "There")
	awaitSignal(t, ctx, model.started["Here"], "the first conversation was not asked")
	awaitSignal(t, ctx, model.started["There"], "another conversation must not wait for the first")
	close(model.release["Here"])
	close(model.release["There"])
	for _, done := range []<-chan handled{here, there} {
		if got := awaitHandled(t, ctx, done); got.err != nil {
			t.Fatal(got.err)
		}
	}
}

func TestAQueuedSendWhoseRequestStopsIsFailedWithoutAskingTheModel(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	archive := archiveFor(t)
	model := newGatedModel("First", "Second")
	handler := NewChatDomainHandler(model).WithArchive(archive)
	held, _ := archive.Start(ctx, conversation.OwnerScope, conversation.KindGeneral, "", "")

	first := sendAsync(ctx, handler, held.ID, "send-0001", "First")
	awaitSignal(t, ctx, model.started["First"], "the first send was not asked")
	waitingCtx, stop := context.WithCancel(ctx)
	second := sendAsync(waitingCtx, handler, held.ID, "send-0002", "Second")
	awaitTurns(t, ctx, archive, held.ID, 2)
	stop()
	got := awaitHandled(t, ctx, second)
	if got.err == nil || got.err.Error() != reasonStoppedWaiting {
		t.Fatal("a send stopped in the queue must say it never reached the model", got.result, got.err)
	}
	turns := awaitTurns(t, ctx, archive, held.ID, 2)
	if turns[1].State != conversation.TurnFailed || turns[1].Reason != reasonStoppedWaiting || model.asked("Second") {
		t.Fatal("the stopped send must be failed and never asked", turns[1], model.asked("Second"))
	}

	close(model.release["First"])
	if got := awaitHandled(t, ctx, first); got.err != nil {
		t.Fatal("the earlier send must still be answered", got.err)
	}
}

func TestSendsPastTheQueueBoundAreRefusedBeforeTheyAreRecorded(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	archive := archiveFor(t)
	questions := []string{}
	for i := 0; i < maxWaitingSends; i++ {
		questions = append(questions, fmt.Sprintf("Question %d", i))
	}
	model := newGatedModel(questions...)
	handler := NewChatDomainHandler(model).WithArchive(archive)
	held, _ := archive.Start(ctx, conversation.OwnerScope, conversation.KindGeneral, "", "")

	waiting := []<-chan handled{}
	for i, question := range questions {
		waiting = append(waiting, sendAsync(ctx, handler, held.ID, fmt.Sprintf("send-%04d", i), question))
		awaitTurns(t, ctx, archive, held.ID, i+1)
	}
	_, err := handler.Handle(ctx, ownerTurn(map[string]any{"content": "One too many", "conversation_id": held.ID, "send_id": "send-9999"}), archiveStep)
	if err == nil || !strings.Contains(err.Error(), "too many messages are waiting") {
		t.Fatal("a send past the bound must be refused", err)
	}
	if turns := awaitTurns(t, ctx, archive, held.ID, maxWaitingSends); len(turns) != maxWaitingSends {
		t.Fatal("a refused send must not be recorded", len(turns))
	}
	for i, question := range questions {
		close(model.release[question])
		if got := awaitHandled(t, ctx, waiting[i]); got.err != nil {
			t.Fatal(question, got.err)
		}
	}
}

func TestAFullQueueStillAnswersARepeatedSendFromTheArchive(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	archive := archiveFor(t)
	questions := []string{"Settled"}
	for i := 0; i < maxWaitingSends; i++ {
		questions = append(questions, fmt.Sprintf("Question %d", i))
	}
	model := newGatedModel(questions...)
	handler := NewChatDomainHandler(model).WithArchive(archive)
	held, _ := archive.Start(ctx, conversation.OwnerScope, conversation.KindGeneral, "", "")

	close(model.release["Settled"])
	settled := ownerTurn(map[string]any{"content": "Settled", "conversation_id": held.ID, "send_id": "send-done"})
	if _, err := handler.Handle(ctx, settled, archiveStep); err != nil {
		t.Fatal(err)
	}
	waiting := []<-chan handled{}
	for i, question := range questions[1:] {
		waiting = append(waiting, sendAsync(ctx, handler, held.ID, fmt.Sprintf("send-%04d", i), question))
		awaitTurns(t, ctx, archive, held.ID, i+2)
	}

	replayed, err := handler.Handle(ctx, settled, archiveStep)
	if err != nil || replayed["reply"] != "Answer to Settled" || replayed["replayed"] != true {
		t.Fatal("a settled send repeated while the queue is full must be answered from the archive", replayed, err)
	}
	_, err = handler.Handle(ctx, ownerTurn(map[string]any{"content": "Question 3", "conversation_id": held.ID, "send_id": "send-0003"}), archiveStep)
	if err == nil || !strings.Contains(err.Error(), "still on its way") {
		t.Fatal("a pending send repeated while the queue is full must say it is on its way", err)
	}
	_, err = handler.Handle(ctx, ownerTurn(map[string]any{"content": "Other text", "conversation_id": held.ID, "send_id": "send-0003"}), archiveStep)
	if !errors.Is(err, conversation.ErrConflict) {
		t.Fatal("a send id reused with other text must conflict", err)
	}
	if turns := awaitTurns(t, ctx, archive, held.ID, maxWaitingSends+1); len(turns) != maxWaitingSends+1 {
		t.Fatal("repeated sends must not be recorded again", len(turns))
	}
	for i, question := range questions[1:] {
		close(model.release[question])
		if got := awaitHandled(t, ctx, waiting[i]); got.err != nil {
			t.Fatal(question, got.err)
		}
	}
}

func TestASendThatWaitsSaysSoAndThenThatItWasDispatched(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	previous := queuedReportEvery
	queuedReportEvery = 5 * time.Millisecond
	defer func() { queuedReportEvery = previous }()
	archive := archiveFor(t)
	model := newGatedModel("First", "Second")
	handler := NewChatDomainHandler(model).WithArchive(archive)
	held, _ := archive.Start(ctx, conversation.OwnerScope, conversation.KindGeneral, "", "")

	var mu sync.Mutex
	var reports []core.SendState
	seen := func(state core.SendState) int {
		mu.Lock()
		defer mu.Unlock()
		count := 0
		for _, each := range reports {
			if each == state {
				count++
			}
		}
		return count
	}
	report := func(state core.SendState) {
		mu.Lock()
		defer mu.Unlock()
		reports = append(reports, state)
	}
	first := sendAsync(core.WithSendProgress(ctx, report), handler, held.ID, "send-0001", "First")
	awaitSignal(t, ctx, model.started["First"], "the first send was not asked")
	if len(reports) != 0 {
		t.Fatal("a send with nothing ahead reports nothing", reports)
	}
	second := sendAsync(core.WithSendProgress(ctx, report), handler, held.ID, "send-0002", "Second")
	for seen(core.SendQueued) < 2 { // repeated while it waits, so a stream notices a closed client
		select {
		case <-ctx.Done():
			t.Fatal("the queued state was not repeated", ctx.Err())
		case <-time.After(time.Millisecond):
		}
	}
	if seen(core.SendDispatched) != 0 {
		t.Fatal("not dispatched while the first is answered")
	}
	close(model.release["First"])
	awaitHandled(t, ctx, first)
	awaitSignal(t, ctx, model.started["Second"], "the second send was not asked")
	close(model.release["Second"])
	awaitHandled(t, ctx, second)

	mu.Lock()
	defer mu.Unlock()
	if last := reports[len(reports)-1]; last != core.SendDispatched || len(reports) < 3 {
		t.Fatal("dispatched must come last", reports)
	}
}
