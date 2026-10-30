package core

import (
	"context"
	"errors"
	"testing"
	"time"
)

func admitAt(t *testing.T, q *OrderedQueue, key string, position int) *QueueTicket {
	t.Helper()
	ticket, err := q.Admit(key, func(bool) (int, bool, error) { return position, true, nil })
	if err != nil || ticket == nil {
		t.Fatal(ticket, err)
	}
	return ticket
}

func waitAsync(ticket *QueueTicket, ctx context.Context) <-chan error {
	done := make(chan error, 1)
	go func() { done <- ticket.Wait(ctx) }()
	return done
}

func TestOrderedQueueRunsALaneInPositionOrder(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	q := NewOrderedQueue(4)
	first, second := admitAt(t, q, "a", 1), admitAt(t, q, "a", 2)
	if err := first.Wait(ctx); err != nil {
		t.Fatal("the first entry must not wait", err)
	}
	waiting := waitAsync(second, ctx)
	select {
	case err := <-waiting:
		t.Fatal("the second entry ran before the first was released", err)
	case <-time.After(20 * time.Millisecond):
	}
	first.Release()
	if err := <-waiting; err != nil {
		t.Fatal(err)
	}
	second.Release()
}

func TestOrderedQueueKeepsLanesIndependent(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	q := NewOrderedQueue(4)
	held := admitAt(t, q, "a", 1)
	other := admitAt(t, q, "b", 7)
	if err := other.Wait(ctx); err != nil {
		t.Fatal("another lane must not wait for lane a", err)
	}
	held.Release()
	other.Release()
}

func TestOrderedQueueDoesNotHoldARetryBehindItsOwnPosition(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	q := NewOrderedQueue(4)
	stale := admitAt(t, q, "a", 3)
	retry := admitAt(t, q, "a", 3)
	if err := retry.Wait(ctx); err != nil {
		t.Fatal("a retry must not wait for the older attempt of the same position", err)
	}
	stale.Release()
	retry.Release()
}

func TestOrderedQueueNeverEnrollsIntoAFullLane(t *testing.T) {
	q := NewOrderedQueue(1)
	held := admitAt(t, q, "a", 1)
	told := false
	ticket, err := q.Admit("a", func(full bool) (int, bool, error) { told = full; return 2, true, nil })
	if !errors.Is(err, ErrQueueFull) || ticket != nil || !told {
		t.Fatal("a full lane must say so and never enroll", ticket, err, told)
	}
	replayed, err := q.Admit("a", func(bool) (int, bool, error) { return 1, false, nil })
	if err != nil || replayed != nil {
		t.Fatal("a full lane may still answer from what is recorded", replayed, err)
	}
	held.Release()
	admitAt(t, q, "a", 2).Release()
}

func TestOrderedQueueWaitStopsWithTheContext(t *testing.T) {
	q := NewOrderedQueue(4)
	held := admitAt(t, q, "a", 1)
	waiting := admitAt(t, q, "a", 2)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := waiting.Wait(ctx); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	waiting.Release()
	held.Release()
}

func TestOrderedQueueForgetsFinishedLanes(t *testing.T) {
	q := NewOrderedQueue(4)
	if _, err := q.Admit("replayed", func(bool) (int, bool, error) { return 1, false, nil }); err != nil {
		t.Fatal(err)
	}
	if _, err := q.Admit("failed", func(bool) (int, bool, error) { return 0, false, errors.New("refused") }); err == nil {
		t.Fatal("the record error must reach the caller")
	}
	ticket := admitAt(t, q, "done", 1)
	ticket.Release()
	ticket.Release()
	if len(q.lanes) != 0 {
		t.Fatal("finished lanes must be dropped", q.lanes)
	}
}

func TestOrderedQueueSaysWhetherAnEntryWouldWait(t *testing.T) {
	q := NewOrderedQueue(4)
	first := admitAt(t, q, "a", 1)
	second := admitAt(t, q, "a", 2)
	sameAsSecond := admitAt(t, q, "a", 2)
	if first.Blocked() {
		t.Fatal("the first entry waits for nobody")
	}
	if !second.Blocked() || !sameAsSecond.Blocked() {
		t.Fatal("entries behind the first must be blocked")
	}
	first.Release()
	if second.Blocked() {
		t.Fatal("an entry at the same position does not hold another back")
	}
	second.Release()
	sameAsSecond.Release()
}
