package handlers

import (
	"context"
	"fmt"
	communications "read_books/dokja_domain/dokja_communications"
	"read_books/internal/core"
	"read_books/internal/logger"
)

// WhatsAppSender is the WhatsApp interface seen from the desktop's send: the conversations the
// owner named (by alias) and a way to send text into one of them.
type WhatsAppSender = communications.ConversationSender

// WithWhatsApp lets communications.send reach the conversations named on the WhatsApp interface.
func (h *SystemDomainHandler) WithWhatsApp(sender WhatsAppSender) *SystemDomainHandler {
	h.whatsapp = sender
	return h
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
	plan, err := communications.ValidateConversationSend(communicationSendRequest(event))
	if err != nil {
		return nil, err
	}
	content := plan.Content
	if h.whatsapp == nil {
		return nil, fmt.Errorf("the WhatsApp interface is not configured")
	}
	if err := h.deliveryPolicy().AuthorizeConversation(plan.Targets[0]); err != nil {
		return nil, err
	}

	channel := channels[0]
	listed, err := h.whatsapp.List(ctx)
	if err != nil {
		return nil, fmt.Errorf("the WhatsApp conversations are not available")
	}
	alias, err := communications.NamedConversation(channel, listed)
	if err != nil {
		return nil, err
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
