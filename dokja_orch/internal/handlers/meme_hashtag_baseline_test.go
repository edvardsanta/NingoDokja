package handlers

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

// These tests pin the current hashtag suggestion behavior as the step 03 baseline
// (steps.json). Later workspace experiments must reproduce these outcomes or record
// an intended change.

func TestBaselineSuggestionBelowThresholdKeepsTheHashtagButIsNotRelevant(t *testing.T) {
	knowledge := &fakeHashtagKnowledge{answers: map[string]map[string]any{
		"knowledge.search": {
			"threshold": 0.6, "degraded": false,
			"hits": []any{map[string]any{
				"title": "#Humor", "text": "an earlier joke", "source_ref": "https://media.example/old.png",
				"score": 0.41, "relevant": false,
			}},
		},
	}}
	workflow := NewMemeHashtagWorkflow(&fakeMemeTextReader{}, knowledge)

	result, err := workflow.SuggestHashtag(context.Background(), "", "a fresh joke", nil)
	if err != nil {
		t.Fatal(err)
	}
	// Abstention is signalled by relevant=false; the nearest label is still returned.
	if result["hashtag"] != "#Humor" || result["relevant"] != false || result["score"] != 0.41 {
		t.Fatalf("unexpected abstention: %#v", result)
	}
	if _, exists := result["reason"]; exists {
		t.Fatalf("a below-threshold hit carries no reason today: %#v", result)
	}
}

func TestBaselineSuggestionWithoutExamplesExplainsTheAbstention(t *testing.T) {
	knowledge := &fakeHashtagKnowledge{answers: map[string]map[string]any{
		"knowledge.search": {"threshold": 0.6, "degraded": false, "hits": []any{}},
	}}
	workflow := NewMemeHashtagWorkflow(&fakeMemeTextReader{}, knowledge)

	result, err := workflow.SuggestHashtag(context.Background(), "", "a fresh joke", nil)
	if err != nil {
		t.Fatal(err)
	}
	if result["hashtag"] != nil || result["relevant"] != false ||
		result["reason"] != "no tagged meme examples in knowledge" {
		t.Fatalf("unexpected empty suggestion: %#v", result)
	}
}

func TestBaselineDegradedKnowledgeReasonIsKept(t *testing.T) {
	knowledge := &fakeHashtagKnowledge{answers: map[string]map[string]any{
		"knowledge.search": {"threshold": 0.6, "degraded": true, "reason": "keyword search only", "hits": []any{}},
	}}
	workflow := NewMemeHashtagWorkflow(&fakeMemeTextReader{}, knowledge)

	result, err := workflow.SuggestHashtag(context.Background(), "", "a fresh joke", nil)
	if err != nil {
		t.Fatal(err)
	}
	if result["degraded"] != true || result["reason"] != "keyword search only" || result["hashtag"] != nil {
		t.Fatalf("unexpected degraded suggestion: %#v", result)
	}
}

func TestBaselineKnowledgeFailureIsReturnedAsAnError(t *testing.T) {
	knowledge := &fakeHashtagKnowledge{err: errors.New("no reply from knowledge")}
	workflow := NewMemeHashtagWorkflow(&fakeMemeTextReader{}, knowledge)

	result, err := workflow.SuggestHashtag(context.Background(), "", "a fresh joke", nil)
	if err == nil || result != nil {
		t.Fatalf("expected the dependency error, got %#v, %v", result, err)
	}
}

func TestBaselineMissingTextNeverReachesKnowledge(t *testing.T) {
	cases := []struct {
		name  string
		media *fakeMemeTextReader
		url   string
		want  string
	}{
		{"no url or text", &fakeMemeTextReader{}, "", "requires a url or text"},
		{"ocr failure", &fakeMemeTextReader{err: errors.New("ocr unavailable")}, "https://media.example/a.png", "ocr unavailable"},
		{"no text in media", &fakeMemeTextReader{result: map[string]any{"text": "  "}}, "https://media.example/a.png", "provide explicit text"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			knowledge := &fakeHashtagKnowledge{}
			workflow := NewMemeHashtagWorkflow(tc.media, knowledge)

			_, err := workflow.SuggestHashtag(context.Background(), tc.url, "", nil)
			if err == nil || !strings.Contains(err.Error(), tc.want) || len(knowledge.calls) != 0 {
				t.Fatalf("expected %q without a knowledge call, got %v, %#v", tc.want, err, knowledge.calls)
			}
		})
	}
}

func TestBaselineExplicitTextSkipsOCR(t *testing.T) {
	media := &fakeMemeTextReader{result: map[string]any{"text": "ocr words"}}
	knowledge := &fakeHashtagKnowledge{answers: map[string]map[string]any{"knowledge.search": {"hits": []any{}}}}
	workflow := NewMemeHashtagWorkflow(media, knowledge)

	if _, err := workflow.SuggestHashtag(context.Background(), "https://media.example/a.png", "typed words", nil); err != nil {
		t.Fatal(err)
	}
	if len(media.urls) != 0 || knowledge.calls[0].payload["query"] != "typed words" {
		t.Fatalf("explicit text should be used as is: %#v, %#v", media.urls, knowledge.calls)
	}
}

func TestBaselineMinScoreOverrideAndSearchBudget(t *testing.T) {
	cases := []struct {
		name     string
		minScore float64
		want     float64
	}{
		{"in range", 0.9, 0.9},
		{"out of range falls back", 1.5, defaultMemeHashtagMinScore},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			knowledge := &fakeHashtagKnowledge{answers: map[string]map[string]any{"knowledge.search": {"hits": []any{}}}}
			workflow := NewMemeHashtagWorkflow(&fakeMemeTextReader{}, knowledge)
			minScore := tc.minScore

			if _, err := workflow.SuggestHashtag(context.Background(), "", "a joke", &minScore); err != nil {
				t.Fatal(err)
			}
			finished := time.Now()
			call := knowledge.calls[0]
			if call.payload["min_score"] != tc.want || call.payload["k"] != 1 {
				t.Fatalf("unexpected search payload: %#v", call.payload)
			}
			if call.deadline.IsZero() || call.deadline.After(finished.Add(memeHashtagSuggestionBudget)) {
				t.Fatalf("search should be bounded by %s, got deadline %v", memeHashtagSuggestionBudget, call.deadline)
			}
		})
	}
}
