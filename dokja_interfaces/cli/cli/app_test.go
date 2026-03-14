package cli

import "testing"

func TestExtractChatReply(t *testing.T) {
	reply, err := extractChatReply(map[string]any{
		"result": map[string]any{
			"chat": map[string]any{
				"reply": "hello",
			},
		},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if reply != "hello" {
		t.Fatalf("expected hello, got %q", reply)
	}
}

func TestExtractChatReplyValidation(t *testing.T) {
	if _, err := extractChatReply("bad"); err == nil {
		t.Fatal("expected invalid response error")
	}
	if _, err := extractChatReply(map[string]any{}); err == nil {
		t.Fatal("expected missing result error")
	}
	if _, err := extractChatReply(map[string]any{"result": map[string]any{}}); err == nil {
		t.Fatal("expected missing chat result error")
	}
	if _, err := extractChatReply(map[string]any{
		"result": map[string]any{
			"chat": map[string]any{},
		},
	}); err == nil {
		t.Fatal("expected empty reply error")
	}
}
