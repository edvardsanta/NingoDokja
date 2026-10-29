package clients

import (
	"encoding/base64"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestVoiceClientReadsSpeechAndRejectsMalformedAudio(t *testing.T) {
	valid := true
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/stt" {
			fmt.Fprint(w, `{"text":"hello"}`)
			return
		}
		if !valid {
			fmt.Fprint(w, "not audio")
			return
		}
		wav := make([]byte, 44)
		copy(wav, "RIFF")
		copy(wav[8:], "WAVE")
		w.Write(wav)
	}))
	defer server.Close()
	client := NewVoiceServiceClient(server.URL)
	result, err := client.Invoke(t.Context(), "tts", map[string]any{"text": "hello"})
	if err != nil {
		t.Fatal(err)
	}
	data, err := base64.StdEncoding.DecodeString(result["audio_bytes_b64"].(string))
	if err != nil || len(data) != 44 {
		t.Fatal(result, err)
	}
	result, err = client.Invoke(t.Context(), "stt", nil)
	if err != nil || result["text"] != "hello" {
		t.Fatal(result, err)
	}
	valid = false
	if _, err = client.Invoke(t.Context(), "tts", nil); err == nil {
		t.Fatal("malformed audio accepted")
	}
}
