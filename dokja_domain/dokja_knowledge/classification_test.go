package knowledge

import (
	"reflect"
	"strings"
	"testing"
)

func TestClassificationSeparatesFormFromContentAndTracksEachChoice(t *testing.T) {
	input := ClassificationInput{
		Explicit:      ClassificationValues{Form: FormBook},
		Classifier:    &ClassifierProposal{Values: ClassificationValues{Form: FormArticle, ContentKind: ContentResearch}, ID: "local-rules", Version: "1"},
		SourceDefault: ClassificationValues{Form: FormArticle, ContentKind: ContentNews},
	}
	got, err := ResolveClassification(input)
	if err != nil {
		t.Fatal(err)
	}
	if got.Form != FormBook || got.ContentKind != ContentResearch {
		t.Fatalf("a research book must keep both axes: %+v", got)
	}
	if got.FormChoice != (ClassificationChoice{Origin: OriginUser}) ||
		got.KindChoice != (ClassificationChoice{Origin: OriginClassifier, ClassifierID: "local-rules", ClassifierVersion: "1"}) {
		t.Fatalf("axis provenance was conflated: %+v", got)
	}
	if input.Classifier.Values.Form != FormArticle || input.SourceDefault.ContentKind != ContentNews {
		t.Fatal("resolution modified the supplied candidates")
	}
}

func TestClassificationPrecedenceAndProviderFallback(t *testing.T) {
	cases := []struct {
		name  string
		input ClassificationInput
		want  Classification
	}{
		{name: "cold start", want: Classification{Form: FormGeneral, ContentKind: ContentGeneral,
			FormChoice: ClassificationChoice{Origin: OriginGeneral}, KindChoice: ClassificationChoice{Origin: OriginGeneral}}},
		{name: "source default", input: ClassificationInput{SourceDefault: ClassificationValues{Form: FormArticle, ContentKind: ContentNews}},
			want: Classification{Form: FormArticle, ContentKind: ContentNews,
				FormChoice: ClassificationChoice{Origin: OriginSource}, KindChoice: ClassificationChoice{Origin: OriginSource}}},
		{name: "explicit general is not missing", input: ClassificationInput{Explicit: ClassificationValues{Form: FormGeneral, ContentKind: ContentGeneral},
			SourceDefault: ClassificationValues{Form: FormBook, ContentKind: ContentResearch}},
			want: Classification{Form: FormGeneral, ContentKind: ContentGeneral,
				FormChoice: ClassificationChoice{Origin: OriginUser}, KindChoice: ClassificationChoice{Origin: OriginUser}}},
		{name: "normalize user values", input: ClassificationInput{Explicit: ClassificationValues{Form: " BOOK ", ContentKind: " Reference "}},
			want: Classification{Form: FormBook, ContentKind: ContentReference,
				FormChoice: ClassificationChoice{Origin: OriginUser}, KindChoice: ClassificationChoice{Origin: OriginUser}}},
		{name: "invalid classifier kind falls back independently", input: ClassificationInput{
			Classifier:    &ClassifierProposal{Values: ClassificationValues{Form: FormBook, ContentKind: "invented"}, ID: "rules", Version: "2"},
			SourceDefault: ClassificationValues{ContentKind: ContentGeneral}},
			want: Classification{Form: FormBook, ContentKind: ContentGeneral,
				FormChoice: ClassificationChoice{Origin: OriginClassifier, ClassifierID: "rules", ClassifierVersion: "2"},
				KindChoice: ClassificationChoice{Origin: OriginSource, FallbackReason: "classifier_value_invalid"}}},
		{name: "provider without version cannot be attributed", input: ClassificationInput{
			Classifier: &ClassifierProposal{Values: ClassificationValues{Form: FormBook, ContentKind: ContentResearch}, ID: "rules"}},
			want: Classification{Form: FormGeneral, ContentKind: ContentGeneral,
				FormChoice: ClassificationChoice{Origin: OriginGeneral, FallbackReason: "classifier_identity_missing"},
				KindChoice: ClassificationChoice{Origin: OriginGeneral, FallbackReason: "classifier_identity_missing"}}},
		{name: "partial proposal does not erase source form", input: ClassificationInput{
			Classifier:    &ClassifierProposal{Values: ClassificationValues{ContentKind: ContentNarrative}, ID: "rules", Version: "1"},
			SourceDefault: ClassificationValues{Form: FormBook}},
			want: Classification{Form: FormBook, ContentKind: ContentNarrative,
				FormChoice: ClassificationChoice{Origin: OriginSource},
				KindChoice: ClassificationChoice{Origin: OriginClassifier, ClassifierID: "rules", ClassifierVersion: "1"}}},
		{name: "explicit content survives classifier", input: ClassificationInput{
			Explicit:   ClassificationValues{ContentKind: ContentReference},
			Classifier: &ClassifierProposal{Values: ClassificationValues{Form: FormBook, ContentKind: ContentNarrative}, ID: "rules", Version: "1"}},
			want: Classification{Form: FormBook, ContentKind: ContentReference,
				FormChoice: ClassificationChoice{Origin: OriginClassifier, ClassifierID: "rules", ClassifierVersion: "1"},
				KindChoice: ClassificationChoice{Origin: OriginUser}}},
		{name: "invalid provider form keeps valid content", input: ClassificationInput{
			Classifier: &ClassifierProposal{Values: ClassificationValues{Form: "pdf", ContentKind: ContentResearch}, ID: "rules", Version: "1"}},
			want: Classification{Form: FormGeneral, ContentKind: ContentResearch,
				FormChoice: ClassificationChoice{Origin: OriginGeneral, FallbackReason: "classifier_value_invalid"},
				KindChoice: ClassificationChoice{Origin: OriginClassifier, ClassifierID: "rules", ClassifierVersion: "1"}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ResolveClassification(tc.input)
			if err != nil || !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("got=%+v err=%v want=%+v", got, err, tc.want)
			}
		})
	}
}

func TestClassificationRejectsInvalidUserAndSourceValues(t *testing.T) {
	cases := []struct {
		input ClassificationInput
		want  string
	}{
		{ClassificationInput{Explicit: ClassificationValues{Form: "pdf"}}, "unsupported explicit form"},
		{ClassificationInput{Explicit: ClassificationValues{ContentKind: "book"}}, "unsupported explicit content kind"},
		{ClassificationInput{SourceDefault: ClassificationValues{Form: "epub"}}, "unsupported source-default form"},
		{ClassificationInput{SourceDefault: ClassificationValues{ContentKind: "article"}}, "unsupported source-default content kind"},
	}
	for _, tc := range cases {
		got, err := ResolveClassification(tc.input)
		if err == nil || !strings.Contains(err.Error(), tc.want) || got != (Classification{}) {
			t.Fatalf("invalid candidate must not become a valid result: got=%+v err=%v want=%s", got, err, tc.want)
		}
	}
}

func TestClassificationSameContentAcrossFormsRetainsTheSameSubject(t *testing.T) {
	for _, form := range []Form{FormBook, FormArticle, FormNote} {
		got, err := ResolveClassification(ClassificationInput{
			Explicit: ClassificationValues{Form: form, ContentKind: ContentResearch},
		})
		if err != nil || got.Form != form || got.ContentKind != ContentResearch {
			t.Fatalf("source form changed the research label: %+v err=%v", got, err)
		}
	}
}
