package handlers

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	conversation "read_books/dokja_domain/dokja_conversation"
	"read_books/internal/core"
)

// ConversationDomainHandler answers the owner's interfaces about their conversations with Ningo:
// start one, list them, read one, rename and delete. The scope comes from the event's source, never
// from the payload, so only the owner's interfaces reach the archive. Sending a turn is the chat
// handler's, which records it here.
type ConversationDomainHandler struct {
	archive  *conversation.Archive
	realtime *conversation.ConversationContextEngine
}

func NewConversationDomainHandler(archive *conversation.Archive) *ConversationDomainHandler {
	return &ConversationDomainHandler{archive: archive}
}

func (h *ConversationDomainHandler) Domain() core.Domain {
	return core.DomainConversation
}

func (h *ConversationDomainHandler) Handle(ctx context.Context, event core.Event, _ core.WorkflowStep) (map[string]any, error) {
	if h != nil && strings.HasPrefix(event.Type, "realtime.") {
		return h.realtimeHandle(ctx, event)
	}
	if h == nil || h.archive == nil {
		return nil, fmt.Errorf("the conversation archive is unavailable")
	}
	scope, ok := conversation.ScopeFor(string(event.Source))
	if !ok {
		return nil, fmt.Errorf("only the owner's interfaces keep conversations")
	}
	payload := event.Payload
	switch event.Type {
	case "conversation.start":
		kind := conversation.KindGeneral
		reading := payloadString(payload, "reading_id")
		if reading != "" {
			kind = conversation.KindReading
		}
		c, err := h.archive.Start(ctx, scope, kind, reading, payloadString(payload, "title"))
		if err != nil {
			return nil, archiveError(err)
		}
		return map[string]any{"conversation": conversationView(c)}, nil
	case "conversation.list":
		items, next, err := h.archive.List(ctx, scope, payloadString(payload, "cursor"), payloadInt(payload, "limit"))
		if err != nil {
			return nil, archiveError(err)
		}
		views := make([]map[string]any, 0, len(items))
		for _, c := range items {
			views = append(views, conversationView(c))
		}
		return map[string]any{"conversations": views, "next": next}, nil
	case "conversation.read":
		id := payloadString(payload, "conversation_id")
		c, err := h.archive.Get(ctx, scope, id)
		if err != nil {
			return nil, archiveError(err)
		}
		turns, err := h.archive.Turns(ctx, scope, id, payloadInt(payload, "before_seq"), payloadInt(payload, "limit"))
		if err != nil {
			return nil, archiveError(err)
		}
		views := make([]map[string]any, 0, len(turns))
		for _, t := range turns {
			views = append(views, turnView(t))
		}
		return map[string]any{"conversation": conversationView(c), "turns": views}, nil
	case "conversation.rename":
		c, err := h.archive.Rename(ctx, scope, payloadString(payload, "conversation_id"), payloadRevision(payload), payloadString(payload, "title"))
		if err != nil {
			return nil, archiveError(err)
		}
		return map[string]any{"conversation": conversationView(c)}, nil
	case "conversation.delete":
		id := payloadString(payload, "conversation_id")
		if err := h.archive.Delete(ctx, scope, id, payloadRevision(payload)); err != nil {
			return nil, archiveError(err)
		}
		return map[string]any{"conversation_id": id, "deleted": true}, nil
	default:
		return nil, fmt.Errorf("unsupported conversation action %q", event.Type)
	}
}

// archiveError names what the owner can act on and keeps storage details out of the reply.
func archiveError(err error) error {
	switch {
	case errors.Is(err, conversation.ErrNotFound):
		return conversation.ErrNotFound
	case errors.Is(err, conversation.ErrConflict):
		return conversation.ErrConflict
	case errors.Is(err, conversation.ErrInvalid):
		return conversation.ErrInvalid
	case errors.Is(err, conversation.ErrUnavailable):
		return conversation.ErrUnavailable
	default:
		return fmt.Errorf("the conversation archive could not be read or written")
	}
}

func conversationView(c conversation.Conversation) map[string]any {
	return map[string]any{
		"id": c.ID, "kind": string(c.Kind), "reading_id": c.ReadingID, "title": c.Title,
		"created_at": c.CreatedAt.Format(time.RFC3339Nano), "updated_at": c.UpdatedAt.Format(time.RFC3339Nano),
		"revision": c.Revision, "turns": c.Turns,
	}
}

// turnViewVersion names the shape below, the projection of a turn every interface reads. It grows by
// adding fields; changing or removing one is a new version. testdata/turn_view_v1.json pins it for the
// orchestrator and for the interfaces. Provider and model are what the runtime reported answering,
// not what was selected, and they are empty when it reported nothing.
const turnViewVersion = 1

func turnView(t conversation.Turn) map[string]any {
	view := map[string]any{
		"view": turnViewVersion,
		"id":   t.ID, "seq": t.Seq, "send_id": t.SendID, "content": t.Content, "reply": t.Reply,
		"state": string(t.State), "reason": t.Reason, "attempts": t.Attempts,
		"provider": t.Provenance.ObservedProvider, "model": t.Provenance.ObservedModel,
		"created_at": t.CreatedAt.Format(time.RFC3339Nano),
	}
	if !t.FinishedAt.IsZero() {
		view["finished_at"] = t.FinishedAt.Format(time.RFC3339Nano)
	}
	return view
}

func payloadString(payload map[string]any, key string) string {
	value, _ := payload[key].(string)
	return value
}

// Numbers arrive from JSON as float64; anything else, or a fraction, reads as zero, which the
// archive treats as its default.
func payloadInt(payload map[string]any, key string) int {
	value, ok := payload[key].(float64)
	if !ok || value != float64(int(value)) || value < 0 || value > 1e9 {
		return 0
	}
	return int(value)
}

func payloadRevision(payload map[string]any) uint64 {
	value, ok := payload["revision"].(float64)
	if !ok || value < 1 || value > 1e15 || value != float64(uint64(value)) {
		return 0
	}
	return uint64(value)
}
