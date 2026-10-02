package handlers

import (
	"context"
	"errors"
	"math"
	"read_books/internal/clients"
	"read_books/internal/core"
	"read_books/internal/infrastructure/repreq"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"
)

type memoryCall struct {
	eventType string
	payload   map[string]any
}

// recordingMemory is a memory service that remembers every call and answers from canned results.
type recordingMemory struct {
	mu      sync.Mutex
	calls   []memoryCall
	answers map[string]map[string]any
	errors  map[string]error
	gate    chan struct{}
}

func (r *recordingMemory) Dispatch(_ context.Context, eventType string, payload map[string]any) (map[string]any, error) {
	if r.gate != nil {
		<-r.gate
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.calls = append(r.calls, memoryCall{eventType, payload})
	if err := r.errors[eventType]; err != nil {
		return nil, err
	}
	return r.answers[eventType], nil
}

func (r *recordingMemory) sent() []memoryCall {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]memoryCall(nil), r.calls...)
}

func (r *recordingMemory) eventTypes() []string {
	var out []string
	for _, call := range r.sent() {
		out = append(out, call.eventType)
	}
	return out
}

func synchronous(service *recordingMemory, enabled func() bool) *HashtagExperience {
	experience := NewHashtagExperience(service, enabled)
	experience.spawn = func(work func()) { work() }
	return experience
}

func defaultAnswers() map[string]map[string]any {
	return map[string]map[string]any{
		"memory.recall":  {"neighbors": []any{}, "outcomes": map[string]any{"accepted": float64(6), "replaced": float64(4)}},
		"memory.record":  {"created": true},
		"memory.get":     {"found": true, "detail": "#Humor"},
		"memory.resolve": {"found": true, "resolved": true},
	}
}

const (
	testMemeURL  = "https://media.example/a.png"
	testMemeText = "words in the image"
)

func TestSuggestionIsRecordedWithItsPredictionAndWithoutTheAddress(t *testing.T) {
	service := &recordingMemory{answers: defaultAnswers()}

	synchronous(service, nil).Suggested(testMemeURL, "  "+testMemeText+" ", "#Humor")

	if got := strings.Join(service.eventTypes(), ","); got != "memory.recall,memory.record" {
		t.Fatalf("a suggestion is predicted, then recorded: %s", got)
	}
	record := service.sent()[1].payload
	if !regexp.MustCompile(`^hashtag:[0-9a-f]{32}$`).MatchString(record["ref"].(string)) || strings.Contains(record["ref"].(string), "media") {
		t.Fatalf("the ref must identify the meme without keeping its address: %v", record["ref"])
	}
	if record["action"] != "hashtag.suggest" || record["context"] != testMemeText || record["detail"] != "#Humor" {
		t.Fatalf("record %v", record)
	}
	// 6 accepted and 4 replaced give the baseline (6+1)/(10+2); with no similar experience yet the
	// prediction is that baseline.
	for _, key := range []string{"predicted_p", "baseline_p"} {
		if math.Abs(record[key].(float64)-7.0/12.0) > 1e-9 {
			t.Fatalf("%s = %v, want %v", key, record[key], 7.0/12.0)
		}
	}
}

func TestSuggestionIsStillRecordedWhenItCannotBePredicted(t *testing.T) {
	service := &recordingMemory{answers: defaultAnswers(), errors: map[string]error{"memory.recall": errors.New("offline")}}

	synchronous(service, nil).Suggested(testMemeURL, testMemeText, "#Humor")

	if got := strings.Join(service.eventTypes(), ","); got != "memory.recall,memory.record" {
		t.Fatalf("expected the record to follow the failed prediction: %s", got)
	}
	record := service.sent()[1].payload
	if _, scored := record["predicted_p"]; scored {
		t.Fatalf("an experience with no prediction must not invent one: %v", record)
	}
}

func TestMemoryFailuresNeverEscape(t *testing.T) {
	service := &recordingMemory{answers: defaultAnswers(), errors: map[string]error{
		"memory.record": errors.New("disk full"), "memory.get": errors.New("offline"),
	}}
	experience := synchronous(service, nil)

	experience.Suggested(testMemeURL, testMemeText, "#Humor")
	experience.Tagged(testMemeURL, "#Humor")

	// The suggestion is predicted and its record fails; the tag finds no experience to compare with.
	if got := strings.Join(service.eventTypes(), ","); got != "memory.recall,memory.record,memory.get" {
		t.Fatalf("both notes should have been attempted and stopped at their failure: %s", got)
	}
}

