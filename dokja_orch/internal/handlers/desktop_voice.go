package handlers

import (
	"context"
	"encoding/base64"
	"fmt"
	chat "read_books/dokja_domain/dokja_chat"
	communications "read_books/dokja_domain/dokja_communications"
	"read_books/internal/core"
)

// maxSpokenAudio bounds the synthesized WAV returned to the Desktop.
const maxSpokenAudio = 8 << 20

// WithSpeech lets Desktop replies be read aloud through the voice service.
func (h *ChatDomainHandler) WithSpeech(speech communications.Voice, enabled func() bool) *ChatDomainHandler {
	h.speech, h.speechEnabled = speech, enabled
	return h
}

// speakReply prepares speech for a reply the conversation already kept. The
// interface sends text, never a provider request: chat decides what is
// speakable and the voice service only synthesizes it.
func (h *ChatDomainHandler) speakReply(ctx context.Context, event core.Event) (map[string]any, error) {
	if event.Source != core.SourceDesktop {
		return nil, fmt.Errorf("spoken replies require the desktop interface")
	}
	if h.speech == nil || (h.speechEnabled != nil && !h.speechEnabled()) {
		return nil, fmt.Errorf("speech is unavailable")
	}
	reply, _ := event.Payload["text"].(string)
	spoken, err := chat.SpeakableReply(reply)
	if err != nil {
		return nil, err
	}
	if spoken == "" {
		return map[string]any{"spoken": ""}, nil
	}
	language, _ := event.Payload["language"].(string)
	speech, err := communications.ProcessVoice(ctx, h.speech, "synthesize-voice", map[string]any{"text": spoken, "language": language})
	if err != nil {
		return nil, err
	}
	encoded, _ := speech["audio_bytes_b64"].(string)
	data, err := base64.StdEncoding.Strict().DecodeString(encoded)
	if err != nil || len(data) < 44 || len(data) > maxSpokenAudio || string(data[:4]) != "RIFF" || string(data[8:12]) != "WAVE" {
		return nil, fmt.Errorf("speech service returned invalid audio")
	}
	return map[string]any{"spoken": spoken, "audio_bytes_b64": encoded, "audio_format": "wav"}, nil
}
