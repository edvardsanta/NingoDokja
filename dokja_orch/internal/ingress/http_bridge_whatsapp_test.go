package ingress

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"

	"read_books/internal/core"

	"github.com/gofiber/fiber/v2"
)

func TestBridgeEventFromRequestMapsWhatsAppChat(t *testing.T) {
	event := bridgeEventFromRequest(bridgeRequestFromJSON(t, `{
		"event_type": "message.created",
		"interface": "whatsapp",
		"payload": {"content": "hello dokja", "userId": "u1@s.whatsapp.net", "channelId": "chat1", "messageId": "m1",
			"sessionKey": "whatsapp:chat1:u1@s.whatsapp.net", "entryKind": "message", "chatKind": "direct",
			"senderNumbers": ["5511987654321"], "isSelfChat": false, "mentionsBot": false}
	}`))

	if event.Source != core.SourceWhatsApp {
		t.Fatalf("expected whatsapp source, got %q", event.Source)
	}
	if event.Type != "message.created" || event.Payload["content"] != "hello dokja" {
		t.Fatalf("unexpected event %q %#v", event.Type, event.Payload)
	}
	if event.Context["interface"] != "whatsapp" || event.Context["session_key"] != "whatsapp:chat1:u1@s.whatsapp.net" {
		t.Fatalf("unexpected context %#v", event.Context)
	}
	if event.Context["gateway_inbound"] != true || event.Context["entry_kind"] != "message" || event.Context["chat_kind"] != "direct" {
		t.Fatalf("missing eligibility metadata %#v", event.Context)
	}
	if numbers, ok := event.Context["sender_numbers"].([]string); !ok || len(numbers) != 1 || numbers[0] != "5511987654321" {
		t.Fatalf("sender numbers were not preserved: %#v", event.Context["sender_numbers"])
	}
}

func TestBridgeCarriesWhetherTheLinkedAccountWrote(t *testing.T) {
	event := bridgeEventFromRequest(bridgeRequestFromJSON(t, `{
		"event_type": "message.created", "interface": "whatsapp",
		"payload": {"content": "hello", "userId": "u1", "channelId": "group1@g.us", "chatKind": "group",
			"senderNumbers": [], "isSelfChat": false, "isOwnAccount": true, "mentionsBot": true}
	}`))
	if own, ok := event.Context["is_own_account"].(bool); !ok || !own {
		t.Fatalf("the flag was not carried: %#v", event.Context["is_own_account"])
	}
}

func TestBridgeInterfaceResolvesWhatsApp(t *testing.T) {
	for _, name := range []string{"whatsapp", " WhatsApp "} {
		if _, source, ok := bridgeInterface(name); !ok || source != core.SourceWhatsApp {
			t.Fatalf("%q resolved to %q, ok=%t", name, source, ok)
		}
	}
	for _, name := range []string{"whats", "whatsapp2", "wa"} {
		if _, _, ok := bridgeInterface(name); ok {
			t.Fatalf("%q must not be accepted", name)
		}
	}
}

func TestBridgeRefusesMissingOrWrongGatewayCredential(t *testing.T) {
	app := fiber.New()
	app.Post("/orchestrator", (&HTTPBridgeIngress{token: "fixture-gateway-secret"}).handle)
	for _, credential := range []string{"", "wrong", "Bearer wrong", "fixture-gateway-secret"} {
		request := httptest.NewRequest("POST", "/orchestrator", strings.NewReader(`{"event_type":"message.created","interface":"whatsapp","payload":{"content":"hello"}}`))
		request.Header.Set("Content-Type", "application/json")
		if credential != "" {
			request.Header.Set("Authorization", credential)
		}
		response, err := app.Test(request)
		if err != nil {
			t.Fatal(err)
		}
		if response.StatusCode != fiber.StatusUnauthorized {
			t.Fatalf("credential %q returned %d", credential, response.StatusCode)
		}
	}
}

func TestBridgeHandleRefusesNonChatWhatsAppEvents(t *testing.T) {
	app := fiber.New()
	app.Post("/orchestrator", (&HTTPBridgeIngress{}).handle)

	for name, body := range map[string]string{
		"whatsapp meme event":    `{"event_type":"meme.fetch","interface":"whatsapp","payload":{"content":"x"}}`,
		"whatsapp custom event":  `{"event_type":"discord.send","interface":"whatsapp","payload":{"content":"x"}}`,
		"whatsapp default event": `{"interface":"whatsapp","payload":{"content":"x"}}`,
		"whatsapp music event":   `{"event_type":"music.play","interface":"whatsapp","payload":{"content":"x"}}`,
	} {
		t.Run(name, func(t *testing.T) {
			request := httptest.NewRequest("POST", "/orchestrator", strings.NewReader(body))
			request.Header.Set("Content-Type", "application/json")
			response, err := app.Test(request)
			if err != nil {
				t.Fatal(err)
			}
			if response.StatusCode != fiber.StatusBadRequest {
				t.Fatalf("expected 400, got %d", response.StatusCode)
			}
		})
	}
}

