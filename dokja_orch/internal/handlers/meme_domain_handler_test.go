package handlers

import (
	"context"
	"read_books/internal/clients"
	"read_books/internal/core"
	"read_books/internal/infrastructure/repreq"
	"testing"
)

type fakeMemeRequester struct {
	response string
	err      error
}

func (f *fakeMemeRequester) Request(string) (string, error) {
	if f.err != nil {
		return "", f.err
	}
	return f.response, nil
}

func (f *fakeMemeRequester) Close() error {
	return nil
}

func TestMemeDomainHandlerDelegatesToDomainService(t *testing.T) {
	client := clients.NewMemeServiceClientWithFactory(func() (repreq.RequesterReply, error) {
		return &fakeMemeRequester{
			response: `{"status":"ok","result":{"count":3}}`,
		}, nil
	})

	handler := NewMemeDomainHandler(client)

	result, err := handler.Handle(context.Background(), core.Event{
		EventID: "event-1",
		Source:  core.SourceOrchestrator,
		Type:    "meme.fetch",
		Payload: map[string]any{"limit": 3},
	}, core.WorkflowStep{
		Domain: core.DomainMeme,
		Action: "fetch-memes",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result["count"] != float64(3) {
		t.Fatalf("expected count 3, got %#v", result["count"])
	}
}
