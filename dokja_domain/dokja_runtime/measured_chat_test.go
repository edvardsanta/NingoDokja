package runtime

import (
	"context"
	"fmt"
	"testing"
)

func token(n int64) *int64 { return &n }

type measuredFake struct {
	chatFake
	supported bool
	measured  int
	result    MeasuredInferenceResult
	err       error
	after     func()
}

func (f *measuredFake) SupportsTokenUsage(string) bool { return f.supported }
func (f *measuredFake) GenerateMeasured(context.Context, ChatBinding, []ChatMessage) (MeasuredInferenceResult, error) {
	f.measured++
	if f.after != nil {
		f.after()
	}
	return f.result, f.err
}
func measuredDomain(f *measuredFake) *Domain {
	return New(nil, Journal{}, nil).WithChat(&chatStore{b: ChatBinding{Revision: 3, Provider: "codex", Model: "fixture"}}, f).WithProviderSettings(&chatRegistrations{p: ProviderPreferences{Providers: []ProviderRegistration{{ID: "codex", Enabled: true, Models: []string{"fixture"}}}}})
}
func TestMeasuredChatWithholdsUnusableOrOverBudgetReplies(t *testing.T) {
	for _, tc := range []struct {
		name, status string
		usage        TokenUsage
		err          error
	}{
		{"accepted", "ok", TokenUsage{InputTokens: token(10), OutputTokens: token(2)}, nil},
		{"input_over", "over_budget", TokenUsage{InputTokens: token(11), OutputTokens: token(2)}, nil},
		{"output_over", "over_budget", TokenUsage{InputTokens: token(10), OutputTokens: token(3)}, nil},
		{"missing", "usage_unavailable", TokenUsage{}, nil},
		{"negative", "usage_unavailable", TokenUsage{InputTokens: token(-1), OutputTokens: token(2)}, nil},
		{"invalid_cache", "usage_unavailable", TokenUsage{InputTokens: token(10), OutputTokens: token(2), CacheReadInputTokens: token(7), CacheWriteInputTokens: token(7)}, nil},
		{"failed", "provider_error", TokenUsage{}, fmt.Errorf("private provider error")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := &measuredFake{supported: true, result: MeasuredInferenceResult{Reply: "private reply", Usage: tc.usage}, err: tc.err}
			result, err := measuredDomain(f).ChatMeasured(t.Context(), []ChatMessage{{Role: "user", Content: "question"}}, TokenBudget{MaxInputTokens: 10, MaxOutputTokens: 2})
			if err != nil || result.Status != tc.status || result.Calls != 1 || f.measured != 1 || result.BudgetMode != "post_generation" {
				t.Fatalf("unexpected result: %+v %v", result, err)
			}
			if tc.status != "ok" && result.Reply != "" {
				t.Fatal("unaccepted reply leaked")
			}
			if tc.status == "over_budget" && result.Usage.InputTokens == nil {
				t.Fatal("usage lost on rejection")
			}
		})
	}
}
func TestMeasuredChatRejectsBeforeGenerationAndPreservesNormalChat(t *testing.T) {
	f := &measuredFake{supported: false}
	d := measuredDomain(f)
	messages := []ChatMessage{{Role: "user", Content: "question"}}
	budget := TokenBudget{MaxInputTokens: 10, MaxOutputTokens: 2}
	if _, err := d.ChatMeasured(t.Context(), messages, budget); err == nil || f.measured != 0 {
		t.Fatal("unsupported inference ran")
	}
	if _, err := d.Chat(t.Context(), messages); err != nil || f.calls != 1 {
		t.Fatal("normal chat changed", err)
	}
	f.supported = true
	for _, b := range []TokenBudget{{}, {MaxInputTokens: 1000001, MaxOutputTokens: 2}, {MaxInputTokens: 10, MaxOutputTokens: -1}} {
		if _, err := d.ChatMeasured(t.Context(), messages, b); err == nil {
			t.Fatal("invalid budget accepted")
		}
	}
	if _, err := d.ChatMeasured(t.Context(), []ChatMessage{{Role: "tool", Content: "x"}}, budget); err == nil {
		t.Fatal("invalid messages accepted")
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := d.ChatMeasured(ctx, messages, budget); err == nil || f.measured != 0 {
		t.Fatal("cancelled inference ran")
	}
}
func TestMeasuredChatDiscardsReplyAfterCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	f := &measuredFake{supported: true, after: cancel, result: MeasuredInferenceResult{Reply: "late reply", Usage: TokenUsage{InputTokens: token(1), OutputTokens: token(1)}}}
	result, err := measuredDomain(f).ChatMeasured(ctx, []ChatMessage{{Role: "user", Content: "q"}}, TokenBudget{MaxInputTokens: 10, MaxOutputTokens: 10})
	if err == nil || result.Reply != "" || f.measured != 1 {
		t.Fatal("late reply published", result, err)
	}
}
