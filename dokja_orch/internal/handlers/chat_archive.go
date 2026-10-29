package handlers

import (
	"context"
	"errors"
	"fmt"
	"strings"

	conversation "read_books/dokja_domain/dokja_conversation"
	"read_books/internal/core"
)

// Context budgets for an archived conversation: the completed turns, newest first, until either runs
// out. A long transcript never goes to the model whole.
const (
	archiveContextTurns = 20
	archiveContextRunes = 24000
	archiveRecentTurns  = conversation.MaxTurnLimit
)

// WithArchive keeps the owner's conversations: a turn that names one by its id, with a send id, is
// recorded there before the model is asked, and ends there completed or failed.
func (h *ChatDomainHandler) WithArchive(archive *conversation.Archive) *ChatDomainHandler {
	h.archive = archive
	return h
}

// archivedRequest reads whether the event asks for an archived turn: from one of the owner's
// interfaces, a send id for a new message or a turn id to send a failed one again. Anything else,
// including a channel's message that happens to carry those fields, goes on as before.
func archivedRequest(event core.Event) (conversationID, sendID, retryID string, archived bool) {
	if _, owner := conversation.ScopeFor(string(event.Source)); !owner {
		return "", "", "", false
	}
	conversationID = payloadString(event.Payload, "conversation_id")
	sendID = payloadString(event.Payload, "send_id")
	retryID = payloadString(event.Payload, "retry_turn_id")
	return conversationID, sendID, retryID, sendID != "" || retryID != ""
}

func (h *ChatDomainHandler) handleArchived(ctx context.Context, event core.Event, workflow core.WorkflowStep, conversationID, sendID, retryID string) (map[string]any, error) {
	if h.archive == nil {
		return nil, fmt.Errorf("the conversation archive is unavailable")
	}
	scope, ok := conversation.ScopeFor(string(event.Source))
	if !ok {
		return nil, fmt.Errorf("only the owner's interfaces keep conversations")
	}
	held, err := h.archive.Get(ctx, scope, conversationID)
	if err != nil {
		return nil, archiveError(err)
	}

	var turn conversation.Turn
	if retryID != "" {
		turn, err = h.archive.RetryInConversation(ctx, scope, held.ID, retryID)
		if err != nil {
			return nil, archiveError(err)
		}
	} else {
		var replayed bool
		turn, replayed, err = h.archive.Send(ctx, scope, held.ID, sendID, chatMessageFromEvent(event), conversation.Provenance{})
		if err != nil {
			return nil, archiveError(err)
		}
		// The same send asked again is answered from the archive, never by the model a second time.
		if replayed {
			return replayedTurn(workflow, held, turn)
		}
	}

	// What came before, from the archive: completed turns only, within the budgets.
	recent, err := h.archive.Turns(ctx, scope, held.ID, 0, archiveRecentTurns)
	if err != nil {
		return nil, h.failArchived(ctx, scope, turn, "the conversation archive could not be read")
	}
	earlier := make([]conversation.Turn, 0, len(recent))
	for _, each := range recent {
		if each.ID != turn.ID {
			earlier = append(earlier, each)
		}
	}
	history := []chatMessage{}
	for _, each := range conversation.ContextTurns(earlier, archiveContextTurns, archiveContextRunes) {
		history = append(history, chatMessage{Role: "user", Content: each.Content}, chatMessage{Role: "assistant", Content: each.Reply})
	}
	history = append(history, chatMessage{Role: "user", Content: turn.Content})

	// A reading discussion is grounded by the reading its conversation was started for, and a general
	// one by none: a turn cannot slip a reading into a conversation that is not about it.
	event.Payload = clonePayload(event.Payload)
	delete(event.Payload, "reading_id")
	if held.Kind == conversation.KindReading {
		event.Payload["reading_id"] = held.ReadingID
	}
	reference, anchor, err := h.readingReference(ctx, event)
	if err != nil {
		return nil, h.failArchived(ctx, scope, turn, err.Error())
	}
	// As in any other turn, a question about what came in lately is grounded by the cached feeds.
	if reference == "" && wantsRecentReadings(turn.Content) && h.recent != nil {
		items, err := h.recent(ctx, turn.Content, 8)
		if err != nil {
			return nil, h.failArchived(ctx, scope, turn, fmt.Sprintf("read cached feeds: %v", err))
		}
		reference = recentReadingReference(items)
	}
	sources := h.lookupKnowledge(ctx, turn.Content, anchor)
	result, err := h.generateReply(ctx, event, turn.Content, history, sources, reference)
	if err != nil {
		return nil, h.failArchived(ctx, scope, turn, err.Error())
	}
	raw := strings.TrimSpace(chatReplyFromResult(result))
	if raw == "" {
		return nil, h.failArchived(ctx, scope, turn, "the model returned no reply")
	}
	// The transcript keeps what the owner was shown, citations included.
	sources = citedSources(raw, sources)
	reply := citeSources(raw, sources)
	provider, _ := result["provider"].(string)
	model, _ := result["model"].(string)
	done, err := h.archive.Complete(context.WithoutCancel(ctx), scope, turn, reply, provider, model)
	if errors.Is(err, conversation.ErrInvalid) {
		// A provenance the archive does not know is left out rather than losing the reply.
		done, err = h.archive.Complete(context.WithoutCancel(ctx), scope, turn, reply, "", "")
	}
	if err != nil {
		return nil, archiveError(err)
	}
	response := map[string]any{
		"action": workflow.Action, "reply": reply, "raw": result,
		"conversation_id": held.ID, "turn": turnView(done),
	}
	if len(sources) > 0 {
		response["sources"] = sourcesForResult(sources)
	}
	return response, nil
}

// failArchived records why the turn got no reply, so it stays visibly failed, and gives the error
// back to the caller. A cancelled request still records its outcome, in plain words.
func (h *ChatDomainHandler) failArchived(ctx context.Context, scope conversation.Scope, turn conversation.Turn, reason string) error {
	if ctx.Err() != nil {
		reason = "the request stopped before the reply"
	}
	if _, err := h.archive.Fail(context.WithoutCancel(ctx), scope, turn, reason); err != nil {
		return fmt.Errorf("%s; and the conversation archive could not record it", reason)
	}
	return errors.New(reason)
}

func replayedTurn(workflow core.WorkflowStep, held conversation.Conversation, turn conversation.Turn) (map[string]any, error) {
	switch turn.State {
	case conversation.TurnCompleted:
		return map[string]any{
			"action": workflow.Action, "reply": turn.Reply, "replayed": true,
			"conversation_id": held.ID, "turn": turnView(turn),
		}, nil
	case conversation.TurnPending:
		return nil, fmt.Errorf("this message is still on its way")
	default:
		return nil, fmt.Errorf("this message got no reply (%s); retry it", turn.Reason)
	}
}

func clonePayload(original map[string]any) map[string]any {
	copy := make(map[string]any, len(original)+1)
	for key, value := range original {
		copy[key] = value
	}
	return copy
}
