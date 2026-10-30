package handlers

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	conversation "read_books/dokja_domain/dokja_conversation"
	digestdomain "read_books/dokja_domain/dokja_digest"
	"read_books/internal/core"
	"read_books/internal/infrastructure/conversations"
)

// scriptedModel answers in turn and keeps what it was asked; a nil answer is an error.
type scriptedModel struct {
	mu      sync.Mutex
	answers []map[string]any
	asked   [][]map[string]string
	// system is the persona and identity note of each request, the conversation left out.
	system []string
}

func (m *scriptedModel) Generate(ctx context.Context, message string) (map[string]any, error) {
	return m.GenerateMessages(ctx, []map[string]string{{"role": "user", "content": message}})
}

func (m *scriptedModel) GenerateMessages(_ context.Context, messages []map[string]string) (map[string]any, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(messages) > 0 && messages[0]["role"] == "system" && strings.HasPrefix(messages[0]["content"], chatPersona) {
		m.system = append(m.system, messages[0]["content"])
		messages = messages[1:] // the persona is checked in chat_persona_test.go
	}
	m.asked = append(m.asked, messages)
	if len(m.answers) == 0 || m.answers[0] == nil {
		if len(m.answers) > 0 {
			m.answers = m.answers[1:]
		}
		return nil, errors.New("chat inference is unavailable")
	}
	answer := m.answers[0]
	m.answers = m.answers[1:]
	return answer, nil
}

func archiveFor(t *testing.T) *conversation.Archive {
	t.Helper()
	store, err := conversations.Open(filepath.Join(t.TempDir(), conversations.FileName))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })
	return conversation.New(store)
}

func ownerTurn(payload map[string]any) core.Event {
	return core.Event{Source: core.SourceDesktop, Type: "message.created", User: core.EventUser{ID: "desktop"}, Channel: core.EventChannel{ID: "desktop"}, Payload: payload}
}

var archiveStep = core.WorkflowStep{Domain: core.DomainChat, Action: "generate-response"}

func TestAnArchivedTurnIsRecordedAndAnsweredWithItsProvenance(t *testing.T) {
	archive := archiveFor(t)
	model := &scriptedModel{answers: []map[string]any{{"reply": "One who judges.", "provider": "ollama", "model": "qwen3:8b"}}}
	handler := NewChatDomainHandler(model).WithArchive(archive)
	held, _ := archive.Start(context.Background(), conversation.OwnerScope, conversation.KindGeneral, "", "")

	result, err := handler.Handle(context.Background(), ownerTurn(map[string]any{"content": "What is a reader?", "conversation_id": held.ID, "send_id": "send-0001"}), archiveStep)
	if err != nil || result["reply"] != "One who judges." || result["conversation_id"] != held.ID {
		t.Fatal(result, err)
	}
	turn := result["turn"].(map[string]any)
	if turn["state"] != "completed" || turn["provider"] != "ollama" || turn["model"] != "qwen3:8b" || turn["seq"] != 1 {
		t.Fatal(turn)
	}
	turns, _ := archive.Turns(context.Background(), conversation.OwnerScope, held.ID, 0, 10)
	if len(turns) != 1 || turns[0].Reply != "One who judges." {
		t.Fatal(turns)
	}
}

func TestTheSameSendIsAnsweredFromTheArchiveNeverByTheModelAgain(t *testing.T) {
	archive := archiveFor(t)
	model := &scriptedModel{answers: []map[string]any{{"reply": "Once."}}}
	handler := NewChatDomainHandler(model).WithArchive(archive)
	held, _ := archive.Start(context.Background(), conversation.OwnerScope, conversation.KindGeneral, "", "")
	turn := ownerTurn(map[string]any{"content": "q", "conversation_id": held.ID, "send_id": "send-0001"})
	handler.Handle(context.Background(), turn, archiveStep)
	again, err := handler.Handle(context.Background(), turn, archiveStep)
	if err != nil || again["reply"] != "Once." || again["replayed"] != true || len(model.asked) != 1 {
		t.Fatal(again, err, len(model.asked))
	}
}

