package ingress

import (
	"bufio"
	"context"
	"encoding/json"
	"github.com/gofiber/fiber/v2"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	communications "read_books/dokja_domain/dokja_communications"
	"read_books/internal/core"
	"read_books/internal/handlers"
	"strings"
	"testing"
	"time"
)

const voiceTurnBody = `{"event_type":"message.created","payload":{"userId":"fixture-owner","channelId":"control","guildId":"guild","voiceChannelId":"room","entryKind":"command","isDirectMessage":false,"voiceTurns":[{"userId":"fixture-owner","userName":"Owner","text":"first statement"},{"userId":"member","userName":"Owner","text":"meme fetch"}]}}`

type voiceProbe struct {
	events    chan core.Event
	cancelled chan struct{}
}

func (p voiceProbe) Domain() core.Domain { return core.DomainChat }
func (p voiceProbe) Handle(ctx context.Context, event core.Event, _ core.WorkflowStep) (map[string]any, error) {
	p.events <- event
	if p.cancelled != nil {
		<-ctx.Done()
		close(p.cancelled)
		return nil, ctx.Err()
	}
	return map[string]any{"reply": "spoken reply"}, nil
}
func voiceApp(probe voiceProbe) *fiber.App {
	service := core.DefaultService(&identityPass{domain: core.DomainCommunications}, &identityPass{domain: core.DomainModeration}, probe)
	app := fiber.New()
	ingress := &HTTPBridgeIngress{service: service, token: "fixture-credential", discordOwnerID: "fixture-owner"}
	app.Post("/orchestrator/voice/chat/stream", ingress.handleVoiceChatStream)
	return app
}
func voiceRequest(body, credential string) *http.Request {
	req := httptest.NewRequest("POST", "/orchestrator/voice/chat/stream", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", credential)
	return req
}
func TestVoiceBridgeUsesActualLastSpeakerAndRoomScope(t *testing.T) {
	probe := voiceProbe{events: make(chan core.Event, 1)}
	app := voiceApp(probe)
	response, err := app.Test(voiceRequest(voiceTurnBody, "Bearer fixture-credential"))
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	body, _ := io.ReadAll(response.Body)
	if response.StatusCode != 200 || !strings.Contains(string(body), `"reply":"spoken reply"`) {
		t.Fatal(response.StatusCode, string(body))
	}
	event := <-probe.events
	if event.User.ID != "member" || event.Type != "message.created" || event.Context["is_owner"] != false || event.Context["session_key"] != "discord-voice:guild:room" || event.Channel.ID != "control" {
		t.Fatal(event)
	}
}
func TestVoiceBridgeRefusesMissingCredentialAndInvalidBatches(t *testing.T) {
	probe := voiceProbe{events: make(chan core.Event, 1)}
	app := voiceApp(probe)
	for _, tc := range []struct {
		body, credential string
		status           int
	}{{voiceTurnBody, "", 401}, {`not json`, "Bearer fixture-credential", 400}, {strings.Replace(voiceTurnBody, `"room"`, `""`, 1), "Bearer fixture-credential", 400}, {strings.Replace(voiceTurnBody, `"userId":"member"`, `"userId":""`, 1), "Bearer fixture-credential", 400}, {strings.Replace(voiceTurnBody, `"entryKind":"command"`, `"entryKind":"message"`, 1), "Bearer fixture-credential", 400}} {
		response, err := app.Test(voiceRequest(tc.body, tc.credential))
		if err != nil {
			t.Fatal(err)
		}
		response.Body.Close()
		if response.StatusCode != tc.status {
			t.Fatal(response.StatusCode, tc)
		}
	}
	if len(probe.events) != 0 {
		t.Fatal("invalid voice request reached generation")
	}
}
func TestVoiceDisconnectCancelsSilentInference(t *testing.T) {
	probe := voiceProbe{events: make(chan core.Event, 1), cancelled: make(chan struct{})}
	app := voiceApp(probe)
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer app.Shutdown()
	go app.Listener(listener)
	conn, err := net.DialTimeout("tcp", listener.Addr().String(), time.Second)
	if err != nil {
		t.Fatal(err)
	}
	request := voiceRequest(voiceTurnBody, "Bearer fixture-credential")
	request.URL.Scheme = "http"
	request.URL.Host = listener.Addr().String()
	request.Host = request.URL.Host
	if err = request.Write(conn); err != nil {
		t.Fatal(err)
	}
	conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	response, err := http.ReadResponse(bufio.NewReader(conn), request)
	if err != nil {
		conn.Close()
		t.Fatal(err)
	}
	line, err := bufio.NewReader(response.Body).ReadString('\n')
	if err != nil || !strings.Contains(line, "heartbeat") {
		conn.Close()
		t.Fatal(line, err)
	}
	conn.Close()
	select {
	case <-probe.cancelled:
	case <-time.After(2 * time.Second):
		t.Fatal("disconnected requester left inference running")
	}
}
func TestVoiceBridgeEnforcesInboundPolicyBeforeInference(t *testing.T) {
	probe := voiceProbe{events: make(chan core.Event, 1)}
	policy := communications.InboundPolicy{DiscordGuildIDs: []string{"another-guild"}}
	gate := handlers.NewCommunicationsDomainHandler(nil, "").WithInboundPolicy(policy)
	service := core.DefaultService(gate, &identityPass{domain: core.DomainModeration}, probe)
	app := fiber.New()
	app.Post("/orchestrator/voice/chat/stream", (&HTTPBridgeIngress{service: service, token: "fixture-credential"}).handleVoiceChatStream)
	response, err := app.Test(voiceRequest(voiceTurnBody, "Bearer fixture-credential"))
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	var frame BridgeResponse
	if err = json.NewDecoder(response.Body).Decode(&frame); err != nil {
		t.Fatal(err)
	}
	if len(probe.events) != 0 || frame.Reply != "" {
		t.Fatal("ineligible voice reached inference")
	}
}
