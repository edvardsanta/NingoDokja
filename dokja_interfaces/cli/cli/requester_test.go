package cli

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/pebbe/zmq4"
)

// A request the orchestrator never answers, cancelled by the person (Stop in the TUI, Ctrl+C in the
// CLI). The caller then closes the socket. When the receive ran on a goroutine of its own, that close
// hit a socket another thread was still receiving on, and libzmq aborted the whole process.
func TestCancelledRequestReturnsAndClosesSafely(t *testing.T) {
	silent, err := zmq4.NewSocket(zmq4.REP)
	if err != nil {
		t.Fatal(err)
	}
	defer silent.Close()
	if err := silent.Bind("tcp://127.0.0.1:*"); err != nil {
		t.Fatal(err)
	}
	endpoint, err := silent.GetLastEndpoint()
	if err != nil {
		t.Fatal(err)
	}
	for round := 0; round < 20; round++ {
		requester, err := NewRequester(endpoint)
		if err != nil {
			t.Fatal(err)
		}
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
		started := time.Now()
		_, err = requester.Request(ctx, Event{Type: "ningo.status"})
		cancel()
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("round %d: cancelled request returned %v", round, err)
		}
		if waited := time.Since(started); waited > 2*time.Second {
			t.Fatalf("round %d: cancellation took %s", round, waited)
		}
		if err := requester.Close(); err != nil {
			t.Fatalf("round %d: close: %v", round, err)
		}
		// Drain the request the silent peer received so the next round's request is read too.
		if _, err := silent.Recv(0); err != nil {
			t.Fatal(err)
		}
		if _, err := silent.Send("", 0); err != nil {
			t.Fatal(err)
		}
	}
}
