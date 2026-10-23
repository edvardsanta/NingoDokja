package runtime

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"

	domain "read_books/dokja_domain/dokja_runtime"
)

type failingChatProvider struct{ calls int }

func (*failingChatProvider) ReadChat() (domain.ChatBinding, error) {
	return domain.ChatBinding{Revision: 1, Provider: "claude_code", Model: "fixture"}, nil
}
func (*failingChatProvider) WriteChat(b domain.ChatBinding) (domain.ChatBinding, error) {
	return b, nil
}
func (*failingChatProvider) ReadProviders() (domain.ProviderPreferences, error) {
	return domain.ProviderPreferences{Providers: []domain.ProviderRegistration{{ID: "claude_code", Enabled: true, Models: []string{"fixture"}}}}, nil
}
func (*failingChatProvider) WriteProvider(uint64, domain.ProviderRegistration) (uint64, error) {
	return 0, nil
}
func (p *failingChatProvider) Generate(context.Context, domain.ChatBinding, []domain.ChatMessage) (string, error) {
	p.calls++
	return "", &domain.ChatProviderFailure{Code: domain.ChatSessionLimit}
}

func TestChatFailureCrossesHTTPAsSafeMessageWithoutReplyOrRetry(t *testing.T) {
	p := &failingChatProvider{}
	h := ChatHandler(domain.New(nil, domain.Journal{}, nil).WithProviderSettings(p).WithChat(p, p))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("POST", "/chat", strings.NewReader(`{"messages":[{"role":"user","content":"private-prompt-marker"}]}`)))
	var body map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	want := (&domain.ChatProviderFailure{Code: domain.ChatSessionLimit}).Error()
	if w.Code != 503 || len(body) != 1 || body["error"] != want || strings.Contains(w.Body.String(), "private-") || p.calls != 1 {
		t.Fatal(w.Code, body, p.calls)
	}
}
