package handlers

import (
	"context"
	"fmt"
	knowledgedomain "read_books/dokja_domain/dokja_knowledge"
	"strings"
	"time"
)

const defaultMemeHashtagMinScore = 0.6
const memeHashtagSuggestionBudget = 5 * time.Second

// MemeTextReader is the media capability needed by hashtag knowledge. It owns OCR,
// but it does not store labels or decide which hashtag fits.
type MemeTextReader interface {
	ReadText(ctx context.Context, url string) (map[string]any, error)
}

// MemeHashtagWorkflow coordinates media OCR with the shared knowledge base. Prediction
// and feedback remain in HashtagExperience, after a retrieved suggestion is actually used.
type MemeHashtagWorkflow struct {
	media     MemeTextReader
	knowledge *knowledgedomain.Domain
	minScore  float64
	enabled   func() bool
}

func NewMemeHashtagWorkflow(media MemeTextReader, knowledge knowledgedomain.Service) *MemeHashtagWorkflow {
	return &MemeHashtagWorkflow{
		media: media, knowledge: knowledgedomain.New(knowledge), minScore: defaultMemeHashtagMinScore,
	}
}

func (w *MemeHashtagWorkflow) WithMinScore(score float64) *MemeHashtagWorkflow {
	if score >= 0 && score <= 1 {
		w.minScore = score
	}
	return w
}

func (w *MemeHashtagWorkflow) WithKnowledgeEnabled(enabled func() bool) *MemeHashtagWorkflow {
	w.enabled = enabled
	return w
}

func (w *MemeHashtagWorkflow) Handle(ctx context.Context, action string, payload map[string]any) (map[string]any, error) {
	if w == nil || (w.enabled != nil && !w.enabled()) {
		return nil, fmt.Errorf("knowledge service is switched off")
	}
	switch action {
	case "tag-meme-hashtag":
		return w.TagHashtag(ctx, stringValue(payload, "url"), stringValue(payload, "hashtag"), stringValue(payload, "text"))
	case "suggest-meme-hashtag":
		return w.SuggestHashtag(ctx, stringValue(payload, "url"), stringValue(payload, "text"), optionalFloat(payload["min_score"]))
	case "list-meme-hashtags":
		return w.ListHashtags(ctx, intValueOr(payload["limit"], 50), intValueOr(payload["offset"], 0))
	case "untag-meme-hashtag":
		return w.UntagHashtag(ctx, stringValue(payload, "url"))
	default:
		return nil, fmt.Errorf("unsupported meme hashtag action %q", action)
	}
}

func (w *MemeHashtagWorkflow) TagHashtag(ctx context.Context, url, hashtag, text string) (map[string]any, error) {
	example, err := knowledgedomain.PrepareHashtagExample(url, hashtag)
	if err != nil {
		return nil, err
	}
	text, err = w.text(ctx, example.SourceURL(), text)
	if err != nil {
		return nil, err
	}
	return w.knowledge.StoreHashtagExample(ctx, example, text)
}

func (w *MemeHashtagWorkflow) SuggestHashtag(ctx context.Context, url, text string, minScore *float64) (map[string]any, error) {
	if w == nil || (w.enabled != nil && !w.enabled()) {
		return nil, fmt.Errorf("knowledge service is switched off")
	}
	var err error
	text, err = w.text(ctx, strings.TrimSpace(url), text)
	if err != nil {
		return nil, err
	}
	return w.search(ctx, text, minScore)
}

// search finds the labelled example closest to text. SuggestHashtag and the hashtag task
// share it, so both read the same result shape.
func (w *MemeHashtagWorkflow) search(ctx context.Context, text string, minScore *float64) (map[string]any, error) {
	threshold := w.minScore
	if minScore != nil && *minScore >= 0 && *minScore <= 1 {
		threshold = *minScore
	}
	searchCtx, cancel := context.WithTimeout(ctx, memeHashtagSuggestionBudget)
	defer cancel()
	return w.knowledge.SearchHashtagExamples(searchCtx, text, threshold)
}

func (w *MemeHashtagWorkflow) knowledgeEnabled() bool {
	return w != nil && (w.enabled == nil || w.enabled())
}

func (w *MemeHashtagWorkflow) ListHashtags(ctx context.Context, limit, offset int) (map[string]any, error) {
	return w.knowledge.ListHashtagExamples(ctx, limit, offset)
}

func (w *MemeHashtagWorkflow) UntagHashtag(ctx context.Context, url string) (map[string]any, error) {
	return w.knowledge.DeleteHashtagExample(ctx, url)
}

func (w *MemeHashtagWorkflow) text(ctx context.Context, url, explicit string) (string, error) {
	if text := strings.TrimSpace(explicit); text != "" {
		return text, nil
	}
	if url == "" {
		return "", fmt.Errorf("meme hashtag suggest requires a url or text")
	}
	if w == nil || w.media == nil {
		return "", fmt.Errorf("meme text reader is not configured")
	}
	answer, err := w.media.ReadText(ctx, url)
	if err != nil {
		return "", err
	}
	text := strings.TrimSpace(stringValue(answer, "text"))
	if text == "" {
		return "", fmt.Errorf("no text was found in the media at %s; provide explicit text", url)
	}
	return text, nil
}

func stringValue(values map[string]any, key string) string {
	value, _ := values[key].(string)
	return strings.TrimSpace(value)
}

func optionalFloat(value any) *float64 {
	if parsed, ok := numberValue(value); ok {
		return &parsed
	}
	return nil
}

func numberValue(value any) (float64, bool) {
	switch typed := value.(type) {
	case float64:
		return typed, true
	case float32:
		return float64(typed), true
	case int:
		return float64(typed), true
	case int64:
		return float64(typed), true
	default:
		return 0, false
	}
}

func intValueOr(value any, fallback int) int {
	if parsed, ok := numberValue(value); ok {
		return int(parsed)
	}
	return fallback
}
