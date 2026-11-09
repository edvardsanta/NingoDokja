package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"testing"

	quiz "read_books/dokja_domain/dokja_quiz"
	"read_books/internal/core"
)

var quizStep = core.WorkflowStep{Domain: core.DomainQuiz, Action: quiz.ActionTake}

// fakeQuizFeeds plays a Feeds host with one quiz runner, recording what it was asked.
type fakeQuizFeeds struct {
	mu        sync.Mutex
	providers []any
	questions map[string]any
	submit    map[string]any
	calls     []map[string]any
}

func (f *fakeQuizFeeds) Dispatch(_ context.Context, eventType string, payload map[string]any) (map[string]any, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if eventType == "feeds.status" {
		return map[string]any{"providers": f.providers}, nil
	}
	f.calls = append(f.calls, payload)
	switch payload["action"] {
	case quiz.RunnerQuestions:
		return f.questions, nil
	case quiz.RunnerSubmit:
		return f.submit, nil
	}
	return nil, fmt.Errorf("unexpected action %v", payload["action"])
}

func (f *fakeQuizFeeds) actions() []string {
	var actions []string
	for _, call := range f.calls {
		actions = append(actions, fmt.Sprint(call["plugin"], ":", call["action"]))
	}
	return actions
}

// scriptedQuizModel answers each call with the next reply and records the messages it saw.
type scriptedQuizModel struct {
	replies  []string
	messages [][]map[string]string
}

func (m *scriptedQuizModel) GenerateMessages(_ context.Context, messages []map[string]string) (map[string]any, error) {
	m.messages = append(m.messages, messages)
	if len(m.replies) == 0 {
		return map[string]any{"reply": "Fitting, though it underrates me."}, nil
	}
	reply := m.replies[0]
	m.replies = m.replies[1:]
	return map[string]any{"reply": reply}, nil
}

func quizProviders() []any {
	return []any{
		map[string]any{"id": "z-runner", "enabled": true, "provides": []any{quiz.Capability}},
		map[string]any{"id": "a-disabled", "enabled": false, "provides": []any{quiz.Capability}},
		map[string]any{"id": "a-posts", "enabled": true, "provides": []any{"social.resolve.v1"}},
	}
}

func threeQuestions() map[string]any {
	options := []any{"Concordo", "Neutro", "Discordo"}
	return map[string]any{"handled": true, "quiz": map[string]any{"title": "Example test", "questions": []any{
		map[string]any{"id": "q1", "text": "The state should plan the economy.", "options": options},
		map[string]any{"id": "q2", "text": "Tradition matters.", "options": options},
		map[string]any{"id": "religion", "text": "Your religion?", "options": []any{"Cristianismo", "Outra", "Nenhuma"}},
	}}}
}

func quizAnswers(pairs ...string) string {
	var answers []map[string]string
	for i := 0; i+1 < len(pairs); i += 2 {
		answers = append(answers, map[string]string{"id": pairs[i], "option": pairs[i+1]})
	}
	encoded, _ := json.Marshal(map[string]any{"answers": answers})
	return string(encoded)
}

func quizEvent(link string) core.Event {
	return core.Event{Source: core.SourceDiscord, Type: "quiz.take", Payload: map[string]any{"url": link}}
}

func TestNingoTakesAQuizAndReturnsTheSitesResultLink(t *testing.T) {
	feeds := &fakeQuizFeeds{providers: quizProviders(), questions: threeQuestions(),
		submit: map[string]any{"handled": true, "result": map[string]any{"url": "https://quiz.example.com/results?a=1", "label": "Example verdict"}}}
	model := &scriptedQuizModel{replies: []string{"```json\n" + quizAnswers("q1", "concordo", "q2", "Concordo", "religion", "Cristianismo") + "\n```"}}
	handler := NewQuizDomainHandler(feeds, model, nil).WithVisions("- Merit over birth.")

	result, err := handler.Handle(context.Background(), quizEvent("https://quiz.example.com/start"), quizStep)
	if err != nil {
		t.Fatal(err)
	}
	if result["status"] != "ok" || result["url"] != "https://quiz.example.com/results?a=1" || result["label"] != "Example verdict" || result["answered"] != 3 {
		t.Fatalf("result = %#v", result)
	}
	if result["comment"] != "Fitting, though it underrates me." {
		t.Fatalf("comment = %#v", result["comment"])
	}
	if got := strings.Join(feeds.actions(), ","); got != "z-runner:questions,z-runner:submit" {
		t.Fatalf("only the enabled quiz runner is asked, in order: %s", got)
	}
	answers, _ := feeds.calls[1]["input"].(map[string]any)["answers"].(map[string]string)
	if answers["q1"] != "Concordo" || answers["religion"] != "Cristianismo" || len(answers) != 3 {
		t.Fatalf("the runner must receive the offered options as written: %#v", answers)
	}
	system := model.messages[0][0]["content"]
	if !strings.Contains(system, "You are Ningo Dokja") || !strings.Contains(system, "- Merit over birth.") {
		t.Fatal("the answers must come from Ningo's persona and worldview")
	}
	if !strings.Contains(model.messages[0][1]["content"], "Tradition matters.") {
		t.Fatal("the questions must reach the model")
	}
	if !strings.Contains(model.messages[1][1]["content"], "Example verdict") || !strings.Contains(model.messages[1][1]["content"], "Brazilian Portuguese") {
		t.Fatal("the comment must react to the site's verdict in the interface's language")
	}
}

