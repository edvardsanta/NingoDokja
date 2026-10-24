package clients

import (
	"context"
	"encoding/json"
	"fmt"
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

func inferenceSocket(t *testing.T, h http.HandlerFunc) *ChatProviderClient {
	t.Helper()
	path := filepath.Join(t.TempDir(), "inference.sock")
	l, err := net.Listen("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	s := &http.Server{Handler: h}
	t.Cleanup(func() { s.Close() })
	go s.Serve(l)
	return NewChatProviderClient(path)
}

func TestChatStreamHandsOverPreviewsThenReturnsTheReply(t *testing.T) {
	c := inferenceSocket(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/chat/stream" {
			t.Error(r.URL.Path)
		}
		w.Write([]byte("{\"preview\":\"The\"}\n{\"preview\":\"The ans\"}\n{\"reply\":\"The answer\",\"provider\":\"fixture\"}\n"))
	})
	var seen []string
	reply, err := c.GenerateMessagesStream(context.Background(), []map[string]string{{"role": "user", "content": "q"}}, func(text string) { seen = append(seen, text) })
	if err != nil || reply["reply"] != "The answer" || strings.Join(seen, "|") != "The|The ans" {
		t.Fatal(reply, err, seen)
	}
}

func TestChatStreamReturnsAFailureSentAfterPreviews(t *testing.T) {
	c := inferenceSocket(t, func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("{\"preview\":\"half\"}\n{\"error\":\"chat provider attempted a tool or unsupported event\"}\n"))
	})
	if _, err := c.GenerateMessagesStream(context.Background(), []map[string]string{{"role": "user", "content": "q"}}, func(string) {}); err == nil || !strings.Contains(err.Error(), "tool") {
		t.Fatal(err)
	}
}

func TestChatStreamFallsBackToChatOnAnOlderRuntime(t *testing.T) {
	c := inferenceSocket(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/chat" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		json.NewEncoder(w).Encode(map[string]any{"reply": "whole"})
	})
	previews := 0
	reply, err := c.GenerateMessagesStream(context.Background(), []map[string]string{{"role": "user", "content": "q"}}, func(string) { previews++ })
	if err != nil || reply["reply"] != "whole" || previews != 0 {
		t.Fatal(reply, err, previews)
	}
}

// Each preview repeats the text so far: a long, slow reply streams many times its own size.
func TestChatStreamReturnsALongReplyWhosePreviewsAddUpPastMegabytes(t *testing.T) {
	text := strings.Repeat("word ", 2000)
	c := inferenceSocket(t, func(w http.ResponseWriter, r *http.Request) {
		for end := 10; end <= len(text); end += 10 {
			fmt.Fprintf(w, "{\"preview\":%q}\n", text[:end])
		}
		fmt.Fprintf(w, "{\"reply\":%q,\"provider\":\"fixture\"}\n", text)
	})
	previews := 0
	reply, err := c.GenerateMessagesStream(context.Background(), []map[string]string{{"role": "user", "content": "q"}}, func(string) { previews++ })
	if err != nil || reply["reply"] != text || previews != 1000 {
		t.Fatal(err, previews)
	}
}