func TestNotesDoNothingWhenDisabledOrIncomplete(t *testing.T) {
	service := &recordingMemory{answers: defaultAnswers()}

	off := synchronous(service, func() bool { return false })
	off.Suggested(testMemeURL, testMemeText, "#Humor")
	off.Tagged(testMemeURL, "#Humor")

	on := synchronous(service, func() bool { return true })
	on.Suggested("", testMemeText, "#Humor")
	on.Suggested(testMemeURL, "   ", "#Humor")
	on.Suggested(testMemeURL, testMemeText, " ")
	on.Tagged("", "#Humor")
	on.Tagged(testMemeURL, "")

	var missing *HashtagExperience
	missing.Suggested(testMemeURL, testMemeText, "#Humor")
	missing.Tagged(testMemeURL, "#Humor")

	if len(service.sent()) != 0 {
		t.Fatalf("nothing should reach the memory: %v", service.eventTypes())
	}
}

func TestVeryLongTextIsCutToWhatTheMemoryAccepts(t *testing.T) {
	service := &recordingMemory{answers: defaultAnswers()}

	synchronous(service, nil).Suggested(testMemeURL, strings.Repeat("é", 5000), "#Humor")

	record := service.sent()[1].payload
	if got := len([]rune(record["context"].(string))); got != maxExperienceText {
		t.Fatalf("the context should be cut to %d characters, got %d", maxExperienceText, got)
	}
}

func TestTaggingResolvesTheSuggestionByWhatWasObserved(t *testing.T) {
	for name, tc := range map[string]struct{ observed, outcome string }{
		"the same hashtag": {"#humor", "accepted"},
		"another hashtag":  {"#Other", "replaced"},
	} {
		service := &recordingMemory{answers: defaultAnswers()}

		synchronous(service, nil).Tagged(testMemeURL, tc.observed)

		if got := strings.Join(service.eventTypes(), ","); got != "memory.get,memory.resolve" {
			t.Fatalf("%s: %s", name, got)
		}
		resolve := service.sent()[1].payload
		if resolve["ref"] != experienceRef(testMemeURL) || resolve["outcome"] != tc.outcome {
			t.Fatalf("%s: resolved %v, want outcome %s", name, resolve, tc.outcome)
		}
	}
}

func TestTheSameAddressAlwaysGivesTheSameRef(t *testing.T) {
	if experienceRef(testMemeURL) != experienceRef("  "+testMemeURL+" ") || experienceRef(testMemeURL) == experienceRef(testMemeURL+"?x=1") {
		t.Fatal("a suggestion and the tag that follows it must meet at one ref, and different memes must not")
	}
}

func TestNotesRunInTheBackgroundSoADeliveryIsNeverHeldUp(t *testing.T) {
	service := &recordingMemory{answers: defaultAnswers(), gate: make(chan struct{})}
	experience := NewHashtagExperience(service, nil)

	returned := make(chan struct{})
	go func() {
		experience.Suggested(testMemeURL, testMemeText, "#Humor")
		close(returned)
	}()
	select {
	case <-returned:
	case <-time.After(time.Second):
		t.Fatal("a slow memory must not hold the caller")
	}

	close(service.gate)
	deadline := time.After(2 * time.Second)
	for len(service.sent()) < 2 {
		select {
		case <-deadline:
			t.Fatalf("the work should finish in the background: %v", service.eventTypes())
		case <-time.After(5 * time.Millisecond):
		}
	}
}

func TestNothingPrivateIsLogged(t *testing.T) {
	logs := captureLogs(t)
	service := &recordingMemory{answers: defaultAnswers()}
	experience := synchronous(service, nil)

	experience.Suggested(testMemeURL, testMemeText, "#Humor")
	experience.Tagged(testMemeURL, "#Humor")

	for _, secret := range []string{testMemeText, "media.example", "#Humor"} {
		if strings.Contains(logs.String(), secret) {
			t.Fatalf("%q is the user's own material and must not be logged: %q", secret, logs.String())
		}
	}
	for _, want := range []string{"hashtag experience recorded", "hashtag experience resolved"} {
		if !strings.Contains(logs.String(), want) {
			t.Fatalf("the log should still say what happened (otherwise this test proves nothing), missing %q in %q", want, logs.String())
		}
	}
}

// ---- how the handlers feed the notes -----------------------------------------------------

type recordedNotes struct {
	suggested [][3]string
	tagged    [][2]string
}

