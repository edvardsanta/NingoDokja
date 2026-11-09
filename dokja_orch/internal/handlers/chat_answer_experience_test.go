package handlers

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"

	conversation "read_books/dokja_domain/dokja_conversation"
	memorydomain "read_books/dokja_domain/dokja_memory"
	"read_books/internal/core"
)

// answerMemory stands in for the memory service with its store's rules: a record with a known ref
// changes nothing, and the first verdict wins.
type answerMemory struct {
	mu       sync.Mutex
	down     bool
	records  []map[string]any
	outcomes map[string]string
	calls    []string
}

func newAnswerMemory() *answerMemory { return &answerMemory{outcomes: map[string]string{}} }

func (m *answerMemory) Dispatch(_ context.Context, eventType string, payload map[string]any) (map[string]any, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.calls = append(m.calls, eventType)
	if m.down {
		return nil, errors.New("memory service unavailable")
	}
	switch eventType {
	case "memory.recall":
		return map[string]any{"neighbors": []any{}, "outcomes": map[string]any{}}, nil
	case "memory.record":
		ref := payload["ref"].(string)
		if _, known := m.outcomes[ref]; !known {
			m.outcomes[ref] = ""
			m.records = append(m.records, payload)
		}
		return map[string]any{"recorded": true}, nil
	case "memory.resolve":
		ref, outcome := payload["ref"].(string), payload["outcome"].(string)
		current, known := m.outcomes[ref]
		if !known {
			return map[string]any{"found": false, "resolved": false, "outcome": ""}, nil
		}
		if current != "" {
			return map[string]any{"found": true, "resolved": false, "outcome": current}, nil
		}
		m.outcomes[ref] = outcome
		return map[string]any{"found": true, "resolved": true, "outcome": outcome}, nil
	}
	return nil, errors.New("unexpected memory request " + eventType)
}

func (m *answerMemory) recorded() []map[string]any {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]map[string]any(nil), m.records...)
}

// inline runs the background work at once, so a test sees what it recorded.
func inline(e *ChatAnswerExperience) *ChatAnswerExperience {
	e.spawn = func(work func()) { work() }
	return e
}

func answeredConversation(t *testing.T, model *scriptedModel, memory *answerMemory) (*ChatDomainHandler, *conversation.Archive, conversation.Conversation) {
	t.Helper()
	archive := archiveFor(t)
	handler := NewChatDomainHandler(model).WithArchive(archive).WithAnswerExperience(inline(NewChatAnswerExperience(memory, nil)))
	held, _ := archive.Start(context.Background(), conversation.OwnerScope, conversation.KindGeneral, "", "")
	return handler, archive, held
}

func TestACompletedAnswerIsRecordedOnceWithAFrozenPrediction(t *testing.T) {
	memory := newAnswerMemory()
	handler, _, held := answeredConversation(t, &scriptedModel{answers: []map[string]any{{"reply": "One who judges."}}}, memory)
	send := ownerTurn(map[string]any{"content": "What is a reader?", "conversation_id": held.ID, "send_id": "send-0001"})
	result, err := handler.Handle(context.Background(), send, archiveStep)
	if err != nil {
		t.Fatal(err)
	}
	// A replayed send is answered from the archive and notes nothing new.
	if _, err := handler.Handle(context.Background(), send, archiveStep); err != nil {
		t.Fatal(err)
	}
	records := memory.recorded()
	if len(records) != 1 {
		t.Fatalf("recorded %d experiences", len(records))
	}
	turn := result["turn"].(map[string]any)
	record := records[0]
	if record["ref"] != memorydomain.ChatAnswerRef(turn["id"].(string)) || record["action"] != memorydomain.ChatAnswerAction ||
		record["context"] != "What is a reader?" || record["detail"] != memorydomain.ChatAnswerUncited {
		t.Fatalf("unexpected record: %v", record)
	}
	if _, ok := record["predicted_p"].(float64); !ok {
		t.Fatal("the prediction was not frozen with the answer")
	}
	if _, ok := record["baseline_p"].(float64); !ok {
		t.Fatal("the baseline was not frozen with the answer")
	}
}

func TestAFailedTurnRecordsNoAnswer(t *testing.T) {
	memory := newAnswerMemory()
	handler, _, held := answeredConversation(t, &scriptedModel{answers: []map[string]any{nil}}, memory)
	if _, err := handler.Handle(context.Background(), ownerTurn(map[string]any{"content": "q", "conversation_id": held.ID, "send_id": "send-0001"}), archiveStep); err == nil {
		t.Fatal("the failure was not reported")
	}
	if len(memory.recorded()) != 0 || len(memory.calls) != 0 {
		t.Fatal("a failed turn reached memory")
	}
}

func TestMemoryBeingDownNeverFailsTheAnswer(t *testing.T) {
	memory := newAnswerMemory()
	memory.down = true
	handler, _, held := answeredConversation(t, &scriptedModel{answers: []map[string]any{{"reply": "Still here."}}}, memory)
	result, err := handler.Handle(context.Background(), ownerTurn(map[string]any{"content": "q", "conversation_id": held.ID, "send_id": "send-0001"}), archiveStep)
	if err != nil || result["reply"] != "Still here." {
		t.Fatal(result, err)
	}
}

// A group chat has no verdict to give, and its questions are other people's.
func TestAChannelAnswerIsNotRecorded(t *testing.T) {
	memory := newAnswerMemory()
	handler, _, _ := answeredConversation(t, &scriptedModel{answers: []map[string]any{{"reply": "hello"}}}, memory)
	if _, err := handler.Handle(context.Background(), core.Event{Source: core.SourceDiscord, Type: "message.created",
		Payload: map[string]any{"content": "hi", "conversation_id": "0123456789abcdef0123456789abcdef", "send_id": "send-0001"}}, archiveStep); err != nil {
		t.Fatal(err)
	}
	if len(memory.calls) != 0 {
		t.Fatal("a channel answer reached memory")
	}
}

