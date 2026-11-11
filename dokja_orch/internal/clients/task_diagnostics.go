package clients

import (
	"context"
	"encoding/json"
	"errors"
	"read_books/internal/logger"
	"time"
)

type TaskAttempt struct {
	TaskID       uint64    `json:"task_id"`
	Profile      string    `json:"profile"`
	Provider     string    `json:"provider"`
	Model        string    `json:"model"`
	DurationMS   int64     `json:"duration_ms"`
	Outcome      string    `json:"outcome"`
	Reason       string    `json:"reason,omitempty"`
	RetryAfterMS int64     `json:"retry_after_ms,omitempty"`
	StartedAt    time.Time `json:"started_at"`
	Cause        string    `json:"cause,omitempty"`
	Stage        string    `json:"stage"`
}

// TaskInferenceError exposes safe attempt metadata without retaining prompts or provider output.
type TaskInferenceError struct {
	Attempts []TaskAttempt `json:"attempts"`
	Reason   string        `json:"reason"`
}

func (*TaskInferenceError) Error() string {
	return "task inference is unavailable; inspect LLM task attempt diagnostics"
}

type inferenceCallError struct {
	message, code string
}

func (e *inferenceCallError) Error() string { return e.message }

func taskFailureReason(err error) string {
	if errors.Is(err, context.Canceled) {
		return "cancelled"
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return "timeout"
	}
	var failure *inferenceCallError
	if errors.As(err, &failure) {
		switch failure.code {
		case "queue_full", "timeout", "cancelled", "session_limit", "usage_limit", "authentication_required", "model_unavailable", "cli_incompatible", "output_limit", "unavailable", "cooldown", "runtime_unavailable", "invalid_response":
			return failure.code
		}
	}
	return "provider_error"
}

func logTaskAttempt(attempt TaskAttempt) {
	data, _ := json.Marshal(attempt)
	logger.Info("LLM task attempt " + string(data))
}

// Reporting has a small independent budget so cancellation still leaves safe metadata.
func (c *ChatProviderClient) recordTaskAttempt(a TaskAttempt) {
	logTaskAttempt(a)
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	_, _ = c.call(ctx, "POST", "/chat/task-attempts", a)
}
