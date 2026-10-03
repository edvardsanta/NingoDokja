package handlers

import (
	"context"
	"read_books/internal/core"
	"strings"
	"testing"
)

func TestCommunicationsSendUsesExistingDeliveryRules(t *testing.T) {
	token := strings.Repeat("x", 32)
	deliverer := &fakeDiscordDeliverer{}
	handler := NewSystemDomainHandler(nil, nil, nil, deliverer, "123")
	service := core.DefaultService(handler).WithCommunicationsToken(token)
	event := core.Event{Source: "desktop", Type: "communications.send", Context: map[string]any{"communications_token": token}, Payload: map[string]any{"channel_ids": []any{"456"}, "content": "hello"}}
	if _, err := service.ProcessWithResult(context.Background(), event); err == nil || len(deliverer.deliveries) != 0 {
		t.Fatal("unconfigured channel accepted")
	}
	event.Payload["channel_ids"] = []any{"123"}
	event.Context = map[string]any{"communications_token": "wrong"}
	if _, err := service.ProcessWithResult(context.Background(), event); err == nil || len(deliverer.deliveries) != 0 {
		t.Fatal("unauthenticated send accepted")
	}
	event.Context = map[string]any{"communications_token": token}
	if _, err := service.ProcessWithResult(context.Background(), event); err != nil || len(deliverer.deliveries) != 1 {
		t.Fatal("valid send failed", err)
	}
	event.Payload["attachment_url"] = "https://example.com/a.png"
	event.Payload["force_nsfw"] = true
	if _, err := service.ProcessWithResult(context.Background(), event); err == nil || len(deliverer.deliveries) != 1 {
		t.Fatal("desktop bypass accepted")
	}
}
