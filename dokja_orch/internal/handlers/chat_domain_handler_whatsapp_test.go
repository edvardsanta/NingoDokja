package handlers

import (
	"testing"

	"read_books/internal/core"
)

func TestChatMessageFromEventReadsTheContextForWhatsApp(t *testing.T) {
	event := core.Event{Source: core.SourceWhatsApp, Context: map[string]any{"content": "hello"}}

	if got := chatMessageFromEvent(event); got != "hello" {
		t.Fatalf("expected the WhatsApp message, got %q", got)
	}
}
