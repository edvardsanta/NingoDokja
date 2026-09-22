package meme

import (
	"context"
	"errors"
	"testing"
)

type fakeService struct {
	fetchLimit         *int
	refreshMax         int
	fetchResult        map[string]any
	refreshResult      map[string]any
	statusResult       map[string]any
	screenResult       map[string]any
	screenURL          string
	screenCaption      string
	listArgs           []any
	listResult         map[string]any
	tagArgs            []any
	tagResult          map[string]any
	suggestArgs        []any
	suggestResult      map[string]any
	listHashtagsArgs   []any
	listHashtagsResult map[string]any
	untagURL           string
	untagResult        map[string]any
	err                error
}

func (f *fakeService) Fetch(_ context.Context, limit *int) (map[string]any, error) {
	f.fetchLimit = limit
	return f.fetchResult, f.err
}

func (f *fakeService) Screen(_ context.Context, url, caption string) (map[string]any, error) {
	f.screenURL = url
	f.screenCaption = caption
	return f.screenResult, f.err
}

func (f *fakeService) List(_ context.Context, scope string, limit, offset int) (map[string]any, error) {
	f.listArgs = []any{scope, limit, offset}
	return f.listResult, f.err
}

func (f *fakeService) RefreshPool(_ context.Context, maxItemsPerScraper int) (map[string]any, error) {
	f.refreshMax = maxItemsPerScraper
	return f.refreshResult, f.err
}

func (f *fakeService) Status(_ context.Context) (map[string]any, error) {
	return f.statusResult, f.err
}

func (f *fakeService) TagHashtag(_ context.Context, url, hashtag, text string) (map[string]any, error) {
	f.tagArgs = []any{url, hashtag, text}
	return f.tagResult, f.err
}

func (f *fakeService) SuggestHashtag(_ context.Context, url, text string, minScore *float64) (map[string]any, error) {
	f.suggestArgs = []any{url, text, minScore}
	return f.suggestResult, f.err
}

func (f *fakeService) ListHashtags(_ context.Context, limit, offset int) (map[string]any, error) {
	f.listHashtagsArgs = []any{limit, offset}
	return f.listHashtagsResult, f.err
}

