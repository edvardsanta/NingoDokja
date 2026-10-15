package handlers

import (
	"context"
	"testing"
)

type fakeMemeTextReader struct {
	urls   []string
	result map[string]any
	err    error
}

func (f *fakeMemeTextReader) ReadText(_ context.Context, url string) (map[string]any, error) {
	f.urls = append(f.urls, url)
	return f.result, f.err
}

type knowledgeCall struct {
	eventType string
	payload   map[string]any
}

type fakeHashtagKnowledge struct {
	calls   []knowledgeCall
	answers map[string]map[string]any
}

func (f *fakeHashtagKnowledge) Dispatch(_ context.Context, eventType string, payload map[string]any) (map[string]any, error) {
	f.calls = append(f.calls, knowledgeCall{eventType: eventType, payload: payload})
	return f.answers[eventType], nil
}

func TestMemeHashtagWorkflowStoresLabelsAsKnowledge(t *testing.T) {
	media := &fakeMemeTextReader{result: map[string]any{"text": "words read from the meme"}}
	knowledge := &fakeHashtagKnowledge{answers: map[string]map[string]any{
		"knowledge.ingest": {"changed": true, "embedded": float64(1), "degraded": false},
	}}
	workflow := NewMemeHashtagWorkflow(media, knowledge)

	result, err := workflow.TagHashtag(context.Background(), " https://media.example/a.png ", " Humor ", "")
	if err != nil {
		t.Fatal(err)
	}
	if result["hashtag"] != "#Humor" || result["text"] != "words read from the meme" || result["embedded"] != true {
		t.Fatalf("unexpected tag result: %#v", result)
	}
	if len(media.urls) != 1 || media.urls[0] != "https://media.example/a.png" {
		t.Fatalf("expected OCR through the meme service, got %#v", media.urls)
	}
	call := knowledge.calls[0]
	if call.eventType != "knowledge.ingest" || call.payload["kind"] != memeHashtagKnowledgeKind || call.payload["title"] != "#Humor" {
		t.Fatalf("unexpected knowledge ingest: %#v", call)
	}
	if call.payload["source_id"] != hashtagKnowledgeID("https://media.example/a.png") {
		t.Fatalf("unexpected stable source id: %v", call.payload["source_id"])
	}
}

func TestMemeHashtagWorkflowRetrievesOnlyHashtagKnowledge(t *testing.T) {
	knowledge := &fakeHashtagKnowledge{answers: map[string]map[string]any{
		"knowledge.search": {
			"threshold": 0.6, "degraded": false,
			"hits": []any{map[string]any{
				"title": "#Humor", "text": "an earlier joke", "source_ref": "https://media.example/old.png",
				"score": 0.83, "relevant": true,
			}},
		},
	}}
	workflow := NewMemeHashtagWorkflow(&fakeMemeTextReader{}, knowledge)

	result, err := workflow.SuggestHashtag(context.Background(), "", "a fresh joke", nil)
	if err != nil {
		t.Fatal(err)
	}
	if result["hashtag"] != "#Humor" || result["matched_text"] != "an earlier joke" || result["relevant"] != true {
		t.Fatalf("unexpected suggestion: %#v", result)
	}
	call := knowledge.calls[0]
	if call.eventType != "knowledge.search" || call.payload["kind"] != memeHashtagKnowledgeKind ||
		call.payload["query"] != "a fresh joke" || call.payload["min_score"] != defaultMemeHashtagMinScore {
		t.Fatalf("unexpected knowledge search: %#v", call)
	}
}

func TestMemeHashtagWorkflowListsAndDeletesKnowledge(t *testing.T) {
	url := "https://media.example/a.png"
	knowledge := &fakeHashtagKnowledge{answers: map[string]map[string]any{
		"knowledge.list": {
			"total": float64(1), "offset": float64(0),
			"documents": []any{map[string]any{
				"source_ref": url, "title": "#Humor", "text": "a joke", "embedded": float64(1), "updated_at": "now",
			}},
		},
		"knowledge.delete": {"deleted": true},
	}}
	workflow := NewMemeHashtagWorkflow(&fakeMemeTextReader{}, knowledge)

	listed, err := workflow.ListHashtags(context.Background(), 20, 0)
	if err != nil {
		t.Fatal(err)
	}
	example := listed["examples"].([]any)[0].(map[string]any)
	if example["source_url"] != url || example["hashtag"] != "#Humor" || example["embedded"] != true {
		t.Fatalf("unexpected example: %#v", example)
	}
	deleted, err := workflow.UntagHashtag(context.Background(), url)
	if err != nil || deleted["deleted"] != true {
		t.Fatalf("unexpected delete: %#v, %v", deleted, err)
	}
	if knowledge.calls[1].payload["source_id"] != hashtagKnowledgeID(url) {
		t.Fatalf("delete did not address the same knowledge document: %#v", knowledge.calls[1])
	}
}

func TestMemeHashtagWorkflowHonorsKnowledgeSwitch(t *testing.T) {
	knowledge := &fakeHashtagKnowledge{answers: map[string]map[string]any{}}
	workflow := NewMemeHashtagWorkflow(&fakeMemeTextReader{}, knowledge).
		WithKnowledgeEnabled(func() bool { return false })

	_, err := workflow.Handle(context.Background(), "list-meme-hashtags", map[string]any{})
	if err == nil || len(knowledge.calls) != 0 {
		t.Fatalf("expected a switched-off error without a knowledge call, got %v, %#v", err, knowledge.calls)
	}
}
