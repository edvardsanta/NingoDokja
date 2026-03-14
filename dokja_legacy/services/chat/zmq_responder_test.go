package chat

import (
	"context"
	"errors"
	"read_books/internal/legacy/infrastructure/repreq"
	"strings"
	"testing"
)

type fakeRequester struct {
	requestBody string
	response    string
	err         error
}

func (f *fakeRequester) Request(message string) (string, error) {
	f.requestBody = message
	if f.err != nil {
		return "", f.err
	}
	return f.response, nil
}

func (f *fakeRequester) Close() error {
	return nil
}

func TestZMQResponderRespond(t *testing.T) {
	requester := &fakeRequester{response: `{"status":"ok","result":"pong"}`}
	responder := NewZMQResponderWithFactory(func() (repreq.RequesterReply, error) {
		return nil, nil
	})
	_ = responder

	responder = NewZMQResponderWithFactory(func() (repreq.RequesterReply, error) {
		return requester, nil
	})

	response, err := responder.Respond(context.Background(), "oi", "u1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if response != "pong" {
		t.Fatalf("expected pong, got %q", response)
	}

	if !strings.Contains(requester.requestBody, `"event_type":"chat_message"`) {
		t.Fatalf("request payload should contain event type, got %q", requester.requestBody)
	}
	if !strings.Contains(requester.requestBody, `"content":"oi"`) {
		t.Fatalf("request payload should contain content, got %q", requester.requestBody)
	}
	if !strings.Contains(requester.requestBody, `"userId":"u1"`) {
		t.Fatalf("request payload should contain user id, got %q", requester.requestBody)
	}
}

func TestZMQResponderRespondErrors(t *testing.T) {
	responder := NewZMQResponderWithFactory(nil)
	_, err := responder.Respond(context.Background(), "oi", "u1")
	if err == nil {
		t.Fatal("expected error when factory is nil")
	}

	factoryErr := errors.New("factory")
	responder = NewZMQResponderWithFactory(func() (repreq.RequesterReply, error) {
		return nil, factoryErr
	})
	_, err = responder.Respond(context.Background(), "oi", "u1")
	if !errors.Is(err, factoryErr) {
		t.Fatalf("expected factory error, got %v", err)
	}

	requestErr := errors.New("request")
	responder = NewZMQResponderWithFactory(func() (repreq.RequesterReply, error) {
		return &fakeRequester{err: requestErr}, nil
	})
	_, err = responder.Respond(context.Background(), "oi", "u1")
	if !errors.Is(err, requestErr) {
		t.Fatalf("expected request error, got %v", err)
	}

	responder = NewZMQResponderWithFactory(func() (repreq.RequesterReply, error) {
		return &fakeRequester{response: "not-json"}, nil
	})
	_, err = responder.Respond(context.Background(), "oi", "u1")
	if err == nil {
		t.Fatal("expected unmarshal error")
	}

	responder = NewZMQResponderWithFactory(func() (repreq.RequesterReply, error) {
		return &fakeRequester{response: `{"status":"error","result":"x"}`}, nil
	})
	_, err = responder.Respond(context.Background(), "oi", "u1")
	if err == nil {
		t.Fatal("expected non-ok status error")
	}
}

func TestZMQResponderRespondContextCancelled(t *testing.T) {
	requester := &fakeRequester{response: `{"status":"ok","result":"pong"}`}
	responder := NewZMQResponderWithFactory(func() (repreq.RequesterReply, error) {
		return requester, nil
	})

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := responder.Respond(ctx, "oi", "u1")
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context canceled, got %v", err)
	}
}
