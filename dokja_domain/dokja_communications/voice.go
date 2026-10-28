package communications

import (
	"context"
	"encoding/base64"
	"fmt"
	"strings"
)

// Voice is a stateless speech integration; eligibility and bounds belong here.
type Voice interface {
	Invoke(context.Context, string, map[string]any) (map[string]any, error)
}

func VoiceAction(action string) bool {
	return action == "synthesize-voice" || action == "transcribe-voice" || action == "detect-voice-wakeword"
}

func ProcessVoice(ctx context.Context, voice Voice, action string, payload map[string]any) (map[string]any, error) {
	if voice == nil {
		return nil, fmt.Errorf("voice service is not configured")
	}
	language, _ := payload["language"].(string)
	if language == "" {
		language = "pt-BR"
	}
	if len(language) > 32 {
		return nil, fmt.Errorf("invalid voice language")
	}
	request := map[string]any{"language": language}
	operation := ""
	switch action {
	case "synthesize-voice":
		text, _ := payload["text"].(string)
		if strings.TrimSpace(text) == "" || len([]rune(text)) > 4000 {
			return nil, fmt.Errorf("voice text must contain 1 to 4000 characters")
		}
		request["text"], request["voice"], request["format"] = text, "default", "wav"
		operation = "tts"
	case "transcribe-voice", "detect-voice-wakeword":
		encoded, _ := payload["audio_bytes_b64"].(string)
		if len(encoded) > 2800000 {
			return nil, fmt.Errorf("voice segment is too large")
		}
		data, err := base64.StdEncoding.DecodeString(encoded)
		if err != nil || len(data) < 44 || string(data[:4]) != "RIFF" || string(data[8:12]) != "WAVE" {
			return nil, fmt.Errorf("voice segment must be WAV audio")
		}
		request["audio_bytes_b64"], request["audio_format"] = encoded, "wav"
		operation = "stt"
		if action == "detect-voice-wakeword" {
			operation = "wakeword"
			request["hotword"] = "Ningo"
		}
	default:
		return nil, fmt.Errorf("unsupported voice action")
	}
	return voice.Invoke(ctx, operation, request)
}
