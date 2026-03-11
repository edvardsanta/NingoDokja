package chat

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
)

// ChatUIAdapter defines events emitted by the orchestrator to any interface adapter.
type ChatUIAdapter interface {
	SendThinking(ctx context.Context) error
	SendTakingLong(ctx context.Context) error
	SendReply(ctx context.Context, content string) error
	SendFailure(ctx context.Context, cause error) error
	SendTimeout(ctx context.Context) error
	Cleanup(ctx context.Context) error
}

type ChatOrchestratorConfig struct {
	ThinkingDelay   time.Duration
	TakingLongDelay time.Duration
	Timeout         time.Duration
}

func DefaultChatOrchestratorConfig() ChatOrchestratorConfig {
	return ChatOrchestratorConfig{
		ThinkingDelay:   2 * time.Second,
		TakingLongDelay: 5 * time.Second,
		Timeout:         3 * time.Minute,
	}
}

// ChatOrchestrator coordinates a chat request through adapters.
type ChatOrchestrator struct {
	responder ChatResponder
	config    ChatOrchestratorConfig
}

func NewChatOrchestrator(responder ChatResponder, config ChatOrchestratorConfig) *ChatOrchestrator {
	if config.ThinkingDelay <= 0 {
		config.ThinkingDelay = DefaultChatOrchestratorConfig().ThinkingDelay
	}
	if config.TakingLongDelay <= 0 {
		config.TakingLongDelay = DefaultChatOrchestratorConfig().TakingLongDelay
	}
	if config.Timeout <= 0 {
		config.Timeout = DefaultChatOrchestratorConfig().Timeout
	}

	return &ChatOrchestrator{
		responder: responder,
		config:    config,
	}
}

func (o *ChatOrchestrator) Handle(ctx context.Context, content, userID string, ui ChatUIAdapter) error {
	if o == nil {
		return fmt.Errorf("chat orchestrator is nil")
	}
	if o.responder == nil {
		return fmt.Errorf("chat responder is not configured")
	}
	if ui == nil {
		return fmt.Errorf("chat ui adapter is not configured")
	}
	if strings.TrimSpace(content) == "" {
		return fmt.Errorf("content cannot be empty")
	}

	timeoutCtx, cancel := context.WithTimeout(ctx, o.config.Timeout)
	defer cancel()

	type responseResult struct {
		response string
		err      error
	}

	responseCh := make(chan responseResult, 1)
	go func() {
		response, err := o.responder.Respond(timeoutCtx, content, userID)
		responseCh <- responseResult{response: response, err: err}
	}()

	thinkingTimer := time.NewTimer(o.config.ThinkingDelay)
	defer thinkingTimer.Stop()

	var takingLongTimer *time.Timer
	var takingLongCh <-chan time.Time

	for {
		select {
		case <-timeoutCtx.Done():
			if errors.Is(timeoutCtx.Err(), context.DeadlineExceeded) {
				if err := ui.SendTimeout(ctx); err != nil {
					return fmt.Errorf("send timeout: %w", err)
				}
			} else {
				if err := ui.SendFailure(ctx, timeoutCtx.Err()); err != nil {
					return fmt.Errorf("send cancellation failure: %w", err)
				}
			}
			if err := ui.Cleanup(ctx); err != nil {
				return fmt.Errorf("cleanup ui: %w", err)
			}
			return timeoutCtx.Err()

		case result := <-responseCh:
			if err := ui.Cleanup(ctx); err != nil {
				return fmt.Errorf("cleanup ui: %w", err)
			}
			if result.err != nil {
				if errors.Is(result.err, context.DeadlineExceeded) {
					if err := ui.SendTimeout(ctx); err != nil {
						return fmt.Errorf("send timeout: %w", err)
					}
					return result.err
				}
				if errors.Is(result.err, context.Canceled) {
					if err := ui.SendFailure(ctx, result.err); err != nil {
						return fmt.Errorf("send cancellation failure: %w", err)
					}
					return result.err
				}
				if err := ui.SendFailure(ctx, result.err); err != nil {
					return fmt.Errorf("send failure: %w", err)
				}
				return result.err
			}
			if strings.TrimSpace(result.response) == "" {
				return nil
			}
			if err := ui.SendReply(ctx, result.response); err != nil {
				return fmt.Errorf("send reply: %w", err)
			}
			return nil

		case <-thinkingTimer.C:
			if err := ui.SendThinking(ctx); err != nil {
				return fmt.Errorf("send thinking: %w", err)
			}
			takingLongTimer = time.NewTimer(o.config.TakingLongDelay)
			takingLongCh = takingLongTimer.C

		case <-takingLongCh:
			if err := ui.SendTakingLong(ctx); err != nil {
				return fmt.Errorf("send taking-long: %w", err)
			}
			if takingLongTimer != nil {
				takingLongTimer.Stop()
			}
			takingLongCh = nil
		}
	}
}
