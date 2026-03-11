package handlers

import (
	"fmt"
	"os"
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
}

type chatSessionLifecycle struct {
	SessionID string
	Created   bool
	Revoked   bool
	Reason    string
}

type chatSessionManager struct {
	mu      sync.Mutex
	timeout time.Duration
	maxMsgs int
	nowFn   func() time.Time
	items   map[string]chatSessionState
}

type chatMessage struct {
	Role    string
	Content string
}

func newChatSessionManager() *chatSessionManager {
	return &chatSessionManager{
		timeout: durationFromEnv("DOKJA_CHAT_SESSION_TIMEOUT", defaultChatSessionTimeout),
		maxMsgs: intFromEnv("DOKJA_CHAT_SESSION_MAX_MESSAGES", 20),
		nowFn:   func() time.Time { return time.Now().UTC() },
		items:   map[string]chatSessionState{},
	}
}

func (m *chatSessionManager) Touch(event core.Event) chatSessionLifecycle {
	if m == nil {
		return chatSessionLifecycle{}
	}

	baseKey := chatSessionBaseKey(event)
	if baseKey == "" {
		return chatSessionLifecycle{}
	}

	forceNew := boolFromContext(event.Context, "force_new_session")
	now := m.nowFn()

	m.mu.Lock()
	defer m.mu.Unlock()

	current, ok := m.items[baseKey]
	lifecycle := chatSessionLifecycle{}
	if ok {
		if forceNew {
			lifecycle.Revoked = true
			lifecycle.Reason = "new chat session requested"
			ok = false
		} else if now.Sub(current.lastSeenAt) > m.timeout {
			lifecycle.Revoked = true
			lifecycle.Reason = fmt.Sprintf("session timed out after %s", m.timeout.Round(time.Second))
			ok = false
		}
	}

	if !ok {
		current = chatSessionState{
			id:         fmt.Sprintf("chat-%d", now.UnixNano()),
			lastSeenAt: now,
		}
		m.items[baseKey] = current
		lifecycle.Created = true
	} else {
		current.lastSeenAt = now
		m.items[baseKey] = current
	}

	lifecycle.SessionID = current.id
	return lifecycle
}

func (m *chatSessionManager) BeginTurn(event core.Event, content string) (chatSessionLifecycle, []chatMessage) {
	lifecycle := m.Touch(event)
	if m == nil {
		return lifecycle, nil
	}

	baseKey := chatSessionBaseKey(event)
	if baseKey == "" {
		return lifecycle, []chatMessage{{Role: "user", Content: content}}
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	state := m.items[baseKey]
	state.messages = appendAndTrim(state.messages, chatMessage{Role: "user", Content: strings.TrimSpace(content)}, m.maxMsgs)
	m.items[baseKey] = state
	return lifecycle, cloneMessages(state.messages)
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
	state.messages = appendAndTrim(state.messages, chatMessage{Role: "assistant", Content: strings.TrimSpace(reply)}, m.maxMsgs)
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

func appendAndTrim(messages []chatMessage, message chatMessage, max int) []chatMessage {
	if strings.TrimSpace(message.Content) == "" {
		return messages
	}
	messages = append(messages, message)
	if max > 0 && len(messages) > max {
		messages = append([]chatMessage(nil), messages[len(messages)-max:]...)
	}
	return messages
}

func cloneMessages(messages []chatMessage) []chatMessage {
	if len(messages) == 0 {
		return nil
	}
	cloned := make([]chatMessage, len(messages))
	copy(cloned, messages)
	return cloned
}