func TestUnansweredItemsAreRetriedOnceAndNeverSubmittedPartially(t *testing.T) {
	feeds := &fakeQuizFeeds{providers: quizProviders(), questions: threeQuestions(),
		submit: map[string]any{"handled": true, "result": map[string]any{"url": "https://quiz.example.com/r"}}}
	model := &scriptedQuizModel{replies: []string{
		quizAnswers("q1", "Concordo", "q2", "It depends"),
		quizAnswers("q2", "Discordo", "religion", "Cristianismo"),
	}}
	result, err := NewQuizDomainHandler(feeds, model, nil).Handle(context.Background(), quizEvent("https://quiz.example.com/start"), quizStep)
	if err != nil || result["status"] != "ok" {
		t.Fatalf("a retry that completes the batch must go on: %#v %v", result, err)
	}
	if retry := model.messages[1][1]["content"]; strings.Contains(retry, `"q1"`) || !strings.Contains(retry, `"q2"`) {
		t.Fatal("the retry must ask only for what is still unanswered")
	}

	feeds = &fakeQuizFeeds{providers: quizProviders(), questions: threeQuestions()}
	model = &scriptedQuizModel{replies: []string{quizAnswers("q1", "Concordo"), "I would rather not."}}
	result, err = NewQuizDomainHandler(feeds, model, nil).Handle(context.Background(), quizEvent("https://quiz.example.com/start"), quizStep)
	if err != nil || result["refused"] != quiz.RefusedUnanswered {
		t.Fatalf("result = %#v, %v", result, err)
	}
	if got := strings.Join(feeds.actions(), ","); got != "z-runner:questions" {
		t.Fatalf("a partial questionnaire must not be submitted: %s", got)
	}
}

func TestAResultLinkOffTheQuizHostIsAFailure(t *testing.T) {
	feeds := &fakeQuizFeeds{providers: quizProviders(), questions: threeQuestions(),
		submit: map[string]any{"handled": true, "result": map[string]any{"url": "https://elsewhere.example/phish"}}}
	model := &scriptedQuizModel{replies: []string{quizAnswers("q1", "Concordo", "q2", "Concordo", "religion", "Outra")}}
	result, err := NewQuizDomainHandler(feeds, model, nil).Handle(context.Background(), quizEvent("https://quiz.example.com/start"), quizStep)
	if err != nil || result["refused"] != quiz.RefusedFailed {
		t.Fatalf("result = %#v, %v", result, err)
	}
}

func TestQuizRefusalsAreFixedCodes(t *testing.T) {
	cases := map[string]struct {
		link    string
		feeds   *fakeQuizFeeds
		enabled bool
		want    string
	}{
		"invalid link":       {"not a link", &fakeQuizFeeds{providers: quizProviders()}, true, quiz.RefusedInvalidLink},
		"feeds switched off": {"https://quiz.example.com", &fakeQuizFeeds{providers: quizProviders()}, false, quiz.RefusedUnavailable},
		"no runner":          {"https://quiz.example.com", &fakeQuizFeeds{}, true, quiz.RefusedNoRunner},
		"not handled":        {"https://quiz.example.com", &fakeQuizFeeds{providers: quizProviders(), questions: map[string]any{"handled": false}}, true, quiz.RefusedUnsupported},
		"runner refuses":     {"https://quiz.example.com", &fakeQuizFeeds{providers: quizProviders(), questions: map[string]any{"handled": true, "refused": "unavailable"}}, true, quiz.RefusedUnavailable},
		"invalid quiz":       {"https://quiz.example.com", &fakeQuizFeeds{providers: quizProviders(), questions: map[string]any{"handled": true, "quiz": map[string]any{"questions": []any{}}}}, true, quiz.RefusedFailed},
		"submit refuses":     {"https://quiz.example.com", &fakeQuizFeeds{providers: quizProviders(), questions: threeQuestions(), submit: map[string]any{"handled": true, "refused": "Site said: <b>no</b>"}}, true, quiz.RefusedFailed},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			model := &scriptedQuizModel{replies: []string{quizAnswers("q1", "Concordo", "q2", "Concordo", "religion", "Outra")}}
			result, err := NewQuizDomainHandler(c.feeds, model, func() bool { return c.enabled }).Handle(context.Background(), quizEvent(c.link), quizStep)
			if err != nil || result["status"] != "refused" || result["refused"] != c.want {
				t.Fatalf("result = %#v, %v", result, err)
			}
		})
	}
}

func TestOnlyOneQuizRunsAtATime(t *testing.T) {
	feeds := &fakeQuizFeeds{providers: quizProviders(), questions: threeQuestions()}
	handler := NewQuizDomainHandler(feeds, &scriptedQuizModel{}, nil)
	handler.running.Store(true)
	result, err := handler.Handle(context.Background(), quizEvent("https://quiz.example.com"), quizStep)
	if err != nil || result["refused"] != quiz.RefusedBusy || len(feeds.calls) != 0 {
		t.Fatalf("a second run must wait for the first: %#v, %v, %d calls", result, err, len(feeds.calls))
	}
}

func TestAnEnglishInterfaceGetsTheCommentInEnglish(t *testing.T) {
	event := quizEvent("https://quiz.example.com")
	event.Payload["locale"] = "en-US"
	if quizLanguage(event) != "English" || quizLanguage(quizEvent("https://quiz.example.com")) != "Brazilian Portuguese" {
		t.Fatal("the comment language follows the interface's locale")
	}
}
