package ingress

import (
	"bufio"
	"context"
	"encoding/json"
	"github.com/gofiber/fiber/v2"
	chat "read_books/dokja_domain/dokja_chat"
	"read_books/internal/core"
	"strings"
	"time"
)

func (i *HTTPBridgeIngress) handleVoiceChatStream(c *fiber.Ctx) error {
	if status := i.authorize(c); status != fiber.StatusOK {
		return c.SendStatus(status)
	}
	var request BridgeRequest
	if json.Unmarshal(c.Body(), &request) != nil || request.EventType != "message.created" ||
		(request.Interface != "" && request.Interface != "discord") || request.Payload.GuildID == "" ||
		request.Payload.VoiceChannelID == "" || request.Payload.ChannelID == "" ||
		request.Payload.EntryKind != "command" || len(request.Payload.VoiceTurns) == 0 {
		return c.Status(fiber.StatusBadRequest).JSON(BridgeResponse{Status: "error", Error: "invalid voice turn"})
	}
	if _, err := chat.VoiceTranscript(request.Payload.VoiceChannelID, request.Payload.VoiceTurns); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(BridgeResponse{Status: "error", Error: err.Error()})
	}
	last := request.Payload.VoiceTurns[len(request.Payload.VoiceTurns)-1]
	request.Payload.UserID, request.Payload.UserName, request.Payload.Content = last.UserID, last.UserName, last.Text
	event := bridgeEventFromRequest(request)
	// Speech is always conversation: recognized words must not invoke text commands.
	event.Type = "message.created"
	transcript, _ := chat.VoiceTranscript(request.Payload.VoiceChannelID, request.Payload.VoiceTurns)
	event.Payload = map[string]any{"content": transcript}
	event.Context["content"] = transcript
	event.Context["owner_configured"] = i.discordOwnerID != ""
	event.Context["is_owner"] = i.discordOwnerID != "" && last.UserID == i.discordOwnerID
	c.Set("Content-Type", "application/x-ndjson")
	c.Context().SetBodyStreamWriter(func(w *bufio.Writer) {
		ctx, cancel := context.WithTimeout(context.Background(), chatStreamTimeout)
		defer cancel()
		type completion struct {
			result core.ProcessResult
			err    error
		}
		completed := make(chan completion, 1)
		go func() { result, err := i.service.ProcessWithResult(ctx, event); completed <- completion{result, err} }()
		tick := time.NewTicker(100 * time.Millisecond)
		defer tick.Stop()
		write := func(value any) bool {
			data, err := json.Marshal(value)
			if err != nil {
				return false
			}
			if _, err = w.Write(append(data, '\n')); err != nil {
				return false
			}
			return w.Flush() == nil
		}
		for {
			select {
			case <-ctx.Done():
				return
			case <-tick.C:
				if !write(map[string]bool{"heartbeat": true}) {
					return
				}
			case reply := <-completed:
				if reply.err != nil {
					write(BridgeResponse{Status: "error", Error: "voice reply failed"})
					return
				}
				text := sanitizeDiscordOwnerID(extractBridgeReply(reply.result), i.discordOwnerID)
				write(BridgeResponse{Status: "ok", Reply: strings.TrimSpace(text)})
				return
			}
		}
	})
	return nil
}
