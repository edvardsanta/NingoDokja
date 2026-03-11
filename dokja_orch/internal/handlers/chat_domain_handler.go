package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"read_books/internal/core"
	"read_books/internal/logger"
	"strings"
)

type ChatGenerator interface {
	Generate(ctx context.Context, message string) (map[string]any, error)
}

type ChatConversationGenerator interface {
	GenerateMessages(ctx context.Context, messages []map[string]string) (map[string]any, error)
}

type ChatDomainHandler struct {
	generator ChatGenerator
	sessions  *chatSessionManager
}

func NewChatDomainHandler(generator ChatGenerator) *ChatDomainHandler {
	return &ChatDomainHandler{
		generator: generator,
		sessions:  newChatSessionManager(),
	}
}

func (h *ChatDomainHandler) Domain() core.Domain {
	return core.DomainChat
}

func (h *ChatDomainHandler) Handle(ctx context.Context, event core.Event, workflow core.WorkflowStep) (map[string]any, error) {
	if h == nil || h.generator == nil {
		return nil, fmt.Errorf("chat generator is not configured")
	}

	content := chatMessageFromEvent(event)
	if content == "" {
		return nil, fmt.Errorf("chat content is required")
	}
	logger.Info(fmt.Sprintf(
		"chat handler received event_id=%s action=%s user=%s message_chars=%d",
		event.EventID,
		workflow.Action,
		event.User.ID,
		len(content),
	))

	lifecycle, history := h.sessions.BeginTurn(event, content)
	result, err := h.generateReply(ctx, content, history)
	if err != nil {
		return nil, err
	}
	rawReply := chatReplyFromResult(result)
	h.sessions.CompleteTurn(event, rawReply)
	reply := sessionAwareReply(rawReply, lifecycle, event)
	logger.Info(fmt.Sprintf(
		"chat handler completed event_id=%s action=%s reply_chars=%d",
		event.EventID,
		workflow.Action,
		len(reply),
	))

	return map[string]any{
		"action":  workflow.Action,
		"reply":   reply,
		"raw":     result,
		"session": map[string]any{"id": lifecycle.SessionID, "created": lifecycle.Created, "revoked": lifecycle.Revoked, "reason": lifecycle.Reason},
	}, nil
}

func (h *ChatDomainHandler) generateReply(ctx context.Context, content string, history []chatMessage) (map[string]any, error) {
	if generator, ok := h.generator.(ChatConversationGenerator); ok {
		return generator.GenerateMessages(ctx, chatMessagesForGenerator(history))
	}
	return h.generator.Generate(ctx, content)
}

func sessionAwareReply(reply string, lifecycle chatSessionLifecycle, event core.Event) string {
	reply = strings.TrimSpace(reply)
	notice := sessionLifecycleNotice(lifecycle, event)
	switch {
	case notice == "":
		return reply
	case reply == "":
		return notice
	default:
		return notice + "\n\n" + reply
	}
}

func sessionLifecycleNotice(lifecycle chatSessionLifecycle, event core.Event) string {
	if lifecycle.Revoked {
		return "Previous conversation session was revoked. A new 3-minute session has started."
	}
	if lifecycle.Created && boolFromContext(event.Context, "force_new_session") {
		return "New conversation session started. It will reset after 3 minutes of inactivity."
	}
	return ""
}

func firstString(values map[string]any, keys ...string) string {
	for _, key := range keys {
		value, _ := values[key].(string)
		if value != "" {
			return value
		}
	}
	return ""
}

func chatMessageFromEvent(event core.Event) string {
	candidates := []string{
		firstString(event.Payload, "content", "message", "text", "prompt"),
	}

	switch event.Source {
	case core.SourceDiscord, core.SourceWhatsApp, core.SourceCLI, core.SourceWeb, core.SourceAPI:
		candidates = append(candidates, firstString(event.Context, "content", "message", "text", "prompt"))
	}

	for _, candidate := range candidates {
		trimmed := strings.TrimSpace(candidate)
		if trimmed != "" {
			return trimmed
		}
	}

	return ""
}

func chatReplyFromResult(result map[string]any) string {
	if reply := normalizeDirectReply(result); reply != "" {
		return reply
	}

	if reply := normalizeContentReply(result); reply != "" {
		return reply
	}

	rawContents, ok := result["contents"].([]any)
	if ok && len(rawContents) > 0 {
		return normalizeContentReply(rawContents[0])
	}

	if rawStrings, ok := result["contents"].([]string); ok && len(rawStrings) > 0 {
		return normalizeContentReply(rawStrings[0])
	}

	return ""
}

func normalizeDirectReply(result map[string]any) string {
	for _, key := range []string{"reply", "text", "response"} {
		if value, ok := result[key]; ok {
			if normalized := normalizeContentReply(value); strings.TrimSpace(normalized) != "" {
				return normalized
			}
		}
	}
	return ""
}

func normalizeContentReply(raw any) string {
	switch value := raw.(type) {
	case string:
		trimmed := strings.TrimSpace(value)
		if trimmed == "" {
			return ""
		}

		var payload map[string]any
		if err := json.Unmarshal([]byte(trimmed), &payload); err == nil {
			if normalized := normalizeContentReply(payload); normalized != "" {
				return normalized
			}
		}

		return trimmed
	case map[string]any:
		if ningo, ok := value["ningo_response"].(map[string]any); ok {
			if text := strings.TrimSpace(firstString(ningo, "text")); text != "" {
				return text
			}
		}
		if text := strings.TrimSpace(firstString(value, "reply", "text", "response")); text != "" {
			return text
		}
		for _, key := range []string{"result", "payload", "data"} {
			if nested, ok := value[key]; ok {
				if normalized := normalizeContentReply(nested); normalized != "" {
					return normalized
				}
			}
		}
		return ""
	default:
		return ""
	}
}

func chatMessagesForGenerator(history []chatMessage) []map[string]string {
	messages := make([]map[string]string, 0, len(history))
	for _, message := range history {
		if strings.TrimSpace(message.Content) == "" {
			continue
		}
		messages = append(messages, map[string]string{
			"role":    strings.TrimSpace(message.Role),
			"content": strings.TrimSpace(message.Content),
		})
	}
	return messages
}
