package clients

import (
	"context"
	"errors"
	"read_books/internal/infrastructure/repreq"
	"strings"
	"testing"
)

type fakeMemeRequester struct {
	requestBody string
	response    string
	err         error
}

func (f *fakeMemeRequester) Request(message string) (string, error) {
	f.requestBody = message
	if f.err != nil {
		return "", f.err
	}
	return f.response, nil
}

func (f *fakeMemeRequester) Close() error {
	return nil
}

func TestMemeServiceClientDispatch(t *testing.T) {
	requester := &fakeMemeRequester{
		response: `{"status":"ok","result":{"count":1}}`,
	}

	client := NewMemeServiceClientWithFactory(func() (repreq.RequesterReply, error) {
		return requester, nil
	})

	response, err := client.Fetch(context.Background(), intPtr(1))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if response["count"] != float64(1) {
		t.Fatalf("expected count 1, got %#v", response["count"])
	}
	if !strings.Contains(requester.requestBody, `"type":"meme.fetch"`) {
		t.Fatalf("expected request body to include event type, got %q", requester.requestBody)
	}
}

func TestMemeServiceClientDispatchErrors(t *testing.T) {
	if _, err := (*MemeServiceClient)(nil).Fetch(context.Background(), nil); err == nil {
		t.Fatal("expected nil client error")
	}

	factoryErr := errors.New("factory")
	client := NewMemeServiceClientWithFactory(func() (repreq.RequesterReply, error) {
		return nil, factoryErr
	})
	if _, err := client.Status(context.Background()); !errors.Is(err, factoryErr) {
		t.Fatalf("expected factory error, got %v", err)
	}

	requestErr := errors.New("request")
	client = NewMemeServiceClientWithFactory(func() (repreq.RequesterReply, error) {
		return &fakeMemeRequester{err: requestErr}, nil
	})
	if _, err := client.Status(context.Background()); !errors.Is(err, requestErr) {
		t.Fatalf("expected request error, got %v", err)
	}

	client = NewMemeServiceClientWithFactory(func() (repreq.RequesterReply, error) {
		return &fakeMemeRequester{response: "not-json"}, nil
	})
	if _, err := client.Status(context.Background()); err == nil {
		t.Fatal("expected unmarshal error")
	}

	client = NewMemeServiceClientWithFactory(func() (repreq.RequesterReply, error) {
		return &fakeMemeRequester{response: `{"status":"error","message":"boom"}`}, nil
	})
	if _, err := client.Status(context.Background()); err == nil || err.Error() != "boom" {
		t.Fatalf("expected boom error, got %v", err)
	}
}

func TestValidateMemeServiceEndpoint(t *testing.T) {
	if err := validateMemeServiceEndpoint("tcp://dokja-meme:5557"); err != nil {
		t.Fatalf("expected routable endpoint to be valid, got %v", err)
	}

	for _, endpoint := range []string{"tcp://*:5557", "tcp://0.0.0.0:5557"} {
		err := validateMemeServiceEndpoint(endpoint)
		if err == nil {
			t.Fatalf("expected %q to be rejected", endpoint)
		}
		if !strings.Contains(err.Error(), "bind address") {
			t.Fatalf("expected bind address guidance for %q, got %v", endpoint, err)
		}
	}
}

func intPtr(value int) *int {
	return &value
}
