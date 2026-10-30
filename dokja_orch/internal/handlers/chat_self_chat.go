package handlers

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"time"

	conversation "read_books/dokja_domain/dokja_conversation"
	"read_books/internal/core"
)

// selfChatDeadline bounds a self-chat turn, waiting behind earlier turns included. It stays under the
// WhatsApp interface's default wait for the orchestrator (180 s), so the archive records the turn as
// failed before the phone is told it failed, instead of keeping a reply the phone never received.
const selfChatDeadline = 170 * time.Second

// selfChatRequest reads whether the event is the linked WhatsApp account writing in its own
// "message yourself" chat. Only the WhatsApp interface sets the flag, on its authenticated bridge,
// and it must be a real boolean: anything else is a WhatsApp message like any other.
func selfChatRequest(event core.Event) bool {
	if event.Source != core.SourceWhatsApp {
		return false
	}
	selfChat, ok := event.Context["is_self_chat"].(bool)
	return ok && selfChat
}

// selfChatSendID names a WhatsApp message in the archive. The same message delivered again has the
// same name, so the archive answers it once; a hash keeps the name within the archive's format
// whatever WhatsApp's own ids look like. A message without an id gets a name of its own.
func selfChatSendID(event core.Event) string {
	messageID, _ := event.Context["message_id"].(string)
	if messageID == "" {
		b := make([]byte, 16)
		if _, err := rand.Read(b); err != nil {
			panic(fmt.Sprintf("send id: %v", err))
		}
		return "wa-" + hex.EncodeToString(b)
	}
	sum := sha256.Sum256([]byte(event.Channel.ID + ":" + messageID))
	return "wa-" + hex.EncodeToString(sum[:])[:32]
}

// handleSelfChat answers the owner writing from the phone: the turn continues the owner's latest
// general conversation in the archive, the one the desktop shows, or starts one. The conversation is
// chosen and the turn recorded under one lock, so two first messages cannot start two conversations.
// A repeat of a message still being answered gets no reply here; its first delivery will bring it.
func (h *ChatDomainHandler) handleSelfChat(ctx context.Context, event core.Event, workflow core.WorkflowStep) (map[string]any, error) {
	scope, ok := conversation.SendScopeFor(string(event.Source), true)
	if !ok {
		return nil, fmt.Errorf("only the owner's own chat keeps conversations")
	}
	content := chatMessageFromEvent(event)
	if content == "" {
		return nil, fmt.Errorf("chat content is required")
	}
	ctx, cancel := context.WithTimeout(ctx, selfChatDeadline)
	defer cancel()

	h.selfChat.Lock()
	unlocked := false
	unlock := func() {
		if !unlocked {
			unlocked = true
			h.selfChat.Unlock()
		}
	}
	defer unlock()
	// A command manages the phone's conversation and is answered here; it is no turn of any conversation.
	if command, locale, argument, ok := parseSelfChatCommand(content); ok {
		return map[string]any{"action": workflow.Action, "reply": h.selfChatCommandReply(ctx, scope, command, locale, argument), "command": true}, nil
	}
	held, found, err := h.archive.Continued(ctx, scope)
	if err != nil {
		return nil, archiveError(err)
	}
	if !found {
		held, err = h.archive.Start(ctx, scope, conversation.KindGeneral, "", conversation.TitleFrom(content))
		if err != nil {
			return nil, archiveError(err)
		}
	} else if held.Turns == 0 && held.Title == "" {
		// A conversation started empty (/nova) is named by its first message, like any other.
		if renamed, err := h.archive.Rename(ctx, scope, held.ID, held.Revision, conversation.TitleFrom(content)); err == nil {
			held = renamed
		}
	}
	// The interface's own fields cannot pick a conversation, a send id or a retry.
	event.Payload = map[string]any{"content": content}
	return h.answerArchived(ctx, event, workflow, archivedSend{
		scope: scope, conversationID: held.ID, sendID: selfChatSendID(event), admitted: unlock, quietPending: true,
	})
}
