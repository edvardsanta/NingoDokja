package runtime

import (
	"encoding/json"
	"net/http/httptest"
	domain "read_books/dokja_domain/dokja_runtime"
	"strings"
	"testing"
)

func TestInferenceTasksExposeRegisteredBindings(t *testing.T) {
	handler := streamHandler(streamInference{reply: "should not be requested"})
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, httptest.NewRequest("GET", "/chat/tasks/lightweight", nil))
	var body struct {
		Bindings []struct{ Provider, Model string } `json:"bindings"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil || w.Code != 200 || len(body.Bindings) != 1 || body.Bindings[0].Provider != "ollama" || body.Bindings[0].Model != "fixture" {
		t.Fatalf("response %d %s, error %v", w.Code, w.Body.String(), err)
	}
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, httptest.NewRequest("GET", "/chat/tasks/unknown", nil))
	if w.Code == 200 {
		t.Fatal("unknown task profile accepted")
	}
}

func TestTaskFailuresShareCooldownAcrossRequests(t *testing.T) {
	provider := &failingChatProvider{}
	handler := ChatHandler(domain.New(nil, domain.Journal{}, nil).WithProviderSettings(provider).WithChat(provider, provider))
	for i := 0; i < 3; i++ {
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, httptest.NewRequest("POST", "/chat/tasks/lightweight", strings.NewReader(`{"messages":[{"role":"user","content":"private-prompt"}],"provider":"claude_code","model":"fixture","format":"json"}`)))
		var body map[string]any
		json.Unmarshal(w.Body.Bytes(), &body)
		want := "session_limit"
		if i == 2 {
			want = "cooldown"
		}
		if body["code"] != want || strings.Contains(w.Body.String(), "private-prompt") {
			t.Fatalf("response %s", w.Body.String())
		}
	}
	if provider.calls != 2 {
		t.Fatalf("paused provider invoked %d times", provider.calls)
	}
}
