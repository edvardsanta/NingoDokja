package runtime

import (
	"context"
	"errors"
	"sync"
	"time"
)

const taskFailureThreshold = 2
const taskCooldown = time.Minute

type taskProviderState struct {
	failures int
	until    time.Time
	reason   string
	probing  bool
	epoch    uint64
}

type taskCircuit struct {
	mu     sync.Mutex
	states map[string]*taskProviderState
}

type TaskCooldownError struct {
	Reason       string
	RetryAfterMS int64
}

func (*TaskCooldownError) Error() string {
	return "task provider is temporarily paused after repeated failures"
}

func (c *taskCircuit) status(provider string, now time.Time) (int64, string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	state := c.states[provider]
	if state == nil {
		return 0, ""
	}
	if state.probing {
		return 1000, state.reason
	}
	if now.Before(state.until) {
		return max(1, state.until.Sub(now).Milliseconds()), state.reason
	}
	return 0, ""
}

func (c *taskCircuit) begin(provider string, now time.Time) (uint64, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.states == nil {
		c.states = map[string]*taskProviderState{}
	}
	state := c.states[provider]
	if state == nil {
		state = &taskProviderState{}
		c.states[provider] = state
	}
	if now.Before(state.until) || state.probing {
		return 0, &TaskCooldownError{Reason: state.reason, RetryAfterMS: max(1000, state.until.Sub(now).Milliseconds())}
	}
	if !state.until.IsZero() {
		state.probing = true
	}
	return state.epoch, nil
}

func (c *taskCircuit) finish(provider string, epoch uint64, err error, now time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	state := c.states[provider]
	if state == nil || state.epoch != epoch {
		return
	}
	wasProbing := state.probing
	state.probing = false
	if err == nil {
		state.failures, state.reason, state.until = 0, "", time.Time{}
		if wasProbing {
			state.epoch++
		}
		return
	}
	var failure *ChatProviderFailure
	if !errors.As(err, &failure) {
		return
	}
	switch failure.SafeCode() {
	case ChatAuthentication, ChatSessionLimit, ChatUsageLimit, ChatUnavailable, ChatModelUnavailable, ChatCLIIncompatible:
		state.failures++
		state.reason = string(failure.SafeCode())
	default:
		return
	}
	if state.failures >= taskFailureThreshold {
		state.until = now.Add(taskCooldown)
		state.epoch++
	}
}

// ChatTask shares transient provider recovery state across all task clients on this Runtime.
func (d *Domain) ChatTask(ctx context.Context, profile string, binding ChatBinding, messages []ChatMessage) (ChatReply, error) {
	if !validTaskProfile(profile) {
		return ChatReply{}, errors.New("unsupported inference task profile")
	}
	if err := ctx.Err(); err != nil {
		return ChatReply{}, err
	}
	if err := d.validateChat(binding); err != nil {
		return ChatReply{}, err
	}
	// The HTTP caller's disconnect/deadline cancels both waiting and generation.
	ctx, cancel := context.WithTimeout(ctx, 90*time.Second)
	defer cancel()
	release, err := d.tasks.acquire(ctx, binding.Provider)
	if err != nil {
		return ChatReply{}, err
	}
	defer release()
	epoch, err := d.taskCircuit.begin(binding.Provider, time.Now())
	if err != nil {
		return ChatReply{}, err
	}
	reply, err := d.ChatWithBinding(ctx, binding, messages)
	d.taskCircuit.finish(binding.Provider, epoch, err, time.Now())
	return reply, err
}
