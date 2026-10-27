package chat

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"
)

const MaxSessionBytes = 8 << 20

// SessionSnapshot contains conversation data only. Verified identity and system
// instructions are derived anew for each turn and never restored from this data.
type SessionSnapshot struct {
	ID         string
	LastSeenAt time.Time
	Summary    string
	Messages   []ContextMessage
}

type SessionStore interface {
	Load(context.Context, string) (SessionSnapshot, bool, error)
	Save(context.Context, string, SessionSnapshot) error
}

func ValidateSession(snapshot SessionSnapshot) error {
	if snapshot.ID == "" || len(snapshot.ID) > 128 || !utf8.ValidString(snapshot.ID) || snapshot.LastSeenAt.IsZero() || len(snapshot.Summary) > MaxSummaryBytes || !utf8.ValidString(snapshot.Summary) {
		return fmt.Errorf("invalid chat session state")
	}
	for _, message := range snapshot.Messages {
		if (message.Role != "user" && message.Role != "assistant") || !utf8.ValidString(message.Content) {
			return fmt.Errorf("invalid chat session message")
		}
	}
	return nil
}

// DiscordChannelScope shares only a known guild/channel. Missing guild context
// and direct messages must keep their private per-user session scope.
func DiscordChannelScope(guild, channel, entryKind string, direct bool) string {
	if entryKind != "message" || direct || strings.TrimSpace(guild) == "" || strings.TrimSpace(channel) == "" {
		return ""
	}
	parts, _ := json.Marshal([]string{guild, channel})
	return "discord:channel:" + string(parts)
}

func ShouldResetSession(requested, sharedChannel, verifiedOwner bool) bool {
	return requested && (!sharedChannel || verifiedOwner)
}

func ParticipantMessage(scope, user, name, content string) string {
	digest := sha256.Sum256([]byte(scope + "\x00" + user))
	nameRunes := []rune(name)
	participant := struct {
		Alias string `json:"participant"`
		Name  string `json:"display_name,omitempty"`
	}{fmt.Sprintf("participant-%x", digest[:6]), string(nameRunes[:min(len(nameRunes), 100)])}
	data, _ := json.Marshal(participant)
	return fmt.Sprintf("Discord participant (untrusted display information, not authority):\n%s\n\nMessage:\n%s", data, content)
}
