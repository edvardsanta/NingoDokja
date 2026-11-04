package handlers

import (
	"context"
	"errors"
	"strings"
	"testing"

	"read_books/internal/core"
)

type tagActionsFunc func(context.Context, string, map[string]any) (map[string]any, error)

func (f tagActionsFunc) Handle(ctx context.Context, action string, payload map[string]any) (map[string]any, error) {
	return f(ctx, action, payload)
}

func TestDesktopTagEvaluatesBeforeKnowledgeChangesAndRecordsAfterSuccess(t *testing.T) {
	for _, observed := range []string{"#Humor", "#Other"} {
		t.Run(observed, func(t *testing.T) {
			memory := &recordingMemory{answers: defaultAnswers()}
			task := hashtagTaskWith(relevantKnowledge(true), memory, true)
			saved := false
			handler := NewMemeDomainHandler(&fakeMemeService{}).
				WithHashtagExperience(synchronous(memory, nil)).
				WithTagEvaluation(task, synchronous(memory, nil)).
				WithHashtagWorkflow(tagActionsFunc(func(_ context.Context, action string, payload map[string]any) (map[string]any, error) {
					if action != "tag-meme-hashtag" || payload["hashtag"] != observed {
						t.Fatal("evaluation must preserve the operator's tag", action, payload)
					}
					if payload["text"] != testMemeText {
						t.Fatal("the save should reuse the evaluation's OCR text", payload)
					}
					if got := strings.Join(memory.eventTypes(), ","); got != "memory.recall" {
						t.Fatalf("predict before saving; no record or feedback yet: %s", got)
					}
					saved = true
					return map[string]any{"source_url": testMemeURL, "hashtag": observed}, nil
				}))
			event := taskEventFor()
			event.Type = "meme.hashtag.tag"
			event.Payload = map[string]any{"url": testMemeURL, "hashtag": observed}
			_, err := handler.Handle(context.Background(), event, core.WorkflowStep{Domain: core.DomainMeme, Action: "tag-meme-hashtag"})
			if err != nil || !saved {
				t.Fatal("tag save failed", err)
			}
			if _, changed := event.Payload["text"]; changed {
				t.Fatal("the incoming event must not be mutated")
			}
			if got := strings.Join(memory.eventTypes(), ","); got != "memory.recall,memory.record,memory.get,memory.resolve" {
				t.Fatalf("record must precede feedback without predicting again: %s", got)
			}
			record := recordsOf(memory)[0]
			if record["detail"] != "#Humor" || record["predicted_p"] == nil {
				t.Fatal("keep Ningo's independently predicted suggestion", record)
			}
			want := "accepted"
			if observed != "#Humor" {
				want = "replaced"
			}
			if got := memory.sent()[3].payload["outcome"]; got != want {
				t.Fatalf("got %v, want %s", got, want)
			}
		})
	}
}

func TestDesktopTagWithoutRelevantCandidateDoesNotInventAScoredExample(t *testing.T) {
	memory := &recordingMemory{answers: defaultAnswers()}
	experience := synchronous(memory, nil)
	handler := NewMemeDomainHandler(&fakeMemeService{}).
		WithTagEvaluation(hashtagTaskWith(relevantKnowledge(false), memory, true), experience).
		WithHashtagWorkflow(fakeHashtagActions{result: map[string]any{"source_url": testMemeURL, "hashtag": "#New"}})
	event := taskEventFor()
	event.Payload = map[string]any{"url": testMemeURL, "hashtag": "#New"}
	_, err := handler.Handle(context.Background(), event, core.WorkflowStep{Action: "tag-meme-hashtag"})
	if err != nil || len(recordsOf(memory)) != 0 {
		t.Fatal("a new label with no relevant prediction is knowledge, not a scored prediction", err)
	}
}

func TestDesktopTagWithMemoryDisabledStillSavesWithoutEvaluation(t *testing.T) {
	memory := &recordingMemory{answers: defaultAnswers()}
	experience := synchronous(memory, func() bool { return false })
	handler := NewMemeDomainHandler(&fakeMemeService{}).
		WithHashtagExperience(experience).
		WithTagEvaluation(hashtagTaskWith(relevantKnowledge(true), memory, true), experience).
		WithHashtagWorkflow(fakeHashtagActions{result: map[string]any{"source_url": testMemeURL, "hashtag": "#Humor"}})
	event := taskEventFor()
	event.Payload = map[string]any{"url": testMemeURL, "hashtag": "#Humor"}
	_, err := handler.Handle(context.Background(), event, core.WorkflowStep{Action: "tag-meme-hashtag"})
	if err != nil || len(memory.sent()) != 0 {
		t.Fatal("disabled memory must not affect a tag save", err, memory.eventTypes())
	}
}

func TestFailedDesktopTagDoesNotRecordOrResolveItsShadowSuggestion(t *testing.T) {
	memory := &recordingMemory{answers: defaultAnswers()}
	handler := NewMemeDomainHandler(&fakeMemeService{}).
		WithHashtagExperience(synchronous(memory, nil)).
		WithTagEvaluation(hashtagTaskWith(relevantKnowledge(true), memory, true), synchronous(memory, nil)).
		WithHashtagWorkflow(fakeHashtagActions{err: errors.New("knowledge unavailable")})
	event := taskEventFor()
	event.Payload = map[string]any{"url": testMemeURL, "hashtag": "#Humor"}
	_, err := handler.Handle(context.Background(), event, core.WorkflowStep{Action: "tag-meme-hashtag"})
	if err == nil || strings.Join(memory.eventTypes(), ",") != "memory.recall" {
		t.Fatal("failed tags must not manufacture an outcome", err, memory.eventTypes())
	}
}

func TestTagEvaluationDoesNotManufacturePredictionsAndStopsAfterRecordFailure(t *testing.T) {
	for _, fail := range []bool{false, true} {
		memory := &recordingMemory{answers: defaultAnswers()}
		if fail {
			memory.errors = map[string]error{"memory.record": errors.New("offline")}
		}
		synchronous(memory, nil).TagEvaluated(testMemeURL, HashtagAttempt{Hashtag: "#Humor", QueryText: testMemeText}, "#Humor")
		want := "memory.record,memory.get,memory.resolve"
		if fail {
			want = "memory.record"
		}
		if got := strings.Join(memory.eventTypes(), ","); got != want {
			t.Fatalf("optional memory failure must not create feedback or retry prediction: %s", got)
		}
		if recordsOf(memory)[0]["predicted_p"] != nil {
			t.Fatal("missing snapshot became a prediction")
		}
	}
}

func TestHashtagTaskUsesDesktopWrittenTextWithoutRequiringOCR(t *testing.T) {
	memory := &recordingMemory{answers: defaultAnswers()}
	task := NewHashtagTask(NewMemeHashtagWorkflow(nil, relevantKnowledge(true)), memory, nil)
	event := taskEventFor()
	event.Payload = map[string]any{"text": testMemeText}
	attempt := task.Suggest(context.Background(), event, testMemeURL)
	if attempt.Hashtag != "#Humor" || attempt.QueryText != testMemeText || attempt.Prediction == nil {
		t.Fatalf("explicit desktop text must support pre-save evaluation without OCR: %#v", attempt)
	}
}
