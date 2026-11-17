package chat

import (
	"fmt"
	promptfiles "read_books/dokja_store/prompts"
	"strings"
)

const (
	VoiceAudioLaughter = "laughter"
	VoiceAudioSpeech   = "speech"
	VoiceAudioUnknown  = "unknown"
)

type VoiceTurn struct {
	UserID    string `json:"userId"`
	UserName  string `json:"userName,omitempty"`
	Text      string `json:"text"`
	AudioKind string `json:"audioKind,omitempty"`
}

func VoiceTranscript(scope string, turns []VoiceTurn) (string, error) {
	return VoiceTranscriptAs(scope, turns, nil)
}

// VoiceTranscriptAs attributes each speech segment. known resolves a verified account id to the name
// the owner gave it, or empty; it is nil for a transcript without known names. Names come only from
// this resolver, never from the turns, so a request cannot claim one.
func VoiceTranscriptAs(scope string, turns []VoiceTurn, known func(userID string) KnownName) (string, error) {
	if scope == "" || len(turns) < 1 || len(turns) > 32 {
		return "", fmt.Errorf("invalid voice turn batch")
	}
	var parts []string
	total := 0
	for _, turn := range turns {
		total += len(turn.Text)
		if strings.TrimSpace(turn.UserID) == "" || len(turn.UserID) > 100 || strings.TrimSpace(turn.Text) == "" || total > 32000 {
			return "", fmt.Errorf("invalid voice participant or transcript")
		}
		switch turn.AudioKind {
		case "", VoiceAudioSpeech, VoiceAudioLaughter, VoiceAudioUnknown:
		default:
			return "", fmt.Errorf("invalid voice audio kind")
		}
		text := turn.Text
		if turn.AudioKind == VoiceAudioLaughter {
			text = "kkkk"
		}
		var name KnownName
		if known != nil {
			name = known(turn.UserID)
		}
		parts = append(parts, KnownParticipantMessage(scope, turn.UserID, turn.UserName, name, text))
	}
	return strings.Join(parts, "\n\n"), nil
}

// VoicePresentation is sent only after VoicePolicy accepts a turn for a reply.
func VoicePresentation() string { return promptfiles.MustRead("chat_voice_presentation") }

// A no-contribution decision consumes no airtime and is not stored as a spoken turn.
func SpokenReply(reply string) string {
	if strings.TrimSpace(reply) == "[SILENT]" {
		return ""
	}
	return reply
}
