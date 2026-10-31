package core

import (
	"context"
	"errors"
	"sync"
)

// ErrSendStopped is why a send's context ended when its owner stopped it.
var ErrSendStopped = errors.New("the owner stopped this send")

type runningSend struct {
	conversationID string
	sendID         string
}

// RunningSends holds the sends being answered, queued ones included, so one can be stopped from a
// request other than its own. It only cancels: the send's own request settles the turn, so a stopped
// send still has one outcome. It is in memory, like the queue; after a restart nothing is running.
type RunningSends struct {
	mu    sync.Mutex
	stops map[runningSend]*context.CancelCauseFunc
}

func NewRunningSends() *RunningSends {
	return &RunningSends{stops: map[runningSend]*context.CancelCauseFunc{}}
}

// Track makes a send stoppable until done is called. A retry of the same send replaces the attempt
// that can be stopped, and the older attempt's done leaves the newer one in place.
func (r *RunningSends) Track(ctx context.Context, conversationID, sendID string) (context.Context, func()) {
	key := runningSend{conversationID, sendID}
	tracked, cancel := context.WithCancelCause(ctx)
	r.mu.Lock()
	r.stops[key] = &cancel
	r.mu.Unlock()
	return tracked, func() {
		r.mu.Lock()
		if r.stops[key] == &cancel {
			delete(r.stops, key)
		}
		r.mu.Unlock()
		cancel(nil)
	}
}

// Stop asks a running send to stop and reports whether one was running. Stopping is a request: a
// reply that had already arrived is still recorded, so the turn is what says how the send ended.
func (r *RunningSends) Stop(conversationID, sendID string) bool {
	r.mu.Lock()
	cancel, running := r.stops[runningSend{conversationID, sendID}]
	r.mu.Unlock()
	if running {
		(*cancel)(ErrSendStopped)
	}
	return running
}
