package memory

import (
	"context"
	"math"
	"reflect"
	"strings"
	"testing"
	"unicode/utf8"
)

const hashtagSource = "https://media.example/a.png"
const hashtagRef = "hashtag:4286467e0b88473247929fa128ef8c24"

func TestHashtagExperiencePreservesLegacyIdentityAndPayload(t *testing.T) {
	observation, ok := PrepareHashtagExperience("  "+hashtagSource+" ", " words ", " #Humor ", HashtagMatch{Score: 0.83, Text: " earlier words "})
	if !ok {
		t.Fatal("a complete observation was refused")
	}
	if HashtagExperienceRef(hashtagSource) != hashtagRef || HashtagExperienceRef(hashtagSource+"?x=1") == hashtagRef {
		t.Fatal("media identity changed")
	}
	request := observation.RecordRequest(&HashtagPrediction{PredictedP: 0.7, BaselineP: 0.6})
	want := map[string]any{
		"ref": hashtagRef, "action": "hashtag.suggest", "context": "words", "detail": "#Humor",
		"matched_score": 0.83, "matched_context": "earlier words", "predicted_p": 0.7, "baseline_p": 0.6,
	}
	if request.Action != ActionRecordExperience || request.Event.Source != "orchestrator" || !reflect.DeepEqual(request.Event.Payload, want) {
		t.Fatalf("record request: %#v", request)
	}
	service := &fakeService{}
	if _, err := New(service).Handle(context.Background(), request); err != nil {
		t.Fatal(err)
	}
	if len(service.sent) != 1 || service.sent[0].eventType != "memory.record" || !reflect.DeepEqual(service.sent[0].payload, want) {
		t.Fatalf("stored observation: %#v", service.sent)
	}
}

func TestIncompleteHashtagNotesAreOptional(t *testing.T) {
	for _, input := range [][3]string{{"", "words", "#Humor"}, {hashtagSource, " \n ", "#Humor"}, {hashtagSource, "words", " "}} {
		if _, ok := PrepareHashtagExperience(input[0], input[1], input[2], HashtagMatch{}); ok {
			t.Fatalf("prepared incomplete observation: %q", input)
		}
	}
	for _, input := range [][2]string{{"", "#Humor"}, {hashtagSource, " "}} {
		if _, ok := HashtagFeedbackRequest(input[0], input[1]); ok {
			t.Fatalf("prepared incomplete feedback: %q", input)
		}
	}
	// The generic validator also refuses an unprepared zero value without effects.
	service := &fakeService{}
	if _, err := New(service).Handle(context.Background(), (HashtagExperience{}).RecordRequest(nil)); err == nil || len(service.sent) != 0 {
		t.Fatalf("unprepared observation reached storage: err=%v calls=%v", err, service.sent)
	}
}

func TestHashtagEvidenceBoundsAndAbsentScores(t *testing.T) {
	for _, score := range []float64{0, -0.2, math.NaN(), 0.83, 1.0000001, math.Inf(1)} {
		observation, _ := PrepareHashtagExperience(hashtagSource, strings.Repeat("é", 5000), "#Humor", HashtagMatch{
			Score: score, Text: strings.Repeat("界", 5000),
		})
		record := observation.RecordRequest(nil).Event.Payload
		if utf8.RuneCountInString(record["context"].(string)) != 4000 || utf8.RuneCountInString(record["matched_context"].(string)) != 200 {
			t.Fatal("evidence bounds must count Unicode characters")
		}
		value, present := record["matched_score"]
		if score > 0 {
			if !present || value != math.Min(score, 1) {
				t.Fatalf("positive score %v: %v", score, record)
			}
		} else if present {
			t.Fatalf("absent score %v was invented: %v", score, value)
		}
		if _, present := record["predicted_p"]; present {
			t.Fatal("an unscored observation gained a prediction")
		}
	}
}

