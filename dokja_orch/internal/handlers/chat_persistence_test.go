package handlers

import (
	"context"
	"errors"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	chat "read_books/dokja_domain/dokja_chat"
	"read_books/internal/core"
	chatsessions "read_books/internal/infrastructure/chat_sessions"
)

func channelEvent(user, name, content string) core.Event {
	event := chatEvent(content)
	event.User = core.EventUser{ID: user, Name: name}
	event.Context = map[string]any{"guild_id": "fixture-guild", "entry_kind": "message", "is_direct_message": false, "session_key": "legacy-per-user:" + user, "owner_configured": true}
	return event
}

func TestResumedChannelDebateRestoresSettledRulesAcrossRestartAndSpeakers(t *testing.T) {
	path := filepath.Join(t.TempDir(), chatsessions.FileName)
	store, err := chatsessions.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	settled := "Settled: Scholar defined five rulers; a majority of the other four removes a corrupt ruler. The voting rule is answered. Open: evidence and collusion."
	g := &compactionGenerator{summary: settled}
	h := NewChatDomainHandler(g).WithSessionStore(store)
	h.sessions.maxMsgs = 2
	now := time.Unix(100, 0)
	h.sessions.nowFn = func() time.Time { return now }
	event := channelEvent("fixture-scholar", "Scholar", "The other four decide by majority.")
	first, err := h.Handle(t.Context(), event, chatStep)
	if err != nil {
		t.Fatal(err)
	}
	event.Payload["content"] = "Exactly, that is the kingdom's rule."
	if _, err = h.Handle(t.Context(), event, chatStep); err != nil {
		t.Fatal(err)
	}
	saved, found, err := store.Load(t.Context(), chatSessionBaseKey(event))
	if err != nil || !found || saved.Summary != settled {
		t.Fatal("compacted state not persisted", err)
	}
	if err = store.Close(); err != nil {
		t.Fatal(err)
	}

	store, err = chatsessions.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	g2 := &compactionGenerator{summary: settled}
	h2 := NewChatDomainHandler(g2).WithSessionStore(store)
	h2.sessions.nowFn = func() time.Time { return now.Add(4 * time.Hour) }
	owner := channelEvent("fixture-owner", "Keeper", "Impressive that you agreed with Scholar.")
	owner.Context["is_owner"] = true
	resumed, err := h2.Handle(t.Context(), owner, chatStep)
	if err != nil {
		t.Fatal(err)
	}
	if resumed["session"].(map[string]any)["id"] != first["session"].(map[string]any)["id"] {
		t.Fatal("restart or changed speaker reset shared session")
	}
	if len(g2.requests) != 1 || !strings.Contains(g2.requests[0][1]["content"], settled) {
		t.Fatal("previous summary was not merged on resume")
	}
	messages := g2.replies[0]
	if !strings.Contains(messages[0]["content"], "current_speaker_is_owner=true") || !strings.Contains(messages[0]["content"], "shared Discord channel discussion") {
		t.Fatal("current identity or shared participant guidance missing")
	}
	if messages[1]["role"] != "user" || !strings.Contains(messages[1]["content"], settled) {
		t.Fatal("settled voting rule lost or promoted into system authority")
	}
	last := messages[len(messages)-1]["content"]
	if !strings.Contains(last, `"display_name":"Keeper"`) || !strings.Contains(last, "agreed with Scholar") || strings.Contains(last, "fixture-owner") {
		t.Fatal("current speaker attribution missing or raw account exposed")
	}

	other := channelEvent("fixture-member", "Other", "a different channel")
	other.Channel.ID = "another-channel"
	if _, err = h2.Handle(t.Context(), other, chatStep); err != nil {
		t.Fatal(err)
	}
	if len(g2.replies[1]) != 2 || strings.Contains(g2.replies[1][1]["content"], settled) {
		t.Fatal("history leaked across channels")
	}
}

type failingSessionStore struct {
	data                 chat.SessionSnapshot
	found                bool
	readErr, errorOnSave error
	calls, failAt        int
}