func feedbackOn(t *testing.T, handler *ConversationDomainHandler, conversationID string, turn map[string]any, verdict string) (map[string]any, error) {
	t.Helper()
	return handler.Handle(context.Background(), core.Event{Source: core.SourceDesktop, Type: "conversation.feedback", Payload: map[string]any{
		// Requests arrive as JSON, so the turn's number is a float64 as it would be on the wire.
		"conversation_id": conversationID, "turn_id": turn["id"], "seq": float64(turn["seq"].(int)), "verdict": verdict,
	}}, core.WorkflowStep{})
}

func TestTheOwnersVerdictResolvesTheAnswerAndTheFirstOneStands(t *testing.T) {
	memory := newAnswerMemory()
	chat, archive, held := answeredConversation(t, &scriptedModel{answers: []map[string]any{{"reply": "One."}, {"reply": "Two."}}}, memory)
	first, _ := chat.Handle(context.Background(), ownerTurn(map[string]any{"content": "a", "conversation_id": held.ID, "send_id": "send-0001"}), archiveStep)
	second, _ := chat.Handle(context.Background(), ownerTurn(map[string]any{"content": "b", "conversation_id": held.ID, "send_id": "send-0002"}), archiveStep)
	handler := NewConversationDomainHandler(archive).WithAnswerFeedback(memory)

	useful, err := feedbackOn(t, handler, held.ID, first["turn"].(map[string]any), "useful")
	if err != nil || useful["found"] != true || useful["resolved"] != true || useful["verdict"] != "useful" {
		t.Fatal(useful, err)
	}
	wrong, err := feedbackOn(t, handler, held.ID, second["turn"].(map[string]any), "wrong")
	if err != nil || wrong["verdict"] != "wrong" {
		t.Fatal(wrong, err)
	}
	if memory.outcomes[memorydomain.ChatAnswerRef(first["turn"].(map[string]any)["id"].(string))] != memorydomain.OutcomeAccepted ||
		memory.outcomes[memorydomain.ChatAnswerRef(second["turn"].(map[string]any)["id"].(string))] != memorydomain.OutcomeReplaced {
		t.Fatal("verdicts mapped to the wrong outcomes", memory.outcomes)
	}
	changed, err := feedbackOn(t, handler, held.ID, first["turn"].(map[string]any), "wrong")
	if err != nil || changed["resolved"] != false || changed["verdict"] != "useful" {
		t.Fatal("a second verdict replaced the first", changed, err)
	}
}

func TestAVerdictNeedsACompletedAnswerOfThisConversation(t *testing.T) {
	memory := newAnswerMemory()
	chat, archive, held := answeredConversation(t, &scriptedModel{answers: []map[string]any{nil}}, memory)
	chat.Handle(context.Background(), ownerTurn(map[string]any{"content": "q", "conversation_id": held.ID, "send_id": "send-0001"}), archiveStep)
	turns, _ := archive.Turns(context.Background(), conversation.OwnerScope, held.ID, 0, 10)
	failed := map[string]any{"id": turns[0].ID, "seq": turns[0].Seq}
	handler := NewConversationDomainHandler(archive).WithAnswerFeedback(memory)

	if _, err := feedbackOn(t, handler, held.ID, failed, "useful"); !errors.Is(err, conversation.ErrConflict) {
		t.Fatal("a failed turn was judged", err)
	}
	if _, err := feedbackOn(t, handler, held.ID, map[string]any{"id": "another-turn", "seq": turns[0].Seq}, "useful"); !errors.Is(err, conversation.ErrNotFound) {
		t.Fatal("a turn that is not there was judged", err)
	}
	if _, err := feedbackOn(t, handler, held.ID, failed, "meh"); !errors.Is(err, conversation.ErrInvalid) {
		t.Fatal("an unknown verdict was accepted", err)
	}
	if _, err := handler.Handle(context.Background(), core.Event{Source: core.SourceDiscord, Type: "conversation.feedback",
		Payload: map[string]any{"conversation_id": held.ID, "turn_id": turns[0].ID, "seq": float64(1), "verdict": "useful"}}, core.WorkflowStep{}); err == nil {
		t.Fatal("a channel judged the owner's answer")
	}
	if len(memory.calls) != 0 {
		t.Fatal("a refused verdict reached memory")
	}
}

func TestAVerdictWithoutMemorySaysSo(t *testing.T) {
	memory := newAnswerMemory()
	chat, archive, held := answeredConversation(t, &scriptedModel{answers: []map[string]any{{"reply": "One."}}}, memory)
	result, _ := chat.Handle(context.Background(), ownerTurn(map[string]any{"content": "a", "conversation_id": held.ID, "send_id": "send-0001"}), archiveStep)
	if _, err := feedbackOn(t, NewConversationDomainHandler(archive), held.ID, result["turn"].(map[string]any), "useful"); !errors.Is(err, conversation.ErrUnavailable) {
		t.Fatal("feedback without memory wired", err)
	}
	memory.down = true
	_, err := feedbackOn(t, NewConversationDomainHandler(archive).WithAnswerFeedback(memory), held.ID, result["turn"].(map[string]any), "useful")
	if err == nil || !strings.Contains(err.Error(), "memory is unavailable") || strings.Contains(err.Error(), "service unavailable") {
		t.Fatal("memory outage was not reported plainly", err)
	}
}