func TestHashtagRequestsDoNotShareMutablePayloadsOrPredictions(t *testing.T) {
	observation, _ := PrepareHashtagExperience(hashtagSource, "words", "#Humor", HashtagMatch{})
	snapshot := &HashtagPrediction{PredictedP: 0.7, BaselineP: 0.6}
	first := observation.RecordRequest(snapshot)
	snapshot.PredictedP = 0.1
	if first.Event.Payload["predicted_p"] != 0.7 {
		t.Fatal("a later prediction edit changed a prepared record")
	}
	first.Event.Payload["context"] = "edited"
	second := observation.RecordRequest(nil)
	if second.Event.Payload["context"] != "words" || second.Event.Payload["predicted_p"] != nil {
		t.Fatalf("requests share payloads: %v", second.Event.Payload)
	}
	for _, key := range []string{"matched_score", "matched_context"} {
		if _, present := second.Event.Payload[key]; present {
			t.Fatalf("missing evidence %s was invented", key)
		}
	}
}

func TestHashtagPredictionUsesTheSameBoundedContext(t *testing.T) {
	text := "  " + strings.Repeat("é", 5000) + "  "
	observation, _ := PrepareHashtagExperience(hashtagSource, text, "#Humor", HashtagMatch{})
	request := observation.PredictionRequest()
	if request.Action != ActionPredictExperience || request.Event.Source != "orchestrator" || !reflect.DeepEqual(request, HashtagPredictionRequest(text)) {
		t.Fatalf("inconsistent prediction request: %#v", request)
	}
	if request.Event.Payload["action"] != "hashtag.suggest" || request.Event.Payload["context"] != observation.RecordRequest(nil).Event.Payload["context"] {
		t.Fatal("prediction and recording context differ")
	}
}

func TestHashtagFeedbackResolvesTheExistingIdentity(t *testing.T) {
	for _, tc := range []struct{ label, outcome string }{{" #humor ", OutcomeAccepted}, {"#Other", OutcomeReplaced}} {
		request, ok := HashtagFeedbackRequest(" "+hashtagSource+" ", tc.label)
		if !ok || request.Action != ActionResolveExperience || request.Event.Source != "orchestrator" {
			t.Fatalf("feedback request: %#v", request)
		}
		service := &fakeService{answers: map[string]map[string]any{
			"memory.get": {"found": true, "detail": "#Humor"}, "memory.resolve": {"resolved": true},
		}}
		if _, err := New(service).Handle(context.Background(), request); err != nil {
			t.Fatal(err)
		}
		if len(service.sent) != 2 || service.sent[0].eventType != "memory.get" || service.sent[1].eventType != "memory.resolve" || service.sent[1].payload["ref"] != hashtagRef || service.sent[1].payload["outcome"] != tc.outcome {
			t.Fatalf("feedback sequence: %#v", service.sent)
		}
	}
}

func TestHashtagFeedbackDoesNotCreateAMissingExperience(t *testing.T) {
	request, _ := HashtagFeedbackRequest(hashtagSource, "#Humor")
	service := &fakeService{}
	answer, err := New(service).Handle(context.Background(), request)
	if err != nil || answer["found"] != false || answer["resolved"] != false || len(service.sent) != 1 || service.sent[0].eventType != "memory.get" {
		t.Fatalf("missing experience gained feedback: answer=%v err=%v calls=%v", answer, err, service.sent)
	}
}

func TestPreparedHashtagRecordsStillUseGenericValidation(t *testing.T) {
	for _, tc := range []struct {
		label string
		score *HashtagPrediction
	}{
		{"#bad\nlabel", nil},
		{strings.Repeat("x", 201), nil},
		{"#Humor", &HashtagPrediction{PredictedP: math.NaN(), BaselineP: 0.5}},
	} {
		observation, _ := PrepareHashtagExperience(hashtagSource, "words", tc.label, HashtagMatch{})
		service := &fakeService{}
		if _, err := New(service).Handle(context.Background(), observation.RecordRequest(tc.score)); err == nil || len(service.sent) != 0 {
			t.Fatalf("invalid record reached storage: label=%q err=%v calls=%v", tc.label, err, service.sent)
		}
	}
}
