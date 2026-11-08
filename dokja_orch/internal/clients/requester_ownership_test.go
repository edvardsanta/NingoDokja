package clients

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"read_books/internal/infrastructure/repreq"
)

// inFlightRequester stands in for a ZeroMQ socket, which only one thread may use at a time. It
// records a Close that arrives while Request is still running: on a real socket that aborts the
// process (libzmq asserts in its signaler), which a cancelled caller used to cause.
type inFlightRequester struct {
	entered, release, closed chan struct{}
	mu                       sync.Mutex
	inFlight, closedInFlight bool
}

func newInFlightRequester() *inFlightRequester {
	return &inFlightRequester{entered: make(chan struct{}), release: make(chan struct{}), closed: make(chan struct{})}
}

func (r *inFlightRequester) Request(string) (string, error) {
	r.mu.Lock()
	r.inFlight = true
	r.mu.Unlock()
	close(r.entered)
	<-r.release
	r.mu.Lock()
	r.inFlight = false
	r.mu.Unlock()
	// What a receive timeout returns once the service has not answered.
	return "", errors.New("resource temporarily unavailable")
}

func (r *inFlightRequester) Close() error {
	r.mu.Lock()
	r.closedInFlight = r.closedInFlight || r.inFlight
	r.mu.Unlock()
	close(r.closed)
	return nil
}

func TestCancelledRequestNeverClosesASocketStillInUse(t *testing.T) {
	cases := map[string]func(context.Context, func() (repreq.RequesterReply, error)) error{
		"knowledge": func(ctx context.Context, f func() (repreq.RequesterReply, error)) error {
			_, err := NewKnowledgeServiceClientWithFactory(f).Status(ctx)
			return err
		},
		"knowledge write": func(ctx context.Context, f func() (repreq.RequesterReply, error)) error {
			_, err := NewKnowledgeServiceClientWithFactory(f).Dispatch(ctx, "knowledge.ingest", nil)
			return err
		},
		"feeds": func(ctx context.Context, f func() (repreq.RequesterReply, error)) error {
			_, err := NewFeedsServiceClientWithFactory(f).Status(ctx)
			return err
		},
		"meme": func(ctx context.Context, f func() (repreq.RequesterReply, error)) error {
			_, err := NewMemeServiceClientWithFactory(f).Status(ctx)
			return err
		},
		"shared JSON dispatch": func(ctx context.Context, f func() (repreq.RequesterReply, error)) error {
			_, err := dispatchJSON(ctx, "memory", f, "memory.status", nil)
			return err
		},
	}
	for name, call := range cases {
		t.Run(name, func(t *testing.T) {
			requester := newInFlightRequester()
			factory := func() (repreq.RequesterReply, error) { return requester, nil }
			ctx, cancel := context.WithCancel(context.Background())
			done := make(chan error, 1)
			go func() { done <- call(ctx, factory) }()
			<-requester.entered
			cancel()
			select {
			case err := <-done:
				if err == nil {
					t.Fatal("a cancelled request reported success")
				}
			case <-time.After(5 * time.Second):
				t.Fatal("the caller waited for the socket instead of returning on cancel")
			}
			close(requester.release)
			select {
			case <-requester.closed:
			case <-time.After(5 * time.Second):
				t.Fatal("the socket was never closed after its request returned")
			}
			requester.mu.Lock()
			defer requester.mu.Unlock()
			if requester.closedInFlight {
				t.Fatal("the socket was closed while another goroutine was still receiving on it")
			}
		})
	}
}
