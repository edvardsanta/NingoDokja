package core

import (
	"context"
	"errors"
	"fmt"
	"sync"
)

// ErrLaneFull refuses an admission before anything is recorded: the lane already holds as
// many entries as it may.
var ErrLaneFull = errors.New("too many entries are waiting in this lane")

// TaskRef names one logical task: a piece of work a domain can name, not one execution of
// it. The Key belongs to a durable record the consumer already keeps (an archive turn, a
// journal episode); it grants nothing and is never manufactured from caller scope. Workspace
// TaskID/AttemptID stay per-invocation, and the public event ID stays transport correlation.
type TaskRef struct {
	// Domain owns eligibility and result acceptance for this task.
	Domain Domain
	// Workflow is the typed workflow that executes it, versioned by its consumer.
	Workflow string
	// Key is the consumer record's stable identity for the work.
	Key string
}

// String is the canonical lane key. Two references to the same logical task key one lane.
func (r TaskRef) String() string {
	return fmt.Sprintf("%s/%s/%s", r.Domain, r.Workflow, r.Key)
}

// Valid reports whether the reference can name a lane. An empty field names nothing.
func (r TaskRef) Valid() bool {
	return r.Domain != "" && r.Workflow != "" && r.Key != ""
}

// LaneState is where an admitted task stands. accepted/queued/dispatched are progress
// observations, not outcomes: none of them proves a result or an external effect. Settled
// and interrupted are domain-owned terminals and live on the consumer's record.
type LaneState string

const (
	LaneAccepted   LaneState = "accepted"
	LaneQueued     LaneState = "queued"
	LaneDispatched LaneState = "dispatched"
)

// TaskLanes runs the admitted entries of one TaskRef key in their position order and lets
// lanes progress independently. The orchestrator owns it; it is volatile, so a restart
// forgets every entry and whatever recorded them decides what an unfinished entry became.
// OrderedQueue is the archived-chat binding on this one mechanism.
type TaskLanes struct {
	mu         sync.Mutex
	lanes      map[string]*taskLane
	maxEntries int
	progress   func(TaskRef, LaneState)
}

type taskLane struct {
	// admit serializes admissions, so positions are assigned in the order entries join the lane.
	admit   sync.Mutex
	entries map[*LaneTicket]struct{}
	// users counts admissions under way plus enrolled entries; the lane is dropped at zero.
	users int
}

// LaneTicket is one admitted entry. Release it once its work has ended, whatever the outcome.
type LaneTicket struct {
	lane     *TaskLanes
	ref      TaskRef
	key      string
	position int
	done     chan struct{}
	once     sync.Once
	stateMu  sync.Mutex
	state    LaneState
}

// NewTaskLanes bounds each lane to maxEntries admitted and unreleased entries.
func NewTaskLanes(maxEntries int) *TaskLanes {
	if maxEntries <= 0 {
		panic("task lanes need a positive bound")
	}
	return &TaskLanes{lanes: map[string]*taskLane{}, maxEntries: maxEntries}
}

// OnProgress optionally subscribes a sink to accepted/queued/dispatched transitions. A
// consumer that can deliver progress (a chat stream, for one) reports these; sources that
// cannot, report nothing. The sink must not block.
func (l *TaskLanes) OnProgress(sink func(TaskRef, LaneState)) *TaskLanes {
	l.progress = sink
	return l
}

func (l *TaskLanes) emit(ref TaskRef, state LaneState) {
	if l.progress != nil {
		l.progress(ref, state)
	}
}