func (r *recordedNotes) Suggested(url, text, hashtag string) {
	r.suggested = append(r.suggested, [3]string{url, text, hashtag})
}

func (r *recordedNotes) Tagged(url, hashtag string) {
	r.tagged = append(r.tagged, [2]string{url, hashtag})
}

func TestALearnedHashtagIsNotedOnlyWhenItIsAppended(t *testing.T) {
	for _, tc := range []struct {
		name   string
		result map[string]any
		err    error
		noted  bool
	}{
		{"relevant", map[string]any{"hashtag": "#Humor", "relevant": true, "query_text": testMemeText}, nil, true},
		{"irrelevant", map[string]any{"hashtag": "#Humor", "relevant": false, "query_text": testMemeText}, nil, false},
		{"no hashtag", map[string]any{"relevant": true, "query_text": testMemeText}, nil, false},
		{"unavailable", nil, errors.New("offline"), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			notes := &recordedNotes{}
			handler := NewSystemDomainHandler(nil, nil, nil, &fakeDiscordDeliverer{}, "channel").
				WithHashtagSuggestions(&fakeMemeHashtagSuggester{result: tc.result, err: tc.err}).
				WithHashtagExperience(notes)
			event, step := discordSendEvent(map[string]any{"channel_ids": []any{"channel"}, "content": "meme", "attachment_url": testMemeURL})

			if _, err := handler.Handle(context.Background(), event, step); err != nil {
				t.Fatal(err)
			}

			if tc.noted {
				if len(notes.suggested) != 1 || notes.suggested[0] != [3]string{testMemeURL, testMemeText, "#Humor"} {
					t.Fatalf("expected the suggestion to be noted once, got %v", notes.suggested)
				}
			} else if len(notes.suggested) != 0 {
				t.Fatalf("nothing was appended, so nothing should be noted: %v", notes.suggested)
			}
		})
	}
}

func memeHandlerAnswering(response string, err error, notes HashtagExperienceNotes) *MemeDomainHandler {
	client := clients.NewMemeServiceClientWithFactory(func() (repreq.RequesterReply, error) {
		return &fakeMemeRequester{response: response, err: err}, nil
	})
	return NewMemeDomainHandler(client).WithHashtagExperience(notes)
}

func TestTheOperatorsTagIsNotedButAnUntagIsNot(t *testing.T) {
	tagged := `{"status":"ok","result":{"source_url":"` + testMemeURL + `","hashtag":"#Humor","text":"t","embedded":true}}`

	notes := &recordedNotes{}
	_, err := memeHandlerAnswering(tagged, nil, notes).Handle(context.Background(), core.Event{
		Type: "meme.hashtag.tag", Payload: map[string]any{"url": testMemeURL, "hashtag": "humor"},
	}, core.WorkflowStep{Domain: core.DomainMeme, Action: "tag-meme-hashtag"})
	if err != nil {
		t.Fatal(err)
	}
	if len(notes.tagged) != 1 || notes.tagged[0] != [2]string{testMemeURL, "#Humor"} {
		t.Fatalf("the tag should be noted as the service normalized it: %v", notes.tagged)
	}

	for name, step := range map[string]core.WorkflowStep{
		"an untag": {Domain: core.DomainMeme, Action: "untag-meme-hashtag"},
		"a list":   {Domain: core.DomainMeme, Action: "list-meme-hashtags"},
	} {
		other := &recordedNotes{}
		_, _ = memeHandlerAnswering(`{"status":"ok","result":{"deleted":true,"examples":[]}}`, nil, other).Handle(context.Background(), core.Event{
			Type: "meme.hashtag.untag", Payload: map[string]any{"url": testMemeURL},
		}, step)
		if len(other.tagged) != 0 {
			t.Fatalf("%s must not count as the operator choosing a tag: %v", name, other.tagged)
		}
	}

	failed := &recordedNotes{}
	if _, err := memeHandlerAnswering("", errors.New("meme service down"), failed).Handle(context.Background(), core.Event{
		Type: "meme.hashtag.tag", Payload: map[string]any{"url": testMemeURL, "hashtag": "humor"},
	}, core.WorkflowStep{Domain: core.DomainMeme, Action: "tag-meme-hashtag"}); err == nil {
		t.Fatal("expected the tag to fail")
	}
	if len(failed.tagged) != 0 {
		t.Fatalf("a tag that did not happen must not be noted: %v", failed.tagged)
	}
}
