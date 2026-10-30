package core

import (
	"context"
	"errors"
	"sync"
)

// ErrQueueFull refuses an admission before anything is recorded: the lane already holds as many
// entries as it may.
var ErrQueueFull = errors.New("too many entries are waiting in this lane")

// OrderedQueue runs the entries of one lane in their position order and lets lanes progress
// independently. The orchestrator owns it; it is volatile, so a restart forgets every entry and
// whatever recorded them decides what an unfinished entry became.
type OrderedQueue struct {
	mu         sync.Mutex
	lanes      map[string]*queueLane
	maxEntries int
}

type queueLane struct {
	// admit serializes admissions, so positions are assigned in the order entries join the lane.
	admit   sync.Mutex
	entries map[*QueueTicket]struct{}
	// users counts admissions under way plus enrolled entries; the lane is dropped at zero.
	users int
}

// QueueTicket is one admitted entry. Release it once its work has ended, whatever the outcome.
type QueueTicket struct {
	queue    *OrderedQueue
	key      string
	position int
	done     chan struct{}
	once     sync.Once
}

// NewOrderedQueue bounds each lane to maxEntries admitted and unreleased entries.
func NewOrderedQueue(maxEntries int) *OrderedQueue {
	if maxEntries <= 0 {
		panic("ordered queue needs a positive bound")
	}
	return &OrderedQueue{lanes: map[string]*queueLane{}, maxEntries: maxEntries}
}

// Admit runs record while no other admission of the lane runs, and enrolls the position it returns.
// record learns whether the lane is full: then it must record nothing new, and may only answer from
// what is already recorded (a replayed send) or refuse, typically with ErrQueueFull. A full lane
// never enrolls. When record answers enroll false (a replayed send has nothing to run), no ticket is
// made.
//
// Positions must not decrease as entries join a lane: an entry may share the position of an earlier
// one (a retry of the same turn) but never come before an entry already waiting.
func (q *OrderedQueue) Admit(key string, record func(full bool) (position int, enroll bool, err error)) (*QueueTicket, error) {
	lane := q.join(key)
	lane.admit.Lock()
	defer lane.admit.Unlock()

	q.mu.Lock()
	full := len(lane.entries) >= q.maxEntries
	q.mu.Unlock()
	position, enroll, err := record(full)
	if err == nil && enroll && full {
		err = ErrQueueFull
	}
	if err != nil || !enroll {
		q.leave(key, lane)
		return nil, err
	}
	// The admission's use of the lane passes to the ticket.
	ticket := &QueueTicket{queue: q, key: key, position: position, done: make(chan struct{})}
	q.mu.Lock()
	lane.entries[ticket] = struct{}{}
	q.mu.Unlock()
	return ticket, nil
}

// Blocked says whether an entry at an earlier position is still in the lane, so Wait would wait.
func (t *QueueTicket) Blocked() bool {
	t.queue.mu.Lock()
	defer t.queue.mu.Unlock()
	for other := range t.queue.lanes[t.key].entries {
		if other.position < t.position {
			return true
		}
	}
	return false
}

// Wait returns once every entry of the lane with an earlier position has been released, or with the
// context's error. An entry at the same position does not hold it back.
func (t *QueueTicket) Wait(ctx context.Context) error {
	t.queue.mu.Lock()
	ahead := []chan struct{}{}
	for other := range t.queue.lanes[t.key].entries {
		if other.position < t.position {
			ahead = append(ahead, other.done)
		}
	}
	t.queue.mu.Unlock()
	for _, done := range ahead {
		select {
		case <-done:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return nil
}

// Release removes the entry from its lane. It is safe to call more than once.
func (t *QueueTicket) Release() {
	t.once.Do(func() {
		q := t.queue
		q.mu.Lock()
		lane := q.lanes[t.key]
		delete(lane.entries, t)
		close(t.done)
		q.mu.Unlock()
		q.leave(t.key, lane)
	})
}

func (q *OrderedQueue) join(key string) *queueLane {
	q.mu.Lock()
	defer q.mu.Unlock()
	lane, ok := q.lanes[key]
	if !ok {
		lane = &queueLane{entries: map[*QueueTicket]struct{}{}}
		q.lanes[key] = lane
	}
	lane.users++
	return lane
}

// leave ends one use of the lane and drops it when nothing uses it, so finished lanes do not
// accumulate and every user of a key always shares the same lane.
func (q *OrderedQueue) leave(key string, lane *queueLane) {
	q.mu.Lock()
	defer q.mu.Unlock()
	lane.users--
	if lane.users == 0 {
		delete(q.lanes, key)
	}
}
