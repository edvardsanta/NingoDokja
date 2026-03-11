package chat

import (
	"context"
	"fmt"
	"strings"
)

// ChatResponder is a port that abstracts how chat replies are produced.
type ChatResponder interface {
	Respond(ctx context.Context, content, userID string) (string, error)
}

// ChatMessageUseCase orchestrates chat message handling without depending on any interface.
type ChatMessageUseCase struct {
	responder ChatResponder
}

func NewChatMessageUseCase(responder ChatResponder) *ChatMessageUseCase {
	return &ChatMessageUseCase{responder: responder}
}

func (u *ChatMessageUseCase) Execute(ctx context.Context, content, userID string) (string, error) {
	if u == nil {
		return "", fmt.Errorf("chat message use case is nil")
	}
	if u.responder == nil {
		return "", fmt.Errorf("chat responder is not configured")
	}

	if strings.TrimSpace(content) == "" {
		return "", fmt.Errorf("content cannot be empty")
	}

	return u.responder.Respond(ctx, content, userID)
}
