package handlers

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	conversation "read_books/dokja_domain/dokja_conversation"
	memorydomain "read_books/dokja_domain/dokja_memory"
	"read_books/internal/core"
)

// ConversationDomainHandler answers the owner's interfaces about their conversations with Ningo:
// start one, list them, read one, rename and delete. The scope comes from the event's source, never
// from the payload, so only the owner's interfaces reach the archive. Sending a turn is the chat
// handler's, which records it here.
type ConversationDomainHandler struct {
	archive  *conversation.Archive
	realtime *conversation.ConversationContextEngine
	facts    *conversation.Facts
	running  *core.RunningSends
	verdicts *memorydomain.Domain
}

func NewConversationDomainHandler(archive *conversation.Archive) *ConversationDomainHandler {
	return &ConversationDomainHandler{archive: archive}
}

// WithFacts lets the owner's interfaces see and delete what known people asked to keep.
func (h *ConversationDomainHandler) WithFacts(facts *conversation.Facts) *ConversationDomainHandler {
	h.facts = facts
	return h
}

// WithAnswerFeedback lets the owner judge an answer as useful or wrong. The verdict resolves the
// memory experience ChatAnswerExperience recorded for that turn.
func (h *ConversationDomainHandler) WithAnswerFeedback(service memorydomain.Service) *ConversationDomainHandler {
	h.verdicts = memorydomain.New(service)
	return h
}

// WithRunningSends lets the owner's interfaces stop a send being answered.
func (h *ConversationDomainHandler) WithRunningSends(running *core.RunningSends) *ConversationDomainHandler {
	h.running = running
	return h
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
	case "conversation.capabilities":
		return map[string]any{"view": conversationCapabilitiesView, "operations": h.operations()}, nil
	case "conversation.stop":
		if h.running == nil {
			return nil, conversation.ErrUnavailable
		}
		sendID := payloadString(payload, "send_id")
		if sendID == "" {
			return nil, conversation.ErrInvalid
		}
		// Reading it first keeps the stop inside the caller's scope.
		held, err := h.archive.Get(ctx, scope, payloadString(payload, "conversation_id"))
		if err != nil {
			return nil, archiveError(err)
		}
		if !h.running.Stop(held.ID, sendID) {
			return nil, conversation.ErrConflict
		}
		return map[string]any{"conversation_id": held.ID, "send_id": sendID, "stopping": true}, nil
	case "conversation.feedback":
		return h.feedback(ctx, scope, payload)
	case "conversation.facts":
		if h.facts == nil {
			return nil, conversation.ErrUnavailable
		}
		facts, err := h.facts.All(ctx)
		if err != nil {
			return nil, archiveError(err)
		}
		views := make([]map[string]any, 0, len(facts))
		for _, fact := range facts {
			views = append(views, factView(fact))
		}
		return map[string]any{"facts": views}, nil
	case "conversation.fact_delete":
		if h.facts == nil {
			return nil, conversation.ErrUnavailable
		}
		id := payloadString(payload, "fact_id")
		if err := h.facts.Delete(ctx, id); err != nil {
			return nil, archiveError(err)
		}
		return map[string]any{"fact_id": id, "deleted": true}, nil
	default:
		return nil, fmt.Errorf("unsupported conversation action %q", event.Type)
	}
}

// conversationCapabilitiesView is the version of the capability report. A new operation may be added
// to a view; changing what an existing one means needs a new view.
const conversationCapabilitiesView = 1

// operations reports what this orchestrator, as it is wired, does with the owner's conversations, so
// an interface offers only what will work instead of finding out by trying. It is read from the
// wiring, never from the request, and an operation it does not name is unsupported. It describes
// support, not that a given send can be stopped now: that is still the action's own answer.
func (h *ConversationDomainHandler) operations() map[string]string {
	state := func(supported bool) string {
		if supported {
			return "supported"
		}
		return "unsupported"
	}
	return map[string]string{
		"send":  "supported",
		"retry": state(h.archive.RetriesInConversation()),
		"stop":  state(h.running != nil),
		// Judging an answer as useful or wrong, the outcome step 08.3 learns from.
		"feedback": state(h.verdicts != nil),
		// Changing a reply while it is written is a separate capability that nothing offers.
		"steer": "unsupported",
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

// factView shows a fact to the owner. Where it was said is told as a platform, not as a chat's id.
func factView(fact conversation.Fact) map[string]any {
	place, _, _ := strings.Cut(fact.Place, ":")
	return map[string]any{
		"id": fact.ID, "owner": fact.Person == conversation.OwnerPerson, "person": conversation.PersonName(fact.Person),
		"content": fact.Content, "place": place, "created_at": fact.CreatedAt.Format(time.RFC3339Nano),
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
	if len(t.Sources) > 0 {
		sources := make([]any, len(t.Sources))
		for i, source := range t.Sources {
			sources[i] = map[string]any{
				"number": source.Number, "source_id": source.SourceID, "title": source.Title,
				"reference": source.Reference, "excerpt": source.Excerpt, "locator": source.Locator,
			}
		}
		view["sources"] = sources
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

// answerVerdicts maps what the owner says about an answer to the outcomes every experience shares.
// No verdict is not "wrong": an answer nobody judged expires unscored.
var answerVerdicts = map[string]string{"useful": memorydomain.OutcomeAccepted, "wrong": memorydomain.OutcomeReplaced}

// feedback resolves the experience of one completed answer in the caller's conversation. The turn
// is read back from the archive, so a verdict can only judge an answer the owner was shown, and the
// experience it resolves is derived from that turn, never taken from the request. The first verdict
// stands: a second one reports the first.
func (h *ConversationDomainHandler) feedback(ctx context.Context, scope conversation.Scope, payload map[string]any) (map[string]any, error) {
	if h.verdicts == nil {
		return nil, conversation.ErrUnavailable
	}
	verdict := payloadString(payload, "verdict")
	outcome, known := answerVerdicts[verdict]
	turnID, seq := payloadString(payload, "turn_id"), payloadInt(payload, "seq")
	if !known || turnID == "" || seq < 1 {
		return nil, conversation.ErrInvalid
	}
	turns, err := h.archive.Turns(ctx, scope, payloadString(payload, "conversation_id"), seq+1, 1)
	if err != nil {
		return nil, archiveError(err)
	}
	if len(turns) != 1 || turns[0].ID != turnID || turns[0].Seq != seq {
		return nil, conversation.ErrNotFound
	}
	ref := feedbackRef(turns[0])
	if ref == "" {
		// Only a completed answer can be judged.
		return nil, conversation.ErrConflict
	}
	answer, err := h.verdicts.Handle(ctx, memorydomain.Request{Action: memorydomain.ActionResolveExperience,
		Event: memorydomain.Event{Source: "orchestrator", Payload: map[string]any{"ref": ref, "outcome": outcome}}})
	if err != nil {
		return nil, fmt.Errorf("the verdict could not be kept: memory is unavailable")
	}
	found, _ := answer["found"].(bool)
	resolved, _ := answer["resolved"].(bool)
	current, _ := answer["outcome"].(string)
	view := map[string]any{"turn_id": turnID, "found": found, "resolved": resolved}
	for word, mapped := range answerVerdicts {
		if mapped == current {
			view["verdict"] = word
		}
	}
	return view, nil
}
