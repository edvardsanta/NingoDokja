package knowledge

import (
	"context"
	"reflect"
	"testing"
)

func TestReclassificationRevisesOnlyAutomaticAxes(t *testing.T) {
	for _, origin := range []string{"user", "source_default", "user_required"} {
		t.Run(origin, func(t *testing.T) {
			original := map[string]any{
				"form": "book", "form_choice": map[string]any{"origin": origin},
				"content_kind": "news", "kind_choice": map[string]any{"origin": "classifier", "classifier_id": "old", "classifier_version": "1"},
				"topics": []any{"physics"},
			}
			service := &patchingService{metadata: map[string]map[string]any{"study": original}, revision: map[string]int{"study": 1}}
			snapshot := &Reclassification{snapshot: map[string]any{"source_id": "study", "metadata": original, "revision": 1}}
			result, err := New(service).KeepReclassification(context.Background(), snapshot, ReadingProposal{Form: "article", ContentKind: "research", Topics: []string{"other"}}, "new", "2")
			if err != nil {
				t.Fatal(err)
			}
			metadata := result["metadata"].(map[string]any)
			if metadata["form"] != "book" || !reflect.DeepEqual(metadata["form_choice"], original["form_choice"]) || metadata["content_kind"] != "research" || !reflect.DeepEqual(metadata["topics"], original["topics"]) {
				t.Fatalf("metadata=%#v", metadata)
			}
			if original["content_kind"] != "news" {
				t.Fatal("snapshot mutated")
			}
		})
	}
}

func TestReclassificationKeepsThePreInferenceRevision(t *testing.T) {
	original := map[string]any{"form": "article", "form_choice": map[string]any{"origin": "classifier"}}
	service := &patchingService{metadata: map[string]map[string]any{"study": original}, revision: map[string]int{"study": 2}}
	snapshot := &Reclassification{snapshot: map[string]any{"source_id": "study", "metadata": original, "revision": 1}}
	if _, err := New(service).KeepReclassification(context.Background(), snapshot, ReadingProposal{Form: "book"}, "new", "2"); err == nil {
		t.Fatal("stale classification accepted")
	}
	if len(service.patches) != 0 {
		t.Fatal("stale patch written")
	}
}

func TestReclassificationDoesNotResolveQuestionsAlreadyPutToUser(t *testing.T) {
	snapshot := &Reclassification{snapshot: map[string]any{"metadata": map[string]any{
		"form_choice": map[string]any{"origin": "user"},
		"kind_choice": map[string]any{"origin": "user_required", "fallback_reason": "user_clarification_required"},
	}}}
	if snapshot.Eligible() {
		t.Fatal("user-only decisions eligible for inference")
	}
}
