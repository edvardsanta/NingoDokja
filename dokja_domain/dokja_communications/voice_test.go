package communications

import (
	"context"
	"encoding/base64"
	"testing"
)

type voiceFixture struct {
	calls     int
	operation string
	payload   map[string]any
}

func (f *voiceFixture) Invoke(_ context.Context, op string, p map[string]any) (map[string]any, error) {
	f.calls++
	f.operation = op
	f.payload = p
	return map[string]any{"text": "hello"}, nil
}
func TestVoicePolicyValidatesBeforeCallingProviders(t *testing.T) {
	f := &voiceFixture{}
	for _, test := range []struct {
		action  string
		payload map[string]any
	}{{"synthesize-voice", map[string]any{"text": ""}}, {"transcribe-voice", map[string]any{"audio_bytes_b64": "invalid"}}, {"unsupported", nil}} {
		if _, err := ProcessVoice(t.Context(), f, test.action, test.payload); err == nil {
			t.Fatal("invalid request accepted")
		}
	}
	if f.calls != 0 {
		t.Fatal("invalid input reached provider")
	}
	_, err := ProcessVoice(t.Context(), f, "synthesize-voice", map[string]any{"text": "hello"})
	if err != nil || f.operation != "tts" || f.payload["language"] != "pt-BR" {
		t.Fatal(err, f)
	}
	wav := make([]byte, 44)
	copy(wav, "RIFF")
	copy(wav[8:], "WAVE")
	_, err = ProcessVoice(t.Context(), f, "transcribe-voice", map[string]any{"audio_bytes_b64": base64.StdEncoding.EncodeToString(wav)})
	if err != nil || f.operation != "stt" {
		t.Fatal(err, f)
	}
}
