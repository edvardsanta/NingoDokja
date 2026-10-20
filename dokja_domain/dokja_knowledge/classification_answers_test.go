package knowledge

import (
	"context"
	"reflect"
	"strings"
	"testing"
)

type metadataService struct {
	calls   []string
	payload map[string]any
}

func (s *metadataService) Dispatch(_ context.Context, eventType string, payload map[string]any) (map[string]any, error) {
	s.calls = append(s.calls, eventType)
	s.payload = payload
	metadata := map[string]any{"form": "book", "content_kind": nil}
	if eventType == "knowledge.metadata.patch" {
		for k, v := range payload["patch"].(map[string]any) {
			metadata[k] = v
		}
	}
	return map[string]any{"source_id": "study", "metadata": metadata, "revision": strings.Repeat("a", 64)}, nil
}
func TestClassifyQueriesStoredQuestionsWithoutReadingOrEmbedding(t *testing.T) {
	service := &metadataService{}
	result, err := New(service).Handle(context.Background(), Request{Action: ActionClassifyKnowledge, Event: Event{Payload: map[string]any{"source_id": "study"}}})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(service.calls, []string{"knowledge.metadata.get"}) || len(service.payload) != 1 {
		t.Fatalf("service=%+v", service)
	}
	classification := result["classification"].(map[string]any)
	if !reflect.DeepEqual(classification["missing_axes"], []string{"content_kind"}) {
		t.Fatalf("result=%#v", result)
	}
}
func TestClassifyPatchesOnlyExplicitUserAnswersWithARevision(t *testing.T) {
	service := &metadataService{}
	result, err := New(service).Handle(context.Background(), Request{Action: ActionClassifyKnowledge, Event: Event{Payload: map[string]any{
		"source_id": "study", "revision": strings.Repeat("a", 64), "answers": map[string]any{"content_kind": " RESEARCH "},
	}}})
	if err != nil {
		t.Fatal(err)
	}
	patch := service.payload["patch"].(map[string]any)
	if !reflect.DeepEqual(service.calls, []string{"knowledge.metadata.patch"}) || len(patch) != 2 || patch["content_kind"] != "research" || !reflect.DeepEqual(patch["kind_choice"], map[string]any{"origin": "user"}) {
		t.Fatalf("payload=%#v", service.payload)
	}
	if result["classification"].(map[string]any)["status"] != "classified" || result["metadata"].(map[string]any)["form"] != "book" {
		t.Fatalf("result=%#v", result)
	}
}
func TestClassifyRejectsInvalidAnswersBeforeCallingStorage(t *testing.T) {
	for _, answers := range []any{nil, []string{"book"}, map[string]any{}, map[string]any{"form": nil}, map[string]any{"form": "general"}, map[string]any{"content_kind": ""}, map[string]any{"topics": []string{"x"}}, map[string]any{"form_choice": map[string]any{"origin": "user"}}} {
		service := &metadataService{}
		_, err := New(service).Handle(context.Background(), Request{Action: ActionClassifyKnowledge, Event: Event{Payload: map[string]any{"source_id": "study", "answers": answers, "revision": strings.Repeat("a", 64)}}})
		if err == nil || len(service.calls) != 0 {
			t.Fatalf("accepted answers=%#v calls=%v error=%v", answers, service.calls, err)
		}
	}
	for _, payload := range []map[string]any{
		{}, {"source_id": "study", "answers": map[string]any{"form": "book"}},
		{"source_id": "study", "answers": map[string]any{"form": "book"}, "revision": "old"},
		{"source_id": "study", "revision": strings.Repeat("a", 64)}, {"source_id": "study", "body": "replacement"},
	} {
		service := &metadataService{}
		if _, err := New(service).Handle(context.Background(), Request{Action: ActionClassifyKnowledge, Event: Event{Payload: payload}}); err == nil || len(service.calls) != 0 {
			t.Fatalf("accepted payload=%#v", payload)
		}
	}
}
