package runtime

import (
	"net/http"
	"net/http/httptest"
	domain "read_books/dokja_domain/dokja_runtime"
	"strings"
	"testing"
)

func TestInferenceSocketCannotControlRuntime(t *testing.T) {
	h := ChatHandler(domain.New(nil, domain.Journal{}, nil))
	for _, path := range []string{"/request", "/runtime.set", "/runtime.chat.configure"} {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest("POST", path, strings.NewReader(`{}`)))
		if w.Code != http.StatusNotFound {
			t.Fatal(path, w.Code)
		}
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("GET", "/health", nil))
	if w.Code != 503 || !strings.Contains(w.Body.String(), "settings") {
		t.Fatal(w.Code, w.Body.String())
	}
	r := httptest.NewRequest("POST", "/chat", strings.NewReader(`{}`))
	r.Header.Set("Origin", "http://localhost")
	w = httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 400 {
		t.Fatal(w.Code)
	}
}

func TestAReplyFormatNeedsAnExplicitBinding(t *testing.T) {
	h := ChatHandler(domain.New(nil, domain.Journal{}, nil))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("POST", "/chat", strings.NewReader(`{"messages":[{"role":"user","content":"hi"}],"format":"json"}`)))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("the saved chat binding cannot be switched to JSON: %d", w.Code)
	}
	w = httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("POST", "/chat", strings.NewReader(`{"messages":[{"role":"user","content":"hi"}],"provider":"ollama","model":"m","format":"xml"}`)))
	if w.Code == http.StatusOK || !strings.Contains(w.Body.String(), "error") {
		t.Fatalf("an unknown format is refused: %d %s", w.Code, w.Body.String())
	}
}
