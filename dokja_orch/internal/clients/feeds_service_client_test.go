package clients

import (
	"context"
	"errors"
	"read_books/internal/infrastructure/repreq"
	"strings"
	"testing"
)

type fakeFeedsRequester struct {
	request  string
	response string
	err      error
}

func (f *fakeFeedsRequester) Request(message string) (string, error) {
	f.request = message
	return f.response, f.err
}

func (f *fakeFeedsRequester) Close() error { return nil }

func feedsClient(requester *fakeFeedsRequester) *FeedsServiceClient {
	return NewFeedsServiceClientWithFactory(func() (repreq.RequesterReply, error) { return requester, nil })
}

func TestFeedsClientSendsTheEventAndReturnsTheResult(t *testing.T) {
	requester := &fakeFeedsRequester{response: `{"status":"ok","result":{"items":[],"total":0}}`}

	result, err := feedsClient(requester).Dispatch(context.Background(), "feeds.items", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result["total"] != float64(0) {
		t.Fatalf("unexpected result %#v", result)
	}
	if !strings.Contains(requester.request, `"type":"feeds.items"`) {
		t.Fatalf("request %q does not name the event", requester.request)
	}
}

func TestFeedsClientSurfacesServiceAndTransportErrors(t *testing.T) {
	_, err := feedsClient(&fakeFeedsRequester{response: `{"status":"error","message":"unsupported feeds event type"}`}).
		Dispatch(context.Background(), "feeds.purge", nil)
	if err == nil || err.Error() != "unsupported feeds event type" {
		t.Fatalf("expected the service message, got %v", err)
	}

	transport := errors.New("connection refused")
	if _, err := feedsClient(&fakeFeedsRequester{err: transport}).Dispatch(context.Background(), "feeds.list", nil); !errors.Is(err, transport) {
		t.Fatalf("expected the transport error, got %v", err)
	}
	if _, err := feedsClient(&fakeFeedsRequester{response: "not json"}).Dispatch(context.Background(), "feeds.list", nil); err == nil {
		t.Fatal("expected a decode error")
	}
	if _, err := (*FeedsServiceClient)(nil).Dispatch(context.Background(), "feeds.list", nil); err == nil {
		t.Fatal("expected a nil client error")
	}
}

func TestFeedsClientHonoursContextCancellation(t *testing.T) {
	block := make(chan struct{})
	defer close(block)
	client := NewFeedsServiceClientWithFactory(func() (repreq.RequesterReply, error) {
		return blockingRequester{block}, nil
	})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := client.Dispatch(ctx, "feeds.list", nil); !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context cancellation, got %v", err)
	}
}

func TestFeedsEndpointRejectsBindAddresses(t *testing.T) {
	for _, endpoint := range []string{"tcp://*:5563", "tcp://0.0.0.0:5563", ""} {
		if err := validateFeedsServiceEndpoint(endpoint); err == nil {
			t.Fatalf("expected %q to be rejected", endpoint)
		}
	}
	if err := validateFeedsServiceEndpoint("tcp://dokja-feeds:5563"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	t.Setenv("FEEDS_SERVICE_ENDPOINT", "")
	if got := resolveFeedsServiceEndpoint(""); got != defaultFeedsServiceEndpoint {
		t.Fatalf("expected the default endpoint, got %q", got)
	}
}

func TestFeedsClientStatusAsksForFeedsStatus(t *testing.T) {
	requester := &fakeFeedsRequester{response: `{"status":"ok","result":{"enabled":1,"failed":0}}`}

	result, err := feedsClient(requester).Status(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result["enabled"] != float64(1) || !strings.Contains(requester.request, `"type":"feeds.status"`) {
		t.Fatalf("unexpected result %#v for request %s", result, requester.request)
	}
}
