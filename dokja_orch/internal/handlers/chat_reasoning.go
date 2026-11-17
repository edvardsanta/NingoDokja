package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	promptfiles "read_books/dokja_store/prompts"
	"strings"
	"time"

	chat "read_books/dokja_domain/dokja_chat"
	"read_books/internal/core"
)

const chatReasoningBudget = 20 * time.Second

func chatReasoningBrief() string { return promptfiles.MustRead("chat_reasoning_brief") }

// reasoningTurn adapts available evidence and transport facts; the domain owns
// the decision. A web reading reaches here only after overlap/triage accepted it
// or the person supplied links this turn. Count documents, not their passages.
func reasoningTurn(ctx context.Context, event core.Event, content string, sources []knowledgeSource) chat.ReasoningTurn {
	_, owner := ownerChannel(event)
	_, voice := event.Context["voice_turns"]
	data, _ := event.Context["web_reading_turn"].(*webTurnData)
	turn := chat.ReasoningTurn{
		Message: content, Owner: owner, Voice: voice || spokenDesktopTurn(event),
		Preview: core.ReplyPreview(ctx) != nil || core.VoiceSpeech(ctx) != nil,
	}
	seen := map[string]bool{}
	for _, source := range sources {
		key := source.SourceID
		if key == "" {
			key = source.Reference
		}
		if key == "" {
			key = source.Title
		}
		seen[key] = true
		if data != nil && source.Web {
			turn.ReadingRelevant = true
		}
	}
	turn.SourceCount = len(seen)
	return turn
}

// reasonedReply never returns the raw structured response or an inference error:
// either would risk carrying private scratch work into history or logs. Failure
// simply leaves the normal generation path to answer using its original context.
func (h *ChatDomainHandler) reasonedReply(ctx context.Context, messages []map[string]string) (map[string]any, bool) {
	task, ok := h.generator.(taskJSONGenerator)
	if !ok || len(messages) == 0 {
		return nil, false
	}
	brief, err := promptfiles.Read("chat_reasoning_brief")
	if err != nil {
		return nil, false
	}
	structured := make([]map[string]string, 0, len(messages)+1)
	structured = append(structured, messages[0], map[string]string{"role": "system", "content": brief})
	structured = append(structured, messages[1:]...)
	structured = fitChatPrompt(structured)
	schema := map[string]any{
		"type": "object", "additionalProperties": false, "required": []string{"analysis", "reply"},
		"properties": map[string]any{
			"analysis": map[string]any{"type": "string"},
			"reply":    map[string]any{"type": "string", "minLength": 1},
		},
	}
	ctx, cancel := context.WithTimeout(ctx, chatReasoningBudget)
	defer cancel()
	result, err := task.GenerateTaskJSON(ctx, "analysis", schema, structured, validateReasonedReply)
	if err != nil || ctx.Err() != nil {
		return nil, false
	}
	// Validate again at this boundary, even if an implementation skips accept.
	if validateReasonedReply(result) != nil {
		return nil, false
	}
	var fields map[string]json.RawMessage
	raw, _ := result["reply"].(string)
	_ = json.Unmarshal([]byte(raw), &fields)
	var reply string
	_ = json.Unmarshal(fields["reply"], &reply)
	clean := map[string]any{"reply": reply}
	for _, key := range []string{"provider", "model"} {
		if value, ok := result[key].(string); ok {
			clean[key] = value
		}
	}
	return clean, true
}

func validateReasonedReply(result map[string]any) error {
	raw, _ := result["reply"].(string)
	var fields map[string]json.RawMessage
	if json.Unmarshal([]byte(raw), &fields) != nil || len(fields) != 2 {
		return fmt.Errorf("invalid reasoned reply")
	}
	// Validate the scratch field's type without decoding or retaining its text.
	analysis := fields["analysis"]
	if len(analysis) == 0 || analysis[0] != '"' {
		return fmt.Errorf("invalid reasoned reply")
	}
	var reply string
	if json.Unmarshal(fields["reply"], &reply) != nil || strings.TrimSpace(stripReplyPreamble(reply)) == "" {
		return fmt.Errorf("invalid reasoned reply")
	}
	return nil
}
