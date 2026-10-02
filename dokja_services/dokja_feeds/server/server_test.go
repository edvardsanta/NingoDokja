package server

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/pebbe/zmq4"
)

type stubHandler struct {
	result  map[string]any
	err     error
	panics  bool
	gotType string
	gotBody map[string]any
}

func (s *stubHandler) Dispatch(_ context.Context, eventType string, payload map[string]any) (map[string]any, error) {
	if s.panics {
		panic("boom")
	}
	s.gotType, s.gotBody = eventType, payload
	return s.result, s.err
}

func decode(t *testing.T, raw []byte) map[string]any {
	t.Helper()
	var out map[string]any
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("reply is not JSON: %s", raw)
	}
	return out
}

func TestHandleRequestAnswersWithTheServiceEnvelope(t *testing.T) {
	handler := &stubHandler{result: map[string]any{"created": true}}
	var logs bytes.Buffer

	reply := decode(t, HandleRequest(context.Background(),
		`{"type":"memory.record","payload":{"context":"a private message","ref":"r1"}}`, handler, log.New(&logs, "", 0)))

	if reply["status"] != "ok" || reply["result"].(map[string]any)["created"] != true {
		t.Fatalf("reply=%v", reply)
	}
	if handler.gotType != "memory.record" || handler.gotBody["ref"] != "r1" {
		t.Fatalf("the handler got %q %v", handler.gotType, handler.gotBody)
	}
	if strings.Contains(logs.String(), "private") || strings.Contains(logs.String(), "r1") {
		t.Fatalf("a payload is the user's own text and must never be logged: %q", logs.String())
	}
	if !strings.Contains(logs.String(), "type=memory.record") || !strings.Contains(logs.String(), "payload_keys=2") {
		t.Fatalf("the log should still say what was handled: %q", logs.String())
	}
}

func TestHandleRequestAnswersEveryFailureWithAnErrorEnvelope(t *testing.T) {
	logger := log.New(&bytes.Buffer{}, "", 0)
	cases := map[string]struct {
		raw     string
		handler *stubHandler
		message string
	}{
		"invalid JSON":      {"{nope", &stubHandler{}, "valid JSON"},
		"a missing type":    {`{"payload":{}}`, &stubHandler{}, "'type'"},
		"a service failure": {`{"type":"memory.status"}`, &stubHandler{err: errors.New("disk full")}, "disk full"},
		"a panic":           {`{"type":"memory.status"}`, &stubHandler{panics: true}, "internal error"},
	}
	for name, tc := range cases {
		reply := decode(t, HandleRequest(context.Background(), tc.raw, tc.handler, logger))
		if reply["status"] != "error" || !strings.Contains(reply["message"].(string), tc.message) {
			t.Fatalf("%s: reply=%v", name, reply)
		}
	}
}

func TestHandleRequestAnswersAnEmptyResultAsAnObject(t *testing.T) {
	reply := decode(t, HandleRequest(context.Background(), `{"type":"memory.forget"}`, &stubHandler{}, log.New(&bytes.Buffer{}, "", 0)))
	if reply["status"] != "ok" || reply["result"] == nil {
		t.Fatalf("a client reads result as an object, even when it is empty: %v", reply)
	}
}

func freeEndpoint(t *testing.T) string {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	return fmt.Sprintf("tcp://127.0.0.1:%d", listener.Addr().(*net.TCPAddr).Port)
}

func TestServeAnswersARealRequesterAndStopsWithItsContext(t *testing.T) {
	endpoint := freeEndpoint(t)
	handler := &stubHandler{result: map[string]any{"pong": true}}
	ctx, cancel := context.WithCancel(context.Background())
	stopped := make(chan error, 1)
	go func() { stopped <- Serve(ctx, endpoint, handler, log.New(&bytes.Buffer{}, "", 0)) }()

	requester, err := zmq4.NewSocket(zmq4.REQ)
	if err != nil {
		t.Fatal(err)
	}
	defer requester.Close()
	requester.SetRcvtimeo(5 * time.Second)
	requester.SetSndtimeo(5 * time.Second)
	if err := requester.Connect(endpoint); err != nil {
		t.Fatal(err)
	}
	if _, err := requester.Send(`{"type":"memory.status","payload":{}}`, 0); err != nil {
		t.Fatal(err)
	}
	raw, err := requester.Recv(0)
	if err != nil {
		t.Fatalf("no reply: %v", err)
	}
	if reply := decode(t, []byte(raw)); reply["status"] != "ok" || reply["result"].(map[string]any)["pong"] != true {
		t.Fatalf("reply=%v", reply)
	}

	cancel()
	select {
	case err := <-stopped:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("Serve ended with %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Serve must stop when its context ends")
	}
}
