package runtime

import (
	"context"
	"errors"
	"strings"
	"testing"
)

type chatStore struct{ b ChatBinding }

func (s *chatStore) ReadChat() (ChatBinding, error) { return s.b, nil }
func (s *chatStore) WriteChat(b ChatBinding) (ChatBinding, error) {
	if b.Revision != s.b.Revision {
		return ChatBinding{}, errors.New("conflict")
	}
	b.Revision++
	s.b = b
	return b, nil
}

type chatRegistrations struct {
	p   ProviderPreferences
	err error
}

func (s *chatRegistrations) ReadProviders() (ProviderPreferences, error) { return s.p, s.err }
func (s *chatRegistrations) WriteProvider(uint64, ProviderRegistration) (uint64, error) {
	return 0, nil
}

type chatFake struct {
	calls    int
	messages []ChatMessage
	binding  ChatBinding
}

func (f *chatFake) Generate(ctx context.Context, b ChatBinding, m []ChatMessage) (string, error) {
	f.calls++
	f.messages = m
	f.binding = b
	return "answer", ctx.Err()
}
func TestChatRequiresExplicitValidBindingAndHonorsProviderChanges(t *testing.T) {
	s := &chatStore{b: ChatBinding{Revision: 1}}
	p := &chatRegistrations{p: ProviderPreferences{Providers: []ProviderRegistration{{ID: "codex", Enabled: true, Models: []string{"fixture/model"}}}}}
	f := &chatFake{}
	d := New(nil, Journal{}, nil).WithProviderSettings(p).WithChat(s, f)
	m := []ChatMessage{{Role: "system", Content: "context"}, {Role: "user", Content: "question"}, {Role: "assistant", Content: "previous"}, {Role: "user", Content: "followup"}}
	if _, err := d.Chat(t.Context(), m); err == nil || !strings.Contains(err.Error(), "select") {
		t.Fatal(err)
	}
	if f.calls != 0 {
		t.Fatal("must not infer without selection")
	}
	if _, err := d.ConfigureChat(ChatBinding{Revision: 1, Provider: "codex", Model: "undeclared"}); err == nil {
		t.Fatal("undeclared model accepted")
	}
	b, err := d.ConfigureChat(ChatBinding{Revision: 1, Provider: "codex", Model: "fixture/model"})
	if err != nil || b.Revision != 2 {
		t.Fatal(b, err)
	}
	if _, err := d.ConfigureChat(ChatBinding{Revision: 1, Provider: "codex", Model: "fixture/model"}); err == nil {
		t.Fatal("stale write")
	}
	result, err := d.Chat(t.Context(), m)
	if err != nil || result.Reply != "answer" || result.Provider != "codex" || len(f.messages) != 4 {
		t.Fatal(result, err)
	}
	p.p.Providers[0].Enabled = false
	if _, err := d.Chat(t.Context(), m); err == nil {
		t.Fatal("disabled provider used")
	}
	p.p.Providers[0].Enabled = true
	p.p.Providers[0].Models = nil
	if _, err := d.Chat(t.Context(), m); err == nil {
		t.Fatal("removed model used")
	}
	if f.calls != 1 {
		t.Fatal("fallback was invoked")
	}
}
func TestChatRejectsInvalidMessagesAndCorruptSettingsBeforeInference(t *testing.T) {
	s := &chatStore{b: ChatBinding{Revision: 1, Provider: "ollama", Model: "fixture"}}
	p := &chatRegistrations{p: ProviderPreferences{Providers: []ProviderRegistration{{ID: "ollama", Enabled: true, Models: []string{"fixture"}}}}}
	f := &chatFake{}
	d := New(nil, Journal{}, nil).WithChat(s, f).WithProviderSettings(p)
	for _, m := range [][]ChatMessage{nil, {{Role: "tool", Content: "x"}}, {{Role: "user", Content: " "}}, {{Role: "user", Content: strings.Repeat("x", 65537)}}} {
		if _, err := d.Chat(t.Context(), m); err == nil {
			t.Fatal("invalid request accepted")
		}
	}
	p.err = errors.New("corrupt")
	if _, err := d.Chat(t.Context(), []ChatMessage{{Role: "user", Content: "x"}}); err == nil || f.calls != 0 {
		t.Fatal(err, f.calls)
	}
}
func TestChatCancellationDoesNotRetry(t *testing.T) {
	s := &chatStore{b: ChatBinding{Revision: 1, Provider: "codex", Model: "fixture"}}
	p := &chatRegistrations{p: ProviderPreferences{Providers: []ProviderRegistration{{ID: "codex", Enabled: true, Models: []string{"fixture"}}}}}
	f := &chatFake{}
	d := New(nil, Journal{}, nil).WithChat(s, f).WithProviderSettings(p)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := d.Chat(ctx, []ChatMessage{{Role: "user", Content: "x"}}); err == nil {
		t.Fatal("cancel ignored")
	}
	if f.calls > 1 {
		t.Fatal("retried")
	}
}

func TestChatIsACapabilityRatherThanStandaloneRuntimeModule(t *testing.T) {
	if _, exists := Lookup("chat_ai"); exists {
		t.Fatal("retired module is still advertised")
	}
	if _, exists := Lookup("chat"); exists {
		t.Fatal("chat capability must not be a container module")
	}
}

type cancelledChatProvider struct{ cancel context.CancelFunc }

func (f cancelledChatProvider) Generate(context.Context, ChatBinding, []ChatMessage) (string, error) {
	f.cancel()
	return "late answer", nil
}

func TestChatRejectsSuccessReturnedAfterCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	s := &chatStore{b: ChatBinding{Revision: 1, Provider: "codex", Model: "fixture"}}
	p := &chatRegistrations{p: ProviderPreferences{Providers: []ProviderRegistration{{ID: "codex", Enabled: true, Models: []string{"fixture"}}}}}
	d := New(nil, Journal{}, nil).WithChat(s, cancelledChatProvider{cancel}).WithProviderSettings(p)
	got, err := d.Chat(ctx, []ChatMessage{{Role: "user", Content: "question"}})
	if !errors.Is(err, context.Canceled) || got.Reply != "" {
		t.Fatalf("late success accepted: %+v %v", got, err)
	}
}
