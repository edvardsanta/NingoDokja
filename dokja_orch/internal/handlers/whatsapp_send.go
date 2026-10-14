package handlers

import (
	"context"
	"fmt"
	communications "read_books/dokja_domain/dokja_communications"
	"read_books/internal/core"
	"read_books/internal/logger"
	"strings"
)

// WhatsAppSender is the WhatsApp interface seen from the desktop's send: the conversations the
// owner named (by alias) and a way to send text into one of them.
type WhatsAppSender interface {
	List(ctx context.Context) ([]communications.Conversation, error)
	Deliver(ctx context.Context, alias, content string) error
}

// WithWhatsApp lets communications.send reach the conversations named on the WhatsApp interface.
func (h *SystemDomainHandler) WithWhatsApp(sender WhatsAppSender) *SystemDomainHandler {
	h.whatsapp = sender
	return h
}

func anyWhatsApp(channels []string) bool {
	for _, channel := range channels {
		if strings.HasPrefix(channel, communications.WhatsAppPrefix) {
			return true
		}
	}
	return false
}

// handleWhatsAppSend sends text into one named WhatsApp conversation. It is stricter than a Discord
// send because it goes out as the owner's own account: only communications.send (so only behind the
// communications credential), exactly one conversation, only text, never "all", and only an alias
// the interface itself names. The interface adds its own budget on top.
func (h *SystemDomainHandler) handleWhatsAppSend(
	ctx context.Context,
	event core.Event,
	workflow core.WorkflowStep,
	channels []string,
) (map[string]any, error) {
	if event.Type != "communications.send" {
		return nil, fmt.Errorf("WhatsApp conversations can only be sent to through communications.send")
	}
	if all, _ := event.Payload["all"].(bool); all || len(channels) != 1 {
		return nil, fmt.Errorf("send to one WhatsApp conversation at a time")
	}
	if strings.TrimSpace(resultString(event.Payload, "attachment_url")) != "" {
		return nil, fmt.Errorf("WhatsApp conversations take text only")
	}
	if forced, _ := event.Payload["force_nsfw"].(bool); forced {
		return nil, fmt.Errorf("force_nsfw does not apply to WhatsApp conversations")
	}
	content := strings.TrimSpace(resultString(event.Payload, "content"))
	if content == "" {
		return nil, fmt.Errorf("send requires content")
	}
	if h.whatsapp == nil {
		return nil, fmt.Errorf("the WhatsApp interface is not configured")
	}
	if !h.serviceEnabled("whatsapp") {
		return nil, fmt.Errorf("nothing sent: whatsapp is switched off")
	}

	channel := channels[0]
	alias := strings.TrimPrefix(channel, communications.WhatsAppPrefix)
	listed, err := h.whatsapp.List(ctx)
	if err != nil {
		return nil, fmt.Errorf("the WhatsApp conversations are not available")
	}
	named := false
	for _, conversation := range listed {
		if conversation.Alias == alias {
			named = true
			break
		}
	}
	if !named {
		return nil, fmt.Errorf("channel %s is not a named WhatsApp conversation", channel)
	}

	if err := h.whatsapp.Deliver(ctx, alias, content); err != nil {
		// The interface's messages are fixed sentences; the text itself is never put in an error.
		return nil, fmt.Errorf("WhatsApp send failed: %s", err.Error())
	}
	logger.Info(fmt.Sprintf("system handler whatsapp send event_id=%s alias=%s content_chars=%d", event.EventID, alias, len([]rune(content))))
	return map[string]any{
		"action":         workflow.Action,
		"sent_to":        []string{channel},
		"has_attachment": false,
	}, nil
}
