package runtime

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// TokenBudget governs acceptance after generation. It is not a provider-side
// generation, billing or quota cap; byte/deadline limits still bound transport.
type TokenBudget struct {
	MaxInputTokens  int64 `json:"max_input_tokens"`
	MaxOutputTokens int64 `json:"max_output_tokens"`
}
type TokenUsage struct {
	InputTokens           *int64 `json:"input_tokens"`
	OutputTokens          *int64 `json:"output_tokens"`
	CacheReadInputTokens  *int64 `json:"cache_read_input_tokens"`
	CacheWriteInputTokens *int64 `json:"cache_write_input_tokens"`
}
type MeasuredInferenceResult struct {
	Reply string
	Usage TokenUsage
}
type MeasuredChatInference interface {
	SupportsTokenUsage(string) bool
	GenerateMeasured(context.Context, ChatBinding, []ChatMessage) (MeasuredInferenceResult, error)
}
type MeasuredChatReply struct {
	Status     string      `json:"status"`
	Reply      string      `json:"reply,omitempty"`
	Provider   string      `json:"provider"`
	Model      string      `json:"requested_model"`
	Revision   uint64      `json:"configuration_revision"`
	BudgetMode string      `json:"budget_mode"`
	Budget     TokenBudget `json:"budget"`
	Usage      TokenUsage  `json:"usage"`
	Calls      int         `json:"calls"`
}

func (u TokenUsage) Complete() bool {
	if u.InputTokens == nil || u.OutputTokens == nil || *u.InputTokens < 0 || *u.OutputTokens < 0 {
		return false
	}
	for _, cache := range []*int64{u.CacheReadInputTokens, u.CacheWriteInputTokens} {
		if cache != nil && (*cache < 0 || *cache > *u.InputTokens) {
			return false
		}
	}
	if u.CacheReadInputTokens != nil && u.CacheWriteInputTokens != nil && *u.CacheReadInputTokens > *u.InputTokens-*u.CacheWriteInputTokens {
		return false
	}
	return true
}

// ChatMeasured uses the selected binding once and withholds the reply when
// counts are missing/invalid or exceed the explicit acceptance budget.
func (d *Domain) ChatMeasured(ctx context.Context, messages []ChatMessage, budget TokenBudget) (MeasuredChatReply, error) {
	result := MeasuredChatReply{BudgetMode: "post_generation", Budget: budget}
	if ctx.Err() != nil {
		return result, ctx.Err()
	}
	if budget.MaxInputTokens < 1 || budget.MaxInputTokens > 1000000 || budget.MaxOutputTokens < 1 || budget.MaxOutputTokens > 65536 {
		return result, fmt.Errorf("invalid token acceptance budget")
	}
	binding, err := d.ChatBinding()
	if err != nil {
		return result, err
	}
	if err = d.validateChat(binding); err != nil {
		return result, err
	}
	adapter, ok := d.chatInference.(MeasuredChatInference)
	if !ok || !adapter.SupportsTokenUsage(binding.Provider) {
		return result, fmt.Errorf("selected provider does not support measured inference")
	}
	if err = validateChatMessages(messages); err != nil {
		return result, err
	}
	result.Provider, result.Model, result.Revision = binding.Provider, binding.Model, binding.Revision
	ctx, cancel := context.WithTimeout(ctx, 90*time.Second)
	defer cancel()
	result.Calls = 1
	reply, err := adapter.GenerateMeasured(ctx, binding, messages)
	if ctx.Err() != nil {
		return result, ctx.Err()
	}
	result.Usage = reply.Usage
	if err != nil {
		result.Status = "provider_error"
		return result, nil
	}
	if !reply.Usage.Complete() {
		result.Status = "usage_unavailable"
		return result, nil
	}
	if *reply.Usage.InputTokens > budget.MaxInputTokens || *reply.Usage.OutputTokens > budget.MaxOutputTokens {
		result.Status = "over_budget"
		return result, nil
	}
	if strings.TrimSpace(reply.Reply) == "" || len(reply.Reply) > 65536 {
		result.Status = "invalid_reply"
		return result, nil
	}
	result.Status, result.Reply = "ok", reply.Reply
	return result, nil
}

func validateChatMessages(messages []ChatMessage) error {
	if len(messages) == 0 || len(messages) > 128 {
		return fmt.Errorf("invalid chat history")
	}
	size := 0
	for _, m := range messages {
		size += len(m.Content)
		if (m.Role != "user" && m.Role != "assistant" && m.Role != "system") || strings.TrimSpace(m.Content) == "" {
			return fmt.Errorf("invalid chat message")
		}
	}
	if size > 65536 {
		return fmt.Errorf("chat history exceeds supported size")
	}
	return nil
}
