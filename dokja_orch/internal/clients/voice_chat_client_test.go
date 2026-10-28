package clients

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
)

func TestVoiceChatClientStreamsCumulativeTextAndFinalReply(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/chat/stream" || r.Method != http.MethodPost {
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		_, _ = fmt.Fprintln(w, `{"text":"First."}`)
		_, _ = fmt.Fprintln(w, `{"text":" Next."}`)
		_, _ = fmt.Fprintln(w, `{"done":true}`)
	}))
	defer server.Close()
	var updates []string
	result, err := NewVoiceChatClient(server.URL).GenerateVoiceMessagesStream(context.Background(), []map[string]string{{"role": "user", "content": "hello"}}, func(text string) { updates = append(updates, text) })
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(updates, []string{"First.", "First. Next."}) || result["reply"] != "First. Next." {
		t.Fatalf("updates=%q result=%#v", updates, result)
	}
}

func TestVoiceChatClientRejectsMalformedAndUnfinishedFrames(t *testing.T) {
	for _, response := range []string{`not json`, `{"text":"partial"}`, `{"done":true}`} {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = fmt.Fprintln(w, response) }))
		_, err := NewVoiceChatClient(server.URL).GenerateVoiceMessagesStream(context.Background(), nil, nil)
		server.Close()
		if err == nil {
			t.Fatalf("accepted invalid response %q", response)
		}
	}
}
