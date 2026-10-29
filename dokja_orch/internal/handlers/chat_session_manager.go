package handlers

import (
	"context"
	"errors"
	"fmt"
	"os"
	chat "read_books/dokja_domain/dokja_chat"
	"strings"
	"sync"
	"time"

	"read_books/internal/core"
)

const defaultChatSessionTimeout = 3 * time.Minute
const compactionRetryDelay = time.Minute

type chatSessionState struct {
	id                string
	lastSeenAt        time.Time
	messages          []chatMessage
	summary           string
	compactionRetryAt time.Time // volatile retry control, never persisted as conversation data
}

type chatSessionLifecycle struct {
	SessionID          string
	Created            bool
	Revoked            bool
	Reason             string
	Compacted          bool
	CompactionFallback string
}

type chatSessionManager struct {
	mu      sync.Mutex
	timeout time.Duration
	maxMsgs int
	nowFn   func() time.Time
	items   map[string]chatSessionState
	turns   map[string]chan struct{}
	store   chat.SessionStore
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
	if err := ctx.Err(); err != nil {
		return lifecycle, nil, err
	}
	baseKey := chatSessionBaseKey(event)
	if m == nil || baseKey == "" {
		return lifecycle, []chatMessage{{Role: "user", Content: content}}, nil
	}
	m.mu.Lock()
	state, exists := m.items[baseKey]
	state.messages = cloneMessages(state.messages)
	m.mu.Unlock()
	if !exists && m.store != nil {
		saved, found, err := m.store.Load(ctx, baseKey)
		if err != nil {
			return lifecycle, nil, err
		}
		if found {
			state = chatSessionState{id: saved.ID, lastSeenAt: saved.LastSeenAt, summary: saved.Summary, messages: cloneMessages(saved.Messages)}
			exists = true
		}
	}
	now := m.nowFn()
	forceNew := chat.ShouldResetSession(boolFromContext(event.Context, "force_new_session"), discordChannelScope(event) != "", boolFromContext(event.Context, "is_owner"))
	if forceNew || !exists {
		lifecycle.Created = true
		lifecycle.Revoked = exists
		if exists {
			lifecycle.Reason = "new chat session requested"
		}
		state = chatSessionState{id: fmt.Sprintf("chat-%d", now.UnixNano())}
	}
	plan := chat.PlanContext(state.summary, state.messages, m.maxMsgs, exists && now.Sub(state.lastSeenAt) > m.timeout)
	if now.Before(state.compactionRetryAt) {
		lifecycle.CompactionFallback = "retry_cooldown"
	} else if len(plan.Older) > 0 && summarize != nil {
		summary, err := summarize(ctx, plan)
		// Caller cancellation remains terminal even if the summary provider ignores it.
		if ctx.Err() != nil {
			return lifecycle, nil, ctx.Err()
		}
		reason := "provider_error"
		if errors.Is(err, context.DeadlineExceeded) {
			reason = "summary_timeout"
		}
		if err == nil {
			summary, err = chat.ValidateSummary(summary)
			reason = "invalid_summary"
		}
		if err != nil {
			lifecycle.CompactionFallback = reason
			state.compactionRetryAt = m.nowFn().Add(compactionRetryDelay)
		} else {
			state.summary = summary
			state.messages = cloneMessages(plan.Recent)
			state.compactionRetryAt = time.Time{}
			lifecycle.Compacted = true
		}
	}
	if _, voice := event.Context["voice_turns"]; voice && summarize == nil {
		state.messages = chat.BoundedVoiceHistory(state.messages)
	}
	state.lastSeenAt = now
	state.messages = append(state.messages, chatMessage{Role: "user", Content: strings.TrimSpace(content)})
	if err := m.persist(ctx, baseKey, state); err != nil {
		return lifecycle, nil, err
	}
	m.mu.Lock()
	m.items[baseKey] = state
	m.mu.Unlock()
	lifecycle.SessionID = state.id
	history := cloneMessages(state.messages)
	if lifecycle.CompactionFallback != "" || (summarize == nil && len(history) > m.maxMsgs) {
		// Keep all raw turns in the checkpoint; only the inference projection is reduced.
		recent, omitted := chat.RecoveryContext(state.messages[:len(state.messages)-1], m.maxMsgs)
		history = append(recent, state.messages[len(state.messages)-1])
		if omitted {
			history = append([]chatMessage{chat.OmittedContextNotice()}, history...)
		}
	}
	if state.summary != "" {
		history = append([]chatMessage{chat.SummaryContext(state.summary)}, history...)
	}
	return lifecycle, history, nil
}

func (m *chatSessionManager) CompleteTurn(ctx context.Context, event core.Event, reply string) error {
	if m == nil {
		return nil
	}
	baseKey := chatSessionBaseKey(event)
	if baseKey == "" {
		return nil
	}

	m.mu.Lock()
	state, ok := m.items[baseKey]
	state.messages = cloneMessages(state.messages)
	m.mu.Unlock()
	if !ok {
		return nil
	}
	if strings.TrimSpace(reply) != "" {
		state.messages = append(state.messages, chatMessage{Role: "assistant", Content: strings.TrimSpace(reply)})
	}
	state.lastSeenAt = m.nowFn()
	if err := m.persist(ctx, baseKey, state); err != nil {
		return err
	}
	m.mu.Lock()
	m.items[baseKey] = state
	m.mu.Unlock()
	return nil
}

func (m *chatSessionManager) persist(ctx context.Context, key string, state chatSessionState) error {
	if m.store == nil {
		return nil
	}
	return m.store.Save(ctx, key, chat.SessionSnapshot{ID: state.id, LastSeenAt: state.lastSeenAt, Summary: state.summary, Messages: state.messages})
}

func discordChannelScope(event core.Event) string {
	if event.Source != core.SourceDiscord {
		return ""
	}
	guild, _ := event.Context["guild_id"].(string)
	kind, _ := event.Context["entry_kind"].(string)
	return chat.DiscordChannelScope(guild, event.Channel.ID, kind, boolFromContext(event.Context, "is_direct_message"))
}

func chatSessionBaseKey(event core.Event) string {
	if scope := discordChannelScope(event); scope != "" {
		return scope
	}
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