func TestWhatsAppTextNeverBecomesADiscordMemeCommand(t *testing.T) {
	for _, text := range []string{"meme", "meme fetch 5", "!meme status", "/meme refresh 3", "Meme Fetch"} {
		event := bridgeEventFromRequest(bridgeRequestFromJSON(t, `{
			"event_type": "message.created",
			"interface": "whatsapp",
			"payload": {"content": `+quoteJSON(t, text)+`, "userId": "u1", "channelId": "chat1", "messageId": "m1"}
		}`))

		if event.Type != "message.created" {
			t.Fatalf("%q became %q: WhatsApp text must stay chat", text, event.Type)
		}
		if event.Payload["content"] != text {
			t.Fatalf("%q lost its content, got %#v", text, event.Payload)
		}
		if event.Source != core.SourceWhatsApp || event.Context["content"] != text {
			t.Fatalf("%q: unexpected source or context %q %#v", text, event.Source, event.Context)
		}
	}
}

func bridgeRequestFromJSON(t *testing.T, raw string) BridgeRequest {
	t.Helper()
	var request BridgeRequest
	if err := json.Unmarshal([]byte(raw), &request); err != nil {
		t.Fatalf("bad fixture: %v", err)
	}
	return request
}

func quoteJSON(t *testing.T, value string) string {
	t.Helper()
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

func TestBridgeEventFromRequestKeepsDiscordWhenNoInterfaceIsSent(t *testing.T) {
	event := bridgeEventFromRequest(bridgeRequestFromJSON(t, `{
		"event_type": "message.created",
		"payload": {"content": "hello", "userId": "u1", "channelId": "c1", "messageId": "m1"}
	}`))

	if event.Source != core.SourceDiscord || event.Context["interface"] != "discord" {
		t.Fatalf("existing clients must stay Discord, got %q %#v", event.Source, event.Context["interface"])
	}
}

func TestBridgeInterfaceResolvesKnownNamesOnly(t *testing.T) {
	for name, want := range map[string]core.EventSource{
		"": core.SourceDiscord, " Discord ": core.SourceDiscord, "whatsapp": core.SourceWhatsApp, "WHATSAPP": core.SourceWhatsApp,
	} {
		if _, source, ok := bridgeInterface(name); !ok || source != want {
			t.Fatalf("%q resolved to %q, ok=%t", name, source, ok)
		}
	}
	for _, name := range []string{"slack", "cli", "orchestrator", "whatsapp2"} {
		if _, _, ok := bridgeInterface(name); ok {
			t.Fatalf("%q must not be accepted", name)
		}
	}
}

func TestBridgeHandleRefusesAnUnknownInterface(t *testing.T) {
	app := fiber.New()
	app.Post("/orchestrator", (&HTTPBridgeIngress{}).handle)

	request := httptest.NewRequest("POST", "/orchestrator", strings.NewReader(`{"event_type":"message.created","interface":"slack","payload":{"content":"x"}}`))
	request.Header.Set("Content-Type", "application/json")
	response, err := app.Test(request)
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != fiber.StatusBadRequest {
		t.Fatalf("expected 400, got %d", response.StatusCode)
	}
}

func TestDiscordKeepsItsMemeTextCommands(t *testing.T) {
	cases := map[string]string{"meme fetch 5": "meme.fetch", "!meme status": "meme.status", "meme refresh 3": "meme.pool.refresh"}
	for text, want := range cases {
		event := bridgeEventFromRequest(bridgeRequestFromJSON(t, `{
			"event_type": "message.created",
			"payload": {"content": `+quoteJSON(t, text)+`, "userId": "u1", "channelId": "c1", "messageId": "m1"}
		}`))

		if event.Type != want || event.Source != core.SourceDiscord {
			t.Fatalf("%q: expected %q from discord, got %q from %q", text, want, event.Type, event.Source)
		}
	}
}

func TestExtractBridgeReplyIsSilentOnlyForAnInterfaceThatIsOff(t *testing.T) {
	silent := core.ProcessResult{Result: map[string]any{
		"skipped": true, "silent": true, "reason": "interface whatsapp is switched off",
	}}
	if reply := extractBridgeReply(silent); reply != "" {
		t.Fatalf("an interface that is off must answer nothing, got %q", reply)
	}

	service := core.ProcessResult{Result: map[string]any{
		"skipped": true, "silent": false, "reason": "service meme is disabled",
	}}
	if reply := extractBridgeReply(service); !strings.Contains(reply, "switched off right now") {
		t.Fatalf("a service that is off keeps its visible reply, got %q", reply)
	}
}
