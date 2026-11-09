package handlers

import (
	"context"
	"encoding/base64"
	"errors"
	"read_books/internal/core"
	"strings"
	"testing"
)

type fakeSpeech struct {
	calls  int
	text   string
	audio  []byte
	failed error
}

func (f *fakeSpeech) Invoke(_ context.Context, operation string, payload map[string]any) (map[string]any, error) {
	f.calls++
	if operation != "tts" {
		return nil, errors.New("unexpected operation " + operation)
	}
	f.text, _ = payload["text"].(string)
	if f.failed != nil {
		return nil, f.failed
	}
	return map[string]any{"audio_bytes_b64": base64.StdEncoding.EncodeToString(f.audio), "audio_format": "wav"}, nil
}

func speakEvent(source core.EventSource, text string) core.Event {
	return core.Event{Type: "voice.reply.speak", Source: source, Payload: map[string]any{"text": text, "language": "pt-BR"}}
}

var speakStep = core.WorkflowStep{Domain: core.DomainChat, Action: "speak-reply"}

func wavBytes() []byte {
	data := make([]byte, 64)
	copy(data, "RIFF")
	copy(data[8:], "WAVE")
	return data
}

func TestSpeakReplySynthesizesTheSpeakableText(t *testing.T) {
	speech := &fakeSpeech{audio: wavBytes()}
	handler := NewChatDomainHandler(&plainGenerator{}).WithSpeech(speech, nil)
	got, err := handler.Handle(t.Context(), speakEvent(core.SourceDesktop, "**Sim.** Leia o [ensaio](https://example.com)."), speakStep)
	if err != nil {
		t.Fatal(err)
	}
	if strings.ContainsAny(speech.text, "*[]") || strings.Contains(speech.text, "example.com") {
		t.Fatalf("markup reached the speech service: %q", speech.text)
	}
	if got["spoken"] != speech.text || got["audio_format"] != "wav" || got["audio_bytes_b64"] == "" {
		t.Fatalf("unexpected result %#v", got)
	}
}

func TestSpeakReplyIsOnlyForTheDesktopAndAnEnabledService(t *testing.T) {
	speech := &fakeSpeech{audio: wavBytes()}
	handler := NewChatDomainHandler(&plainGenerator{}).WithSpeech(speech, nil)
	if _, err := handler.Handle(t.Context(), speakEvent(core.SourceDiscord, "Oi."), speakStep); err == nil {
		t.Fatal("a non-desktop source was spoken")
	}
	disabled := NewChatDomainHandler(&plainGenerator{}).WithSpeech(speech, func() bool { return false })
	if _, err := disabled.Handle(t.Context(), speakEvent(core.SourceDesktop, "Oi."), speakStep); err == nil {
		t.Fatal("a disabled voice service was used")
	}
	unwired := NewChatDomainHandler(&plainGenerator{})
	if _, err := unwired.Handle(t.Context(), speakEvent(core.SourceDesktop, "Oi."), speakStep); err == nil {
		t.Fatal("speech without a voice client succeeded")
	}
	if speech.calls != 0 {
		t.Fatalf("speech service called %d times", speech.calls)
	}
}

func TestSpeakReplySaysNothingForSilenceAndRejectsBadAudio(t *testing.T) {
	speech := &fakeSpeech{audio: wavBytes()}
	handler := NewChatDomainHandler(&plainGenerator{}).WithSpeech(speech, nil)
	got, err := handler.Handle(t.Context(), speakEvent(core.SourceDesktop, "[SILENT]"), speakStep)
	if err != nil || got["spoken"] != "" || speech.calls != 0 {
		t.Fatalf("silence: %#v, %v, calls %d", got, err, speech.calls)
	}
	bad := &fakeSpeech{audio: []byte("not a wav file at all, but long enough to pass a length check")}
	if _, err := NewChatDomainHandler(&plainGenerator{}).WithSpeech(bad, nil).Handle(t.Context(), speakEvent(core.SourceDesktop, "Oi."), speakStep); err == nil {
		t.Fatal("non-WAV speech accepted")
	}
	failing := &fakeSpeech{failed: errors.New("tts down")}
	if _, err := NewChatDomainHandler(&plainGenerator{}).WithSpeech(failing, nil).Handle(t.Context(), speakEvent(core.SourceDesktop, "Oi."), speakStep); err == nil {
		t.Fatal("speech failure hidden")
	}
}
