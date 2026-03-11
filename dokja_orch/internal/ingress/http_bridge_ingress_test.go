package ingress

import (
	"testing"

	"read_books/internal/core"
)

func TestBridgeEventFromRequestMapsDiscordChat(t *testing.T) {
	event := bridgeEventFromRequest(BridgeRequest{
		EventType: "chat_message",
		Payload: struct {
			Content    string "json:\"content\""
			Limit      *int   "json:\"limit\""
			MaxItems   *int   "json:\"maxItemsPerScraper\""
			UserID     string "json:\"userId\""
			ChannelID  string "json:\"channelId\""
			MessageID  string "json:\"messageId\""
			GuildID    string "json:\"guildId\""
			SessionKey string "json:\"sessionKey\""
			StartChat  bool   "json:\"startSession\""
			AccountID  string "json:\"accountId\""
		}{
			Content:   "hello dokja",
			UserID:    "u1",
			ChannelID: "c1",
			MessageID: "m1",
			StartChat: true,
		},
	})

	if event.Type != "message.created" {
		t.Fatalf("expected message.created, got %q", event.Type)
	}
	if event.Payload["content"] != "hello dokja" {
		t.Fatalf("expected content payload, got %#v", event.Payload["content"])
	}
	if event.Source != core.SourceDiscord {
		t.Fatalf("expected discord source, got %q", event.Source)
	}
	if event.Context["force_new_session"] != true {
		t.Fatalf("expected force_new_session true, got %#v", event.Context["force_new_session"])
	}
}

func TestBridgeEventFromRequestMapsMemeFetch(t *testing.T) {
	event := bridgeEventFromRequest(BridgeRequest{
		Payload: struct {
			Content    string "json:\"content\""
			Limit      *int   "json:\"limit\""
			MaxItems   *int   "json:\"maxItemsPerScraper\""
			UserID     string "json:\"userId\""
			ChannelID  string "json:\"channelId\""
			MessageID  string "json:\"messageId\""
			GuildID    string "json:\"guildId\""
			SessionKey string "json:\"sessionKey\""
			StartChat  bool   "json:\"startSession\""
			AccountID  string "json:\"accountId\""
		}{
			Content: "meme fetch 3",
		},
	})

	if event.Type != "meme.fetch" {
		t.Fatalf("expected meme.fetch, got %q", event.Type)
	}
	if event.Payload["limit"] != 3 {
		t.Fatalf("expected limit 3, got %#v", event.Payload["limit"])
	}
}

func TestExtractBridgeReplyFormatsMemeResult(t *testing.T) {
	reply := extractBridgeReply(core.ProcessResult{
		Result: map[string]any{
			"meme": map[string]any{
				"count": 2,
				"memes": []any{
					map[string]any{"title": "one", "url": "https://example.com/1"},
					map[string]any{"title": "two", "url": "https://example.com/2"},
				},
			},
		},
	})

	expected := "one\nhttps://example.com/1\n\ntwo\nhttps://example.com/2"
	if reply != expected {
		t.Fatalf("expected %q, got %q", expected, reply)
	}
}