func TestAFailedTurnStaysFailedAndARetryAnswersTheSameTurn(t *testing.T) {
	archive := archiveFor(t)
	model := &scriptedModel{answers: []map[string]any{nil, {"reply": "Now I answer."}}}
	handler := NewChatDomainHandler(model).WithArchive(archive)
	held, _ := archive.Start(context.Background(), conversation.OwnerScope, conversation.KindGeneral, "", "")

	_, err := handler.Handle(context.Background(), ownerTurn(map[string]any{"content": "q", "conversation_id": held.ID, "send_id": "send-0001"}), archiveStep)
	if err == nil || !strings.Contains(err.Error(), "chat inference is unavailable") {
		t.Fatal(err)
	}
	turns, _ := archive.Turns(context.Background(), conversation.OwnerScope, held.ID, 0, 10)
	if turns[0].State != conversation.TurnFailed || turns[0].Reason != "chat inference is unavailable" {
		t.Fatal(turns[0])
	}
	// The same send again is not a retry: it says the message got no reply.
	if _, err := handler.Handle(context.Background(), ownerTurn(map[string]any{"content": "q", "conversation_id": held.ID, "send_id": "send-0001"}), archiveStep); err == nil || !strings.Contains(err.Error(), "retry it") {
		t.Fatal(err)
	}
	result, err := handler.Handle(context.Background(), ownerTurn(map[string]any{"conversation_id": held.ID, "retry_turn_id": turns[0].ID}), archiveStep)
	if err != nil || result["reply"] != "Now I answer." {
		t.Fatal(result, err)
	}
	retried := result["turn"].(map[string]any)
	if retried["id"] != turns[0].ID || retried["attempts"] != 2 || retried["state"] != "completed" {
		t.Fatal(retried)
	}
	if last := model.asked[len(model.asked)-1]; last[len(last)-1]["content"] != "q" {
		t.Fatal("the retry sends the turn's own message", last)
	}
}

func TestTheContextComesFromTheArchivesCompletedTurnsAcrossARestart(t *testing.T) {
	path := filepath.Join(t.TempDir(), conversations.FileName)
	store, _ := conversations.Open(path)
	archive := conversation.New(store)
	held, _ := archive.Start(context.Background(), conversation.OwnerScope, conversation.KindGeneral, "", "")
	first := &scriptedModel{answers: []map[string]any{{"reply": "Plato."}, nil}}
	handler := NewChatDomainHandler(first).WithArchive(archive)
	handler.Handle(context.Background(), ownerTurn(map[string]any{"content": "Who wrote the Republic?", "conversation_id": held.ID, "send_id": "send-0001"}), archiveStep)
	handler.Handle(context.Background(), ownerTurn(map[string]any{"content": "a failed question", "conversation_id": held.ID, "send_id": "send-0002"}), archiveStep)
	store.Close()

	// A new orchestrator, a new handler: no session survives, only the archive.
	reopened, _ := conversations.Open(path)
	defer reopened.Close()
	later := &scriptedModel{answers: []map[string]any{{"reply": "The cave."}}}
	NewChatDomainHandler(later).WithArchive(conversation.New(reopened)).Handle(context.Background(),
		ownerTurn(map[string]any{"content": "And its best image?", "conversation_id": held.ID, "send_id": "send-0003"}), archiveStep)
	asked := later.asked[0]
	var said []string
	for _, message := range asked {
		said = append(said, message["role"]+":"+message["content"])
	}
	if strings.Join(said, "|") != "user:Who wrote the Republic?|assistant:Plato.|user:And its best image?" {
		t.Fatal("only completed turns, in order, then the new message", said)
	}
}

func TestAReadingConversationIsGroundedByItsOwnReading(t *testing.T) {
	archive := archiveFor(t)
	model := &scriptedModel{answers: []map[string]any{{"reply": "It argues for rules."}}}
	resolved := []string{}
	handler := NewChatDomainHandler(model).WithArchive(archive).WithReading(func(_ context.Context, id string) (digestdomain.Reference, error) {
		resolved = append(resolved, id)
		return digestdomain.Reference{ID: id, Title: "Rules", Source: "Journal", Summary: "Bounded excerpt"}, nil
	})
	held, _ := archive.Start(context.Background(), conversation.OwnerScope, conversation.KindReading, "item-1", "")
	// A payload naming another reading does not change what the conversation is about.
	if _, err := handler.Handle(context.Background(), ownerTurn(map[string]any{"content": "Explain", "conversation_id": held.ID, "send_id": "send-0001", "reading_id": "item-9"}), archiveStep); err != nil {
		t.Fatal(err)
	}
	if len(resolved) != 1 || resolved[0] != "item-1" {
		t.Fatal(resolved)
	}
}

func TestAChannelMessageWithArchiveFieldsGoesOnAsBefore(t *testing.T) {
	model := &scriptedModel{answers: []map[string]any{{"reply": "hello"}}}
	handler := NewChatDomainHandler(model).WithArchive(archiveFor(t))
	result, err := handler.Handle(context.Background(), core.Event{Source: core.SourceDiscord, Type: "message.created",
		Payload: map[string]any{"content": "hi", "conversation_id": "0123456789abcdef0123456789abcdef", "send_id": "send-0001"}}, archiveStep)
	if err != nil || !strings.Contains(result["reply"].(string), "hello") || result["turn"] != nil {
		t.Fatal(result, err)
	}
}

func TestAnArchivedTurnWithoutAnArchiveSaysSo(t *testing.T) {
	handler := NewChatDomainHandler(&scriptedModel{})
	_, err := handler.Handle(context.Background(), ownerTurn(map[string]any{"content": "q", "conversation_id": "0123456789abcdef0123456789abcdef", "send_id": "send-0001"}), archiveStep)
	if err == nil || !strings.Contains(err.Error(), "archive is unavailable") {
		t.Fatal(err)
	}
}

