package handlers

import (
	"context"
	"errors"
	"read_books/internal/core"
	"strings"
	"testing"
	"time"
)

type fakeChatGenerator struct {
	message  string
	messages []map[string]string
	result   map[string]any
	err      error
}

func (f *fakeChatGenerator) Generate(_ context.Context, message string) (map[string]any, error) {
	f.message = message
	return f.result, f.err
}

func (f *fakeChatGenerator) GenerateMessages(_ context.Context, messages []map[string]string) (map[string]any, error) {
	f.messages = messages
	return f.result, f.err
}

func TestChatDomainHandlerHandle(t *testing.T) {
	handler := NewChatDomainHandler(&fakeChatGenerator{
		result: map[string]any{"reply": "hello"},
	})

	result, err := handler.Handle(context.Background(), core.Event{
		Type:    "message.created",
		Payload: map[string]any{"content": "ping"},
	}, core.WorkflowStep{
		Domain: core.DomainChat,
		Action: "generate-response",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result["reply"] != "hello" {
		t.Fatalf("expected reply hello, got %#v", result["reply"])
	}
}

func TestChatDomainHandlerMapsReplyFromRawContents(t *testing.T) {
	handler := NewChatDomainHandler(&fakeChatGenerator{
		result: map[string]any{
			"contents": []any{`{"user_message":"ping","ningo_response":{"text":"pong"}}`},
		},
	})

	result, err := handler.Handle(context.Background(), core.Event{
		Source:  core.SourceCLI,
		Type:    "message.created",
		Payload: map[string]any{"message": "ping"},
	}, core.WorkflowStep{
		Domain: core.DomainChat,
		Action: "generate-response",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result["reply"] != "pong" {
		t.Fatalf("expected reply pong, got %#v", result["reply"])
	}
}

func TestChatDomainHandlerMapsReplyFromRawTopLevelReply(t *testing.T) {
	handler := NewChatDomainHandler(&fakeChatGenerator{
		result: map[string]any{
			"reply": `{"user_message":"ping","ningo_response":{"text":"pong"}}`,
		},
	})

	result, err := handler.Handle(context.Background(), core.Event{
		Source:  core.SourceDiscord,
		Type:    "message.created",
		User:    core.EventUser{ID: "u1"},
		Channel: core.EventChannel{ID: "c1"},
		Payload: map[string]any{"content": "ping"},
		Context: map[string]any{"session_key": "discord:c1:u1", "force_new_session": true},
	}, core.WorkflowStep{
		Domain: core.DomainChat,
		Action: "generate-response",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	reply, _ := result["reply"].(string)
	if !strings.Contains(reply, "pong") {
		t.Fatalf("expected normalized pong reply, got %#v", reply)
	}
}

func TestChatDomainHandlerMapsReplyFromRawTopLevelResultObject(t *testing.T) {
	handler := NewChatDomainHandler(&fakeChatGenerator{
		result: map[string]any{
			"user_message": "ping",
			"ningo_response": map[string]any{
				"text": "pong",
			},
		},
	})

	result, err := handler.Handle(context.Background(), core.Event{
		Source:  core.SourceDiscord,
		Type:    "message.created",
		User:    core.EventUser{ID: "u1"},
		Channel: core.EventChannel{ID: "c1"},
		Payload: map[string]any{"content": "ping"},
		Context: map[string]any{"session_key": "discord:c1:u1", "force_new_session": true},
	}, core.WorkflowStep{
		Domain: core.DomainChat,
		Action: "generate-response",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	reply, _ := result["reply"].(string)
	if !strings.Contains(reply, "pong") {
		t.Fatalf("expected normalized pong reply, got %#v", reply)
	}
}

func TestChatDomainHandlerMapsReplyFromNestedResultObject(t *testing.T) {
	handler := NewChatDomainHandler(&fakeChatGenerator{
		result: map[string]any{
			"result": map[string]any{
				"ningo_response": map[string]any{
					"text": "pong",
				},
			},
		},
	})

	result, err := handler.Handle(context.Background(), core.Event{
		Source:  core.SourceDiscord,
		Type:    "message.created",
		User:    core.EventUser{ID: "u1"},
		Channel: core.EventChannel{ID: "c1"},
		Payload: map[string]any{"content": "ping"},
		Context: map[string]any{"session_key": "discord:c1:u1", "force_new_session": true},
	}, core.WorkflowStep{
		Domain: core.DomainChat,
		Action: "generate-response",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	reply, _ := result["reply"].(string)
	if !strings.Contains(reply, "pong") {
		t.Fatalf("expected normalized pong reply, got %#v", reply)
	}
}

func TestChatDomainHandlerMapsMessageAcrossSources(t *testing.T) {
	generator := &fakeChatGenerator{
		result: map[string]any{"reply": "ok"},
	}
	handler := NewChatDomainHandler(generator)

	_, err := handler.Handle(context.Background(), core.Event{
		Source:  core.SourceWeb,
		Type:    "message.created",
		Payload: map[string]any{"text": "from-web"},
	}, core.WorkflowStep{
		Domain: core.DomainChat,
		Action: "generate-response",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(generator.messages) != 1 || generator.messages[0]["content"] != "from-web" {
		t.Fatalf("expected generator conversation to contain from-web, got %#v", generator.messages)
	}
}

func TestChatDomainHandlerValidation(t *testing.T) {
	if _, err := (*ChatDomainHandler)(nil).Handle(context.Background(), core.Event{}, core.WorkflowStep{}); err == nil {
		t.Fatal("expected nil handler error")
	}

	handler := NewChatDomainHandler(nil)
	if _, err := handler.Handle(context.Background(), core.Event{}, core.WorkflowStep{}); err == nil {
		t.Fatal("expected missing generator error")
	}

	handler = NewChatDomainHandler(&fakeChatGenerator{})
	if _, err := handler.Handle(context.Background(), core.Event{Payload: map[string]any{}}, core.WorkflowStep{}); err == nil {
		t.Fatal("expected missing content error")
	}

	expected := errors.New("boom")
	handler = NewChatDomainHandler(&fakeChatGenerator{err: expected})
	if _, err := handler.Handle(context.Background(), core.Event{Payload: map[string]any{"content": "ping"}}, core.WorkflowStep{}); !errors.Is(err, expected) {
		t.Fatalf("expected propagated error, got %v", err)
	}
}

func TestChatDomainHandlerStartsSessionAndPrefixesReply(t *testing.T) {
	handler := NewChatDomainHandler(&fakeChatGenerator{
		result: map[string]any{"reply": "hello"},
	})
	handler.sessions.nowFn = func() time.Time { return time.Unix(100, 0).UTC() }

	result, err := handler.Handle(context.Background(), core.Event{
		Source:  core.SourceCLI,
		Type:    "message.created",
		User:    core.EventUser{ID: "u1"},
		Channel: core.EventChannel{ID: "cli"},
		Payload: map[string]any{"content": "ping"},
		Context: map[string]any{"session_key": "cli:repl:u1", "force_new_session": true},
	}, core.WorkflowStep{
		Domain: core.DomainChat,
		Action: "generate-response",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	reply, _ := result["reply"].(string)
	if !strings.Contains(reply, "New conversation session started") {
		t.Fatalf("expected session start notice, got %q", reply)
	}
}

func TestChatDomainHandlerRevokesTimedOutSession(t *testing.T) {
	handler := NewChatDomainHandler(&fakeChatGenerator{
		result: map[string]any{"reply": "hello"},
	})
	currentTime := time.Unix(100, 0).UTC()
	handler.sessions.nowFn = func() time.Time { return currentTime }

	event := core.Event{
		Source:  core.SourceDiscord,
		Type:    "message.created",
		User:    core.EventUser{ID: "u1"},
		Channel: core.EventChannel{ID: "c1"},
		Payload: map[string]any{"content": "ping"},
		Context: map[string]any{"session_key": "discord:c1:u1"},
	}
	if _, err := handler.Handle(context.Background(), event, core.WorkflowStep{Domain: core.DomainChat, Action: "generate-response"}); err != nil {
		t.Fatalf("unexpected first error: %v", err)
	}

	currentTime = currentTime.Add(4 * time.Minute)
	result, err := handler.Handle(context.Background(), event, core.WorkflowStep{Domain: core.DomainChat, Action: "generate-response"})
	if err != nil {
		t.Fatalf("unexpected second error: %v", err)
	}

	reply, _ := result["reply"].(string)
	if !strings.Contains(reply, "Previous conversation session was revoked") {
		t.Fatalf("expected revocation notice, got %q", reply)
	}
}

func TestChatDomainHandlerSendsConversationHistoryToGenerator(t *testing.T) {
	generator := &fakeChatGenerator{
		result: map[string]any{"reply": "second"},
	}
	handler := NewChatDomainHandler(generator)
	currentTime := time.Unix(100, 0).UTC()
	handler.sessions.nowFn = func() time.Time { return currentTime }

	event := core.Event{
		Source:  core.SourceDiscord,
		Type:    "message.created",
		User:    core.EventUser{ID: "u1"},
		Channel: core.EventChannel{ID: "c1"},
		Payload: map[string]any{"content": "first"},
		Context: map[string]any{"session_key": "discord:c1:u1", "force_new_session": true},
	}
	if _, err := handler.Handle(context.Background(), event, core.WorkflowStep{Domain: core.DomainChat, Action: "generate-response"}); err != nil {
		t.Fatalf("unexpected first error: %v", err)
	}

	currentTime = currentTime.Add(time.Minute)
	event.Payload = map[string]any{"content": "second"}
	event.Context = map[string]any{"session_key": "discord:c1:u1"}
	if _, err := handler.Handle(context.Background(), event, core.WorkflowStep{Domain: core.DomainChat, Action: "generate-response"}); err != nil {
		t.Fatalf("unexpected second error: %v", err)
	}

	if len(generator.messages) != 3 {
		t.Fatalf("expected 3 history messages, got %#v", generator.messages)
	}
	if generator.messages[0]["content"] != "first" || generator.messages[1]["role"] != "assistant" || generator.messages[2]["content"] != "second" {
		t.Fatalf("unexpected conversation history %#v", generator.messages)
	}
}
