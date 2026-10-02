package tui

// These tests run in Portuguese like the rest of this suite (TestMain in i18n_test.go), so they assert the
// Portuguese catalog text. The English wording is checked in the tests that switch the language.

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

func experienceFixture(ref, detail, outcome string, predicted, baseline any, snippet, created, resolved string) map[string]any {
	return map[string]any{
		"ref": ref, "action": "hashtag.suggest", "detail": detail, "outcome": outcome,
		"predicted_p": predicted, "baseline_p": baseline, "created_at": created, "resolved_at": resolved, "context_snippet": snippet,
	}
}

func memoryBrowseResponses(fake *fakeClient) {
	memoryResponses(fake)
	fake.responses["memory.list"] = map[string]any{
		"total": 12.0, "limit": 8.0, "offset": 0.0,
		"experiences": []any{
			experienceFixture("hashtag:aaaa1111", "#Rates", "accepted", 0.95, 0.5, "central bank policy rate and inflation outlook report number 38", "2026-10-01T03:27:52Z", "2026-10-01T03:29:10Z"),
			experienceFixture("hashtag:bbbb2222", "#Cake", "replaced", 0.12, 0.5, "chocolate carrot cake recipe with cream frosting tonight number 37", "2026-10-01T03:20:00Z", "2026-10-01T03:21:00Z"),
			experienceFixture("hashtag:cccc3333", "#Rates", "", nil, nil, "waiting for a verdict on this one", "2026-10-01T03:10:00Z", ""),
			experienceFixture("hashtag:dddd4444", "#Cake", "expired", 0.4, 0.5, "a very old experience that nobody resolved", "2026-08-01T03:10:00Z", ""),
		},
	}
	fake.responses["memory.predict"] = map[string]any{
		"action": "hashtag.suggest", "predicted_p": 0.72, "baseline_p": 0.60, "support": 5.0, "resolved": 40.0,
		"insufficient": false, "degraded": false, "reason": "",
		"evidence": []any{
			map[string]any{"ref": "hashtag:aaaa1111", "detail": "#Rates", "outcome": "accepted", "similarity": 0.91, "context_snippet": "central bank policy rate and inflation outlook"},
			map[string]any{"ref": "hashtag:bbbb2222", "detail": "#Cake", "outcome": "replaced", "similarity": 0.64, "context_snippet": "chocolate carrot cake recipe"},
		},
	}
	fake.responses["memory.forget"] = map[string]any{"deleted": true}
}

// pagedClient answers memory.list by the offset asked for, so paging and forgetting can be followed;
// everything else goes to the shared fake.
type pagedClient struct {
	*fakeClient
	pages map[int]map[string]any
}

func (p *pagedClient) Request(ctx context.Context, eventType string, payload map[string]any) (map[string]any, error) {
	if eventType == "memory.list" {
		p.calls = append(p.calls, call{eventType, payload})
		offset, _ := payload["offset"].(int)
		return p.pages[offset], nil
	}
	return p.fakeClient.Request(ctx, eventType, payload)
}

func listPage(total, offset int, items ...any) map[string]any {
	return map[string]any{"total": float64(total), "offset": float64(offset), "limit": float64(memoryPageSize), "experiences": items}
}

func browseTUI(t *testing.T) (*Model, *fakeClient) {
	t.Helper()
	m, fake := panelModel(t)
	memoryBrowseResponses(fake)
	return m, fake
}

func TestLOpensTheExperienceListWithWhatEachOneWasAndHowItTurnedOut(t *testing.T) {
	m, fake := browseTUI(t)

	press(m, "m", "l")

	req := fake.last("memory.list")
	if req.payload["limit"] != memoryPageSize || req.payload["offset"] != 0 || req.payload["include_context"] != true || req.payload["state"] != nil {
		t.Fatalf("unexpected request %#v", req.payload)
	}
	view := m.View()
	for _, want := range []string{
		"Experiências", "todas · 12",
		"aceita", "trocada", "pendente", "expirada",
		"#Rates", "0.95/0.50", "central bank policy rate and",
		// the first experience is selected and shown in full
		`"central bank policy rate and inflation outlook report number 38"`,
		"previsão 0.95 · baseline 0.50", "criada 2026-10-01 03:27 · resolvida 2026-10-01 03:29", "ref hashtag:aaaa1111",
		"↑↓ mover · n/p página · t filtro · f esquecer · r recarregar · esc volta",
	} {
		if !strings.Contains(view, want) {
			t.Errorf("the list is missing %q:\n%s", want, view)
		}
	}
	if len(m.history) != 0 {
		t.Fatalf("reading is not an action: %#v", m.history)
	}
}

