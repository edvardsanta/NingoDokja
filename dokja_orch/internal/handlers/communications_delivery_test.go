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

func TestAnOperatorSendMayNameAChannelTheCommunicationsDomainAllows(t *testing.T) {
	token := strings.Repeat("x", 32)
	deliverer := &fakeDiscordDeliverer{}
	asked := []string{}
	handler := NewSystemDomainHandler(nil, nil, nil, deliverer, "123").WithManualTargets(func(_ context.Context, channel string) (bool, error) {
		asked = append(asked, channel)
		return channel == "456", nil
	})
	service := core.DefaultService(handler).WithCommunicationsToken(token)
	send := func(eventType string, payload map[string]any) error {
		_, err := service.ProcessWithResult(context.Background(), core.Event{Source: "desktop", Type: eventType, Context: map[string]any{"communications_token": token}, Payload: payload})
		return err
	}
	if err := send("communications.send", map[string]any{"channel_ids": []any{"456"}, "content": "hello"}); err != nil || len(deliverer.deliveries) != 1 {
		t.Fatal("a channel of a configured server was refused", err)
	}
	if err := send("communications.send", map[string]any{"channel_ids": []any{"789"}, "content": "hello"}); err == nil || len(deliverer.deliveries) != 1 {
		t.Fatal("a channel the domain refuses was accepted")
	}
	if err := send("communications.send", map[string]any{"all": true, "content": "everyone"}); err != nil || len(deliverer.deliveries) != 2 {
		t.Fatal(err)
	}
	if last := deliverer.deliveries[len(deliverer.deliveries)-1]; last.channelID != "123" {
		t.Fatal("'all' goes only to the delivery channels:", deliverer.deliveries)
	}
	before := len(asked)
	if err := send("discord.send", map[string]any{"channel_ids": []any{"456"}, "content": "hello"}); err == nil {
		t.Fatal("discord.send, which the gateway can carry, must stay on the delivery channels")
	}
	if len(asked) != before {
		t.Fatal("discord.send asked about manual targets")
	}
}
