package ingress

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"read_books/internal/core"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v2"
)

// previewingChat stands in for the chat handler: it shows what it would write, then answers.
type previewingChat struct{ previews []string }

func (previewingChat) Domain() core.Domain { return core.DomainChat }
func (h previewingChat) Handle(ctx context.Context, _ core.Event, _ core.WorkflowStep) (map[string]any, error) {
	if preview := core.ReplyPreview(ctx); preview != nil {
		for _, text := range h.previews {
			preview(text)
		}
	}
	return map[string]any{"reply": "The answer"}, nil
}

const desktopTurn = `{"event_id":"e-1","source":"desktop","type":"message.created","user":{"id":"owner"},"channel":{"id":"desktop"},"payload":{"content":"question","conversation_id":"c-1"}}`

func chatStreamApp(service *core.Service) *fiber.App {
	app := fiber.New()
	app.Post("/orchestrator/chat/stream", (&HTTPBridgeIngress{token: "fixture-gateway-secret", service: service}).handleChatStream)
	return app
}

func streamRequest(body, credential string) *http.Request {
	request := httptest.NewRequest("POST", "/orchestrator/chat/stream", strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	if credential != "" {
		request.Header.Set("Authorization", credential)
	}
	return request
}

func TestChatStreamNeedsTheGatewayCredential(t *testing.T) {
	app := chatStreamApp(core.DefaultService(previewingChat{}))
	for _, credential := range []string{"", "Bearer wrong", "fixture-gateway-secret"} {
		response, err := app.Test(streamRequest(desktopTurn, credential))
		if err != nil {
			t.Fatal(err)
		}
		if response.StatusCode != fiber.StatusUnauthorized {
			t.Fatalf("credential %q returned %d", credential, response.StatusCode)
		}
	}
}

func TestChatStreamTakesDesktopChatTurnsOnly(t *testing.T) {
	app := chatStreamApp(core.DefaultService(previewingChat{}))
	for _, body := range []string{
		strings.Replace(desktopTurn, `"source":"desktop"`, `"source":"discord"`, 1),
		strings.Replace(desktopTurn, `"type":"message.created"`, `"type":"knowledge.delete"`, 1),
		`not json`,
	} {
		response, err := app.Test(streamRequest(body, "Bearer fixture-gateway-secret"))
		if err != nil {
			t.Fatal(err)
		}
		if response.StatusCode != fiber.StatusBadRequest {
			t.Fatalf("%s returned %d", body, response.StatusCode)
		}
	}
}

func TestChatStreamSendsPreviewsThenTheRequestResponse(t *testing.T) {
	app := chatStreamApp(core.DefaultService(previewingChat{previews: []string{"The", "The ans"}}))
	response, err := app.Test(streamRequest(desktopTurn, "Bearer fixture-gateway-secret"), -1)
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != fiber.StatusOK || response.Header.Get("Content-Type") != "application/x-ndjson" {
		t.Fatal(response.StatusCode, response.Header)
	}
	raw, _ := io.ReadAll(response.Body)
	var lines []map[string]any
	scanner := bufio.NewScanner(strings.NewReader(string(raw)))
	for scanner.Scan() {
		var line map[string]any
		if err := json.Unmarshal(scanner.Bytes(), &line); err != nil {
			t.Fatal(scanner.Text(), err)
		}
		lines = append(lines, line)
	}
	if len(lines) != 3 || lines[0]["preview"] != "The" || lines[1]["preview"] != "The ans" || lines[2]["status"] != "ok" {
		t.Fatal(string(raw))
	}
	result, _ := lines[2]["result"].(map[string]any)
	if inner, _ := result["result"].(map[string]any); inner["reply"] != "The answer" {
		t.Fatal(string(raw))
	}
}