func (f *fakeService) UntagHashtag(_ context.Context, url string) (map[string]any, error) {
	f.untagURL = url
	return f.untagResult, f.err
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

func TestDomainScreenPassesTrimmedURL(t *testing.T) {
	service := &fakeService{screenResult: map[string]any{"safe": true}}
	result, err := New(service).Handle(context.Background(), Request{
		Action: ActionScreenMeme,
		Event:  Event{Payload: map[string]any{"url": "  https://example.com/a.png "}},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if service.screenURL != "https://example.com/a.png" {
		t.Fatalf("expected trimmed url, got %q", service.screenURL)
	}
	if result["safe"] != true {
		t.Fatalf("expected service result, got %#v", result)
	}
}

func TestDomainScreenRequiresURL(t *testing.T) {
	_, err := New(&fakeService{}).Handle(context.Background(), Request{
		Action: ActionScreenMeme,
		Event:  Event{Payload: map[string]any{}},
	})
	if err == nil {
		t.Fatal("expected missing url error")
	}
}

func TestDomainScreenForwardsCaption(t *testing.T) {
	service := &fakeService{screenResult: map[string]any{"safe": true}}
	_, err := New(service).Handle(context.Background(), Request{
		Action: ActionScreenMeme,
		Event:  Event{Payload: map[string]any{"url": "https://example.com/a.png", "caption": " oi "}},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if service.screenCaption != "oi" {
		t.Fatalf("expected trimmed caption, got %q", service.screenCaption)
	}
}

func TestDomainListDefaultsAndForwardsPaging(t *testing.T) {
	service := &fakeService{listResult: map[string]any{"count": 0}}
	domain := New(service)

	if _, err := domain.Handle(context.Background(), Request{Action: ActionListMemes, Event: Event{Payload: map[string]any{}}}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := service.listArgs; got[0] != "" || got[1] != 20 || got[2] != 0 {
		t.Fatalf("unexpected defaults %#v", got)
	}

	if _, err := domain.Handle(context.Background(), Request{
		Action: ActionListMemes,
		Event:  Event{Payload: map[string]any{"scope": "sent", "limit": float64(5), "offset": float64(10)}},
	}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := service.listArgs; got[0] != "sent" || got[1] != 5 || got[2] != 10 {
		t.Fatalf("unexpected paging %#v", got)
	}
}

func TestDomainTagHashtagTrimsAndForwards(t *testing.T) {
	service := &fakeService{tagResult: map[string]any{"hashtag": "#TioDoPave"}}
	result, err := New(service).Handle(context.Background(), Request{
		Action: ActionTagMemeHashtag,
		Event: Event{Payload: map[string]any{
			"url": "  https://x/a.png ", "hashtag": " TioDoPave ", "text": " o tio pegou o pave ",
		}},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := service.tagArgs; got[0] != "https://x/a.png" || got[1] != "TioDoPave" || got[2] != "o tio pegou o pave" {
		t.Fatalf("unexpected args %#v", got)
	}
	if result["hashtag"] != "#TioDoPave" {
		t.Fatalf("expected service result, got %#v", result)
	}
}

func TestDomainTagHashtagRequiresURL(t *testing.T) {
	_, err := New(&fakeService{}).Handle(context.Background(), Request{
		Action: ActionTagMemeHashtag,
		Event:  Event{Payload: map[string]any{"hashtag": "TioDoPave", "text": "x"}},
	})
	if err == nil {
		t.Fatal("expected missing url error")
	}
}

func TestDomainSuggestHashtagForwardsTextAndMinScore(t *testing.T) {
	service := &fakeService{suggestResult: map[string]any{"hashtag": "#TioDoPave"}}
	_, err := New(service).Handle(context.Background(), Request{
		Action: ActionSuggestMemeHashtag,
		Event:  Event{Payload: map[string]any{"text": " o tio comeu o pave ", "min_score": 0.75}},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	got := service.suggestArgs
	if got[0] != "" || got[1] != "o tio comeu o pave" {
		t.Fatalf("unexpected args %#v", got)
	}
	minScore, ok := got[2].(*float64)
	if !ok || minScore == nil || *minScore != 0.75 {
		t.Fatalf("expected min_score 0.75, got %#v", got[2])
	}
}

func TestDomainSuggestHashtagWithoutMinScoreForwardsNil(t *testing.T) {
	service := &fakeService{suggestResult: map[string]any{}}
	_, err := New(service).Handle(context.Background(), Request{
		Action: ActionSuggestMemeHashtag,
		Event:  Event{Payload: map[string]any{"url": "https://x/a.png"}},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	minScore, ok := service.suggestArgs[2].(*float64)
	if !ok || minScore != nil {
		t.Fatalf("expected a nil min_score, got %#v", service.suggestArgs[2])
	}
}

func TestDomainListHashtagsDefaultsAndForwardsPaging(t *testing.T) {
	service := &fakeService{listHashtagsResult: map[string]any{"total": 0}}
	domain := New(service)

	if _, err := domain.Handle(context.Background(), Request{Action: ActionListMemeHashtags, Event: Event{Payload: map[string]any{}}}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := service.listHashtagsArgs; got[0] != 50 || got[1] != 0 {
		t.Fatalf("unexpected defaults %#v", got)
	}

	if _, err := domain.Handle(context.Background(), Request{
		Action: ActionListMemeHashtags,
		Event:  Event{Payload: map[string]any{"limit": float64(5), "offset": float64(10)}},
	}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := service.listHashtagsArgs; got[0] != 5 || got[1] != 10 {
		t.Fatalf("unexpected paging %#v", got)
	}
}

func TestDomainUntagHashtagTrimsURLAndRequiresIt(t *testing.T) {
	service := &fakeService{untagResult: map[string]any{"deleted": true}}
	result, err := New(service).Handle(context.Background(), Request{
		Action: ActionUntagMemeHashtag,
		Event:  Event{Payload: map[string]any{"url": "  https://x/a.png "}},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if service.untagURL != "https://x/a.png" {
		t.Fatalf("expected trimmed url, got %q", service.untagURL)
	}
	if result["deleted"] != true {
		t.Fatalf("expected service result, got %#v", result)
	}

	if _, err := New(&fakeService{}).Handle(context.Background(), Request{
		Action: ActionUntagMemeHashtag,
		Event:  Event{Payload: map[string]any{}},
	}); err == nil {
		t.Fatal("expected missing url error")
	}
}
