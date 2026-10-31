package core

import (
	"context"
	"errors"
	"testing"
)

func TestStoppingARunningSendEndsItsContextWithTheOwnersCause(t *testing.T) {
	running := NewRunningSends()
	ctx, done := running.Track(context.Background(), "conv", "send")
	defer done()
	if !running.Stop("conv", "send") {
		t.Fatal("a tracked send must be stoppable")
	}
	if !errors.Is(context.Cause(ctx), ErrSendStopped) {
		t.Fatal("the context must say the owner stopped it", context.Cause(ctx))
	}
}

func TestOnlyTheNamedRunningSendStops(t *testing.T) {
	running := NewRunningSends()
	same, doneSame := running.Track(context.Background(), "conv", "send")
	defer doneSame()
	if running.Stop("conv", "other") || running.Stop("other", "send") {
		t.Fatal("a send is named by its conversation and its send id")
	}
	if same.Err() != nil {
		t.Fatal("another send's stop must not reach this one")
	}
}

func TestAFinishedSendCannotBeStopped(t *testing.T) {
	running := NewRunningSends()
	ctx, done := running.Track(context.Background(), "conv", "send")
	done()
	if running.Stop("conv", "send") {
		t.Fatal("a finished send is not running")
	}
	if !errors.Is(context.Cause(ctx), context.Canceled) {
		t.Fatal("finishing must not read as a stop by the owner", context.Cause(ctx))
	}
	if len(running.stops) != 0 {
		t.Fatal("a finished send must not stay held", running.stops)
	}
}

func TestAnOlderAttemptFinishingLeavesTheRetryStoppable(t *testing.T) {
	running := NewRunningSends()
	_, doneOld := running.Track(context.Background(), "conv", "send")
	retry, doneRetry := running.Track(context.Background(), "conv", "send")
	defer doneRetry()
	doneOld()
	if !running.Stop("conv", "send") || !errors.Is(context.Cause(retry), ErrSendStopped) {
		t.Fatal("the retry must be what a stop reaches", context.Cause(retry))
	}
}
