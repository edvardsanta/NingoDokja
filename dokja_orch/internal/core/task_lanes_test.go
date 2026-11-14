package core

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

func waitFor(t *testing.T, mu *sync.Mutex, done func() bool) {
	t.Helper()
	deadline := time.After(2 * time.Second)
	for {
		mu.Lock()
		ok := done()
		mu.Unlock()
		if ok {
			return
		}
		select {
		case <-deadline:
			t.Fatal("timed out waiting for the lanes")
		case <-time.After(2 * time.Millisecond):
		}
	}
}

func TestTaskLanesOrderPositionsAndRunLanesIndependently(t *testing.T) {
	lanes := NewTaskLanes(8)
	var mu sync.Mutex
	runs := []string{}
	admit := func(key string, position int) *LaneTicket {
		ticket, err := lanes.Admit(TaskRef{Domain: DomainKnowledge, Workflow: "wf", Key: key}, func(full bool) (int, bool, error) {
			if full {
				t.Error("the lane must not be full")
			}
			return position, true, nil
		})
		if err != nil {
			t.Fatal(err)
		}
		return ticket
	}
	run := func(id string, ticket *LaneTicket) {
		if err := ticket.Wait(context.Background()); err != nil {
			t.Error(err)
			return
		}
		mu.Lock()
		runs = append(runs, id)
		mu.Unlock()
	}

	a1 := admit("one", 1)
	go run("a1", a1)
	waitFor(t, &mu, func() bool { return len(runs) == 1 })

	a2 := admit("one", 2)
	go run("a2", a2)
	a3 := admit("one", 3)
	go run("a3", a3)

	// b1 is of another lane: it runs although a2 and a3 still wait.
	b1 := admit("two", 1)
	go run("b1", b1)
	waitFor(t, &mu, func() bool { return len(runs) == 2 })

	// Releases settle the lane in position order.
	a1.Release()
	waitFor(t, &mu, func() bool { return len(runs) == 3 })
	a2.Release()
	waitFor(t, &mu, func() bool { return len(runs) == 4 })
	a3.Release()

	mu.Lock()
	defer mu.Unlock()
	want := []string{"a1", "b1", "a2", "a3"}
	for i := range want {
		if runs[i] != want[i] {
			t.Fatalf("run order %v, want %v", runs, want)
		}
	}
}