func TestTheListCursorMovesTheDetailAndStaysInsideTheList(t *testing.T) {
	m, _ := browseTUI(t)
	press(m, "m", "l", "down", "down")

	view := m.View()
	if !strings.Contains(view, `"waiting for a verdict on this one"`) || !strings.Contains(view, "nenhuma previsão foi guardada") ||
		!strings.Contains(view, "criada 2026-10-01 03:10") || strings.Contains(view, "resolvida") || !strings.Contains(view, "ref hashtag:cccc3333") {
		t.Fatalf("a pending experience has no prediction and no resolution date:\n%s", view)
	}

	press(m, "down", "down", "down", "down")
	if m.memoryList.Cursor != 3 {
		t.Fatalf("the cursor must stop at the last row, is at %d", m.memoryList.Cursor)
	}
	press(m, "up", "up", "up", "up", "up")
	if m.memoryList.Cursor != 0 {
		t.Fatalf("the cursor must stop at the first row, is at %d", m.memoryList.Cursor)
	}
}

func TestTCyclesTheFilterAndNAndPPageThroughTheList(t *testing.T) {
	m, fake := browseTUI(t)
	press(m, "m", "l")

	press(m, "t")
	if req := fake.last("memory.list"); req.payload["state"] != "pending" || req.payload["offset"] != 0 {
		t.Fatalf("expected the pending filter, got %#v", req.payload)
	}
	if !strings.Contains(m.View(), "pendentes · 12") {
		t.Fatalf("the title should name the filter:\n%s", m.View())
	}
	press(m, "t")
	if req := fake.last("memory.list"); req.payload["state"] != "resolved" {
		t.Fatalf("expected the resolved filter, got %#v", req.payload)
	}
	press(m, "t")
	if req := fake.last("memory.list"); req.payload["state"] != nil {
		t.Fatalf("the third press is back to everything, got %#v", req.payload)
	}

	calls := fake.count("memory.list")
	press(m, "p")
	if fake.count("memory.list") != calls {
		t.Fatal("there is no page before the first")
	}
	press(m, "n")
	if req := fake.last("memory.list"); req.payload["offset"] != memoryPageSize {
		t.Fatalf("expected the second page, got %#v", req.payload)
	}
}

func TestNDoesNotPagePastTheLastPage(t *testing.T) {
	m, fake := browseTUI(t)
	fake.responses["memory.list"]["offset"] = float64(8) // 12 in all: the second page is the last
	press(m, "m", "l")
	m.memoryList.Offset = 8
	calls := fake.count("memory.list")

	press(m, "n")

	if fake.count("memory.list") != calls {
		t.Fatal("n on the last page must not ask for another")
	}
}

func TestAnEmptyListAFailureAndASwitchedOffMemoryStayInsideTheOverlay(t *testing.T) {
	m, fake := browseTUI(t)
	fake.responses["memory.list"] = listPage(0, 0)
	press(m, "m", "l")
	if view := m.View(); !strings.Contains(view, "nenhuma experiência nesta visão") || m.overlay != overlayMemoryList {
		t.Fatalf("an empty view says so:\n%s", view)
	}

	fake.responses["memory.list"] = map[string]any{"skipped": true, "reason": "service memory is disabled"}
	press(m, "r")
	if view := m.View(); !strings.Contains(view, "O serviço de memória está desligado") || !strings.Contains(view, "service memory is disabled") {
		t.Fatalf("a switched-off memory explains itself:\n%s", view)
	}

	delete(fake.responses, "memory.list")
	fake.errs["memory.list"] = errors.New("dispatch memory: resource temporarily unavailable")
	press(m, "r")
	view := m.View()
	if !strings.Contains(view, "Sem resposta do serviço de memória:") || !strings.Contains(view, "resource temporarily unavailable") || m.statusErr != "" {
		t.Fatalf("a failure stays in the overlay:\n%s", view)
	}
	for _, line := range strings.Split(view, "\n") {
		if len([]rune(line)) > 78 {
			t.Fatalf("a long error must not stretch the box (%d wide): %q", len([]rune(line)), line)
		}
	}
}

