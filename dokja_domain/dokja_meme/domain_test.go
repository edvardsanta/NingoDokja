package meme

import (
	"context"
	"errors"
	"testing"
)

type fakeService struct {
	fetchLimit    *int
	refreshMax    int
	fetchResult   map[string]any
	refreshResult map[string]any
	statusResult  map[string]any
	err           error
}

func (f *fakeService) Fetch(_ context.Context, limit *int) (map[string]any, error) {
	f.fetchLimit = limit
	return f.fetchResult, f.err
}

func (f *fakeService) RefreshPool(_ context.Context, maxItemsPerScraper int) (map[string]any, error) {
	f.refreshMax = maxItemsPerScraper
	return f.refreshResult, f.err
}

func (f *fakeService) Status(_ context.Context) (map[string]any, error) {
	return f.statusResult, f.err
}

func TestHandleFetchesMemes(t *testing.T) {
	svc := &fakeService{fetchResult: map[string]any{"count": 2}}
	domain := New(svc)

	result, err := domain.Handle(context.Background(), Request{
		Action: ActionFetchMemes,
		Event: Event{
			Type:    "meme.fetch",
			Payload: map[string]any{"limit": 3.0},
		},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result["count"] != 2 {
		t.Fatalf("expected count 2, got %#v", result["count"])
	}
	if svc.fetchLimit == nil || *svc.fetchLimit != 3 {
		t.Fatalf("expected fetch limit 3, got %#v", svc.fetchLimit)
	}
}

func TestHandleRefreshesPoolWithDefault(t *testing.T) {
	svc := &fakeService{refreshResult: map[string]any{"status": "refreshed"}}
	domain := New(svc)

	result, err := domain.Handle(context.Background(), Request{
		Action: ActionRefreshMemePool,
		Event:  Event{Type: "meme.pool.refresh", Payload: map[string]any{}},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result["status"] != "refreshed" {
		t.Fatalf("expected refreshed status, got %#v", result["status"])
	}
	if svc.refreshMax != 20 {
		t.Fatalf("expected default refresh max 20, got %d", svc.refreshMax)
	}
}

func TestHandleReadsStatus(t *testing.T) {
	svc := &fakeService{statusResult: map[string]any{"unsent_count": 7}}
	domain := New(svc)

	result, err := domain.Handle(context.Background(), Request{
		Action: ActionInspectMemeStatus,
		Event:  Event{Type: "meme.status"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result["unsent_count"] != 7 {
		t.Fatalf("expected unsent count 7, got %#v", result["unsent_count"])
	}
}

func TestHandleValidation(t *testing.T) {
	if _, err := (*Domain)(nil).Handle(context.Background(), Request{}); err == nil {
		t.Fatal("expected nil domain error")
	}

	domain := New(nil)
	if _, err := domain.Handle(context.Background(), Request{}); err == nil {
		t.Fatal("expected missing service error")
	}

	domain = New(&fakeService{})
	if _, err := domain.Handle(context.Background(), Request{Action: "unknown", Event: Event{Type: "meme.unknown"}}); err == nil {
		t.Fatal("expected unsupported action error")
	}

	svcErr := errors.New("boom")
	domain = New(&fakeService{err: svcErr})
	if _, err := domain.Handle(context.Background(), Request{Action: ActionInspectMemeStatus, Event: Event{Type: "meme.status"}}); !errors.Is(err, svcErr) {
		t.Fatalf("expected propagated service error, got %v", err)
	}
}
