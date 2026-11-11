package runtime

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"
)

var ErrTaskQueueFull = errors.New("task queue is full")

const TaskConcurrency = 2
const TaskQueueLimit = 64
const TaskHistoryLimit = 100

// Transient metadata only: no prompts, completions or raw provider errors.
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
type TaskDiagnostics struct {
	Active    int           `json:"active"`
	Queued    int           `json:"queued"`
	Limit     int           `json:"limit"`
	Attempts  []TaskAttempt `json:"attempts"`
	Providers []TaskBinding `json:"providers"`
}
type taskWorkbench struct {
	mu        sync.Mutex
	active    int
	queued    int
	providers map[string]bool
	changed   chan struct{}
	attempts  []TaskAttempt
}

func (s *taskWorkbench) acquire(ctx context.Context, provider string) (func(), error) {
	s.mu.Lock()
	if s.changed == nil {
		s.changed = make(chan struct{})
		s.providers = map[string]bool{}
	}
	if s.queued >= TaskQueueLimit {
		s.mu.Unlock()
		return nil, ErrTaskQueueFull
	}
	s.queued++
	for {
		if err := ctx.Err(); err != nil {
			s.queued--
			s.mu.Unlock()
			return nil, err
		}
		if s.active < TaskConcurrency && !s.providers[provider] {
			s.queued--
			s.active++
			s.providers[provider] = true
			s.mu.Unlock()
			return func() {
				s.mu.Lock()
				s.active--
				delete(s.providers, provider)
				close(s.changed)
				s.changed = make(chan struct{})
				s.mu.Unlock()
			}, nil
		}
		changed := s.changed
		s.mu.Unlock()
		select {
		case <-ctx.Done():
		case <-changed:
		}
		s.mu.Lock()
	}
}
func validTaskProfile(profile string) bool { return profile == "lightweight" || profile == "analysis" }
func safeTaskReason(value string) bool {
	switch value {
	case "", "queue_full", "cancelled", "timeout", "session_limit", "usage_limit", "authentication_required", "model_unavailable", "cli_incompatible", "output_limit", "unavailable", "cooldown", "runtime_unavailable", "invalid_response", "provider_error", "invalid_json", "contract_rejected":
		return true
	}
	return false
}
func (d *Domain) RecordTaskAttempt(a TaskAttempt) error {
	known := a.Provider == "runtime"
	for _, p := range providerSpecs {
		known = known || a.Provider == p.id
	}
	if !known || !validTaskProfile(a.Profile) || a.TaskID == 0 || a.StartedAt.IsZero() || a.DurationMS < 0 || a.DurationMS > 86400000 || a.RetryAfterMS < 0 || a.RetryAfterMS > 86400000 || !safeTaskReason(a.Reason) || !safeTaskReason(a.Cause) || (a.Model != "" && !modelName.MatchString(a.Model)) || (a.Stage != "catalog" && a.Stage != "generation") || (a.Outcome != "success" && a.Outcome != "failed" && a.Outcome != "skipped") {
		return fmt.Errorf("invalid task metadata")
	}
	d.tasks.mu.Lock()
	defer d.tasks.mu.Unlock()
	d.tasks.attempts = append(d.tasks.attempts, a)
	if len(d.tasks.attempts) > TaskHistoryLimit {
		d.tasks.attempts = append([]TaskAttempt(nil), d.tasks.attempts[len(d.tasks.attempts)-TaskHistoryLimit:]...)
	}
	return nil
}
func (d *Domain) TaskDiagnostics() TaskDiagnostics {
	d.tasks.mu.Lock()
	result := TaskDiagnostics{Active: d.tasks.active, Queued: d.tasks.queued, Limit: TaskConcurrency, Attempts: append([]TaskAttempt{}, d.tasks.attempts...), Providers: []TaskBinding{}}
	d.tasks.mu.Unlock()
	if d.providerSettings != nil {
		if preferences, err := d.providerSettings.ReadProviders(); err == nil {
			for _, p := range preferences.Providers {
				if len(p.Models) == 0 {
					continue
				}
				retry, reason := d.taskCircuit.status(p.ID, time.Now())
				result.Providers = append(result.Providers, TaskBinding{ChatBinding: ChatBinding{Provider: p.ID, Model: p.Models[0]}, RetryAfterMS: retry, FailureReason: reason})
			}
		}
	}
	return result
}
