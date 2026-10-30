package ingress

import (
	"context"
	"io"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v2"
	communications "read_books/dokja_domain/dokja_communications"
	conversation "read_books/dokja_domain/dokja_conversation"
	"read_books/internal/core"
	"read_books/internal/handlers"
)

func TestRealtimeObserverFlowsThroughAuthorizationToDesktopWithoutChat(t *testing.T) {
	engine := conversation.NewConversationContextEngine()
	gate := handlers.NewCommunicationsDomainHandler(nil, "").WithInboundPolicy(communications.InboundPolicy{DiscordGuildIDs: []string{"guild"}, DiscordChannels: []string{"control"}})
	service := core.DefaultService(gate, handlers.NewConversationDomainHandler(nil).WithRealtime(engine))
	app := fiber.New()
	app.Post("/orchestrator", (&HTTPBridgeIngress{service: service, token: "fixture-credential"}).handle)
	send := func(body, credential string) string {
		t.Helper()
		req := httptest.NewRequest("POST", "/orchestrator", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", credential)
		res, err := app.Test(req)
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		data, _ := io.ReadAll(res.Body)
		return string(data)
	}
	start := `{"event_type":"realtime.start","payload":{"userId":"speaker","channelId":"control","guildId":"guild","entryKind":"command","isDirectMessage":false,"realtime":{"session_id":"session","voice_channel_id":"voice"}}}`
	send(start, "")
	if len(engine.Snapshot()) != 0 {
		t.Fatal("unauthenticated capture enabled")
	}
	denied := send(strings.Replace(start, "control", "foreign", 1), "Bearer fixture-credential")
	if strings.Contains(denied, `"accepted":true`) || len(engine.Snapshot()) != 0 {
		t.Fatal("unapproved channel enabled observer")
	}
	if result := send(start, "Bearer fixture-credential"); !strings.Contains(result, `"accepted":true`) {
		t.Fatal(result)
	}
	observe := `{"event_type":"realtime.observe","payload":{"userId":"second-speaker","userName":"Channel Scholar","channelId":"control","guildId":"guild","entryKind":"command","isDirectMessage":false,"realtime":{"session_id":"session","message_id":"m","speaker_id":"forged","speaker_name":"Forged name","text":"Redis with expiration","transcription_status":"final","revision":1,"timestamp_start":"2026-10-29T10:00:00Z","timestamp_end":"2026-10-29T10:00:02Z"}}}`
	if result := send(observe, "Bearer fixture-credential"); !strings.Contains(result, `"accepted":true`) {
		t.Fatal(result)
	}
	result, err := service.ProcessWithResult(context.Background(), core.Event{Type: "realtime.snapshot", Source: core.SourceDesktop})
	if err != nil {
		t.Fatal(err)
	}
	sessions := result.Result[string(core.DomainConversation)].(map[string]any)["sessions"].([]conversation.RealtimeSession)
	if len(sessions) != 1 || len(sessions[0].Messages) != 1 || sessions[0].Messages[0].SpeakerID != "second-speaker" || sessions[0].Messages[0].SpeakerName != "Channel Scholar" {
		t.Fatal("source-derived identity or snapshot lost")
	}
}
