package chat

import (
	"context"
	"errors"
	"testing"
	"time"
)

type fakeUIAdapter struct {
	thinkingSent   bool
	takingLongSent bool
	reply          string
	failure        error
	timeoutSent    bool
	cleanupCount   int
}

func (f *fakeUIAdapter) SendThinking(context.Context) error {
	f.thinkingSent = true
	return nil
}

func (f *fakeUIAdapter) SendTakingLong(context.Context) error {
	f.takingLongSent = true
	return nil
}

func (f *fakeUIAdapter) SendReply(_ context.Context, content string) error {
	f.reply = content
	return nil
}

func (f *fakeUIAdapter) SendFailure(_ context.Context, cause error) error {
	f.failure = cause
	return nil
}

func (f *fakeUIAdapter) SendTimeout(context.Context) error {
	f.timeoutSent = true
	return nil
}

func (f *fakeUIAdapter) Cleanup(context.Context) error {
	f.cleanupCount++
	return nil
}

type delayedResponder struct {
	response string
	err      error
	delay    time.Duration
}

func (d *delayedResponder) Respond(ctx context.Context, _, _ string) (string, error) {
	if d.delay > 0 {
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-time.After(d.delay):
		}
	}
	if d.err != nil {
		return "", d.err
	}
	return d.response, nil
}

func TestChatOrchestratorHandleSendsReply(t *testing.T) {
	ui := &fakeUIAdapter{}
	o := NewChatOrchestrator(&delayedResponder{response: "pong"}, ChatOrchestratorConfig{
		ThinkingDelay:   20 * time.Millisecond,
		TakingLongDelay: 20 * time.Millisecond,
		Timeout:         2 * time.Second,
	})

	err := o.Handle(context.Background(), "ping", "u1", ui)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ui.reply != "pong" {
		t.Fatalf("expected reply pong, got %q", ui.reply)
	}
	if ui.cleanupCount != 1 {
		t.Fatalf("expected cleanup once, got %d", ui.cleanupCount)
	}
}

func TestChatOrchestratorHandleSendsThinkingAndTakingLong(t *testing.T) {
	ui := &fakeUIAdapter{}
	o := NewChatOrchestrator(&delayedResponder{
		response: "pong",
		delay:    40 * time.Millisecond,
	}, ChatOrchestratorConfig{
		ThinkingDelay:   5 * time.Millisecond,
		TakingLongDelay: 5 * time.Millisecond,
		Timeout:         2 * time.Second,
	})

	err := o.Handle(context.Background(), "ping", "u1", ui)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !ui.thinkingSent {
		t.Fatal("expected thinking event to be sent")
	}
	if !ui.takingLongSent {
		t.Fatal("expected taking-long event to be sent")
	}
}

func TestChatOrchestratorHandleFailureAndTimeout(t *testing.T) {
	expected := errors.New("boom")
	failureUI := &fakeUIAdapter{}
	o := NewChatOrchestrator(&delayedResponder{err: expected}, ChatOrchestratorConfig{
		ThinkingDelay:   5 * time.Millisecond,
		TakingLongDelay: 5 * time.Millisecond,
		Timeout:         2 * time.Second,
	})

	err := o.Handle(context.Background(), "ping", "u1", failureUI)
	if !errors.Is(err, expected) {
		t.Fatalf("expected error %v, got %v", expected, err)
	}
	if !errors.Is(failureUI.failure, expected) {
		t.Fatalf("expected failure callback with %v, got %v", expected, failureUI.failure)
	}

	timeoutUI := &fakeUIAdapter{}
	o = NewChatOrchestrator(&delayedResponder{delay: 30 * time.Millisecond}, ChatOrchestratorConfig{
		ThinkingDelay:   5 * time.Millisecond,
		TakingLongDelay: 5 * time.Millisecond,
		Timeout:         10 * time.Millisecond,
	})

	err = o.Handle(context.Background(), "ping", "u1", timeoutUI)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected deadline exceeded, got %v", err)
	}
	if !timeoutUI.timeoutSent {
		t.Fatal("expected timeout callback")
	}
}

func TestChatOrchestratorHandleValidation(t *testing.T) {
	ui := &fakeUIAdapter{}

	if err := (*ChatOrchestrator)(nil).Handle(context.Background(), "ping", "u1", ui); err == nil {
		t.Fatal("expected nil orchestrator error")
	}

	o := NewChatOrchestrator(nil, ChatOrchestratorConfig{})
	if err := o.Handle(context.Background(), "ping", "u1", ui); err == nil {
		t.Fatal("expected nil responder error")
	}

	o = NewChatOrchestrator(&delayedResponder{}, ChatOrchestratorConfig{})
	if err := o.Handle(context.Background(), "   ", "u1", ui); err == nil {
		t.Fatal("expected empty content error")
	}

	if err := o.Handle(context.Background(), "ping", "u1", nil); err == nil {
		t.Fatal("expected nil ui adapter error")
	}
}
