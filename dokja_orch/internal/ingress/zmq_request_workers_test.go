package ingress

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"testing"
	"time"

	"github.com/pebbe/zmq4"

	"read_books/internal/core"
)

// slowSystem answers ningo.status at once, unless the request asks to wait; then it waits until
// released, as a chat turn holds a request for a long time.
type slowSystem struct{ release chan struct{} }

func (s *slowSystem) Domain() core.Domain { return core.DomainSystem }
func (s *slowSystem) Handle(_ context.Context, event core.Event, _ core.WorkflowStep) (map[string]any, error) {
	if wait, _ := event.Payload["wait"].(bool); wait {
		<-s.release
		return map[string]any{"reply": "slow"}, nil
	}
	return map[string]any{"reply": "fast"}, nil
}

func freeEndpoint(t *testing.T) (bind, connect string) {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	listener.Close()
	return fmt.Sprintf("tcp://127.0.0.1:%d", port), fmt.Sprintf("tcp://127.0.0.1:%d", port)
}

// ask sends one request on its own REQ socket, as the desktop does, and returns the reply text.
func ask(t *testing.T, endpoint string, payload map[string]any, timeout time.Duration) (string, error) {
	t.Helper()
	socket, err := zmq4.NewSocket(zmq4.REQ)
	if err != nil {
		return "", err
	}
	defer socket.Close()
	socket.SetLinger(0)
	socket.SetRcvtimeo(timeout)
	if err := socket.Connect(endpoint); err != nil {
		return "", err
	}
	body, _ := json.Marshal(core.Event{Type: "ningo.status", Source: core.SourceCLI, Payload: payload})
	if _, err := socket.Send(string(body), 0); err != nil {
		return "", err
	}
	raw, err := socket.Recv(0)
	if err != nil {
		return "", err
	}
	var response struct {
		Status string `json:"status"`
		Result struct {
			Result map[string]any `json:"result"`
		} `json:"result"`
	}
	if err := json.Unmarshal([]byte(raw), &response); err != nil {
		return "", err
	}
	reply, _ := response.Result.Result["reply"].(string)
	return reply, nil
}

// Regression: the desktop's Library timed out ("the orchestrator took too long") while a chat
// turn was running, because the request socket answered one request at a time.
func TestASlowRequestDoesNotHoldTheOthers(t *testing.T) {
	system := &slowSystem{release: make(chan struct{})}
	bind, endpoint := freeEndpoint(t)
	ingress := NewZeroMQRequestIngress(core.DefaultService(system), bind)
	if err := ingress.Start(); err != nil {
		t.Fatal(err)
	}
	defer ingress.Stop()

	slow := make(chan string, 1)
	go func() {
		reply, _ := ask(t, endpoint, map[string]any{"wait": true}, 10*time.Second)
		slow <- reply
	}()
	time.Sleep(200 * time.Millisecond) // the slow request is being handled
	for n := 0; n < 3; n++ {
		reply, err := ask(t, endpoint, nil, 2*time.Second)
		if err != nil || reply != "fast" {
			t.Fatalf("a quick request waited behind a slow one: reply=%q err=%v", reply, err)
		}
	}
	select {
	case <-slow:
		t.Fatal("the slow request must still be running")
	default:
	}
	close(system.release)
	if reply := <-slow; reply != "slow" {
		t.Fatalf("the slow request lost its reply: %q", reply)
	}
}

func TestTheIngressStopsWithRequestsStillRunning(t *testing.T) {
	system := &slowSystem{release: make(chan struct{})}
	defer close(system.release)
	bind, endpoint := freeEndpoint(t)
	ingress := NewZeroMQRequestIngress(core.DefaultService(system), bind)
	if err := ingress.Start(); err != nil {
		t.Fatal(err)
	}
	if reply, err := ask(t, endpoint, nil, 2*time.Second); err != nil || reply != "fast" {
		t.Fatal(reply, err)
	}
	stopped := make(chan error, 1)
	go func() { stopped <- ingress.Stop() }()
	select {
	case <-stopped:
	case <-time.After(5 * time.Second):
		t.Fatal("stopping hung")
	}
}
