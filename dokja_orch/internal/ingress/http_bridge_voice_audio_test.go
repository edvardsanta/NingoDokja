package ingress

import (
	"context"
	"github.com/gofiber/fiber/v2"
	"io"
	"net/http/httptest"
	communications "read_books/dokja_domain/dokja_communications"
	"read_books/internal/core"
	"read_books/internal/handlers"
	"strings"
	"testing"
)

type bridgeVoiceFixture struct{ calls int }

func (f *bridgeVoiceFixture) Invoke(_ context.Context, operation string, _ map[string]any) (map[string]any, error) {
	f.calls++
	return map[string]any{"audio_bytes_b64": "fixture-audio"}, nil
}
func TestVoiceAudioUsesGatewayAndChannelPolicyWithoutOperatorCredentials(t *testing.T) {
	for _, credential := range []string{"", "Bearer fixture-credential"} {
		voice := &bridgeVoiceFixture{}
		gate := handlers.NewCommunicationsDomainHandler(nil, "").WithInboundPolicy(communications.InboundPolicy{DiscordGuildIDs: []string{"guild"}, DiscordChannels: []string{"control"}}).WithVoice(voice)
		service := core.DefaultService(gate)
		app := fiber.New()
		app.Post("/orchestrator", (&HTTPBridgeIngress{service: service, token: "fixture-credential"}).handle)
		body := `{"event_type":"voice.synthesize","payload":{"userId":"member","channelId":"control","guildId":"guild","entryKind":"command","isDirectMessage":false,"voice":{"text":"hello"}}}`
		request := httptest.NewRequest("POST", "/orchestrator", strings.NewReader(body))
		request.Header.Set("Content-Type", "application/json")
		request.Header.Set("Authorization", credential)
		response, err := app.Test(request)
		if err != nil {
			t.Fatal(err)
		}
		data, _ := io.ReadAll(response.Body)
		response.Body.Close()
		if credential == "" {
			if response.StatusCode != 401 || voice.calls != 0 {
				t.Fatal("missing gateway credential reached speech")
			}
		} else if response.StatusCode != 200 || voice.calls != 1 || !strings.Contains(string(data), "fixture-audio") {
			t.Fatal(response.StatusCode, string(data), voice.calls)
		}
	}
}
