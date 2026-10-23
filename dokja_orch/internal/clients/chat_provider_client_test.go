package clients

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
)

func TestChatUsesPrivateSocketAndForwardsHistory(t *testing.T) {
	path := filepath.Join(t.TempDir(), "inference.sock")
	l, err := net.Listen("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	s := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var v struct {
			Messages []map[string]string `json:"messages"`
		}
		json.NewDecoder(r.Body).Decode(&v)
		if len(v.Messages) != 3 || v.Messages[2]["content"] != "followup" {
			t.Error("history lost")
		}
		json.NewEncoder(w).Encode(map[string]any{"reply": "answer", "provider": "fixture"})
	})}
	defer s.Close()
	go s.Serve(l)
	t.Setenv("CHAT_AI_SERVICE_ENDPOINT", "http://obsolete.invalid")
	c := NewChatProviderClient(path)
	reply, err := c.GenerateMessages(context.Background(), []map[string]string{{"role": "user", "content": "first"}, {"role": "assistant", "content": "previous"}, {"role": "user", "content": "followup"}})
	if err != nil || reply["reply"] != "answer" {
		t.Fatal(reply, err)
	}
}
func TestMissingInferenceSocketShowsSetupWithoutOldEndpoint(t *testing.T) {
	c := NewChatProviderClient(filepath.Join(t.TempDir(), "missing"))
	_, err := c.Generate(t.Context(), "hello")
	if err == nil || !strings.Contains(err.Error(), "runtime controller") {
		t.Fatal(err)
	}
}