func TestTaskLanesBoundRefusesBeforeRecordingAndReplaysWithoutEnrolling(t *testing.T) {
	lanes := NewTaskLanes(2)
	ref := TaskRef{Domain: DomainKnowledge, Workflow: "wf", Key: "bounded"}
	mustAdmit := func() *LaneTicket {
		ticket, err := lanes.Admit(ref, func(full bool) (int, bool, error) {
			return 1, true, nil
		})
		if err != nil {
			t.Fatal(err)
		}
		return ticket
	}
	first, second := mustAdmit(), mustAdmit()

	calls := 0
	ticket, err := lanes.Admit(ref, func(full bool) (int, bool, error) {
		calls++
		if !full {
			t.Fatal("a past-bound admission must learn the lane is full")
		}
		return 1, true, nil
	})
	if !errors.Is(err, ErrLaneFull) || ticket != nil {
		t.Fatalf("a full lane must refuse before enrolling: %v", err)
	}
	if calls != 1 {
		t.Fatalf("record was consulted %d times", calls)
	}

	replay, err := lanes.Admit(ref, func(full bool) (int, bool, error) {
		return 1, false, nil
	})
	if err != nil || replay != nil {
		t.Fatalf("a replay must not enroll or fail: %v", err)
	}

	first.Release()
	third, err := lanes.Admit(ref, func(full bool) (int, bool, error) {
		if full {
			t.Fatal("the lane freed a place")
		}
		return 1, true, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	second.Release()
	third.Release()
}

func TestTaskLanesWaitCancellationAndReleaseTwice(t *testing.T) {
	lanes := NewTaskLanes(8)
	first, err := lanes.Admit(TaskRef{Domain: DomainKnowledge, Workflow: "wf", Key: "lane"}, func(bool) (int, bool, error) {
		return 1, true, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	second, err := lanes.Admit(TaskRef{Domain: DomainKnowledge, Workflow: "wf", Key: "lane"}, func(bool) (int, bool, error) {
		return 2, true, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	waiting := make(chan error)
	go func() { waiting <- second.Wait(ctx) }()
	if !second.Blocked() {
		t.Fatal("the later entry must wait behind the earlier one")
	}
	cancel()
	if err := <-waiting; !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation must surface: %v", err)
	}
	second.Release()
	second.Release()
	first.Release()
	if first.Blocked() {
		t.Fatal("a released lane must not block")
	}
}

func TestTaskLanesSamePositionDoesNotWait(t *testing.T) {
	lanes := NewTaskLanes(8)
	first, err := lanes.Admit(TaskRef{Domain: DomainKnowledge, Workflow: "wf", Key: "lane"}, func(bool) (int, bool, error) {
		return 1, true, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	// A retry shares the turn's position and must not wait behind the stale attempt.
	retry, err := lanes.Admit(TaskRef{Domain: DomainKnowledge, Workflow: "wf", Key: "lane"}, func(bool) (int, bool, error) {
		return 1, true, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if retry.Blocked() {
		t.Fatal("an entry at the same position must not wait")
	}
	first.Release()
	retry.Release()
}

func TestTaskLanesProgressObservationsInOrder(t *testing.T) {
	lanes := NewTaskLanes(8)
	var mu sync.Mutex
	observed := [][2]string{}
	lanes.OnProgress(func(ref TaskRef, state LaneState) {
		mu.Lock()
		observed = append(observed, [2]string{ref.Key, string(state)})
		mu.Unlock()
	})
	ref := TaskRef{Domain: DomainKnowledge, Workflow: "wf", Key: "progress"}
	first, err := lanes.Admit(ref, func(bool) (int, bool, error) { return 1, true, nil })
	if err != nil {
		t.Fatal(err)
	}
	second, err := lanes.Admit(ref, func(bool) (int, bool, error) { return 2, true, nil })
	if err != nil {
		t.Fatal(err)
	}
	secondDone := make(chan error, 1)
	go func() { secondDone <- second.Wait(context.Background()) }()
	waitFor(t, &mu, func() bool {
		return len(observed) == 3 && observed[2][1] == string(LaneQueued)
	})
	first.Release()
	if err := <-secondDone; err != nil {
		t.Fatal(err)
	}
	second.Release()

	other, err := lanes.Admit(TaskRef{Domain: DomainKnowledge, Workflow: "wf", Key: "alone"}, func(bool) (int, bool, error) {
		return 1, true, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := other.Wait(context.Background()); err != nil {
		t.Fatal(err)
	}
	other.Release()

	mu.Lock()
	defer mu.Unlock()
	want := [][2]string{
		{"progress", "accepted"},
		{"progress", "accepted"},
		{"progress", "queued"},
		{"progress", "dispatched"},
		{"alone", "accepted"},
		{"alone", "dispatched"},
	}
	if len(observed) != len(want) {
		t.Fatalf("observations %v, want %v", observed, want)
	}
	for i := range want {
		if observed[i] != want[i] {
			t.Fatalf("observation %d is %v, want %v (all: %v)", i, observed[i], want[i], observed)
		}
	}
}

func TestTaskLanesRefuseAnIncompleteReference(t *testing.T) {
	lanes := NewTaskLanes(8)
	if _, err := lanes.Admit(TaskRef{Workflow: "wf", Key: "no-domain"}, func(bool) (int, bool, error) {
		return 1, true, nil
	}); err == nil {
		t.Fatal("an incomplete reference must not name a lane")
	}
}
