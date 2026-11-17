package handlers

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	promptfiles "read_books/dokja_store/prompts"
)

func TestMissingPrivatePromptReturnsConfigurationErrorBeforeInference(t *testing.T) {
	previous := promptfiles.Directory()
	promptfiles.SetDirectory(t.TempDir())
	t.Cleanup(func() { promptfiles.SetDirectory(previous) })
	g := &fakeChatGenerator{result: map[string]any{"reply": "unexpected"}}
	result, err := NewChatDomainHandler(g).Handle(t.Context(), chatEvent("Hello"), chatStep)
	var configuration *promptfiles.Error
	if result != nil || !errors.As(err, &configuration) || g.personas != 0 {
		t.Fatal("missing configuration reached inference or escaped the operation boundary")
	}
}

func TestMissingReasoningFileFallsBackToNormalGeneration(t *testing.T) {
	previous := promptfiles.Directory()
	directory := t.TempDir()
	for _, name := range []string{"chat_persona", "chat_capability_guidance", "discord_reply_presentation"} {
		if err := os.WriteFile(filepath.Join(directory, name+".md"), []byte("fixture:"+name), 0600); err != nil {
			t.Fatal(err)
		}
	}
	promptfiles.SetDirectory(directory)
	t.Cleanup(func() { promptfiles.SetDirectory(previous) })
	inner := &fakeChatGenerator{result: map[string]any{"reply": "fallback"}}
	g := &triagingGenerator{triageModel: inner}
	h := NewChatDomainHandler(g)
	event := chatEvent("2 + 2?")
	result, err := h.generateReply(t.Context(), event, "2 + 2?", []chatMessage{{Role: "user", Content: "2 + 2?"}}, nil, "", "")
	if err != nil || result["reply"] != "fallback" || inner.personas != 1 {
		t.Fatal("missing optional reasoning file failed the turn", err)
	}
}
