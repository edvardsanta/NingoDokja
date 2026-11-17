package handlers

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"context"
	conversation "read_books/dokja_domain/dokja_conversation"
	digest "read_books/dokja_domain/dokja_digest"
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

func TestArchivedMissingPromptSettlesTheRecordedTurn(t *testing.T) {
	previous := promptfiles.Directory()
	promptfiles.SetDirectory(t.TempDir())
	t.Cleanup(func() { promptfiles.SetDirectory(previous) })
	archive := archiveFor(t)
	held, err := archive.Start(t.Context(), conversation.OwnerScope, conversation.KindReading, "reading-fixture", "Fixture")
	if err != nil {
		t.Fatal(err)
	}
	g := &fakeChatGenerator{result: map[string]any{"reply": "unexpected"}}
	h := NewChatDomainHandler(g).WithArchive(archive).WithReading(func(context.Context, string) (digest.Reference, error) {
		return digest.Reference{ID: "reading-fixture", Title: "Fixture", Summary: "Bounded fixture"}, nil
	})
	result, err := h.Handle(t.Context(), ownerTurn(map[string]any{"content": "Discuss this reading", "conversation_id": held.ID, "send_id": "missing-config"}), chatStep)
	if err == nil || result != nil || g.personas != 0 {
		t.Fatal("missing configuration reached inference")
	}
	turns, err := archive.Turns(t.Context(), conversation.OwnerScope, held.ID, 0, 10)
	if err != nil || len(turns) != 1 || turns[0].State != conversation.TurnFailed {
		t.Fatal("configuration error left the turn pending", err)
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
