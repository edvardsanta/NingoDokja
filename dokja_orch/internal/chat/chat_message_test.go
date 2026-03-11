package chat

import (
	"context"
	"errors"
	"testing"
)

type fakeChatResponder struct {
	response string
	err      error
	content  string
	userID   string
}

func (f *fakeChatResponder) Respond(_ context.Context, content, userID string) (string, error) {
	f.content = content
	f.userID = userID
	if f.err != nil {
		return "", f.err
	}
	return f.response, nil
}

func TestChatMessageUseCaseExecute(t *testing.T) {
	responder := &fakeChatResponder{response: "ok"}
	useCase := NewChatMessageUseCase(responder)

	got, err := useCase.Execute(context.Background(), "hello", "u1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "ok" {
		t.Fatalf("expected response %q, got %q", "ok", got)
	}
	if responder.content != "hello" || responder.userID != "u1" {
		t.Fatalf("unexpected responder args: content=%q userID=%q", responder.content, responder.userID)
	}
}

func TestChatMessageUseCaseExecuteValidation(t *testing.T) {
	_, err := (*ChatMessageUseCase)(nil).Execute(context.Background(), "hello", "u1")
	if err == nil {
		t.Fatal("expected error for nil use case")
	}

	useCase := NewChatMessageUseCase(nil)
	_, err = useCase.Execute(context.Background(), "hello", "u1")
	if err == nil {
		t.Fatal("expected error for nil responder")
	}

	useCase = NewChatMessageUseCase(&fakeChatResponder{})
	_, err = useCase.Execute(context.Background(), "   ", "u1")
	if err == nil {
		t.Fatal("expected error for empty content")
	}
}

func TestChatMessageUseCaseExecutePropagatesError(t *testing.T) {
	expected := errors.New("boom")
	useCase := NewChatMessageUseCase(&fakeChatResponder{err: expected})

	_, err := useCase.Execute(context.Background(), "hello", "u1")
	if !errors.Is(err, expected) {
		t.Fatalf("expected wrapped error %v, got %v", expected, err)
	}
}
