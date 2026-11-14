package core

import (
	"context"
)

// ErrQueueFull refuses an admission before anything is recorded: the lane already holds as
// many entries as it may. It is the archived-chat name of ErrLaneFull.
var ErrQueueFull = ErrLaneFull

// OrderedQueue runs the entries of one conversation in their position order and lets
// conversations progress independently. It is the archived-chat binding on the general
// TaskLanes mechanism: the queue's string key names the lane, and the archive's record
// callback is the admission. The orchestrator owns it; it is volatile, so a restart forgets
// every entry and the archive decides what an unfinished entry became.
type OrderedQueue struct {
	lanes *TaskLanes
}

// QueueTicket is one admitted entry. Release it once its work has ended, whatever the outcome.
type QueueTicket struct {
	ticket *LaneTicket
}

// NewOrderedQueue bounds each lane to maxEntries admitted and unreleased entries.
func NewOrderedQueue(maxEntries int) *OrderedQueue {
	if maxEntries <= 0 {
		panic("ordered queue needs a positive bound")
	}
	return &OrderedQueue{lanes: NewTaskLanes(maxEntries)}
}

// Lanes exposes the general mechanism this queue binds, for consumers that also want the
// accepted/queued/dispatched progress observations.
func (q *OrderedQueue) Lanes() *TaskLanes { return q.lanes }

// Admit runs record while no other admission of the lane runs, and enrolls the position it returns.
// record learns whether the lane is full: then it must record nothing new, and may only answer from
// what is already recorded (a replayed send) or refuse, typically with ErrQueueFull. A full lane
// never enrolls. When record answers enroll false (a replayed send has nothing to run), no ticket is
// made.
//
// Positions must not decrease as entries join a lane: an entry may share the position of an earlier
// one (a retry of the same turn) but never come before an entry already waiting.
func (q *OrderedQueue) Admit(key string, record func(full bool) (position int, enroll bool, err error)) (*QueueTicket, error) {
	ticket, err := q.lanes.Admit(TaskRef{Domain: DomainConversation, Workflow: "archived-send", Key: key}, record)
	if ticket == nil || err != nil {
		return nil, err
	}
	return &QueueTicket{ticket: ticket}, nil
}

// Blocked says whether an entry at an earlier position is still in the lane, so Wait would wait.
func (t *QueueTicket) Blocked() bool { return t.ticket.Blocked() }

// Wait returns once every entry of the lane with an earlier position has been released, or with the
// context's error. An entry at the same position does not hold it back.
func (t *QueueTicket) Wait(ctx context.Context) error { return t.ticket.Wait(ctx) }

// Release removes the entry from its lane. It is safe to call more than once.
func (t *QueueTicket) Release() { t.ticket.Release() }
