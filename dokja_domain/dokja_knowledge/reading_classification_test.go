package knowledge

import (
	"context"
	"errors"
	"reflect"
	"testing"
)

// patchingService keeps one metadata object per source and applies revision-checked patches.
type patchingService struct {
	metadata map[string]map[string]any
	revision map[string]int
	patches  []map[string]any
	conflict string
}

func (s *patchingService) Dispatch(_ context.Context, eventType string, payload map[string]any) (map[string]any, error) {
	id, _ := payload["source_id"].(string)
	switch eventType {
	case "knowledge.metadata.get":
		return map[string]any{"source_id": id, "metadata": s.metadata[id], "revision": s.revision[id]}, nil
	case "knowledge.metadata.patch":
		if id == s.conflict || payload["revision"] != s.revision[id] {
			return nil, errors.New("classification_conflict: document changed; reload the questions")
		}
		patch := payload["patch"].(map[string]any)
		s.patches = append(s.patches, patch)
		merged := map[string]any{}
		for key, value := range s.metadata[id] {
			merged[key] = value
		}
		for key, value := range patch {
			merged[key] = value
		}
		s.metadata[id] = merged
		s.revision[id]++
		return map[string]any{"source_id": id, "metadata": merged}, nil
	}
	return nil, errors.New("unexpected " + eventType)
}

func pending() map[string]any {
	return map[string]any{"origin": "user_required", "fallback_reason": "classification_missing"}
}

func storedDocument(id string, changed bool, metadata map[string]any) map[string]any {
	return map[string]any{"source_id": id, "changed": changed, "metadata": metadata}
}

func TestClassificationCandidatesAreContentNoClassifierSawWithUndecidedAxes(t *testing.T) {
	stored := []map[string]any{
		storedDocument("new", true, map[string]any{"form": nil, "form_choice": pending(), "content_kind": nil, "kind_choice": pending()}),
		storedDocument("seen", false, map[string]any{"form": nil, "form_choice": pending(), "content_kind": nil, "kind_choice": pending(),
			"classifier": map[string]any{"id": "kinds", "version": "1"}}),
		storedDocument("decided", true, map[string]any{"form": "book", "form_choice": map[string]any{"origin": "source_default"},
			"content_kind": "research", "kind_choice": map[string]any{"origin": "user"}}),
		storedDocument("epub", true, map[string]any{"form": "book", "form_choice": map[string]any{"origin": "source_default"},
			"content_kind": nil, "kind_choice": pending()}),
		storedDocument("asked", true, map[string]any{"form": nil, "form_choice": map[string]any{"origin": "user_required", "fallback_reason": "user_clarification_required"},
			"content_kind": "news", "kind_choice": map[string]any{"origin": "user"}}),
		storedDocument("note", true, map[string]any{"form": "note", "form_choice": map[string]any{"origin": "user"}, "content_kind": nil, "kind_choice": pending()}),
		storedDocument("chat", true, map[string]any{"form": "conversation", "form_choice": map[string]any{"origin": "user"}, "content_kind": nil, "kind_choice": pending()}),
		{"source_id": "no metadata", "changed": true},
		storedDocument("stored before, never classified", false, map[string]any{"form": nil, "form_choice": pending(), "content_kind": nil, "kind_choice": pending()}),
	}
	if got := ClassificationCandidates(stored); !reflect.DeepEqual(got, []int{0, 3, 5, 6, 8}) {
		t.Fatalf("candidates=%v, want content no classifier has seen with an undecided axis, notes and conversations included", got)
	}
}

