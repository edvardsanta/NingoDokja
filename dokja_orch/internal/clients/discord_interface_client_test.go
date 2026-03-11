package clients

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func TestDiscordInterfaceClientDeliverBuildsContractPayload(t *testing.T) {
	var requestBody string
	client := &DiscordInterfaceClient{
		endpoint: "http://discord-interface.test/deliver",
		httpClient: &http.Client{
			Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
				body, err := io.ReadAll(req.Body)
				if err != nil {
					t.Fatalf("unexpected read body error: %v", err)
				}
				requestBody = string(body)
				return &http.Response{
					StatusCode: http.StatusOK,
					Body:       io.NopCloser(strings.NewReader(`{"status":"ok","channel_id":"c1","message_id":"m1","has_attachment":true}`)),
					Header:     make(http.Header),
				}, nil
			}),
		},
	}

	if err := client.Deliver(context.Background(), "c1", "hello", "https://example.com/meme.jpg"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	expectedSnippets := []string{
		`"channel_id":"c1"`,
		`"content":"hello"`,
		`"attachment_url":"https://example.com/meme.jpg"`,
	}
	for _, snippet := range expectedSnippets {
		if !strings.Contains(requestBody, snippet) {
			t.Fatalf("expected request body to contain %s, got %s", snippet, requestBody)
		}
	}
}

func TestDiscordInterfaceClientDeliverRejectsEmptyPayload(t *testing.T) {
	client := NewDiscordInterfaceClient("")

	if err := client.Deliver(context.Background(), "", "", ""); err == nil {
		t.Fatal("expected validation error")
	}
}

func TestDiscordInterfaceClientDeliverPropagatesContractErrorMessage(t *testing.T) {
	client := &DiscordInterfaceClient{
		endpoint: "http://discord-interface.test/deliver",
		httpClient: &http.Client{
			Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
				return &http.Response{
					StatusCode: http.StatusBadGateway,
					Body:       io.NopCloser(strings.NewReader(`{"status":"error","message":"discord returned 429"}`)),
					Header:     make(http.Header),
				}, nil
			}),
		},
	}

	err := client.Deliver(context.Background(), "c1", "hello", "")
	if err == nil || !strings.Contains(err.Error(), "discord returned 429") {
		t.Fatalf("expected propagated error message, got %v", err)
	}
}
