package handlers

import (
	"context"
	"errors"
	"read_books/internal/core"
	"testing"
)

type fakeMemeFetcher struct {
	limit  *int
	result map[string]any
	err    error
}

func (f *fakeMemeFetcher) Fetch(_ context.Context, limit *int) (map[string]any, error) {
	f.limit = limit
	return f.result, f.err
}

type deliveredMessage struct {
	channelID     string
	content       string
	attachmentURL string
}

type fakeDiscordDeliverer struct {
	deliveries []deliveredMessage
	err        error
}

func (f *fakeDiscordDeliverer) Deliver(_ context.Context, channelID, content, attachmentURL string) error {
	if f.err != nil {
		return f.err
	}

	f.deliveries = append(f.deliveries, deliveredMessage{
		channelID:     channelID,
		content:       content,
		attachmentURL: attachmentURL,
	})
	return nil
}

func TestSystemDomainHandlerScheduledDispatchDeliversFetchedMemes(t *testing.T) {
	fetcher := &fakeMemeFetcher{
		result: map[string]any{
			"memes": []any{
				map[string]any{"title": "First meme", "url": "https://example.com/first.jpg"},
				map[string]any{"url": "https://example.com/second.jpg"},
			},
		},
	}
	deliverer := &fakeDiscordDeliverer{}
	handler := NewSystemDomainHandler(nil, nil, fetcher, deliverer, "discord-channel")

	result, err := handler.Handle(context.Background(), core.Event{
		Type:    "meme.dispatch.scheduled",
		Payload: map[string]any{"limit": 2},
	}, core.WorkflowStep{
		Domain: core.DomainSystem,
		Action: "deliver-scheduled-memes",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if fetcher.limit == nil || *fetcher.limit != 2 {
		t.Fatalf("expected fetch limit 2, got %#v", fetcher.limit)
	}
	if len(deliverer.deliveries) != 2 {
		t.Fatalf("expected 2 deliveries, got %d", len(deliverer.deliveries))
	}
	if deliverer.deliveries[0].channelID != "discord-channel" {
		t.Fatalf("expected delivery channel discord-channel, got %q", deliverer.deliveries[0].channelID)
	}
	if deliverer.deliveries[0].content != "First meme" {
		t.Fatalf("expected first delivery content to be title, got %q", deliverer.deliveries[0].content)
	}
	if deliverer.deliveries[0].attachmentURL != "https://example.com/first.jpg" {
		t.Fatalf("expected first attachment URL, got %q", deliverer.deliveries[0].attachmentURL)
	}
	if deliverer.deliveries[1].content != "scheduled meme" {
		t.Fatalf("expected fallback content for url-only meme, got %q", deliverer.deliveries[1].content)
	}
	if deliverer.deliveries[1].attachmentURL != "https://example.com/second.jpg" {
		t.Fatalf("expected second attachment URL, got %q", deliverer.deliveries[1].attachmentURL)
	}
	if result["delivered_count"] != 2 {
		t.Fatalf("expected delivered_count 2, got %#v", result["delivered_count"])
	}
}

func TestSystemDomainHandlerScheduledDispatchSkipsEmptyMemeEntries(t *testing.T) {
	fetcher := &fakeMemeFetcher{
		result: map[string]any{
			"memes": []any{
				map[string]any{},
				map[string]any{"title": "Valid meme"},
			},
		},
	}
	deliverer := &fakeDiscordDeliverer{}
	handler := NewSystemDomainHandler(nil, nil, fetcher, deliverer, "discord-channel")

	result, err := handler.Handle(context.Background(), core.Event{
		Type:    "meme.dispatch.scheduled",
		Payload: map[string]any{},
	}, core.WorkflowStep{
		Domain: core.DomainSystem,
		Action: "deliver-scheduled-memes",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(deliverer.deliveries) != 1 {
		t.Fatalf("expected 1 delivery, got %d", len(deliverer.deliveries))
	}
	if result["delivered_count"] != 1 {
		t.Fatalf("expected delivered_count 1, got %#v", result["delivered_count"])
	}
}

func TestSystemDomainHandlerScheduledDispatchPropagatesDeliveryFailure(t *testing.T) {
	expected := errors.New("discord is down")
	handler := NewSystemDomainHandler(
		nil,
		nil,
		&fakeMemeFetcher{
			result: map[string]any{
				"memes": []any{
					map[string]any{"title": "First meme", "url": "https://example.com/first.jpg"},
				},
			},
		},
		&fakeDiscordDeliverer{err: expected},
		"discord-channel",
	)

	_, err := handler.Handle(context.Background(), core.Event{
		Type:    "meme.dispatch.scheduled",
		Payload: map[string]any{},
	}, core.WorkflowStep{
		Domain: core.DomainSystem,
		Action: "deliver-scheduled-memes",
	})
	if !errors.Is(err, expected) {
		t.Fatalf("expected delivery error to propagate, got %v", err)
	}
}

func TestSystemDomainHandlerScheduledDispatchValidation(t *testing.T) {
	if _, err := (*SystemDomainHandler)(nil).Handle(context.Background(), core.Event{}, core.WorkflowStep{}); err == nil {
		t.Fatal("expected nil handler error")
	}

	handler := NewSystemDomainHandler(nil, nil, nil, nil, "")
	if _, err := handler.Handle(context.Background(), core.Event{
		Type:    "meme.dispatch.scheduled",
		Payload: map[string]any{},
	}, core.WorkflowStep{
		Domain: core.DomainSystem,
		Action: "deliver-scheduled-memes",
	}); err == nil {
		t.Fatal("expected missing meme fetch client error")
	}

	handler = NewSystemDomainHandler(nil, nil, &fakeMemeFetcher{}, nil, "")
	if _, err := handler.Handle(context.Background(), core.Event{
		Type:    "meme.dispatch.scheduled",
		Payload: map[string]any{},
	}, core.WorkflowStep{
		Domain: core.DomainSystem,
		Action: "deliver-scheduled-memes",
	}); err == nil {
		t.Fatal("expected missing discord deliverer error")
	}

	handler = NewSystemDomainHandler(nil, nil, &fakeMemeFetcher{}, &fakeDiscordDeliverer{}, "")
	if _, err := handler.Handle(context.Background(), core.Event{
		Type:    "meme.dispatch.scheduled",
		Payload: map[string]any{},
	}, core.WorkflowStep{
		Domain: core.DomainSystem,
		Action: "deliver-scheduled-memes",
	}); err == nil {
		t.Fatal("expected missing delivery channel error")
	}
}