// Admit runs record while no other admission of the lane runs, and enrolls the position it
// returns. record learns whether the lane is full: then it must record nothing new, and may
// only answer from what is already recorded (a replayed send) or refuse, typically with
// ErrLaneFull. A full lane never enrolls. When record answers enroll false (a replayed send
// has nothing to run), no ticket is made.
//
// Positions must not decrease as entries join a lane: an entry may share the position of an
// earlier one (a retry of the same turn) but never come before an entry already waiting.
//
// Admission and the consumer's recording happen in this one call, so lane order is record
// order and a refusal enrolls nothing.
func (l *TaskLanes) Admit(ref TaskRef, record func(full bool) (position int, enroll bool, err error)) (*LaneTicket, error) {
	if !ref.Valid() {
		return nil, fmt.Errorf("a task reference needs a domain, a workflow and a key")
	}
	key := ref.String()
	lane := l.join(key)
	lane.admit.Lock()
	defer lane.admit.Unlock()

	l.mu.Lock()
	full := len(lane.entries) >= l.maxEntries
	l.mu.Unlock()
	position, enroll, err := record(full)
	if err == nil && enroll && full {
		err = ErrLaneFull
	}
	if err != nil || !enroll {
		l.leave(key, lane)
		return nil, err
	}
	// The admission's use of the lane passes to the ticket.
	ticket := &LaneTicket{lane: l, ref: ref, key: key, position: position, done: make(chan struct{}), state: LaneAccepted}
	l.mu.Lock()
	lane.entries[ticket] = struct{}{}
	l.mu.Unlock()
	l.emit(ref, LaneAccepted)
	return ticket, nil
}

// Ref is the logical task this ticket was admitted as.
func (t *LaneTicket) Ref() TaskRef { return t.ref }

// transition records one progress observation, at most once per state.
func (t *LaneTicket) transition(state LaneState) {
	t.stateMu.Lock()
	if t.state == state {
		t.stateMu.Unlock()
		return
	}
	advanced := t.state == LaneAccepted && state == LaneQueued || t.state == LaneQueued && state == LaneDispatched || t.state == LaneAccepted && state == LaneDispatched
	if !advanced {
		t.stateMu.Unlock()
		return
	}
	t.state = state
	t.stateMu.Unlock()
	t.lane.emit(t.ref, state)
}

// Blocked says whether an entry at an earlier position is still in the lane, so Wait would wait.
// A released ticket is not in any lane and waits for nobody.
func (t *LaneTicket) Blocked() bool {
	t.lane.mu.Lock()
	defer t.lane.mu.Unlock()
	lane := t.lane.lanes[t.key]
	if lane == nil {
		return false
	}
	for other := range lane.entries {
		if other.position < t.position {
			return true
		}
	}
	return false
}

// Wait returns once every entry of the lane with an earlier position has been released, or with the
// context's error. An entry at the same position does not hold it back. While it waits the ticket
// is observed as queued; when it returns nil it is observed as dispatched.
func (t *LaneTicket) Wait(ctx context.Context) error {
	t.lane.mu.Lock()
	lane := t.lane.lanes[t.key]
	if lane == nil {
		t.lane.mu.Unlock()
		t.transition(LaneDispatched)
		return nil
	}
	ahead := []chan struct{}{}
	for other := range lane.entries {
		if other.position < t.position {
			ahead = append(ahead, other.done)
		}
	}
	t.lane.mu.Unlock()
	if len(ahead) > 0 {
		t.transition(LaneQueued)
	}
	for _, done := range ahead {
		select {
		case <-done:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	t.transition(LaneDispatched)
	return nil
}

// Release removes the entry from its lane. It is safe to call more than once.
func (t *LaneTicket) Release() {
	t.once.Do(func() {
		l := t.lane
		l.mu.Lock()
		lane := l.lanes[t.key]
		delete(lane.entries, t)
		close(t.done)
		l.mu.Unlock()
		l.leave(t.key, lane)
	})
}

// Len reports how many lanes currently exist, for diagnostics and tests that observe
// cleanup. Finished lanes are dropped, so an idle registry reads zero.
func (l *TaskLanes) Len() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return len(l.lanes)
}

func (l *TaskLanes) join(key string) *taskLane {
	l.mu.Lock()
	defer l.mu.Unlock()
	lane, ok := l.lanes[key]
	if !ok {
		lane = &taskLane{entries: map[*LaneTicket]struct{}{}}
		l.lanes[key] = lane
	}
	lane.users++
	return lane
}

// leave ends one use of the lane and drops it when nothing uses it, so finished lanes do not
// accumulate and every user of a key always shares the same lane.
func (l *TaskLanes) leave(key string, lane *taskLane) {
	l.mu.Lock()
	defer l.mu.Unlock()
	lane.users--
	if lane.users == 0 {
		delete(l.lanes, key)
	}
}
