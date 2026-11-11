package clients

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"read_books/internal/logger"
	"strings"
	"time"
)

// GenerateTaskJSON shares provider selection, deadlines and validated fallback across routines.
// Runtime owns the task profile. The routine supplies its response contract validator.
func (c *ChatProviderClient) GenerateTaskJSON(ctx context.Context, profile string, schema map[string]any, messages []map[string]string, accept func(map[string]any) error) (map[string]any, error) {
	if profile != "lightweight" {
		return nil, fmt.Errorf("unsupported inference task profile")
	}
	inventory, err := c.call(ctx, "GET", "/chat/tasks/"+profile, nil)
	if err != nil {
		return nil, err
	}
	bindings, _ := inventory["bindings"].([]any)
	lastErr := fmt.Errorf("no task bindings are available")
	for index, raw := range bindings {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		binding, _ := raw.(map[string]any)
		provider, _ := binding["provider"].(string)
		model, _ := binding["model"].(string)
		if provider == "" || model == "" {
			continue
		}
		model = legacyTaskModel(provider, model)
		wait := 15 * time.Second
		if deadline, ok := ctx.Deadline(); ok {
			wait = min(wait, time.Until(deadline)/time.Duration(len(bindings)-index))
		}
		attempt, cancel := context.WithTimeout(ctx, wait)
		reply, err := c.GenerateJSONWithBinding(attempt, provider, model, schema, messages)
		cancel()
		if err == nil {
			text, _ := reply["reply"].(string)
			if !json.Valid([]byte(text)) {
				err = fmt.Errorf("task provider returned invalid JSON")
			} else if accept != nil {
				err = accept(reply)
			}
			if err == nil {
				return reply, nil
			}
		}
		lastErr = err
		logger.Info(fmt.Sprintf("LLM task fallback provider=%s model=%s: %v", provider, model, err))
	}
	return nil, fmt.Errorf("task inference unavailable: %w", lastErr)
}

// Existing price model overrides remain compatibility aliases for lightweight tasks.
// New providers and callers use the Runtime registrations instead.
func legacyTaskModel(provider, model string) string {
	aliases := map[string]string{"claude_code": "DOKJA_PRICE_CLAUDE_MODEL", "codex": "DOKJA_PRICE_CODEX_MODEL", "ollama": "DOKJA_PRICE_OLLAMA_MODEL"}
	if name := aliases[provider]; name != "" {
		if override := strings.TrimSpace(os.Getenv(name)); override != "" {
			return override
		}
	}
	return model
}
