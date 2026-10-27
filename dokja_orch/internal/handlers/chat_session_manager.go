package handlers

import (
	"context"
	"fmt"
	"os"
	chat "read_books/dokja_domain/dokja_chat"
	"strings"
	"sync"
	"time"

	"read_books/internal/core"
)

const defaultChatSessionTimeout = 3 * time.Minute

type chatSessionState struct {
	id         string
	lastSeenAt time.Time
	messages   []chatMessage
	summary    string
}

type chatSessionLifecycle struct {
	SessionID string
	Created   bool
	Revoked   bool
	Reason    string
	Compacted bool
}

type chatSessionManager struct {
	mu      sync.Mutex
	timeout time.Duration
	maxMsgs int
	nowFn   func() time.Time
	items   map[string]chatSessionState
	turns   map[string]chan struct{}
}

type chatMessage = chat.ContextMessage

func newChatSessionManager() *chatSessionManager {
	return &chatSessionManager{
		timeout: durationFromEnv("DOKJA_CHAT_SESSION_TIMEOUT", defaultChatSessionTimeout),
		maxMsgs: intFromEnv("DOKJA_CHAT_SESSION_MAX_MESSAGES", 20),
		nowFn:   func() time.Time { return time.Now().UTC() },
		items:   map[string]chatSessionState{},
		turns:   map[string]chan struct{}{},
	}
}

// LockTurn serializes each session through generation without blocking other sessions.
func (m *chatSessionManager) LockTurn(ctx context.Context, event core.Event) (func(), error) {
	key := chatSessionBaseKey(event)
	if m == nil || key == "" {
		return func() {}, nil
	}
	m.mu.Lock()
	gate := m.turns[key]
	if gate == nil {
		gate = make(chan struct{}, 1)
		m.turns[key] = gate
	}
	m.mu.Unlock()
	select {
	case gate <- struct{}{}:
		return func() { <-gate }, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func (m *chatSessionManager) BeginTurn(ctx context.Context, event core.Event, content string, summarize func(context.Context, chat.ContextCompaction) (string, error)) (chatSessionLifecycle, []chatMessage, error) {
	lifecycle := chatSessionLifecycle{}
	baseKey := chatSessionBaseKey(event)
	if m == nil || baseKey == "" {
		return lifecycle, []chatMessage{{Role: "user", Content: content}}, nil
	}
	m.mu.Lock()
	state, exists := m.items[baseKey]
	state.messages = cloneMessages(state.messages)
	m.mu.Unlock()
	now := m.nowFn()
	forceNew := boolFromContext(event.Context, "force_new_session")
	if forceNew || !exists {
		lifecycle.Created = true
		lifecycle.Revoked = exists
		if exists {
			lifecycle.Reason = "new chat session requested"
		}
		state = chatSessionState{id: fmt.Sprintf("chat-%d", now.UnixNano())}
	}
	plan := chat.PlanContext(state.summary, state.messages, m.maxMsgs, exists && now.Sub(state.lastSeenAt) > m.timeout)
	if len(plan.Older) > 0 {
		summary, err := summarize(ctx, plan)
		if err != nil {
			return lifecycle, nil, fmt.Errorf("compact conversation context: %w", err)
		}
		state.summary, err = chat.ValidateSummary(summary)
		if err != nil {
			return lifecycle, nil, err
		}
		state.messages = cloneMessages(plan.Recent)
		lifecycle.Compacted = true
	}
	state.lastSeenAt = now
	state.messages = append(state.messages, chatMessage{Role: "user", Content: strings.TrimSpace(content)})
	m.mu.Lock()
	m.items[baseKey] = state
	m.mu.Unlock()
	lifecycle.SessionID = state.id
	history := cloneMessages(state.messages)
	if state.summary != "" {
		history = append([]chatMessage{chat.SummaryContext(state.summary)}, history...)
	}
	return lifecycle, history, nil
}

func (m *chatSessionManager) CompleteTurn(event core.Event, reply string) {
	if m == nil {
		return
	}
	baseKey := chatSessionBaseKey(event)
	if baseKey == "" {
		return
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	state, ok := m.items[baseKey]
	if !ok {
		return
	}
	if strings.TrimSpace(reply) != "" {
		state.messages = append(state.messages, chatMessage{Role: "assistant", Content: strings.TrimSpace(reply)})
	}
	m.items[baseKey] = state
}

func chatSessionBaseKey(event core.Event) string {
	if event.Context != nil {
		if value, _ := event.Context["session_key"].(string); strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}

	source := strings.TrimSpace(string(event.Source))
	userID := strings.TrimSpace(event.User.ID)
	channelID := strings.TrimSpace(event.Channel.ID)
	if source == "" || userID == "" || channelID == "" {
		return ""
	}
	return fmt.Sprintf("%s:%s:%s", source, channelID, userID)
}

func boolFromContext(context map[string]any, key string) bool {
	if context == nil {
		return false
	}
	switch typed := context[key].(type) {
	case bool:
		return typed
	case string:
		return strings.EqualFold(strings.TrimSpace(typed), "true")
	default:
		return false
	}
}

func durationFromEnv(key string, fallback time.Duration) time.Duration {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return fallback
	}
	parsed, err := time.ParseDuration(raw)
	if err != nil || parsed <= 0 {
		return fallback
	}
	return parsed
}

func intFromEnv(key string, fallback int) int {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return fallback
	}
	var parsed int
	if _, err := fmt.Sscanf(raw, "%d", &parsed); err != nil || parsed <= 0 {
		return fallback
	}
	return parsed
}

func cloneMessages(messages []chatMessage) []chatMessage {
	if len(messages) == 0 {
		return nil
	}
	cloned := make([]chatMessage, len(messages))
	copy(cloned, messages)
	return cloned
}
