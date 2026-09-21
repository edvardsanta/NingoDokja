package clients

import (
	"context"
	"errors"
	"read_books/internal/infrastructure/repreq"
	"strings"
	"testing"
)

type fakeKnowledgeRequester struct {
	request  string
	response string
	err      error
}

func (f *fakeKnowledgeRequester) Request(message string) (string, error) {
	f.request = message
	return f.response, f.err
}

func (f *fakeKnowledgeRequester) Close() error { return nil }

func knowledgeClient(requester *fakeKnowledgeRequester) *KnowledgeServiceClient {
	return NewKnowledgeServiceClientWithFactory(func() (repreq.RequesterReply, error) { return requester, nil })
}

func TestKnowledgeClientSendsTheEventAndReturnsTheResult(t *testing.T) {
	requester := &fakeKnowledgeRequester{response: `{"status":"ok","result":{"hits":[],"relevant_count":0}}`}

	result, err := knowledgeClient(requester).Search(context.Background(), "o que e estoicismo", 4)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result["relevant_count"] != float64(0) {
		t.Fatalf("unexpected result %#v", result)
	}
	for _, want := range []string{`"type":"knowledge.search"`, `"query":"o que e estoicismo"`, `"k":4`} {
		if !strings.Contains(requester.request, want) {
			t.Fatalf("request %q does not contain %s", requester.request, want)
		}
	}
}

func TestKnowledgeClientSurfacesServiceAndTransportErrors(t *testing.T) {
	_, err := knowledgeClient(&fakeKnowledgeRequester{response: `{"status":"error","message":"title is required"}`}).
		Dispatch(context.Background(), "knowledge.ingest", nil)
	if err == nil || err.Error() != "title is required" {
		t.Fatalf("expected the service message, got %v", err)
	}

	transport := errors.New("connection refused")
	if _, err := knowledgeClient(&fakeKnowledgeRequester{err: transport}).Dispatch(context.Background(), "knowledge.list", nil); !errors.Is(err, transport) {
		t.Fatalf("expected the transport error, got %v", err)
	}
	if _, err := knowledgeClient(&fakeKnowledgeRequester{response: "not json"}).Dispatch(context.Background(), "knowledge.list", nil); err == nil {
		t.Fatal("expected a decode error")
	}
	if _, err := (*KnowledgeServiceClient)(nil).Dispatch(context.Background(), "knowledge.list", nil); err == nil {
		t.Fatal("expected a nil client error")
	}
}

func TestKnowledgeClientHonoursContextCancellation(t *testing.T) {
	block := make(chan struct{})
	defer close(block)
	client := NewKnowledgeServiceClientWithFactory(func() (repreq.RequesterReply, error) {
		return blockingRequester{block}, nil
	})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := client.Dispatch(ctx, "knowledge.list", nil); !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context cancellation, got %v", err)
	}
}

type blockingRequester struct{ release chan struct{} }

func (b blockingRequester) Request(string) (string, error) {
	<-b.release
	return "", nil
}
func (blockingRequester) Close() error { return nil }

func TestKnowledgeEndpointRejectsBindAddresses(t *testing.T) {
	for _, endpoint := range []string{"tcp://*:5561", "tcp://0.0.0.0:5561", ""} {
		if err := validateKnowledgeServiceEndpoint(endpoint); err == nil {
			t.Fatalf("expected %q to be rejected", endpoint)
		}
	}
	if err := validateKnowledgeServiceEndpoint("tcp://dokja-knowledge:5561"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	t.Setenv("KNOWLEDGE_SERVICE_ENDPOINT", "")
	if got := resolveKnowledgeServiceEndpoint(""); got != defaultKnowledgeServiceEndpoint {
		t.Fatalf("expected the default endpoint, got %q", got)
	}
}
