package meme

import (
	"context"
	"fmt"
	"strings"
)

const (
	ActionFetchMemes         = "fetch-memes"
	ActionRefreshMemePool    = "refresh-meme-pool"
	ActionInspectMemeStatus  = "inspect-meme-service"
	ActionScreenMeme         = "screen-meme"
	ActionListMemes          = "list-memes"
	ActionTagMemeHashtag     = "tag-meme-hashtag"
	ActionSuggestMemeHashtag = "suggest-meme-hashtag"
	ActionListMemeHashtags   = "list-meme-hashtags"
	ActionUntagMemeHashtag   = "untag-meme-hashtag"
)

type Event struct {
	ID      string
	Source  string
	Type    string
	Payload map[string]any
	Context map[string]any
}

type Request struct {
	Event  Event
	Action string
}

type Service interface {
	Fetch(ctx context.Context, limit *int) (map[string]any, error)
	RefreshPool(ctx context.Context, maxItemsPerScraper int) (map[string]any, error)
	Status(ctx context.Context) (map[string]any, error)
	Screen(ctx context.Context, url, caption string) (map[string]any, error)
	List(ctx context.Context, scope string, limit, offset int) (map[string]any, error)
	TagHashtag(ctx context.Context, url, hashtag, text string) (map[string]any, error)
	SuggestHashtag(ctx context.Context, url, text string, minScore *float64) (map[string]any, error)
	ListHashtags(ctx context.Context, limit, offset int) (map[string]any, error)
	UntagHashtag(ctx context.Context, url string) (map[string]any, error)
}

type Domain struct {
	service Service
}

func New(service Service) *Domain {
	return &Domain{service: service}
}

func (d *Domain) Handle(ctx context.Context, request Request) (map[string]any, error) {
	if d == nil {
		return nil, fmt.Errorf("meme domain is nil")
	}
	if d.service == nil {
		return nil, fmt.Errorf("meme service is not configured")
	}

	switch request.Action {
	case ActionFetchMemes:
		return d.service.Fetch(ctx, intPointer(request.Event.Payload["limit"]))
	case ActionRefreshMemePool:
		maxItems := intValue(request.Event.Payload["max_items_per_scraper"], 20)
		return d.service.RefreshPool(ctx, maxItems)
	case ActionInspectMemeStatus:
		return d.service.Status(ctx)
	case ActionScreenMeme:
		url, _ := request.Event.Payload["url"].(string)
		if strings.TrimSpace(url) == "" {
			return nil, fmt.Errorf("meme screen requires a url")
		}
		caption, _ := request.Event.Payload["caption"].(string)
		return d.service.Screen(ctx, strings.TrimSpace(url), strings.TrimSpace(caption))
	case ActionListMemes:
		scope, _ := request.Event.Payload["scope"].(string)
		return d.service.List(
			ctx,
			strings.TrimSpace(scope),
			intValue(request.Event.Payload["limit"], 20),
			intValue(request.Event.Payload["offset"], 0),
		)
	case ActionTagMemeHashtag:
		url, _ := request.Event.Payload["url"].(string)
		if strings.TrimSpace(url) == "" {
			return nil, fmt.Errorf("meme hashtag tag requires a url")
		}
		hashtag, _ := request.Event.Payload["hashtag"].(string)
		text, _ := request.Event.Payload["text"].(string)
		return d.service.TagHashtag(ctx, strings.TrimSpace(url), strings.TrimSpace(hashtag), strings.TrimSpace(text))
	case ActionSuggestMemeHashtag:
		url, _ := request.Event.Payload["url"].(string)
		text, _ := request.Event.Payload["text"].(string)
		return d.service.SuggestHashtag(
			ctx,
			strings.TrimSpace(url),
			strings.TrimSpace(text),
			floatPointer(request.Event.Payload["min_score"]),
		)
	case ActionListMemeHashtags:
		return d.service.ListHashtags(
			ctx,
			intValue(request.Event.Payload["limit"], 50),
			intValue(request.Event.Payload["offset"], 0),
		)
	case ActionUntagMemeHashtag:
		url, _ := request.Event.Payload["url"].(string)
		if strings.TrimSpace(url) == "" {
			return nil, fmt.Errorf("meme hashtag untag requires a url")
		}
		return d.service.UntagHashtag(ctx, strings.TrimSpace(url))
	default:
		return nil, fmt.Errorf("unsupported meme action %q for event type %q", request.Action, request.Event.Type)
	}
}

func intPointer(value any) *int {
	if parsed, ok := parseInt(value); ok {
		return &parsed
	}
	return nil
}

func floatPointer(value any) *float64 {
	switch typed := value.(type) {
	case float64:
		return &typed
	case float32:
		parsed := float64(typed)
		return &parsed
	case int:
		parsed := float64(typed)
		return &parsed
	default:
		return nil
	}
}

func intValue(value any, fallback int) int {
	if parsed, ok := parseInt(value); ok {
		return parsed
	}
	return fallback
}

func parseInt(value any) (int, bool) {
	switch typed := value.(type) {
	case int:
		return typed, true
	case int8:
		return int(typed), true
	case int16:
		return int(typed), true
	case int32:
		return int(typed), true
	case int64:
		return int(typed), true
	case float32:
		return int(typed), true
	case float64:
		return int(typed), true
	default:
		return 0, false
	}
}
