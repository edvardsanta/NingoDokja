package handlers

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	knowledgedomain "read_books/dokja_domain/dokja_knowledge"
	"strings"
	"time"
	"unicode"
)

const memeHashtagKnowledgeKind = "meme_hashtag"
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
	url = strings.TrimSpace(url)
	if url == "" {
		return nil, fmt.Errorf("meme hashtag tag requires a url")
	}
	label, err := normalizeHashtag(hashtag)
	if err != nil {
		return nil, err
	}
	text, err = w.text(ctx, url, text)
	if err != nil {
		return nil, err
	}
	answer, err := w.knowledge.Handle(ctx, knowledgedomain.Request{
		Action: knowledgedomain.ActionIngestKnowledge,
		Event: knowledgedomain.Event{Source: "orchestrator", Type: "knowledge.ingest", Payload: map[string]any{
			"source_id": hashtagKnowledgeID(url), "source_ref": url, "kind": memeHashtagKnowledgeKind,
			"title": label, "body": text, "tags": []string{"meme-hashtag"},
		}},
	})
	if err != nil {
		return nil, err
	}
	embedded, _ := numberValue(answer["embedded"])
	pending, _ := numberValue(answer["pending_embeddings"])
	changed, _ := answer["changed"].(bool)
	result := map[string]any{
		"source_url": url, "hashtag": label, "text": text,
		"embedded": embedded > 0 || (!changed && pending == 0),
	}
	copyFields(result, answer, "degraded", "reason")
	return result, nil
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
	payload := map[string]any{"query": text, "k": 1, "kind": memeHashtagKnowledgeKind}
	payload["min_score"] = w.minScore
	if minScore != nil && *minScore >= 0 && *minScore <= 1 {
		payload["min_score"] = *minScore
	}
	searchCtx, cancel := context.WithTimeout(ctx, memeHashtagSuggestionBudget)
	defer cancel()
	answer, err := w.knowledge.Handle(searchCtx, knowledgedomain.Request{
		Action: knowledgedomain.ActionSearchKnowledge,
		Event:  knowledgedomain.Event{Source: "orchestrator", Type: "knowledge.search", Payload: payload},
	})
	if err != nil {
		return nil, err
	}
	result := map[string]any{
		"query_text": text, "hashtag": nil, "relevant": false,
		"degraded": answer["degraded"], "threshold": answer["threshold"],
	}
	copyFields(result, answer, "reason")
	hits, _ := answer["hits"].([]any)
	if len(hits) == 0 {
		if _, exists := result["reason"]; !exists {
			result["reason"] = "no tagged meme examples in knowledge"
		}
		return result, nil
	}
	hit, _ := hits[0].(map[string]any)
	result["hashtag"] = hit["title"]
	result["score"] = hit["score"]
	result["relevant"] = hit["relevant"]
	result["matched_text"] = hit["text"]
	result["matched_source_url"] = hit["source_ref"]
	return result, nil
}

func (w *MemeHashtagWorkflow) ListHashtags(ctx context.Context, limit, offset int) (map[string]any, error) {
	answer, err := w.knowledge.Handle(ctx, knowledgedomain.Request{
		Action: knowledgedomain.ActionListKnowledge,
		Event: knowledgedomain.Event{Source: "orchestrator", Type: "knowledge.list", Payload: map[string]any{
			"kind": memeHashtagKnowledgeKind, "include_text": true, "limit": limit, "offset": offset,
		}},
	})
	if err != nil {
		return nil, err
	}
	documents, _ := answer["documents"].([]any)
	examples := make([]any, 0, len(documents))
	for _, raw := range documents {
		document, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		embedded, _ := numberValue(document["embedded"])
		examples = append(examples, map[string]any{
			"source_url": document["source_ref"], "text": document["text"], "hashtag": document["title"],
			"embedded": embedded > 0, "updated_at": document["updated_at"],
		})
	}
	return map[string]any{"total": answer["total"], "offset": answer["offset"], "examples": examples}, nil
}

func (w *MemeHashtagWorkflow) UntagHashtag(ctx context.Context, url string) (map[string]any, error) {
	url = strings.TrimSpace(url)
	if url == "" {
		return nil, fmt.Errorf("meme hashtag untag requires a url")
	}
	answer, err := w.knowledge.Handle(ctx, knowledgedomain.Request{
		Action: knowledgedomain.ActionDeleteKnowledge,
		Event: knowledgedomain.Event{Source: "orchestrator", Type: "knowledge.delete", Payload: map[string]any{
			"source_id": hashtagKnowledgeID(url),
		}},
	})
	if err != nil {
		return nil, err
	}
	return map[string]any{"source_url": url, "deleted": answer["deleted"]}, nil
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

func normalizeHashtag(raw string) (string, error) {
	value := strings.TrimSpace(raw)
	if value == "" {
		return "", fmt.Errorf("hashtag is required")
	}
	if !strings.HasPrefix(value, "#") {
		value = "#" + value
	}
	for _, char := range strings.TrimPrefix(value, "#") {
		if char != '_' && !unicode.IsLetter(char) && !unicode.IsDigit(char) {
			return "", fmt.Errorf("invalid hashtag %q: only letters, digits and _ are allowed after the #", value)
		}
	}
	if value == "#" {
		return "", fmt.Errorf("hashtag is required")
	}
	return value, nil
}

func hashtagKnowledgeID(url string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(url)))
	return "meme-hashtag:" + hex.EncodeToString(sum[:])[:32]
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

func copyFields(target, source map[string]any, fields ...string) {
	for _, field := range fields {
		if value, exists := source[field]; exists {
			target[field] = value
		}
	}
}