func TestApplyClassificationFillsOnlyUndecidedFieldsWithProvenance(t *testing.T) {
	service := &patchingService{
		metadata: map[string]map[string]any{"file:a.epub": {
			"form": "book", "form_choice": map[string]any{"origin": "source_default"},
			"content_kind": nil, "kind_choice": pending(), "topics": []any{"physics"},
		}},
		revision: map[string]int{"file:a.epub": 1},
	}
	stored := []map[string]any{storedDocument("file:a.epub", true, service.metadata["file:a.epub"])}
	err := New(service).ApplyClassification(context.Background(), stored, []int{0}, []ReadingProposal{{
		Form: "article", ContentKind: " Research ", Topics: []string{"optics"},
		Entities: []Entity{{Name: "Newton", Type: "person"}, {Name: " "}}, DateAnchor: "1704-01-01",
	}}, "kinds", "2")
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]any{
		"content_kind": "research",
		"kind_choice":  map[string]any{"origin": "classifier", "classifier_id": "kinds", "classifier_version": "2"},
		"entities":     []map[string]any{{"name": "Newton", "type": "person"}},
		"date_anchor":  "1704-01-01",
		"classifier":   map[string]any{"id": "kinds", "version": "2"},
	}
	if !reflect.DeepEqual(service.patches[0], want) {
		t.Fatalf("patch=%#v\nwant  %#v", service.patches[0], want)
	}
	classification := stored[0]["classification"].(map[string]any)
	if stored[0]["classified"] != true || classification["status"] != "classified" {
		t.Fatalf("the reply must carry the effective metadata: %#v", stored[0])
	}
}

func TestApplyClassificationLeavesUncertainOrInvalidAxesToTheUser(t *testing.T) {
	service := &patchingService{
		metadata: map[string]map[string]any{"doc": {"form": nil, "form_choice": pending(), "content_kind": nil, "kind_choice": pending()}},
		revision: map[string]int{"doc": 1},
	}
	stored := []map[string]any{storedDocument("doc", true, service.metadata["doc"])}
	err := New(service).ApplyClassification(context.Background(), stored, []int{0},
		[]ReadingProposal{{Form: "general", ContentKind: "news", UncertainContentKind: true}}, "kinds", "1")
	if err != nil {
		t.Fatal(err)
	}
	patch := service.patches[0]
	if patch["form"] != nil || patch["content_kind"] != nil ||
		patch["form_choice"].(map[string]any)["fallback_reason"] != "classifier_value_invalid" ||
		patch["kind_choice"].(map[string]any)["fallback_reason"] != "user_clarification_required" {
		t.Fatalf("an unknown label or an unsure axis is a question for the user: %#v", patch)
	}
	questions := stored[0]["classification"].(map[string]any)
	if questions["needs_user_input"] != true {
		t.Fatalf("questions=%#v", questions)
	}
}

func TestApplyClassificationNeverOverwritesAConcurrentAnswer(t *testing.T) {
	service := &patchingService{
		metadata: map[string]map[string]any{
			"a": {"form": nil, "form_choice": pending(), "content_kind": nil, "kind_choice": pending()},
			"b": {"form": nil, "form_choice": pending(), "content_kind": nil, "kind_choice": pending()},
		},
		revision: map[string]int{"a": 1, "b": 1},
		conflict: "a",
	}
	stored := []map[string]any{storedDocument("a", true, service.metadata["a"]), storedDocument("b", true, service.metadata["b"])}
	err := New(service).ApplyClassification(context.Background(), stored, []int{0, 1},
		[]ReadingProposal{{Form: "book"}, {Form: "article"}}, "kinds", "1")
	if err == nil || len(service.patches) != 1 || service.metadata["b"]["form"] != "article" || stored[0]["classified"] != nil {
		t.Fatalf("a conflict skips that document and the others are still classified: err=%v patches=%v", err, service.patches)
	}
}

func TestApplyClassificationRefusesProposalsItCannotAttribute(t *testing.T) {
	service := &patchingService{metadata: map[string]map[string]any{}, revision: map[string]int{}}
	stored := []map[string]any{storedDocument("a", true, map[string]any{})}
	domain := New(service)
	if err := domain.ApplyClassification(context.Background(), stored, []int{0}, nil, "kinds", "1"); err == nil {
		t.Fatal("a missing proposal must be refused")
	}
	if err := domain.ApplyClassification(context.Background(), stored, []int{0}, []ReadingProposal{{}}, "kinds", " "); err == nil {
		t.Fatal("a classifier without a version must be refused")
	}
	if len(service.patches) != 0 {
		t.Fatal("nothing may be written")
	}
}