func (s *failingSessionStore) Load(context.Context, string) (chat.SessionSnapshot, bool, error) {
	return s.data, s.found, s.readErr
}
func (s *failingSessionStore) Save(_ context.Context, _ string, state chat.SessionSnapshot) error {
	s.calls++
	if s.errorOnSave != nil && (s.failAt == 0 || s.calls == s.failAt) {
		return s.errorOnSave
	}
	s.data = state
	s.found = true
	return nil
}

func TestPersistenceErrorsDoNotSilentlyFallBackToVolatileHistory(t *testing.T) {
	for _, phase := range []string{"load", "begin", "complete"} {
		t.Run(phase, func(t *testing.T) {
			store := &failingSessionStore{}
			g := &fakeChatGenerator{result: map[string]any{"reply": "reply"}}
			h := NewChatDomainHandler(g).WithSessionStore(store)
			switch phase {
			case "load":
				store.readErr = errors.New("read unavailable")
			case "begin":
				store.errorOnSave = errors.New("write unavailable")
			case "complete":
				store.errorOnSave = errors.New("write unavailable")
				store.failAt = 2
			}
			event := chatEvent("remember this")
			if _, err := h.Handle(t.Context(), event, chatStep); err == nil {
				t.Fatal("failed durable storage silently accepted")
			}
			if phase != "complete" && (len(h.sessions.items) != 0 || g.personas != 0) {
				t.Fatal("generation or cache mutation occurred after failed load/save")
			}
			if phase == "complete" {
				state := h.sessions.items[chatSessionBaseKey(event)]
				if len(state.messages) != 1 || !reflect.DeepEqual(store.data.Messages, state.messages) {
					t.Fatal("failed final save polluted cache or durable state")
				}
			}
		})
	}
}

func TestSharedChannelResetRequiresVerifiedOwnerAndPersistsAcrossReload(t *testing.T) {
	store, err := chatsessions.Open(filepath.Join(t.TempDir(), chatsessions.FileName))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	g := &compactionGenerator{summary: "Saved definition"}
	h := NewChatDomainHandler(g).WithSessionStore(store)
	event := channelEvent("member-a", "A", "First definition")
	if _, err = h.Handle(t.Context(), event, chatStep); err != nil {
		t.Fatal(err)
	}
	event = channelEvent("member-b", "B", "Start over")
	event.Context["force_new_session"] = true
	if _, err = h.Handle(t.Context(), event, chatStep); err != nil {
		t.Fatal(err)
	}
	if len(g.replies[1]) != 4 {
		t.Fatal("non-owner erased shared context")
	}
	event = channelEvent("owner", "Owner", "Start fresh")
	event.Context["is_owner"] = true
	event.Context["force_new_session"] = true
	if _, err = h.Handle(t.Context(), event, chatStep); err != nil {
		t.Fatal(err)
	}
	if len(g.replies[2]) != 2 {
		t.Fatal("explicit owner reset retained old history")
	}
	h2 := NewChatDomainHandler(g).WithSessionStore(store)
	event.Context["force_new_session"] = false
	event.Payload["content"] = "Continue"
	if _, err = h2.Handle(t.Context(), event, chatStep); err != nil {
		t.Fatal(err)
	}
	if len(g.replies[3]) != 4 || strings.Contains(g.replies[3][1]["content"], "First definition") {
		t.Fatal("reset was not persisted")
	}
}

func TestDirectMessagesStayPrivateAcrossRestart(t *testing.T) {
	store, err := chatsessions.Open(filepath.Join(t.TempDir(), chatsessions.FileName))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	g := &compactionGenerator{summary: "Private detail"}
	h := NewChatDomainHandler(g).WithSessionStore(store)
	event := channelEvent("member-a", "A", "private detail")
	event.Context["is_direct_message"] = true
	if _, err = h.Handle(t.Context(), event, chatStep); err != nil {
		t.Fatal(err)
	}
	h2 := NewChatDomainHandler(g).WithSessionStore(store)
	other := channelEvent("member-b", "B", "Hello")
	other.Context["is_direct_message"] = true
	if _, err = h2.Handle(t.Context(), other, chatStep); err != nil {
		t.Fatal(err)
	}
	if len(g.replies[1]) != 2 || strings.Contains(g.replies[1][1]["content"], "private detail") {
		t.Fatal("DM data crossed users")
	}
}
