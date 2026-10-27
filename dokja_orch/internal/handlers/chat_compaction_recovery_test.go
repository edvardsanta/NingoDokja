package handlers

import (
	"context"
	"errors"
	"path/filepath"
	chat "read_books/dokja_domain/dokja_chat"
	"read_books/internal/core"
	chatsessions "read_books/internal/infrastructure/chat_sessions"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestCompactionRecoveryPreservesDurableTurnsAcrossRestart(t *testing.T) {
	path := filepath.Join(t.TempDir(), chatsessions.FileName)
	store, err := chatsessions.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	g := &compactionGenerator{summary: strings.Repeat("x", chat.MaxSummaryBytes+1)}
	h := NewChatDomainHandler(g).WithSessionStore(store)
	h.sessions.maxMsgs = 2
	event := channelEvent("fixture-scholar", "Scholar", "Keep this exact definition")
	if _, err = h.Handle(t.Context(), event, chatStep); err != nil {
		t.Fatal(err)
	}
	before, found, err := store.Load(t.Context(), chatSessionBaseKey(event))
	if err != nil || !found {
		t.Fatal(err)
	}
	event.Payload["content"] = "Continue the discussion"
	if _, err = h.Handle(t.Context(), event, chatStep); err != nil {
		t.Fatal(err)
	}
	after, found, err := store.Load(t.Context(), chatSessionBaseKey(event))
	if err != nil || !found {
		t.Fatal(err)
	}
	if after.ID != before.ID || after.Summary != before.Summary || len(after.Messages) != len(before.Messages)+2 || !reflect.DeepEqual(before.Messages, after.Messages[:len(before.Messages)]) {
		t.Fatal("failed summary discarded durable turns or changed session")
	}
	if err = store.Close(); err != nil {
		t.Fatal(err)
	}
	store, err = chatsessions.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	recovered := &compactionGenerator{summary: "The original definition remains settled."}
	h2 := NewChatDomainHandler(recovered).WithSessionStore(store)
	h2.sessions.maxMsgs = 2
	if _, err = h2.Handle(t.Context(), event, chatStep); err != nil {
		t.Fatal(err)
	}
	if len(recovered.requests) != 1 || !strings.Contains(recovered.requests[0][1]["content"], "Keep this exact definition") || !strings.Contains(recovered.requests[0][1]["content"], "Continue the discussion") {
		t.Fatal("restart lost pending raw history before a valid summary")
	}
}
func TestCompactionRecoveryKeepsCurrentInputAndPreviousSummary(t *testing.T) {
	manager := newChatSessionManager()
	manager.maxMsgs = 4
	event := chatEvent("current")
	key := chatSessionBaseKey(event)
	messages := []chatMessage{}
	for i := 0; i < 24; i++ {
		role := "user"
		if i%2 == 1 {
			role = "assistant"
		}
		messages = append(messages, chatMessage{Role: role, Content: strings.Repeat("x", 1000)})
	}
	manager.items[key] = chatSessionState{id: "original", summary: "Previously settled facts", messages: cloneMessages(messages), lastSeenAt: time.Now()}
	current := strings.Repeat("q", 20<<10)
	lifecycle, history, err := manager.BeginTurn(t.Context(), event, current, func(context.Context, chat.ContextCompaction) (string, error) { return "", context.DeadlineExceeded })
	if err != nil || lifecycle.CompactionFallback != "summary_timeout" || lifecycle.Compacted {
		t.Fatal(lifecycle, err)
	}
	if history[len(history)-1].Content != current || history[0].Role != "user" || !strings.Contains(history[0].Content, "Previously settled facts") {
		t.Fatal("current input or last valid summary missing")
	}
	if len(history) > 7 || manager.items[key].id != "original" || len(manager.items[key].messages) != len(messages)+1 {
		t.Fatal("projection unbounded or raw history lost")
	}
	for _, message := range history {
		if message.Role == "system" {
			t.Fatal("recovered data promoted to authority")
		}
	}
}
func TestCompactionRecoveryDoesNotSwallowParentCancellation(t *testing.T) {
	manager := newChatSessionManager()
	manager.maxMsgs = 2
	event := chatEvent("current")
	key := chatSessionBaseKey(event)
	before := chatSessionState{id: "original", messages: []chatMessage{{Role: "user", Content: "old"}, {Role: "assistant", Content: "old reply"}}, lastSeenAt: time.Now()}
	manager.items[key] = before
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	_, _, err := manager.BeginTurn(ctx, event, "current", func(context.Context, chat.ContextCompaction) (string, error) {
		cancel()
		return "A valid but late summary", nil
	})
	if !errors.Is(err, context.Canceled) || !reflect.DeepEqual(before, manager.items[key]) {
		t.Fatal("cancelled turn changed history", err)
	}
}
func TestRecoveryStorageFailureStillStopsBeforeGeneration(t *testing.T) {
	store := &failingSessionStore{}
	g := &compactionGenerator{summary: ""}
	h := NewChatDomainHandler(g).WithSessionStore(store)
	h.sessions.maxMsgs = 2
	event := chatEvent("old")
	if _, err := h.Handle(t.Context(), event, chatStep); err != nil {
		t.Fatal(err)
	}
	before := h.sessions.items[chatSessionBaseKey(event)]
	store.errorOnSave = errors.New("storage unavailable")
	if _, err := h.Handle(t.Context(), event, chatStep); err == nil || len(g.replies) != 1 || !reflect.DeepEqual(before, h.sessions.items[chatSessionBaseKey(event)]) {
		t.Fatal("durability failure was hidden")
	}
}
func TestCompactionFailureDoesNotAbortOrchestratorConversation(t *testing.T) {
	g := &compactionGenerator{summary: ""}
	handler := NewChatDomainHandler(g)
	handler.sessions.maxMsgs = 2
	service := core.DefaultService(NewModerationDomainHandler(nil), handler)
	event := chatEvent("continue")
	for i := 0; i < 3; i++ {
		result, err := service.ProcessWithResult(t.Context(), event)
		if err != nil {
			t.Fatal("compaction became a failed orchestration request", err)
		}
		response := result.Result[string(core.DomainChat)].(map[string]any)
		if response["reply"] != "An assistant claim about the kingdom." {
			t.Fatal("missing real reply", response)
		}
	}
	if len(g.requests) != 1 || len(g.replies) != 3 {
		t.Fatal("recovery retried summary on every message")
	}
}