func TestEscFromTheListGoesBackToTheMemoryOverlayThenCloses(t *testing.T) {
	m, _ := browseTUI(t)
	press(m, "m", "l", "esc")
	if m.overlay != overlayMemory || !strings.Contains(m.View(), "Memória de experiência") {
		t.Fatalf("esc from the list returns to the memory overlay, got overlay %v", m.overlay)
	}
	press(m, "esc")
	if m.overlay != overlayNone {
		t.Fatal("esc again closes it")
	}
	press(m, "m", "l", "q")
	if m.overlay != overlayMemory {
		t.Fatal("q in the list also goes back one step")
	}
}

func TestLDoesNotOpenTheListWhileAnotherRequestRuns(t *testing.T) {
	m, fake := browseTUI(t)
	press(m, "m")
	m.busy, m.busyLabel = true, "outra coisa"

	press(m, "l")

	if m.overlay != overlayMemory || fake.count("memory.list") != 0 {
		t.Fatal("nothing should open or be asked while a request is in flight")
	}
}

func TestFForgetsTheSelectedExperienceAfterConfirmingAndStaysInTheList(t *testing.T) {
	m, fake := browseTUI(t)
	press(m, "m", "l", "down", "f") // the second experience, #Cake

	view := m.View()
	if m.overlay != overlayConfirm || !strings.Contains(view, "Esquecer esta experiência?") || !strings.Contains(view, "#Cake") {
		t.Fatalf("expected a confirmation naming the experience:\n%s", view)
	}
	if fake.count("memory.forget") != 0 {
		t.Fatal("nothing is forgotten before the answer")
	}

	listed := fake.count("memory.list")
	press(m, "y")

	if req := fake.last("memory.forget"); req.payload["ref"] != "hashtag:bbbb2222" {
		t.Fatalf("expected the selected ref to be forgotten, got %#v", req.payload)
	}
	if m.overlay != overlayMemoryList || fake.count("memory.list") != listed+1 {
		t.Fatalf("the list must be shown again and reloaded (overlay %v, list requests %d)", m.overlay, fake.count("memory.list"))
	}
	if len(m.history) != 1 || m.history[0].Label != "esquecer experiência" || !strings.Contains(m.notice, "esquecida") {
		t.Fatalf("forgetting is an action: history %#v notice %q", m.history, m.notice)
	}
}

func TestCancellingAForgetReturnsToTheListAndAFailureLeavesItThere(t *testing.T) {
	m, fake := browseTUI(t)
	press(m, "m", "l", "f", "n")
	if m.overlay != overlayMemoryList || fake.count("memory.forget") != 0 || m.notice != "cancelado" {
		t.Fatalf("n goes back to the list without forgetting (overlay %v, notice %q)", m.overlay, m.notice)
	}

	press(m, "f", "esc")
	if m.overlay != overlayMemoryList || fake.count("memory.forget") != 0 {
		t.Fatal("esc cancels the same way")
	}

	fake.errs["memory.forget"] = errors.New("disk full")
	press(m, "f", "y")
	if m.overlay != overlayMemoryList || !strings.Contains(m.notice, "falhou") || !strings.Contains(m.notice, "disk full") {
		t.Fatalf("a failed forget leaves the list on screen with the reason (overlay %v, notice %q)", m.overlay, m.notice)
	}
}

func TestForgettingTheLastExperienceOfTheLastPageStepsBackAPage(t *testing.T) {
	fake := newFake()
	memoryBrowseResponses(fake)
	client := &pagedClient{fakeClient: fake, pages: map[int]map[string]any{
		0: listPage(9, 0, experienceFixture("hashtag:first", "#A", "accepted", 0.9, 0.5, "first page", "2026-10-01T03:00:00Z", "2026-10-01T03:01:00Z")),
		8: listPage(9, 8, experienceFixture("hashtag:last", "#B", "accepted", 0.9, 0.5, "the only one on page two", "2026-10-01T03:00:00Z", "2026-10-01T03:01:00Z")),
	}}
	m := NewModel(client, time.Millisecond, time.Second)
	m.now = func() time.Time { return panelNow }
	pump(m, m.Init())
	press(m, "m", "l", "n")
	if m.memoryList.Offset != 8 {
		t.Fatalf("expected to be on the second page, at offset %d", m.memoryList.Offset)
	}

	// Once the last experience is forgotten, page two is empty and page one has one fewer in all.
	client.pages[8] = listPage(8, 8)
	client.pages[0] = listPage(8, 0, experienceFixture("hashtag:first", "#A", "accepted", 0.9, 0.5, "first page", "2026-10-01T03:00:00Z", "2026-10-01T03:01:00Z"))
	press(m, "f", "y")

	if m.memoryList.Offset != 0 || len(m.memoryList.Items) != 1 || !strings.Contains(m.View(), "first page") {
		t.Fatalf("expected to land on the first page, offset %d items %d:\n%s", m.memoryList.Offset, len(m.memoryList.Items), m.View())
	}
}

