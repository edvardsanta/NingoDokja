package runtime

import (
	"context"
	"errors"
	"reflect"
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

func TestModelAndProviderSwitchesKeepTheApplicationTranscript(t *testing.T) {
	store := &chatStore{b: ChatBinding{Revision: 1}}
	registrations := &chatRegistrations{p: ProviderPreferences{Providers: []ProviderRegistration{
		{ID: "codex", Enabled: true, Models: []string{"fixture/first", "fixture/second"}},
		{ID: "claude_code", Enabled: true, Models: []string{"fixture/third"}},
	}}}
	inference := &chatFake{}
	d := New(nil, Journal{}, nil).WithProviderSettings(registrations).WithChat(store, inference)
	history := []ChatMessage{{Role: "user", Content: "A question"}}
	for _, selected := range []ChatBinding{
		{Provider: "codex", Model: "fixture/first"},
		{Provider: "codex", Model: "fixture/second"},
		{Provider: "claude_code", Model: "fixture/third"},
	} {
		selected.Revision = store.b.Revision
		binding, err := d.ConfigureChat(selected)
		if err != nil {
			t.Fatal(err)
		}
		reply, err := d.Chat(t.Context(), history)
		if err != nil {
			t.Fatal(err)
		}
		if inference.binding != binding || !reflect.DeepEqual(inference.messages, history) {
			t.Fatal("selection changed or discarded the supplied transcript")
		}
		history = append(history, ChatMessage{Role: "assistant", Content: reply.Reply}, ChatMessage{Role: "user", Content: "Continue"})
	}
	if len(inference.messages) != 5 || inference.messages[3].Role != "assistant" {
		t.Fatal("prior provider replies did not reach the final provider")
	}
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

type streamingChat struct {
	previews []string
	reply    string
}

func (s *streamingChat) Generate(context.Context, ChatBinding, []ChatMessage) (string, error) {
	return "whole", nil
}

func (s *streamingChat) GenerateStream(_ context.Context, _ ChatBinding, _ []ChatMessage, onPreview func(string)) (string, error) {
	for _, preview := range s.previews {
		onPreview(preview)
	}
	return s.reply, nil
}

func streamingDomain(inference ChatInference) *Domain {
	s := &chatStore{b: ChatBinding{Revision: 2, Provider: "codex", Model: "fixture/model"}}
	p := &chatRegistrations{p: ProviderPreferences{Providers: []ProviderRegistration{{ID: "codex", Enabled: true, Models: []string{"fixture/model"}}}}}
	return New(nil, Journal{}, nil).WithChat(s, inference).WithProviderSettings(p)
}

func TestChatStreamPreviewsWhileWritingAndReturnsTheReply(t *testing.T) {
	d := streamingDomain(&streamingChat{previews: []string{"Th", "The ans"}, reply: "The answer"})
	var seen []string
	reply, err := d.ChatStream(t.Context(), []ChatMessage{{Role: "user", Content: "question"}}, func(text string) { seen = append(seen, text) })
	if err != nil || reply.Reply != "The answer" {
		t.Fatal(reply, err)
	}
	if strings.Join(seen, "|") != "Th|The ans" {
		t.Fatal(seen)
	}
}

func TestChatStreamStillRefusesAnInvalidReplyAfterAPreview(t *testing.T) {
	d := streamingDomain(&streamingChat{previews: []string{"something"}, reply: "   "})
	if _, err := d.ChatStream(t.Context(), []ChatMessage{{Role: "user", Content: "question"}}, func(string) {}); err == nil {
		t.Fatal("a blank reply must be refused even after a preview")
	}
}

func TestChatStreamGivesNoPreviewWhenTheInferenceCannotStream(t *testing.T) {
	d := streamingDomain(&chatFake{})
	previews := 0
	reply, err := d.ChatStream(t.Context(), []ChatMessage{{Role: "user", Content: "question"}}, func(string) { previews++ })
	if err != nil || reply.Reply != "answer" || previews != 0 {
		t.Fatal(reply, err, previews)
	}
}

func TestChatStreamValidatesBeforeAnyPreview(t *testing.T) {
	inference := &streamingChat{previews: []string{"leak"}, reply: "ok"}
	d := streamingDomain(inference)
	previews := 0
	if _, err := d.ChatStream(t.Context(), []ChatMessage{{Role: "tool", Content: "x"}}, func(string) { previews++ }); err == nil || previews != 0 {
		t.Fatal("an invalid history must be refused before the provider is asked", err, previews)
	}
}
