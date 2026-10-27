package handlers

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	chat "read_books/dokja_domain/dokja_chat"
)

type compactionGenerator struct {
	summary  string
	err      error
	requests [][]map[string]string
	replies  [][]map[string]string
}

func (g *compactionGenerator) Generate(context.Context, string) (map[string]any, error) {
	return nil, errors.New("unexpected plain generation")
}
func (g *compactionGenerator) GenerateMessages(_ context.Context, messages []map[string]string) (map[string]any, error) {
	if strings.HasPrefix(messages[0]["content"], chatPersona) {
		g.replies = append(g.replies, messages)
		return map[string]any{"reply": "An assistant claim about the kingdom."}, nil
	}
	g.requests = append(g.requests, messages)
	return map[string]any{"reply": g.summary}, g.err
}

func TestIdleCompactionKeepsSummaryAndSessionAcrossFollowingTurns(t *testing.T) {
	g := &compactionGenerator{summary: "The user defined a kingdom. Ningo challenged its voting rule."}
	h := NewChatDomainHandler(g)
	now := time.Unix(100, 0)
	h.sessions.nowFn = func() time.Time { return now }
	event := chatEvent("The kingdom has five rulers.")
	first, err := h.Handle(t.Context(), event, chatStep)
	if err != nil {
		t.Fatal(err)
	}
	now = now.Add(4 * time.Minute)
	event.Payload["content"] = "Exactly, continue the debate."
	second, err := h.Handle(t.Context(), event, chatStep)
	if err != nil {
		t.Fatal(err)
	}
	if second["notice"] != "chat.context_compacted" || len(g.requests) != 1 {
		t.Fatal("missing actual compaction")
	}
	if !strings.Contains(g.requests[0][1]["content"], "kingdom has five") || !strings.Contains(g.requests[0][1]["content"], `"role":"assistant"`) {
		t.Fatal("summary lost original turns or speakers")
	}
	if !reflect.DeepEqual(first["session"].(map[string]any)["id"], second["session"].(map[string]any)["id"]) || second["session"].(map[string]any)["revoked"] != false {
		t.Fatal("idle compaction reset session")
	}
	for _, turn := range g.replies[1:] {
		if turn[1]["role"] != "user" || !strings.Contains(turn[1]["content"], g.summary) {
			t.Fatal("summary not supplied as untrusted context")
		}
	}
	event.Payload["content"] = "What if three conspire?"
	third, err := h.Handle(t.Context(), event, chatStep)
	if err != nil {
		t.Fatal(err)
	}
	if third["notice"] != nil || len(g.requests) != 1 || !strings.Contains(g.replies[2][1]["content"], g.summary) {
		t.Fatal("summary not carried forward")
	}
	other := chatEvent("unrelated discussion")
	other.User.ID = "another-user"
	if _, err := h.Handle(t.Context(), other, chatStep); err != nil {
		t.Fatal(err)
	}
	if len(g.replies[3]) != 2 || strings.Contains(g.replies[3][1]["content"], g.summary) {
		t.Fatal("summary leaked into another user's session")
	}
}

func TestMessageLimitCompactionMergesPriorSummaryAndPreservesRecentTurns(t *testing.T) {
	g := &compactionGenerator{summary: "Earlier goals and corrections."}
	h := NewChatDomainHandler(g)
	h.sessions.maxMsgs = 6
	event := chatEvent("A discussion")
	for i := 0; i < 7; i++ {
		if _, err := h.Handle(t.Context(), event, chatStep); err != nil {
			t.Fatal(err)
		}
	}
	if len(g.requests) < 2 || !strings.Contains(g.requests[1][1]["content"], g.summary) {
		t.Fatal("incremental compaction lost prior summary")
	}
	state := h.sessions.items[chatSessionBaseKey(event)]
	if len(state.messages) > 6 || state.summary != g.summary {
		t.Fatal("history was not bounded with summary")
	}
	last := g.replies[len(g.replies)-1]
	if last[len(last)-1]["content"] != "A discussion" {
		t.Fatal("current user turn compacted")
	}
}

