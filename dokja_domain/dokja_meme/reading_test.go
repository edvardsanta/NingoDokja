package meme

import (
	"context"
	"errors"
	"testing"
)

type readingStub struct {
	calls    []string
	items    []any
	ingested []any
	present  bool
}

func (r *readingStub) Dispatch(_ context.Context, kind string, _ map[string]any) (map[string]any, error) {
	r.calls = append(r.calls, kind)
	if kind == "feeds.items" {
		return map[string]any{"items": r.items}, nil
	}
	return map[string]any{"failed": float64(0)}, nil
}
func (r *readingStub) Invoke(_ context.Context, capability, action string, input map[string]any) (map[string]any, error) {
	r.calls = append(r.calls, capability+":"+action)
	switch action {
	case "ingest":
		r.ingested = input["items"].([]any)
		return map[string]any{"added": len(r.ingested)}, nil
	case "contains":
		return map[string]any{"exists": r.present}, nil
	case "screen":
		return nil, errors.New("no perception provider")
	case "fetch":
		return map[string]any{"memes": []any{map[string]any{"url": "https://images.example/m.png"}}}, nil
	}
	return map[string]any{}, nil
}
func TestReadingRefreshSelectsMediaAndAppliesPerPluginLimit(t *testing.T) {
	r := &readingStub{items: []any{
		map[string]any{"kind": "article", "plugin": "news"},
		map[string]any{"kind": "media", "plugin": "first", "url": "https://images.example/a.png"},
		map[string]any{"kind": "media", "plugin": "first", "url": "https://images.example/b.png"},
		map[string]any{"kind": "media", "plugin": "second", "url": "https://images.example/c.png"},
	}}
	result, err := NewReadingService(r).RefreshPool(context.Background(), 1)
	if err != nil || result["status"] != "refreshed" || len(r.ingested) != 2 {
		t.Fatal(result, r.ingested, err)
	}
	if len(r.calls) != 3 || r.calls[0] != "feeds.refresh" || r.calls[2] != "media.library.v1:ingest" {
		t.Fatal(r.calls)
	}
}
func TestReadingAttachmentCannotFetchAnUnpooledURL(t *testing.T) {
	r := &readingStub{}
	if _, _, err := NewReadingService(r).FetchAttachment(context.Background(), "https://images.example/a.png"); err == nil {
		t.Fatal("unpooled url accepted")
	}
	if len(r.calls) != 1 || r.calls[0] != "media.library.v1:contains" {
		t.Fatal(r.calls)
	}
}
func TestReadingFetchLabelsUnscreenedMediaAsUnsafe(t *testing.T) {
	result, err := NewReadingService(&readingStub{}).Fetch(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	row := result["memes"].([]any)[0].(map[string]any)
	if row["nsfw"].(map[string]any)["safe"] != false {
		t.Fatal(row)
	}
}
