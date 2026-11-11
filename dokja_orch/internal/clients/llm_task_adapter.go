package clients

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"sync/atomic"
	"time"
)

var taskSequence atomic.Uint64

// GenerateTaskJSON shares provider selection, deadlines and validated fallback across routines.
// Runtime owns the task profile. The routine supplies its response contract validator.
func (c *ChatProviderClient) GenerateTaskJSON(ctx context.Context, profile string, schema map[string]any, messages []map[string]string, accept func(map[string]any) error) (map[string]any, error) {
	if profile != "lightweight" {
		return nil, fmt.Errorf("unsupported inference task profile")
	}
	taskID := taskSequence.Add(1)
	startedInventory := time.Now()
	inventory, err := c.call(ctx, "GET", "/chat/tasks/"+profile, nil)
	if err != nil {
		entry := TaskAttempt{TaskID: taskID, Profile: profile, Provider: "runtime", StartedAt: startedInventory.UTC(), DurationMS: time.Since(startedInventory).Milliseconds(), Outcome: "failed", Reason: taskFailureReason(err), Stage: "catalog"}
		logTaskAttempt(entry)
		return nil, &TaskInferenceError{Reason: "inventory_unavailable", Attempts: []TaskAttempt{entry}}
	}
	bindings, _ := inventory["bindings"].([]any)
	attempts := []TaskAttempt{}
	eligible := 0
	for _, raw := range bindings {
		binding, _ := raw.(map[string]any)
		retry, _ := binding["retry_after_ms"].(float64)
		if retry <= 0 {
			eligible++
		}
	}
	for _, raw := range bindings {
		if err := ctx.Err(); err != nil {
			return nil, &TaskInferenceError{Attempts: attempts, Reason: taskFailureReason(err)}
		}
		binding, _ := raw.(map[string]any)
		provider, _ := binding["provider"].(string)
		model, _ := binding["model"].(string)
		if provider == "" || model == "" {
			continue
		}
		model = legacyTaskModel(provider, model)
		entry := TaskAttempt{TaskID: taskID, Profile: profile, Provider: provider, Model: model, StartedAt: time.Now().UTC(), Stage: "generation"}
		retry, _ := binding["retry_after_ms"].(float64)
		if retry > 0 {
			entry.Outcome, entry.Reason, entry.RetryAfterMS = "skipped", "cooldown", int64(retry)
			if cause, _ := binding["failure_reason"].(string); cause != "" {
				entry.Cause = taskFailureReason(&inferenceCallError{code: cause})
			}
			attempts = append(attempts, entry)
			logTaskAttempt(entry)
			continue
		}
		wait := 15 * time.Second
		if deadline, ok := ctx.Deadline(); ok {
			wait = min(wait, time.Until(deadline)/time.Duration(max(1, eligible)))
		}
		eligible--
		attempt, cancel := context.WithTimeout(ctx, wait)
		started := time.Now()
		reply, err := c.call(attempt, "POST", "/chat/tasks/"+profile, map[string]any{
			"provider": provider, "model": model, "format": schema, "messages": messages,
		})
		cancel()
		reason := taskFailureReason(err)
		if err == nil {
			text, _ := reply["reply"].(string)
			if !json.Valid([]byte(text)) {
				err = fmt.Errorf("invalid JSON")
				reason = "invalid_json"
			} else if accept != nil {
				err = accept(reply)
				if err != nil {
					reason = "contract_rejected"
				}
			}
		}
		entry.DurationMS = time.Since(started).Milliseconds()
		entry.Outcome, entry.Reason = "failed", reason
		if err == nil {
			entry.Outcome, entry.Reason = "success", ""
		}
		attempts = append(attempts, entry)
		logTaskAttempt(entry)
		if err == nil {
			reply["attempts"] = attempts
			return reply, nil
		}
	}
	return nil, &TaskInferenceError{Attempts: attempts, Reason: "providers_exhausted"}
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
