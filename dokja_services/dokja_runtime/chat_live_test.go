package runtime

import (
	"context"
	"errors"
	"flag"
	"strings"
	"testing"
	"time"

	domain "read_books/dokja_domain/dokja_runtime"
)

var liveChatProvider = flag.String("live-chat-provider", "", "opt-in provider: codex, claude_code or ollama")
var liveChatModel = flag.String("live-chat-model", "", "explicit model for the opt-in live chat test")
var liveChatRoot = flag.String("live-chat-root", "", "existing lite checkout for the opt-in Ollama test")

// Live checks never open the operator database, persist a binding, start a
// service or retry another provider. Only this fixed synthetic history is sent.
type liveChatConfiguration struct {
	binding domain.ChatBinding
	enabled bool
}

func (s *liveChatConfiguration) ReadChat() (domain.ChatBinding, error) { return s.binding, nil }
func (s *liveChatConfiguration) WriteChat(domain.ChatBinding) (domain.ChatBinding, error) {
	return domain.ChatBinding{}, errors.New("live fixture is read-only")
}
func (s *liveChatConfiguration) ReadProviders() (domain.ProviderPreferences, error) {
	return domain.ProviderPreferences{Revision: 1, Providers: []domain.ProviderRegistration{{ID: s.binding.Provider, Enabled: s.enabled, Models: []string{s.binding.Model}}}}, nil
}
func (s *liveChatConfiguration) WriteProvider(uint64, domain.ProviderRegistration) (uint64, error) {
	return 0, errors.New("live fixture is read-only")
}

type liveChatInvocation struct {
	adapter ChatAdapters
	calls   int
}

func (p *liveChatInvocation) Generate(ctx context.Context, b domain.ChatBinding, m []domain.ChatMessage) (string, error) {
	p.calls++
	return p.adapter.Generate(ctx, b, m)
}

func TestLiveChatProvider(t *testing.T) {
	if *liveChatProvider == "" && *liveChatModel == "" {
		t.Skip("opt in with -live-chat-provider and -live-chat-model; uses actual authenticated inference")
	}
	switch *liveChatProvider {
	case "codex", "claude_code", "ollama":
	default:
		t.Fatal("select a supported live provider")
	}
	if *liveChatModel == "" {
		t.Fatal("explicit live model required")
	}
	if *liveChatProvider == "ollama" && *liveChatRoot == "" {
		t.Fatal("an existing lite checkout is required for Ollama; no service or model is installed by the test")
	}
	settings := &liveChatConfiguration{binding: domain.ChatBinding{Revision: 1, Provider: *liveChatProvider, Model: *liveChatModel}, enabled: true}
	adapter := &liveChatInvocation{adapter: ChatAdapters{Root: *liveChatRoot}}
	d := domain.New(nil, domain.Journal{}, nil).WithProviderSettings(settings).WithChat(settings, adapter)
	messages := []domain.ChatMessage{
		{Role: "system", Content: "This is a bounded integration test. Use only the supplied conversation. Tools and external actions are not authorized."},
		{Role: "user", Content: "Remember this marker: DOKJA_CHAT_FIXTURE_BLUE"},
		{Role: "assistant", Content: "I will remember that marker for this conversation."},
		{Role: "user", Content: "Return only the exact marker from the earlier user message, without quotes or explanation."},
	}
	started := time.Now()
	result, err := d.Chat(t.Context(), messages)
	if err != nil {
		t.Fatalf("live provider did not complete a bounded inference: %v", err)
	}
	if strings.TrimSpace(result.Reply) != "DOKJA_CHAT_FIXTURE_BLUE" || result.Provider != settings.binding.Provider || result.Model != settings.binding.Model || adapter.calls != 1 {
		t.Fatal("live response did not satisfy the conversation marker contract; raw output is omitted")
	}
	elapsed := time.Since(started).Milliseconds()
	settings.enabled = false
	if _, err = d.Chat(t.Context(), messages); err == nil || adapter.calls != 1 {
		t.Fatal("disabled registration reached inference")
	}
	t.Logf("provider=%s requested_model=%s messages=%d exact_marker=true disabled_control=true inference_calls=%d elapsed_ms=%d", result.Provider, result.Model, len(messages), adapter.calls, elapsed)
}
