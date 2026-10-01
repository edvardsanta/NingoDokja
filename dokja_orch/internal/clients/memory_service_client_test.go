package clients

import (
	"bytes"
	"context"
	"errors"
	"log"
	"read_books/internal/infrastructure/repreq"
	"read_books/internal/logger"
	"strings"
	"testing"
)

// captureLogs turns the orchestrator logger on for one test and returns what it writes.
func captureLogs(t *testing.T) *bytes.Buffer {
	t.Helper()
	var logs bytes.Buffer
	previous := log.Writer()
	log.SetOutput(&logs)
	logger.Init()
	t.Cleanup(func() {
		logger.InitWithOptions(false, "")
		log.SetOutput(previous)
	})
	return &logs
}

type fakeMemoryRequester struct {
	request  string
	response string
	err      error
}

func (f *fakeMemoryRequester) Request(message string) (string, error) {
	f.request = message
	return f.response, f.err
}

func (f *fakeMemoryRequester) Close() error { return nil }

func memoryClient(requester *fakeMemoryRequester) *MemoryServiceClient {
	return NewMemoryServiceClientWithFactory(func() (repreq.RequesterReply, error) { return requester, nil })
}

func TestMemoryClientSendsTheEventAndReturnsTheResult(t *testing.T) {
	requester := &fakeMemoryRequester{response: `{"status":"ok","result":{"created":true}}`}

	result, err := memoryClient(requester).Dispatch(context.Background(), "memory.record", map[string]any{"ref": "e1", "action": "tag.suggest"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result["created"] != true {
		t.Fatalf("unexpected result %#v", result)
	}
	for _, want := range []string{`"type":"memory.record"`, `"ref":"e1"`, `"action":"tag.suggest"`} {
		if !strings.Contains(requester.request, want) {
			t.Fatalf("request %q does not contain %s", requester.request, want)
		}
	}
}

func TestMemoryClientSurfacesServiceAndTransportErrors(t *testing.T) {
	_, err := memoryClient(&fakeMemoryRequester{response: `{"status":"error","message":"ref is required"}`}).
		Dispatch(context.Background(), "memory.resolve", nil)
	if err == nil || err.Error() != "ref is required" {
		t.Fatalf("expected the service message, got %v", err)
	}

	transport := errors.New("connection refused")
	if _, err := memoryClient(&fakeMemoryRequester{err: transport}).Dispatch(context.Background(), "memory.status", nil); !errors.Is(err, transport) {
		t.Fatalf("expected the transport error, got %v", err)
	}
	if _, err := memoryClient(&fakeMemoryRequester{response: "not json"}).Dispatch(context.Background(), "memory.status", nil); err == nil {
		t.Fatal("expected a decode error")
	}
	if _, err := memoryClient(&fakeMemoryRequester{response: `{"status":"error"}`}).Dispatch(context.Background(), "memory.status", nil); err == nil ||
		!strings.Contains(err.Error(), "non-ok") {
		t.Fatalf("expected a generic non-ok error, got %v", err)
	}
	if _, err := (*MemoryServiceClient)(nil).Dispatch(context.Background(), "memory.status", nil); err == nil {
		t.Fatal("expected a nil client error")
	}
	failing := NewMemoryServiceClientWithFactory(func() (repreq.RequesterReply, error) { return nil, errors.New("no socket") })
	if _, err := failing.Dispatch(context.Background(), "memory.status", nil); err == nil || !strings.Contains(err.Error(), "no socket") {
		t.Fatalf("expected the factory error, got %v", err)
	}
}

func TestMemoryClientHonoursContextCancellation(t *testing.T) {
	block := make(chan struct{})
	defer close(block)
	client := NewMemoryServiceClientWithFactory(func() (repreq.RequesterReply, error) {
		return blockingRequester{block}, nil
	})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := client.Dispatch(ctx, "memory.status", nil); !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context cancellation, got %v", err)
	}
}

func TestMemoryClientNeverLogsThePayload(t *testing.T) {
	logs := captureLogs(t)

	requester := &fakeMemoryRequester{response: `{"status":"ok","result":{}}`}
	if _, err := memoryClient(requester).Dispatch(context.Background(), "memory.record", map[string]any{"context": "private words"}); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(logs.String(), "private") {
		t.Fatalf("a payload is the user's own text and must not be logged: %q", logs.String())
	}
	if !strings.Contains(logs.String(), "type=memory.record") {
		t.Fatalf("the log should still say what was sent: %q", logs.String())
	}
}

func TestMemoryEndpointRejectsBindAddresses(t *testing.T) {
	for _, endpoint := range []string{"tcp://*:5562", "tcp://0.0.0.0:5562", ""} {
		if err := validateMemoryServiceEndpoint(endpoint); err == nil {
			t.Fatalf("expected %q to be rejected", endpoint)
		}
	}
	if err := validateMemoryServiceEndpoint("tcp://dokja-memory:5562"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	t.Setenv("MEMORY_SERVICE_ENDPOINT", "")
	if got := resolveMemoryServiceEndpoint(""); got != defaultMemoryServiceEndpoint {
		t.Fatalf("expected the default endpoint, got %q", got)
	}
	t.Setenv("MEMORY_SERVICE_ENDPOINT", "tcp://elsewhere:6000")
	if got := resolveMemoryServiceEndpoint(""); got != "tcp://elsewhere:6000" {
		t.Fatalf("expected the environment endpoint, got %q", got)
	}
	if got := resolveMemoryServiceEndpoint("tcp://explicit:1"); got != "tcp://explicit:1" {
		t.Fatalf("an explicit endpoint wins, got %q", got)
	}
}