// typeText sends each character as a key press. The commands a text field returns (the cursor blink)
// are not run: they only sleep.
func typeText(m *Model, text string) {
	for _, r := range text {
		m.Update(key(string(r)))
	}
}

func TestPOpensThePredictionFormAndEverythingTypedIsText(t *testing.T) {
	m, fake := browseTUI(t)

	press(m, "m", "p")
	typeText(m, "q 7 central bank")

	if m.overlay != overlayMemoryPredict {
		t.Fatalf("expected the form, got overlay %v", m.overlay)
	}
	if view := m.View(); !strings.Contains(view, "q 7 central bank") || !strings.Contains(view, "Prever para hashtag.suggest") {
		t.Fatalf("q and digits must be typed as text:\n%s", view)
	}
	if fake.count("memory.predict") != 0 {
		t.Fatal("nothing is asked until enter")
	}
}

func TestEnterAsksForTheChanceAndShowsWhatItRestsOn(t *testing.T) {
	m, fake := browseTUI(t)
	press(m, "m", "p")
	typeText(m, "central bank rates")

	press(m, "enter")

	req := fake.last("memory.predict")
	if req.payload["action"] != "hashtag.suggest" || req.payload["context"] != "central bank rates" || req.payload["include_context"] != true {
		t.Fatalf("unexpected request %#v", req.payload)
	}
	view := m.View()
	for _, want := range []string{
		"chance de uma sugestão para este texto ser mantida: 72% (base 60%)",
		"5 experiências parecidas · 40 resolvidas no total",
		"Experiências anteriores mais próximas:",
		"[0.91] aceita", "#Rates", "central bank policy rate",
		"[0.64] trocada", "#Cake",
		"digite um texto · enter prevê · esc volta",
	} {
		if !strings.Contains(view, want) {
			t.Errorf("the prediction is missing %q:\n%s", want, view)
		}
	}
	if len(m.history) != 0 {
		t.Fatalf("a prediction is not an action: %#v", m.history)
	}
}

func TestAnInsufficientOrDegradedPredictionIsOnlyTheBaselineAndSaysSo(t *testing.T) {
	m, fake := browseTUI(t)
	fake.responses["memory.predict"]["insufficient"] = true
	fake.responses["memory.predict"]["reason"] = "too few similar experiences"
	fake.responses["memory.predict"]["degraded"] = true
	press(m, "m", "p")
	typeText(m, "words")

	press(m, "enter")

	view := m.View()
	for _, want := range []string{"só o baseline: too few similar experiences", "a similaridade está indisponível, então isto é só o baseline"} {
		if !strings.Contains(view, want) {
			t.Errorf("missing %q:\n%s", want, view)
		}
	}
}

func TestThePredictionFormRefusesAnEmptyTextAndKeepsFailuresInside(t *testing.T) {
	m, fake := browseTUI(t)
	press(m, "m", "p", "enter")
	if fake.count("memory.predict") != 0 || m.notice != "digite um texto antes" {
		t.Fatalf("an empty text asks nothing (notice %q)", m.notice)
	}

	typeText(m, "words")
	fake.responses["memory.predict"] = map[string]any{"skipped": true, "reason": "service memory is disabled"}
	press(m, "enter")
	if !strings.Contains(m.View(), "O serviço de memória está desligado") {
		t.Fatalf("a switched-off memory explains itself:\n%s", m.View())
	}

	delete(fake.responses, "memory.predict")
	fake.errs["memory.predict"] = errors.New("dispatch memory: resource temporarily unavailable")
	press(m, "enter")
	view := m.View()
	if !strings.Contains(view, "Sem resposta do serviço de memória:") || m.overlay != overlayMemoryPredict {
		t.Fatalf("a failure stays in the form:\n%s", view)
	}

	press(m, "esc")
	if m.overlay != overlayMemory {
		t.Fatal("esc goes back to the memory overlay")
	}
}

