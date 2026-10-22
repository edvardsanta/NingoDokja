package meme

import (
	"context"
	"encoding/base64"
	"fmt"
	"sort"
)

// ReadingPort is implemented by the orchestrator's Feeds transport. Collection,
// pool storage and perception providers can be extended independently.
type ReadingPort interface {
	Dispatch(context.Context, string, map[string]any) (map[string]any, error)
	Invoke(context.Context, string, string, map[string]any) (map[string]any, error)
}

type ReadingService struct{ reading ReadingPort }

func NewReadingService(reading ReadingPort) *ReadingService { return &ReadingService{reading: reading} }

func (s *ReadingService) library(ctx context.Context, action string, input map[string]any) (map[string]any, error) {
	return s.reading.Invoke(ctx, "media.library.v1", action, input)
}

func (s *ReadingService) RefreshPool(ctx context.Context, maxItems int) (map[string]any, error) {
	if maxItems < 1 || maxItems > 200 {
		return nil, fmt.Errorf("max_items_per_scraper must be between 1 and 200")
	}
	refresh, err := s.reading.Dispatch(ctx, "feeds.refresh", nil)
	if err != nil {
		return nil, err
	}
	page, err := s.reading.Dispatch(ctx, "feeds.items", nil)
	if err != nil {
		return nil, err
	}
	// Only explicit media observations belong in this intake cache; a news link
	// is not implicitly an attachment. Keep the per-source bound of the facade.
	items := make([]any, 0)
	counts := map[string]int{}
	for _, raw := range observations(page["items"]) {
		kind, _ := raw["kind"].(string)
		media, _ := raw["media_url"].(string)
		if kind != "media" && media == "" {
			continue
		}
		plugin, _ := raw["plugin"].(string)
		if counts[plugin] >= maxItems {
			continue
		}
		counts[plugin]++
		items = append(items, raw)
	}
	result, err := s.library(ctx, "ingest", map[string]any{"items": items})
	if err != nil {
		return nil, err
	}
	result["status"] = "refreshed"
	result["max_items_per_scraper"] = maxItems
	names := make([]string, 0, len(counts))
	for name := range counts {
		names = append(names, name)
	}
	sort.Strings(names)
	result["scrapers"] = names
	result["collection"] = refresh
	if failed := intValue(refresh["failed"], 0); failed > 0 {
		return nil, fmt.Errorf("%d source plugins failed; successful observations were retained", int(failed))
	}
	return result, nil
}

func (s *ReadingService) List(ctx context.Context, scope string, limit, offset int) (map[string]any, error) {
	return s.library(ctx, "list", map[string]any{"scope": scope, "limit": limit, "offset": offset})
}
func (s *ReadingService) Status(ctx context.Context) (map[string]any, error) {
	return s.library(ctx, "status", nil)
}
func (s *ReadingService) Fetch(ctx context.Context, limit *int) (map[string]any, error) {
	input := map[string]any{}
	if limit != nil {
		input["limit"] = *limit
	}
	result, err := s.library(ctx, "fetch", input)
	if err != nil {
		return nil, err
	}
	for _, item := range observations(result["memes"]) {
		url, _ := item["url"].(string)
		title, _ := item["title"].(string)
		verdict, err := s.Screen(ctx, url, title)
		if err != nil {
			verdict = map[string]any{"safe": false, "reason": "screen unavailable"}
		}
		item["nsfw"] = verdict
	}
	return result, nil
}
func (s *ReadingService) Screen(ctx context.Context, url, caption string) (map[string]any, error) {
	return s.reading.Invoke(ctx, "media.perceive.v1", "screen", map[string]any{"url": url, "caption": caption})
}
func (s *ReadingService) ReadText(ctx context.Context, url string) (map[string]any, error) {
	return s.reading.Invoke(ctx, "media.perceive.v1", "read-text", map[string]any{"url": url})
}
func (s *ReadingService) MarkSent(ctx context.Context, url string) error {
	result, err := s.library(ctx, "mark-sent", map[string]any{"url": url})
	if err != nil {
		return err
	}
	if marked, _ := result["marked"].(bool); !marked {
		return fmt.Errorf("media was not marked as sent")
	}
	return nil
}
func (s *ReadingService) FetchAttachment(ctx context.Context, url string) ([]byte, string, error) {
	result, err := s.library(ctx, "contains", map[string]any{"url": url})
	if err != nil {
		return nil, "", err
	}
	if exists, _ := result["exists"].(bool); !exists {
		return nil, "", fmt.Errorf("url is not in the media library")
	}
	result, err = s.reading.Invoke(ctx, "media.perceive.v1", "attachment", map[string]any{"url": url})
	if err != nil {
		return nil, "", err
	}
	encoded, _ := result["data_b64"].(string)
	if encoded == "" {
		return nil, "", fmt.Errorf("media plugin returned no attachment data")
	}
	data, err := base64.StdEncoding.DecodeString(encoded)
	contentType, _ := result["content_type"].(string)
	return data, contentType, err
}

func observations(value any) []map[string]any {
	switch items := value.(type) {
	case []map[string]any:
		return items
	case []any:
		out := make([]map[string]any, 0, len(items))
		for _, item := range items {
			if row, ok := item.(map[string]any); ok {
				out = append(out, row)
			}
		}
		return out
	}
	return nil
}
