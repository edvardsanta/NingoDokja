package runtime

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	domain "read_books/dokja_domain/dokja_runtime"
)

type streamStore struct{ b domain.ChatBinding }

func (s *streamStore) ReadChat() (domain.ChatBinding, error) { return s.b, nil }
func (s *streamStore) WriteChat(b domain.ChatBinding) (domain.ChatBinding, error) {
	return b, nil
}

type streamProviders struct{}

func (streamProviders) ReadProviders() (domain.ProviderPreferences, error) {
	return domain.ProviderPreferences{Providers: []domain.ProviderRegistration{{ID: "ollama", Enabled: true, Models: []string{"fixture"}}}}, nil
}
func (streamProviders) WriteProvider(uint64, domain.ProviderRegistration) (uint64, error) {
	return 0, nil
}

type streamInference struct {
	previews []string
	reply    string
	err      error
}

func (s streamInference) Generate(context.Context, domain.ChatBinding, []domain.ChatMessage) (string, error) {
	return s.reply, s.err
}

func (s streamInference) GenerateStream(_ context.Context, _ domain.ChatBinding, _ []domain.ChatMessage, onPreview func(string)) (string, error) {
	for _, preview := range s.previews {
		onPreview(preview)
		time.Sleep(previewEvery + 10*time.Millisecond)
	}
	return s.reply, s.err
}

func streamHandler(inference streamInference) http.Handler {
	store := &streamStore{b: domain.ChatBinding{Revision: 2, Provider: "ollama", Model: "fixture"}}
	return ChatHandler(domain.New(nil, domain.Journal{}, nil).WithChat(store, inference).WithProviderSettings(streamProviders{}))
}

func lines(t *testing.T, body string) []map[string]any {
	t.Helper()
	var out []map[string]any
	scanner := bufio.NewScanner(strings.NewReader(body))
	for scanner.Scan() {
		var line map[string]any
		if err := json.Unmarshal(scanner.Bytes(), &line); err != nil {
			t.Fatal(scanner.Text(), err)
		}
		out = append(out, line)
	}
	return out
}

const question = `{"messages":[{"role":"user","content":"question"}]}`

func TestChatStreamSendsPreviewsThenTheReply(t *testing.T) {
	w := httptest.NewRecorder()
	streamHandler(streamInference{previews: []string{"The", "The ans"}, reply: "The answer"}).ServeHTTP(w, httptest.NewRequest("POST", "/chat/stream", strings.NewReader(question)))
	got := lines(t, w.Body.String())
	if w.Code != 200 || len(got) != 3 || got[0]["preview"] != "The" || got[1]["preview"] != "The ans" || got[2]["reply"] != "The answer" {
		t.Fatal(w.Code, w.Body.String())
	}
	if w.Header().Get("Content-Type") != "application/x-ndjson" {
		t.Fatal(w.Header())
	}
}

func TestChatStreamSaysAFailureAfterPreviewsOnItsLastLine(t *testing.T) {
	w := httptest.NewRecorder()
	streamHandler(streamInference{previews: []string{"half"}, err: errors.New("chat provider attempted a tool or unsupported event")}).ServeHTTP(w, httptest.NewRequest("POST", "/chat/stream", strings.NewReader(question)))
	got := lines(t, w.Body.String())
	if len(got) != 2 || got[0]["preview"] != "half" || got[1]["error"] == nil || got[1]["reply"] != nil {
		t.Fatal(w.Body.String())
	}
}

func TestChatStreamAnswersAFailureBeforeAnyPreviewAsChatDoes(t *testing.T) {
	w := httptest.NewRecorder()
	streamHandler(streamInference{reply: "x"}).ServeHTTP(w, httptest.NewRequest("POST", "/chat/stream", strings.NewReader(`{"messages":[]}`)))
	if w.Code != http.StatusServiceUnavailable || !strings.Contains(w.Body.String(), "invalid chat history") {
		t.Fatal(w.Code, w.Body.String())
	}
}

func TestChatStreamRefusesABrowserOrigin(t *testing.T) {
	r := httptest.NewRequest("POST", "/chat/stream", strings.NewReader(question))
	r.Header.Set("Origin", "http://localhost")
	w := httptest.NewRecorder()
	streamHandler(streamInference{reply: "x"}).ServeHTTP(w, r)
	if w.Code != http.StatusBadRequest {
		t.Fatal(w.Code)
	}
}
