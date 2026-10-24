package knowledge

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"unicode"
)

const HashtagKnowledgeKind = "meme_hashtag"

// HashtagExample is a validated label and source identity. Preparation is pure
// so orchestration can reject invalid intent before acquiring media text.
type HashtagExample struct {
	sourceURL string
	hashtag   string
}

func (e HashtagExample) SourceURL() string { return e.sourceURL }

func PrepareHashtagExample(sourceURL, hashtag string) (HashtagExample, error) {
	sourceURL = strings.TrimSpace(sourceURL)
	if sourceURL == "" {
		return HashtagExample{}, fmt.Errorf("meme hashtag tag requires a url")
	}
	label := strings.TrimSpace(hashtag)
	if label == "" || label == "#" {
		return HashtagExample{}, fmt.Errorf("hashtag is required")
	}
	if !strings.HasPrefix(label, "#") {
		label = "#" + label
	}
	for _, char := range strings.TrimPrefix(label, "#") {
		if char != '_' && !unicode.IsLetter(char) && !unicode.IsDigit(char) {
			return HashtagExample{}, fmt.Errorf("invalid hashtag %q: only letters, digits and _ are allowed after the #", label)
		}
	}
	return HashtagExample{sourceURL: sourceURL, hashtag: label}, nil
}

// HashtagKnowledgeID preserves the existing document and task-evidence identity.
// A reread or retag of the same trimmed source addresses the same example.
func HashtagKnowledgeID(sourceURL string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(sourceURL)))
	return "meme-hashtag:" + hex.EncodeToString(sum[:])[:32]
}

func (d *Domain) StoreHashtagExample(ctx context.Context, example HashtagExample, text string) (map[string]any, error) {
	// A zero value is not a prepared example and cannot reach storage.
	if example.sourceURL == "" {
		return nil, fmt.Errorf("meme hashtag tag requires a url")
	}
	answer, err := d.Handle(ctx, Request{
		Action: ActionIngestKnowledge,
		Event: Event{Source: "orchestrator", Type: "knowledge.ingest", Payload: map[string]any{
			"source_id": HashtagKnowledgeID(example.sourceURL), "source_ref": example.sourceURL,
			"kind": HashtagKnowledgeKind, "title": example.hashtag, "body": text,
			"tags": []string{"meme-hashtag"},
		}},
	})
	if err != nil {
		return nil, err
	}
	embedded, _ := hashtagNumber(answer["embedded"])
	pending, _ := hashtagNumber(answer["pending_embeddings"])
	changed, _ := answer["changed"].(bool)
	result := map[string]any{
		"source_url": example.sourceURL, "hashtag": example.hashtag, "text": text,
		"embedded": embedded > 0 || (!changed && pending == 0),
	}
	for _, field := range []string{"degraded", "reason"} {
		if value, exists := answer[field]; exists {
			result[field] = value
		}
	}
	return result, nil
}

// SearchHashtagExamples restricts retrieval to explicit labelled examples and
// preserves the nearest candidate even when the provider marks it irrelevant.
func (d *Domain) SearchHashtagExamples(ctx context.Context, text string, minScore float64) (map[string]any, error) {
	answer, err := d.Handle(ctx, Request{
		Action: ActionSearchKnowledge,
		Event: Event{Source: "orchestrator", Type: "knowledge.search", Payload: map[string]any{
			"query": text, "k": 1, "kind": HashtagKnowledgeKind, "min_score": minScore,
		}},
	})
	if err != nil {
		return nil, err
	}
	result := map[string]any{
		"query_text": text, "hashtag": nil, "relevant": false,
		"degraded": answer["degraded"], "threshold": answer["threshold"],
	}
	if reason, exists := answer["reason"]; exists {
		result["reason"] = reason
	}
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

func (d *Domain) ListHashtagExamples(ctx context.Context, limit, offset int) (map[string]any, error) {
	answer, err := d.Handle(ctx, Request{
		Action: ActionListKnowledge,
		Event: Event{Source: "orchestrator", Type: "knowledge.list", Payload: map[string]any{
			"kind": HashtagKnowledgeKind, "include_text": true, "limit": limit, "offset": offset,
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
		embedded, _ := hashtagNumber(document["embedded"])
		examples = append(examples, map[string]any{
			"source_url": document["source_ref"], "text": document["text"], "hashtag": document["title"],
			"embedded": embedded > 0, "updated_at": document["updated_at"],
		})
	}
	return map[string]any{"total": answer["total"], "offset": answer["offset"], "examples": examples}, nil
}

func (d *Domain) DeleteHashtagExample(ctx context.Context, sourceURL string) (map[string]any, error) {
	sourceURL = strings.TrimSpace(sourceURL)
	if sourceURL == "" {
		return nil, fmt.Errorf("meme hashtag untag requires a url")
	}
	answer, err := d.Handle(ctx, Request{
		Action: ActionDeleteKnowledge,
		Event: Event{Source: "orchestrator", Type: "knowledge.delete", Payload: map[string]any{
			"source_id": HashtagKnowledgeID(sourceURL),
		}},
	})
	if err != nil {
		return nil, err
	}
	return map[string]any{"source_url": sourceURL, "deleted": answer["deleted"]}, nil
}

func hashtagNumber(value any) (float64, bool) {
	switch value := value.(type) {
	case float64:
		return value, true
	case float32:
		return float64(value), true
	case int:
		return float64(value), true
	case int64:
		return float64(value), true
	default:
		return 0, false
	}
}
