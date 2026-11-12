package handlers

import (
	"context"
	"fmt"
	chat "read_books/dokja_domain/dokja_chat"
	"strings"
	"testing"
	"time"
)

func budgetMessages(turns, chars int) []map[string]string {
	messages := []map[string]string{{"role": "system", "content": strings.Repeat("p", 1000)}}
	for i := 0; i < turns; i++ {
		messages = append(messages,
			map[string]string{"role": "user", "content": fmt.Sprintf("u%d-%s", i, strings.Repeat("x", chars))},
			map[string]string{"role": "assistant", "content": fmt.Sprintf("a%d-%s", i, strings.Repeat("y", chars))},
		)
	}
	return messages
}

func TestFitChatPromptKeepsAWithinBudgetPromptUntouched(t *testing.T) {
	messages := budgetMessages(2, 100)
	if got := fitChatPrompt(messages); len(got) != len(messages) {
		t.Fatal("a within-budget prompt must not change")
	}
}

func TestFitChatPromptDropsOldestTurnsAndKeepsTheFrame(t *testing.T) {
	messages := budgetMessages(20, 4000)
	if chatHistorySize(messages) <= chatPromptBudget {
		t.Fatal("fixture must exceed the budget")
	}
	got := fitChatPrompt(messages)
	if chatHistorySize(got) > chatPromptBudget {
		t.Fatalf("still over budget: %d", chatHistorySize(got))
	}
	last := got[len(got)-1]
	if last["role"] != "assistant" || !strings.HasPrefix(last["content"], "a19-") {
		t.Fatalf("the latest turn must survive, got %q", last["content"][:4])
	}
	if got[0]["role"] != "system" || !strings.Contains(got[0]["content"], "oldest turns were left out") {
		t.Fatalf("the trim must say itself: %#v", got[0])
	}
	// Every system message of the frame stays.
	for _, message := range got {
		if message["role"] == "system" && strings.HasPrefix(message["content"], "p") {
			return
		}
	}
	t.Fatal("the persona frame was dropped")
}

func TestHalveChatHistoryKeepsTheLatestTurn(t *testing.T) {
	messages := budgetMessages(4, 10)
	got, ok := halveChatHistory(messages)
	if !ok {
		t.Fatal("expected a drop")
	}
	turns := 0
	for _, message := range got {
		if message["role"] == "user" || message["role"] == "assistant" {
			turns++
		}
	}
	if turns != 4 {
		t.Fatalf("expected half the turns, got %d", turns)
	}
	last := got[len(got)-1]
	if !strings.HasPrefix(last["content"], "a3-") {
		t.Fatalf("the latest turn must survive, got %q", last["content"])
	}
}

// Regression: a PDF turn on a long session made the assembled prompt exceed the
// runtime's supported size and the whole turn failed silently. The prompt is now
// trimmed to the budget before generation, and a size refusal retries once with
// half the history.
func TestBigAttachmentTurnStillAnswersInsteadOfFailingSilently(t *testing.T) {
	reader := &webReadingService{}
	g := &presentationCaptureGenerator{}
	h := NewChatDomainHandler(g).WithWebReading(reader, nil)
	event := chatEvent("")
	event.Payload["document_attachments"] = []chat.WebAttachment{{URL: "https://example.com/malthus.pdf", Filename: "malthus.pdf", Size: 1}}
	event.Payload["locale"] = "en"

	history := []chatMessage{}
	for i := 0; i < 16; i++ {
		history = append(history,
			chatMessage{Role: "user", Content: fmt.Sprintf("q%d %s", i, strings.Repeat("x", 4000))},
			chatMessage{Role: "assistant", Content: fmt.Sprintf("a%d %s", i, strings.Repeat("y", 4000))},
		)
	}
	h.sessions.items[chatSessionBaseKey(event)] = chatSessionState{id: "big-session", lastSeenAt: time.Now(), messages: history}

	result, err := h.Handle(context.Background(), event, chatStep)
	if err != nil {
		t.Fatalf("the turn must not fail: %v", err)
	}
	if reply, _ := result["reply"].(string); strings.TrimSpace(reply) == "" {
		t.Fatalf("expected an answer, got %#v", result)
	}
	if chatHistorySize(g.messages) > chatPromptBudget {
		t.Fatalf("the generator still received %d bytes", chatHistorySize(g.messages))
	}
}

type refusingGenerator struct {
	presentationCaptureGenerator
	calls int
}

func (r *refusingGenerator) GenerateMessages(ctx context.Context, messages []map[string]string) (map[string]any, error) {
	r.calls++
	r.messages = messages
	if r.calls == 1 {
		return nil, fmt.Errorf("chat history exceeds supported size")
	}
	return map[string]any{"reply": "model reply"}, nil
}

func TestASizeRefusalRetriesOnceWithHalfTheHistory(t *testing.T) {
	g := &refusingGenerator{}
	h := NewChatDomainHandler(g)
	event := chatEvent("e ai?")
	history := []chatMessage{}
	for i := 0; i < 8; i++ {
		history = append(history,
			chatMessage{Role: "user", Content: fmt.Sprintf("q%d", i)},
			chatMessage{Role: "assistant", Content: fmt.Sprintf("a%d", i)},
		)
	}
	h.sessions.items[chatSessionBaseKey(event)] = chatSessionState{id: "retry-session", lastSeenAt: time.Now(), messages: history}

	result, err := h.Handle(context.Background(), event, chatStep)
	if err != nil {
		t.Fatal(err)
	}
	if g.calls != 2 {
		t.Fatalf("expected one retry, got %d calls", g.calls)
	}
	if reply, _ := result["reply"].(string); reply != "model reply" {
		t.Fatalf("expected the retry's answer, got %#v", result)
	}
	turns := 0
	keepsFirst := false
	for _, message := range g.messages {
		if message["role"] == "user" || message["role"] == "assistant" {
			turns++
			if strings.HasPrefix(message["content"], "q0 ") || message["content"] == "q0" {
				keepsFirst = true
			}
		}
	}
	if turns >= 17 || keepsFirst {
		t.Fatalf("the retry must carry a reduced history, got %d turns (first kept: %t)", turns, keepsFirst)
	}
}
