package ingress

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	communications "read_books/dokja_domain/dokja_communications"
	"read_books/internal/core"
	"read_books/internal/handlers"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v2"
)

type bridgeWebReader struct{ calls int }

func (r *bridgeWebReader) Dispatch(_ context.Context, kind string, payload map[string]any) (map[string]any, error) {
	if kind != "feeds.web.read" {
		return nil, fmt.Errorf("unexpected operation")
	}
	r.calls++
	address := payload["url"].(string)
	return map[string]any{"url": address, "title": "Law", "passages": []map[string]any{{"text": "Art. 140. Include the financial impact estimate.", "label": "Art. 140", "url": address}}}, nil
}

type bridgeWebGenerator struct{ calls int }

func (g *bridgeWebGenerator) Generate(context.Context, string) (map[string]any, error) {
	return nil, fmt.Errorf("flat prompts forbidden")
}
func (g *bridgeWebGenerator) GenerateMessages(_ context.Context, messages []map[string]string) (map[string]any, error) {
	g.calls++
	return map[string]any{"reply": "The impact estimate is required. [1]"}, nil
}

func TestDiscordWebReadingIsAuthorizedBeforeFetchingAndKeepsChannelFocus(t *testing.T) {
	for _, allowed := range []bool{false, true} {
		t.Run(fmt.Sprint(allowed), func(t *testing.T) {
			reader := &bridgeWebReader{}
			generator := &bridgeWebGenerator{}
			gate := handlers.NewCommunicationsDomainHandler(nil, "").WithInboundPolicy(communications.InboundPolicy{DiscordChannels: []string{"allowed-channel"}})
			chat := handlers.NewChatDomainHandler(generator).WithWebReading(reader, nil)
			service := core.DefaultService(gate, handlers.NewModerationDomainHandler(nil), chat)
			app := fiber.New()
			app.Post("/orchestrator", (&HTTPBridgeIngress{service: service, token: "fixture-credential"}).handle)
			channel := "denied-channel"
			if allowed {
				channel = "allowed-channel"
			}
			send := func(user, content string) BridgeResponse {
				body, _ := json.Marshal(map[string]any{"event_type": "message.created", "interface": "discord", "payload": map[string]any{"content": content, "userId": user, "channelId": channel, "guildId": "fixture-guild", "messageId": "fixture-message", "entryKind": "message", "isDirectMessage": false, "mentionsBot": false, "locale": "en"}})
				req := httptest.NewRequest("POST", "/orchestrator", strings.NewReader(string(body)))
				req.Header.Set("Content-Type", "application/json")
				req.Header.Set("Authorization", "Bearer fixture-credential")
				res, err := app.Test(req)
				if err != nil {
					t.Fatal(err)
				}
				defer res.Body.Close()
				var answer BridgeResponse
				if err := json.NewDecoder(res.Body).Decode(&answer); err != nil {
					t.Fatal(err)
				}
				if res.StatusCode != 200 {
					t.Fatalf("bridge failed: %d %+v", res.StatusCode, answer)
				}
				return answer
			}
			reply := send("member-one", "https://example.com/law#art140 https://example.com/news")
			if !allowed {
				if reply.Reply != "" || reader.calls != 0 || generator.calls != 0 {
					t.Fatal("unauthorized link caused work")
				}
				return
			}
			if reader.calls != 2 || !strings.Contains(reply.Reply, "https://example.com/law#art140") {
				t.Fatalf("reading failed: %+v", reply)
			}
			reply = send("member-two", "What does that mean?")
			if reader.calls != 2 || generator.calls != 2 || !strings.Contains(reply.Reply, "https://example.com/law#art140") {
				t.Fatalf("shared channel lost reading: %+v", reply)
			}
		})
	}
}