func TestFailedCompactionRepliesWithoutLosingHistoryAndRetriesAfterCooldown(t *testing.T) {
	for _, bad := range []string{"", strings.Repeat("x", chat.MaxSummaryBytes+1), "provider-error", "timeout"} {
		t.Run(bad[:min(len(bad), 16)], func(t *testing.T) {
			g := &compactionGenerator{summary: bad}
			h := NewChatDomainHandler(g)
			h.sessions.maxMsgs = 2
			now := time.Unix(100, 0)
			h.sessions.nowFn = func() time.Time { return now }
			event := chatEvent("A correction to remember")
			if _, err := h.Handle(t.Context(), event, chatStep); err != nil {
				t.Fatal(err)
			}
			before := h.sessions.items[chatSessionBaseKey(event)]
			if bad == "provider-error" {
				g.err = errors.New("offline")
			}
			if bad == "timeout" {
				g.err = context.DeadlineExceeded
			}
			result, err := h.Handle(t.Context(), event, chatStep)
			if err != nil {
				t.Fatal("failed compaction blocked the reply", err)
			}
			after := h.sessions.items[chatSessionBaseKey(event)]
			if len(g.replies) != 2 || len(after.messages) != len(before.messages)+2 || after.summary != before.summary || after.id != before.id || result["notice"] != nil {
				t.Fatal("fallback lost history, reset session or claimed compaction")
			}
			if !reflect.DeepEqual(before.messages, after.messages[:len(before.messages)]) {
				t.Fatal("older turns changed")
			}
			// The current question remains present even when all old raw turns are withheld.
			last := g.replies[len(g.replies)-1]
			if last[len(last)-1]["content"] != "A correction to remember" {
				t.Fatal("current input omitted")
			}
			g.err = nil
			g.summary = "Preserved correction."
			if _, err = h.Handle(t.Context(), event, chatStep); err != nil {
				t.Fatal(err)
			}
			if len(g.requests) != 1 || len(g.replies) != 3 {
				t.Fatal("cooldown retried compaction or blocked reply")
			}
			now = now.Add(compactionRetryDelay + time.Second)
			result, err = h.Handle(t.Context(), event, chatStep)
			if err != nil || len(g.requests) != 2 || result["notice"] != "chat.context_compacted" {
				t.Fatal("compaction did not recover after cooldown", err)
			}
			if state := h.sessions.items[chatSessionBaseKey(event)]; state.summary != g.summary || !state.compactionRetryAt.IsZero() {
				t.Fatal("valid summary not adopted")
			}
		})
	}
}

func TestExplicitNewSessionClearsSummaryWithoutCompactionNotice(t *testing.T) {
	g := &compactionGenerator{summary: "Old discussion"}
	h := NewChatDomainHandler(g)
	event := chatEvent("old")
	if _, err := h.Handle(t.Context(), event, chatStep); err != nil {
		t.Fatal(err)
	}
	event.Context = map[string]any{"force_new_session": true}
	event.Payload["content"] = "start fresh"
	result, err := h.Handle(t.Context(), event, chatStep)
	if err != nil {
		t.Fatal(err)
	}
	if result["notice"] != "chat.session_started" || len(g.requests) != 0 || len(g.replies[1]) != 2 {
		t.Fatal("explicit reset retained old history or claimed compaction")
	}
}

func TestPlainGenerationReceivesCompactedHistory(t *testing.T) {
	g := &plainGenerator{}
	h := NewChatDomainHandler(g)
	h.sessions.maxMsgs = 2
	event := chatEvent("A prior turn")
	if _, err := h.Handle(t.Context(), event, chatStep); err != nil {
		t.Fatal(err)
	}
	event.Payload["content"] = "Continue from the summary"
	result, err := h.Handle(t.Context(), event, chatStep)
	if err != nil {
		t.Fatal(err)
	}
	if result["notice"] != "chat.context_compacted" || !strings.Contains(g.prompt, "Earlier conversation summary") || !strings.HasSuffix(g.prompt, "Continue from the summary") {
		t.Fatal("plain generation lost compacted history or current turn")
	}
}

func TestSessionTurnLockSupportsCancellationAndIndependentSessions(t *testing.T) {
	m := newChatSessionManager()
	event := chatEvent("hello")
	unlock, err := m.LockTurn(t.Context(), event)
	if err != nil {
		t.Fatal(err)
	}
	defer unlock()
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := m.LockTurn(ctx, event); !errors.Is(err, context.Canceled) {
		t.Fatal("same session did not respect cancellation")
	}
	other := chatEvent("other")
	other.User.ID = "another-user"
	otherCtx, stop := context.WithTimeout(t.Context(), time.Second)
	defer stop()
	release, err := m.LockTurn(otherCtx, other)
	if err != nil {
		t.Fatal(err)
	}
	release()
}