func TestAGeneralConversationIgnoresAReadingSentWithATurn(t *testing.T) {
	archive := archiveFor(t)
	model := &scriptedModel{answers: []map[string]any{{"reply": "General answer."}}}
	resolved := 0
	handler := NewChatDomainHandler(model).WithArchive(archive).WithReading(func(context.Context, string) (digestdomain.Reference, error) {
		resolved++
		return digestdomain.Reference{}, nil
	})
	held, _ := archive.Start(context.Background(), conversation.OwnerScope, conversation.KindGeneral, "", "")
	if _, err := handler.Handle(context.Background(), ownerTurn(map[string]any{"content": "Explain", "conversation_id": held.ID, "send_id": "send-0001", "reading_id": "item-9"}), archiveStep); err != nil {
		t.Fatal(err)
	}
	if resolved != 0 {
		t.Fatal("a general conversation is grounded by no reading")
	}
}

func TestAnArchivedQuestionAboutRecentReadingsIsGroundedByTheFeeds(t *testing.T) {
	archive := archiveFor(t)
	model := &scriptedModel{answers: []map[string]any{{"reply": "Two pieces came in."}}}
	looked := 0
	handler := NewChatDomainHandler(model).WithArchive(archive).WithRecentReadings(func(_ context.Context, question string, limit int) ([]digestdomain.Reference, error) {
		looked++
		return []digestdomain.Reference{{Title: "A new essay", Source: "Example feed", Summary: "An excerpt."}}, nil
	})
	held, _ := archive.Start(context.Background(), conversation.OwnerScope, conversation.KindGeneral, "", "")
	if _, err := handler.Handle(context.Background(), ownerTurn(map[string]any{"content": "Which articles came in today?", "conversation_id": held.ID, "send_id": "send-0001"}), archiveStep); err != nil {
		t.Fatal(err)
	}
	first := model.asked[0][0]
	if looked != 1 || first["role"] != "system" || !strings.Contains(first["content"], "A new essay") {
		t.Fatal(looked, model.asked[0])
	}
}

// cancellingModel is the caller going away while the reply is written, as a dropped stream does.
type cancellingModel struct{ cancel context.CancelFunc }

func (m cancellingModel) Generate(ctx context.Context, message string) (map[string]any, error) {
	return m.GenerateMessages(ctx, nil)
}
func (m cancellingModel) GenerateMessages(ctx context.Context, _ []map[string]string) (map[string]any, error) {
	m.cancel()
	return nil, ctx.Err()
}

func TestACancelledTurnIsRecordedInPlainWords(t *testing.T) {
	archive := archiveFor(t)
	ctx, cancel := context.WithCancel(context.Background())
	handler := NewChatDomainHandler(cancellingModel{cancel}).WithArchive(archive)
	held, _ := archive.Start(context.Background(), conversation.OwnerScope, conversation.KindGeneral, "", "")
	handler.Handle(ctx, ownerTurn(map[string]any{"content": "q", "conversation_id": held.ID, "send_id": "send-0001"}), archiveStep)
	turns, _ := archive.Turns(context.Background(), conversation.OwnerScope, held.ID, 0, 10)
	if len(turns) != 1 || turns[0].State != conversation.TurnFailed || turns[0].Reason != "the request stopped before the reply" {
		t.Fatal(turns)
	}
}

// The CLI's turns go through moderation before chat; a retry carries no text of its own.
func TestTheCLISendsAndRetriesThroughTheService(t *testing.T) {
	archive := archiveFor(t)
	model := &scriptedModel{answers: []map[string]any{nil, {"reply": "Answered on retry."}}}
	service := core.DefaultService(NewModerationDomainHandler(nil), NewChatDomainHandler(model).WithArchive(archive))
	held, _ := archive.Start(context.Background(), conversation.OwnerScope, conversation.KindGeneral, "", "")
	cli := func(id string, payload map[string]any) (core.ProcessResult, error) {
		return service.ProcessWithResult(context.Background(), core.Event{EventID: id, Source: core.SourceCLI, Type: "message.created", User: core.EventUser{ID: "cli"}, Channel: core.EventChannel{ID: "cli"}, Payload: payload})
	}
	if _, err := cli("e-1", map[string]any{"content": "What is a reader?", "conversation_id": held.ID, "send_id": "send-0001"}); err == nil {
		t.Fatal("the first send fails with the model down")
	}
	turns, _ := archive.Turns(context.Background(), conversation.OwnerScope, held.ID, 0, 10)
	result, err := cli("e-2", map[string]any{"conversation_id": held.ID, "retry_turn_id": turns[0].ID})
	if err != nil {
		t.Fatal(err)
	}
	chat, _ := result.Result["chat"].(map[string]any)
	if chat["reply"] != "Answered on retry." {
		t.Fatal(result.Result)
	}
}
