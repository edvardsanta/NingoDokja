package moderation

import (
	"context"
	"errors"
	"testing"
)

type mediaScreenStub struct {
	result       map[string]any
	err          error
	calls        int
	url, caption string
	ctx          context.Context
}

func (s *mediaScreenStub) Screen(ctx context.Context, url, caption string) (map[string]any, error) {
	s.calls++
	s.ctx, s.url, s.caption = ctx, url, caption
	return s.result, s.err
}

func TestMediaScreenRequiresAnExplicitSafeObservation(t *testing.T) {
	for _, tc := range []struct {
		name   string
		result map[string]any
		err    error
		want   MediaDecision
	}{
		{"safe", map[string]any{"safe": true, "reason": "provider detail"}, nil, MediaDecision{Safe: true}},
		{"unsafe", map[string]any{"safe": false, "reason": "flagged content"}, nil, MediaDecision{Reason: "flagged content"}},
		{"no reason", map[string]any{"safe": false}, nil, MediaDecision{Reason: "flagged by the nsfw screen"}},
		{"nil result", nil, nil, MediaDecision{Reason: "flagged by the nsfw screen"}},
		{"malformed safe", map[string]any{"safe": "true"}, nil, MediaDecision{Reason: "flagged by the nsfw screen"}},
		{"malformed reason", map[string]any{"reason": 1}, nil, MediaDecision{Reason: "flagged by the nsfw screen"}},
		{"error overrides safe", map[string]any{"safe": true}, errors.New("model unavailable"), MediaDecision{Reason: "screen failed: model unavailable"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			screen := &mediaScreenStub{result: tc.result, err: tc.err}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			got := (MediaPolicy{Screener: screen, Enabled: true}).Screen(ctx, "https://example.com/media", "caption")
			if got != tc.want || screen.calls != 1 || screen.ctx != ctx || screen.url != "https://example.com/media" || screen.caption != "caption" {
				t.Fatalf("decision=%+v, want=%+v, screen=%+v", got, tc.want, screen)
			}
		})
	}
}

func TestMediaScreenDoesNotCallADisabledProvider(t *testing.T) {
	screen := &mediaScreenStub{result: map[string]any{"safe": true}}
	got := (MediaPolicy{Screener: screen}).Screen(context.Background(), "media", "caption")
	if got.Safe || got.Reason != "meme service is disabled, so the image cannot be screened" || screen.calls != 0 {
		t.Fatalf("decision=%+v, calls=%d", got, screen.calls)
	}
	for _, enabled := range []bool{false, true} {
		got := (MediaPolicy{Enabled: enabled}).Screen(context.Background(), "media", "caption")
		if got.Safe || got.Reason != "no screener configured" {
			t.Fatalf("missing provider decision=%+v", got)
		}
	}
}

func TestCachedMediaLabelsRequireABooleanAndPreserveTheirReason(t *testing.T) {
	for _, tc := range []struct {
		label map[string]any
		want  MediaDecision
	}{
		{nil, MediaDecision{Reason: "no nsfw label"}},
		{map[string]any{"safe": "yes", "reason": "untrusted"}, MediaDecision{Reason: "no nsfw label"}},
		{map[string]any{"safe": true, "reason": "observed safe"}, MediaDecision{Safe: true, Reason: "observed safe"}},
		{map[string]any{"safe": false, "reason": "flagged"}, MediaDecision{Reason: "flagged"}},
		{map[string]any{"safe": false}, MediaDecision{}},
		{map[string]any{"safe": true, "reason": 42}, MediaDecision{Safe: true}},
	} {
		if got := AssessMediaLabel(tc.label); got != tc.want {
			t.Fatalf("label=%v: got=%+v want=%+v", tc.label, got, tc.want)
		}
	}
}