func suggestOnMemes(t *testing.T) (*Model, *fakeClient) {
	t.Helper()
	m, fake := browseTUI(t)
	fake.responses["meme.hashtag.suggest"] = map[string]any{"hashtag": "#Rates", "relevant": true, "score": 0.8, "query_text": "central bank policy rate"}
	press(m, "2")
	return m, fake
}

func TestGShowsTheSuggestionAndThenTheChanceItIsKept(t *testing.T) {
	m, fake := suggestOnMemes(t)

	press(m, "g")

	var order []string
	for _, c := range fake.calls {
		if c.eventType == "meme.hashtag.suggest" || c.eventType == "memory.predict" {
			order = append(order, c.eventType)
		}
	}
	if strings.Join(order, ",") != "meme.hashtag.suggest,memory.predict" {
		t.Fatalf("the suggestion comes first, then the chance: %v", order)
	}
	req := fake.last("memory.predict")
	if req.payload["action"] != "hashtag.suggest" || req.payload["context"] != "central bank policy rate" || req.payload["include_context"] != nil {
		t.Fatalf("unexpected request %#v", req.payload)
	}
	suggestion := strings.Index(m.notice, "#Rates")
	chance := strings.Index(m.notice, "chance de ser mantida: 72% (base 60%)")
	if suggestion < 0 || chance < suggestion {
		t.Fatalf("the chance is appended to the suggestion's notice, got %q", m.notice)
	}
	if len(m.history) != 1 || m.noticeErr {
		t.Fatalf("only the suggestion is an action (history %#v, error %v)", m.history, m.noticeErr)
	}
}

func TestAnInsufficientChanceIsSaidToBeOnlyTheBaseline(t *testing.T) {
	m, fake := suggestOnMemes(t)
	fake.responses["memory.predict"]["insufficient"] = true

	press(m, "g")

	if !strings.Contains(m.notice, "só o baseline é conhecido: 60% (pouco histórico)") || strings.Contains(m.notice, "chance de ser mantida") {
		t.Fatalf("expected the baseline wording, got %q", m.notice)
	}
}

func TestNoChanceIsAskedWhenThereIsNothingWorthAskingFor(t *testing.T) {
	for name, arrange := range map[string]func(*fakeClient){
		"not relevant": func(f *fakeClient) { f.responses["meme.hashtag.suggest"]["relevant"] = false },
		"no hashtag":   func(f *fakeClient) { f.responses["meme.hashtag.suggest"]["hashtag"] = "" },
		"no text":      func(f *fakeClient) { f.responses["meme.hashtag.suggest"]["query_text"] = "  " },
		"memory switched off": func(f *fakeClient) {
			f.responses["ningo.status"]["services"].(map[string]any)["memory"] = map[string]any{"status": "unchecked", "enabled": false}
		},
	} {
		m, fake := browseTUI(t)
		fake.responses["meme.hashtag.suggest"] = map[string]any{"hashtag": "#Rates", "relevant": true, "query_text": "central bank policy rate"}
		arrange(fake)
		press(m, "r", "2", "g") // r refreshes the panel, so a switch in the status is seen

		if fake.count("memory.predict") != 0 {
			t.Errorf("%s: the memory must not be asked", name)
		}
		if strings.Contains(m.notice, "chance") || m.noticeErr {
			t.Errorf("%s: the suggestion must stand alone, got %q", name, m.notice)
		}
	}
}

func TestAStoppedMemoryCostsTheSuggestionNothingAndIsNotAskedAgainForAWhile(t *testing.T) {
	m, fake := suggestOnMemes(t)
	fake.errs["memory.predict"] = errors.New("dispatch memory: resource temporarily unavailable")
	clock := panelNow
	m.now = func() time.Time { return clock }

	press(m, "g")
	if !strings.Contains(m.notice, "#Rates") || m.noticeErr || strings.Contains(m.notice, "chance") {
		t.Fatalf("a failed chance leaves the suggestion's notice as it was, got %q (error %v)", m.notice, m.noticeErr)
	}
	if fake.count("memory.predict") != 1 {
		t.Fatalf("expected one attempt, got %d", fake.count("memory.predict"))
	}

	press(m, "g")
	clock = clock.Add(30 * time.Second)
	press(m, "g")
	if fake.count("memory.predict") != 1 {
		t.Fatalf("a stopped memory must not be asked again within the minute, asked %d times", fake.count("memory.predict"))
	}
	if fake.count("meme.hashtag.suggest") != 3 {
		t.Fatal("the suggestion itself must keep working")
	}

	clock = clock.Add(2 * time.Minute)
	delete(fake.errs, "memory.predict")
	press(m, "g")
	if fake.count("memory.predict") != 2 || !strings.Contains(m.notice, "chance de ser mantida: 72% (base 60%)") {
		t.Fatalf("after the pause it asks again and a success shows (asked %d, notice %q)", fake.count("memory.predict"), m.notice)
	}
}

