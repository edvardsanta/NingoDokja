package runtime

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"math"
	domain "read_books/dokja_domain/dokja_runtime"
)

func (a ChatAdapters) SupportsTokenUsage(provider string) bool {
	return provider == "codex" || provider == "claude_code"
}
func (a ChatAdapters) GenerateMeasured(ctx context.Context, b domain.ChatBinding, m []domain.ChatMessage) (domain.MeasuredInferenceResult, error) {
	if !a.SupportsTokenUsage(b.Provider) {
		return domain.MeasuredInferenceResult{}, fmt.Errorf("selected provider does not support measured inference")
	}
	var usage domain.TokenUsage
	reply, err := a.generate(ctx, b, m, nil, &usage)
	return domain.MeasuredInferenceResult{Reply: reply, Usage: usage}, err
}

// Counts are provider-reported, not estimates or a billing reconciliation.
// Missing fields stay null. Normal chat does not depend on this parser.
func reportedTokenUsage(provider string, output []byte) domain.TokenUsage {
	var usage domain.TokenUsage
	if provider == "claude_code" {
		var result struct {
			Usage struct {
				Input  *int64 `json:"input_tokens"`
				Output *int64 `json:"output_tokens"`
				Read   *int64 `json:"cache_read_input_tokens"`
				Write  *int64 `json:"cache_creation_input_tokens"`
			} `json:"usage"`
		}
		if json.Unmarshal(output, &result) != nil {
			return usage
		}
		u := result.Usage
		usage.OutputTokens = u.Output
		usage.CacheReadInputTokens = u.Read
		usage.CacheWriteInputTokens = u.Write
		if u.Input == nil || u.Read == nil || u.Write == nil {
			return usage
		}
		if *u.Input < 0 || *u.Read < 0 || *u.Write < 0 || *u.Input > math.MaxInt64-*u.Read || *u.Input+*u.Read > math.MaxInt64-*u.Write {
			return domain.TokenUsage{}
		}
		total := *u.Input + *u.Read + *u.Write
		usage.InputTokens = &total
		return usage
	}
	if provider == "codex" {
		count := 0
		for _, line := range bytes.Split(output, []byte("\n")) {
			if len(bytes.TrimSpace(line)) == 0 {
				continue
			}
			var event struct {
				Type string `json:"type"`
				Raw  struct {
					Input  *int64 `json:"input_tokens"`
					Output *int64 `json:"output_tokens"`
					Read   *int64 `json:"cached_input_tokens"`
					Write  *int64 `json:"cache_write_input_tokens"`
				} `json:"usage"`
			}
			if json.Unmarshal(line, &event) != nil {
				return domain.TokenUsage{}
			}
			if event.Type == "turn.completed" {
				count++
				usage = domain.TokenUsage{InputTokens: event.Raw.Input, OutputTokens: event.Raw.Output, CacheReadInputTokens: event.Raw.Read, CacheWriteInputTokens: event.Raw.Write}
			}
		}
		if count != 1 {
			return domain.TokenUsage{}
		}
	}
	return usage
}
