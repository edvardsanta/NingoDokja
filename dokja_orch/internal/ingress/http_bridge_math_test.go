package ingress

import (
	"encoding/json"
	"strings"
	"testing"

	chat "read_books/dokja_domain/dokja_chat"
	"read_books/internal/core"
)

func TestBridgePreservesChatImageAttachments(t *testing.T) {
	attachment := chat.Attachment{Filename: "formula-1.png", ContentType: "image/png", DataB64: "cG5n"}
	result := core.ProcessResult{Result: map[string]any{"chat": map[string]any{
		"reply": "x / 2", "attachments": []chat.Attachment{attachment},
	}}}
	response := BridgeResponse{Status: "ok", Reply: extractBridgeReply(result), Attachments: extractChatAttachments(result)}
	encoded, err := json.Marshal(response)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(encoded), `"attachments":[{"filename":"formula-1.png"`) || response.Reply != "x / 2" {
		t.Fatalf("image response lost its text or attachment: %s", encoded)
	}
}
