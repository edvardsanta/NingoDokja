package handlers

import (
	"context"
	"strings"
	"testing"

	chat "read_books/dokja_domain/dokja_chat"
	"read_books/internal/core"
)

type voiceCaptureGenerator struct {
	calls    int
	messages []map[string]string
}

func (g *voiceCaptureGenerator) Generate(context.Context, string) (map[string]any, error) {
	g.calls++
	return map[string]any{"reply": "One sentence. Extra sentence."}, nil
}
func (g *voiceCaptureGenerator) GenerateMessages(_ context.Context, messages []map[string]string) (map[string]any, error) {
	g.calls++
	g.messages = messages
	return map[string]any{"reply": "One sentence. Extra sentence."}, nil
}
func TestPassiveVoiceHistoryDoesNotInferOrCompactButIsUsedWhenAddressed(t *testing.T) {
	g := &voiceCaptureGenerator{}
	h := NewChatDomainHandler(g)
	h.sessions.maxMsgs = 4
	event := chatEvent("ambient")
	event.Context = map[string]any{"session_key": "voice-room", "voice_turns": []chat.VoiceTurn{{UserID: "member", Text: "the group chose blue"}}}
	var decisions []bool
	ctx := core.WithVoiceDecision(t.Context(), func(respond bool) { decisions = append(decisions, respond) })
	for i := 0; i < 20; i++ {
		if _, err := h.Handle(ctx, event, chatStep); err != nil {
			t.Fatal(err)
		}
	}
	if g.calls != 0 {
		t.Fatal("ambient listening invoked inference or compaction", g.calls)
	}
	state := h.sessions.items[chatSessionBaseKey(event)]
	if len(state.messages) != 20 {
		t.Fatal("passive conversation was not retained")
	}
	// Avoid a summary for this assertion so the captured messages expose raw context.
	h.sessions.maxMsgs = 4
	event.Context["voice_turns"] = []chat.VoiceTurn{{UserID: "member", Text: "Ningo, what color did we choose?"}}
	event.Payload["content"] = "Ningo, what color did we choose?"
	result, err := h.Handle(ctx, event, chatStep)
	if err != nil {
		t.Fatal(err)
	}
	if g.calls != 1 || result["reply"] != "One sentence." {
		t.Fatal("addressed reply was not concise", g.calls, result)
	}
	var combined strings.Builder
	for _, message := range g.messages {
		combined.WriteString(message["content"])
	}
	if !strings.Contains(combined.String(), "chose blue") {
		t.Fatal("ambient context missing from reply")
	}
	if len(decisions) != 21 || decisions[0] || !decisions[20] {
		t.Fatal(decisions)
	}
	event.Context["voice_turns"] = []chat.VoiceTurn{{UserID: "member", Text: "And explain more in detail"}}
	event.Payload["content"] = "And explain more in detail"
	result, err = h.Handle(ctx, event, chatStep)
	if err != nil || result["reply"] != "One sentence. Extra sentence." {
		t.Fatal("explicit detail was trimmed", result, err)
	}
	var prompt strings.Builder
	for _, message := range g.messages {
		if message["role"] == "system" {
			prompt.WriteString(message["content"])
		}
	}
	if !strings.Contains(prompt.String(), "The application has selected this batch for your reply") || strings.Contains(prompt.String(), "[SILENT]") {
		t.Fatal("accepted followup was instructed to make a second silence decision")
	}
}