func TestAChanceArrivingAfterTheNoticeChangedIsDropped(t *testing.T) {
	m, _ := browseTUI(t)
	m.notice = "something else"
	prediction := memoryPrediction{Predicted: 0.7, Baseline: 0.6}

	m.Update(suggestionChanceMsg{prediction: &prediction, base: "the suggestion"})

	if m.notice != "something else" {
		t.Fatalf("a notice that changed meanwhile must be left alone, got %q", m.notice)
	}
}

func TestTheBrowseScreensExistInEnglishAndLeakNoPortuguese(t *testing.T) {
	withLanguage(t, "en")
	screens := map[string]func(m *Model, fake *fakeClient){
		"list":                  func(m *Model, _ *fakeClient) { press(m, "m", "l") },
		"list pending selected": func(m *Model, _ *fakeClient) { press(m, "m", "l", "down", "down") },
		"list expired selected": func(m *Model, _ *fakeClient) { press(m, "m", "l", "down", "down", "down") },
		"list pending filter":   func(m *Model, _ *fakeClient) { press(m, "m", "l", "t") },
		"list empty": func(m *Model, f *fakeClient) {
			f.responses["memory.list"] = listPage(0, 0)
			press(m, "m", "l")
		},
		"list off": func(m *Model, f *fakeClient) {
			f.responses["memory.list"] = map[string]any{"skipped": true, "reason": "service memory is disabled"}
			press(m, "m", "l")
		},
		"list error": func(m *Model, f *fakeClient) {
			delete(f.responses, "memory.list")
			f.errs["memory.list"] = errors.New("dispatch memory: resource temporarily unavailable")
			press(m, "m", "l")
		},
		"forget confirmation": func(m *Model, _ *fakeClient) { press(m, "m", "l", "f") },
		"predict form":        func(m *Model, _ *fakeClient) { press(m, "m", "p") },
		"predict result": func(m *Model, _ *fakeClient) {
			press(m, "m", "p")
			typeText(m, "words")
			press(m, "enter")
		},
		"predict baseline only": func(m *Model, f *fakeClient) {
			f.responses["memory.predict"]["insufficient"] = true
			f.responses["memory.predict"]["reason"] = "too few similar experiences"
			f.responses["memory.predict"]["degraded"] = true
			press(m, "m", "p")
			typeText(m, "words")
			press(m, "enter")
		},
		"predict error": func(m *Model, f *fakeClient) {
			delete(f.responses, "memory.predict")
			f.errs["memory.predict"] = errors.New("dispatch memory: resource temporarily unavailable")
			press(m, "m", "p")
			typeText(m, "words")
			press(m, "enter")
		},
	}
	for name, arrange := range screens {
		m, fake := browseTUI(t)
		arrange(m, fake)
		view := m.View()
		if strings.TrimSpace(view) == "" {
			t.Errorf("%s rendered nothing", name)
		}
		if leaked := portugueseIn(view); len(leaked) > 0 {
			t.Errorf("%s shows Portuguese words in English mode %v:\n%s", name, leaked, view)
		}
	}

	m, fake := suggestOnMemes(t)
	press(m, "g")
	if !strings.Contains(m.notice, "chance of being kept: 72% (baseline 60%)") || len(portugueseIn(m.notice)) > 0 {
		t.Errorf("the suggestion notice in English: %q", m.notice)
	}
	fake.responses["memory.predict"]["insufficient"] = true
	press(m, "g")
	if !strings.Contains(m.notice, "only the baseline is known: 60% (too little history)") {
		t.Errorf("the baseline-only notice in English: %q", m.notice)
	}
}
