package handlers

import (
	"context"
	"errors"
	communications "read_books/dokja_domain/dokja_communications"
	"read_books/internal/core"
	"strings"
	"testing"
)

type fakeWhatsApp struct {
	listed     []communications.Conversation
	listErr    error
	deliverErr error
	delivered  []string
}

func (f *fakeWhatsApp) List(context.Context) ([]communications.Conversation, error) {
	return f.listed, f.listErr
}

func (f *fakeWhatsApp) Deliver(_ context.Context, alias, content string) error {
	f.delivered = append(f.delivered, alias+"|"+content)
	return f.deliverErr
}

func whatsappSendHandler(t *testing.T, sender *fakeWhatsApp) (*SystemDomainHandler, *core.Controls, *fakeDiscordDeliverer) {
	t.Helper()
	handler, controls := quietHandler(t)
	discord := &fakeDiscordDeliverer{}
	handler.discord = discord
	handler.deliveryChannels = []string{"open-chan"}
	if sender != nil {
		handler.WithWhatsApp(sender)
	}
	return handler, controls, discord
}

func sendEvent(eventType string, payload map[string]any) (core.Event, core.WorkflowStep) {
	return core.Event{Type: eventType, Payload: payload}, core.WorkflowStep{Domain: core.DomainSystem, Action: "send-discord-message"}
}

var named = []communications.Conversation{{Alias: "friend", Kind: "direct", RetentionDays: 30}}

func TestSendToANamedWhatsAppConversationGoesThroughTheInterface(t *testing.T) {
	sender := &fakeWhatsApp{listed: named}
	handler, _, discord := whatsappSendHandler(t, sender)

	event, step := sendEvent("communications.send", map[string]any{"channel_ids": []any{"whatsapp:friend"}, "content": "  hello  "})
	result, err := handler.Handle(context.Background(), event, step)
	if err != nil {
		t.Fatal(err)
	}

	if len(sender.delivered) != 1 || sender.delivered[0] != "friend|hello" {
		t.Fatalf("unexpected deliveries %v", sender.delivered)
	}
	if sent, _ := result["sent_to"].([]string); len(sent) != 1 || sent[0] != "whatsapp:friend" {
		t.Fatalf("unexpected result %#v", result)
	}
	if len(discord.deliveries) != 0 {
		t.Fatal("a WhatsApp send must never reach Discord")
	}
}

func TestWhatsAppSendRefusesWhatGoesBeyondOneNamedTextMessage(t *testing.T) {
	for name, payload := range map[string]map[string]any{
		"an alias that is not named": {"channel_ids": []any{"whatsapp:stranger"}, "content": "hi"},
		"a picture":                  {"channel_ids": []any{"whatsapp:friend"}, "content": "hi", "attachment_url": "https://example.com/a.jpg"},
		"two conversations":          {"channel_ids": []any{"whatsapp:friend", "whatsapp:friend"}, "content": "hi"},
		"discord mixed in":           {"channel_ids": []any{"whatsapp:friend", "open-chan"}, "content": "hi"},
		"all":                        {"channel_ids": []any{"whatsapp:friend"}, "content": "hi", "all": true},
		"force":                      {"channel_ids": []any{"whatsapp:friend"}, "content": "hi", "force_nsfw": true},
		"no text":                    {"channel_ids": []any{"whatsapp:friend"}, "content": "   "},
	} {
		t.Run(name, func(t *testing.T) {
			sender := &fakeWhatsApp{listed: named}
			handler, _, discord := whatsappSendHandler(t, sender)
			event, step := sendEvent("communications.send", payload)

			if _, err := handler.Handle(context.Background(), event, step); err == nil {
				t.Fatal("expected a refusal")
			}
			if len(sender.delivered) != 0 || len(discord.deliveries) != 0 {
				t.Fatal("a refused send must reach nobody")
			}
		})
	}
}

func TestWhatsAppSendIsOnlyForCommunicationsSend(t *testing.T) {
	sender := &fakeWhatsApp{listed: named}
	handler, _, _ := whatsappSendHandler(t, sender)

	// discord.send is the other way a send reaches this handler, and it is refused for WhatsApp.
	event, step := sendEvent("discord.send", map[string]any{"channel_ids": []any{"whatsapp:friend"}, "content": "hi"})
	if _, err := handler.Handle(context.Background(), event, step); err == nil {
		t.Fatal("discord.send must not send to WhatsApp")
	}
	// No other event type reaches the send at all, so nothing is delivered whatever its payload says.
	for _, eventType := range []string{"meme.dispatch.scheduled", "message.created", "ningo.status"} {
		event, step := sendEvent(eventType, map[string]any{"channel_ids": []any{"whatsapp:friend"}, "content": "hi"})
		_, _ = handler.Handle(context.Background(), event, step)
	}
	if len(sender.delivered) != 0 {
		t.Fatal("nothing may be delivered")
	}
}

func TestWhatsAppSendStopsWhenTheInterfaceIsSwitchedOffOrNotConfigured(t *testing.T) {
	sender := &fakeWhatsApp{listed: named}
	handler, controls, _ := whatsappSendHandler(t, sender)
	if err := controls.SetService("whatsapp", false); err != nil {
		t.Fatal(err)
	}
	event, step := sendEvent("communications.send", map[string]any{"channel_ids": []any{"whatsapp:friend"}, "content": "hi"})

	if _, err := handler.Handle(context.Background(), event, step); err == nil || !strings.Contains(err.Error(), "switched off") {
		t.Fatalf("expected a switched-off refusal, got %v", err)
	}
	if len(sender.delivered) != 0 {
		t.Fatal("nothing may be delivered while the interface is off")
	}

	unconfigured, _, _ := whatsappSendHandler(t, nil)
	if _, err := unconfigured.Handle(context.Background(), event, step); err == nil {
		t.Fatal("expected a refusal when there is no WhatsApp interface")
	}
}

func TestWhatsAppSendErrorsAreTheInterfacesFixedSentenceAndNeverTheText(t *testing.T) {
	sender := &fakeWhatsApp{listed: named, deliverErr: errors.New("too many messages: wait a moment before sending again")}
	handler, _, _ := whatsappSendHandler(t, sender)
	event, step := sendEvent("communications.send", map[string]any{"channel_ids": []any{"whatsapp:friend"}, "content": "a private sentence"})

	_, err := handler.Handle(context.Background(), event, step)

	if err == nil || !strings.Contains(err.Error(), "too many messages") || strings.Contains(err.Error(), "private") {
		t.Fatalf("unexpected error %v", err)
	}

	sender.listErr = errors.New("dial tcp 10.0.0.9:8094: refused")
	_, err = handler.Handle(context.Background(), event, step)
	if err == nil || strings.Contains(err.Error(), "10.0.0.9") {
		t.Fatalf("an unreachable interface must not leak its address, got %v", err)
	}
}

func TestDiscordSendsAreUnchangedByTheWhatsAppBranch(t *testing.T) {
	sender := &fakeWhatsApp{listed: named}
	handler, _, discord := whatsappSendHandler(t, sender)
	event, step := sendEvent("communications.send", map[string]any{"channel_ids": []any{"open-chan"}, "content": "hi"})

	if _, err := handler.Handle(context.Background(), event, step); err != nil {
		t.Fatal(err)
	}

	if len(discord.deliveries) != 1 || len(sender.delivered) != 0 {
		t.Fatalf("expected a Discord delivery only, got discord=%d whatsapp=%d", len(discord.deliveries), len(sender.delivered))
	}
}
