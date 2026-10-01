package tui

import (
	"errors"
	"strings"
	"testing"
)

func memoryResponses(fake *fakeClient) {
	fake.responses["memory.status"] = map[string]any{
		"experiences": 40.0, "pending": 3.0, "resolved": 35.0, "expired": 2.0, "embedded": 40.0, "needs_reindex": 0.0,
		"embed_model": "bge-m3", "embeddings": true, "embedder_reachable": true, "degraded": false,
	}
	fake.responses["memory.stats"] = map[string]any{
		"action": "hashtag.suggest", "scored": 34.0, "unscored": 3.0, "min_scored": 30.0, "enough_data": true,
		"brier_prediction": 0.08, "brier_baseline": 0.27, "skill": 0.70, "beats_baseline": true,
	}
	fake.responses["memory.reindex"] = map[string]any{"embedded": 2.0, "remaining": 1.0}
}

func memoryTUI(t *testing.T) (*Model, *fakeClient) {
	t.Helper()
	m, fake := panelModel(t)
	memoryResponses(fake)
	return m, fake
}

func TestTheMemoryServiceShowsInThePanelAndCanBeSwitched(t *testing.T) {
	m, fake := panelModel(t)
	fake.responses["ningo.status"]["services"].(map[string]any)["memory"] = map[string]any{"status": "unchecked", "enabled": true}
	press(m, "r")

	var row string
	for _, line := range strings.Split(m.View(), "\n") {
		if strings.Contains(line, "memory") {
			row = line
		}
	}
	if row == "" || !strings.Contains(row, "sem sonda de saúde") {
		t.Fatalf("the memory service should be a row with no health probe, got %q in:\n%s", row, m.View())
	}

	press(m, "down", "down", "down", "space") // meme, chat_ai, book, then memory
	if req := fake.last("services.set"); req.payload["name"] != "memory" || req.payload["enabled"] != false {
		t.Fatalf("expected memory to be switched off, got %#v", req.payload)
	}
}

func TestMOpensTheMemoryOverlayWithItsStateAndTheScore(t *testing.T) {
	m, fake := memoryTUI(t)
	if fake.count("memory.status") != 0 {
		t.Fatal("the panel refresh must never ask the memory: a stopped service would stall it")
	}

	press(m, "m")

	view := m.View()
	for _, want := range []string{
		"Memória de experiência",
		"40 experiências · 3 pendentes · 35 resolvidas · 2 expiradas",
		"similaridade: ligada (modelo bge-m3)",
		"Previsões de hashtag.suggest contra o baseline",
		"34 pontuadas · 3 não pontuadas",
		"Brier (menor é melhor): previsão 0.080 · baseline 0.270",
		"habilidade +0.70: bate o baseline",
		"r atualizar · x reindexar · esc fecha",
	} {
		if !strings.Contains(view, want) {
			t.Errorf("memory overlay is missing %q:\n%s", want, view)
		}
	}
	if got := fake.last("memory.stats").payload["action"]; got != "hashtag.suggest" {
		t.Fatalf("the score is for the hashtag suggestion, asked for %v", got)
	}
	var order []string
	for _, c := range fake.calls {
		if strings.HasPrefix(c.eventType, "memory.") {
			order = append(order, c.eventType)
		}
	}
	if strings.Join(order, ",") != "memory.status,memory.stats" {
		t.Fatalf("expected the state then the score, got %v", order)
	}
	if len(m.history) != 0 {
		t.Fatalf("reading is not an action and must not clutter the history: %#v", m.history)
	}
}

func TestTheCaveatComesBeforeTheNumbersAndThereIsNoVerdictWithoutEnoughData(t *testing.T) {
	m, fake := memoryTUI(t)
	fake.responses["memory.stats"]["scored"] = 5.0
	fake.responses["memory.stats"]["enough_data"] = false
	fake.responses["memory.stats"]["beats_baseline"] = true // a lucky streak must not be called a win

	press(m, "m")

	view := m.View()
	caveat := strings.Index(view, "só 5 pontuadas; são precisas 30 para julgar: leia como ruído")
	numbers := strings.Index(view, "Brier (menor é melhor)")
	if caveat < 0 || numbers < 0 || caveat > numbers {
		t.Fatalf("the caveat must come first (at %d, numbers at %d):\n%s", caveat, numbers, view)
	}
	if strings.Contains(view, "bate o baseline") || strings.Contains(view, "habilidade") {
		t.Fatalf("no verdict before there are enough scored predictions:\n%s", view)
	}
}

func TestAnEnoughScoredPredictionThatDoesNotBeatTheBaselineSaysSo(t *testing.T) {
	m, fake := memoryTUI(t)
	fake.responses["memory.stats"]["beats_baseline"] = false
	fake.responses["memory.stats"]["skill"] = -0.11

	press(m, "m")

	if view := m.View(); !strings.Contains(view, "habilidade -0.11: não bate o baseline") || strings.Contains(view, "só 34 pontuadas") {
		t.Fatalf("unexpected verdict:\n%s", view)
	}
}

func TestNothingScoredYetIsSaidPlainly(t *testing.T) {
	m, fake := memoryTUI(t)
	fake.responses["memory.stats"] = map[string]any{"scored": 0.0, "unscored": 4.0, "min_scored": 30.0, "enough_data": false}

	press(m, "m")

	view := m.View()
	if !strings.Contains(view, "nada pontuado ainda (nenhuma experiência resolvida tem previsão)") || strings.Contains(view, "Brier (") {
		t.Fatalf("expected a plain statement and no numbers:\n%s", view)
	}
}

func TestASwitchedOffMemoryExplainsItselfAndAsksNothingElse(t *testing.T) {
	m, fake := memoryTUI(t)
	fake.responses["memory.status"] = map[string]any{"skipped": true, "reason": "service memory is disabled"}

	press(m, "m")

	view := m.View()
	if !strings.Contains(view, "O serviço de memória está desligado") || !strings.Contains(view, "service memory is disabled") {
		t.Fatalf("expected the switch-off and its reason:\n%s", view)
	}
	if fake.count("memory.stats") != 0 {
		t.Fatal("a refused request is not followed by another")
	}
}

func TestAFailingMemoryServiceStaysInsideTheOverlay(t *testing.T) {
	m, fake := memoryTUI(t)
	fake.errs["memory.status"] = errors.New("dispatch memory: resource temporarily unavailable")

	press(m, "m")

	view := m.View()
	if !strings.Contains(view, "Sem resposta do serviço de memória:") || !strings.Contains(view, "dispatch memory: resource temporarily unavailable") {
		t.Fatalf("expected the failure inside the overlay:\n%s", view)
	}
	for _, line := range strings.Split(view, "\n") {
		if len([]rune(line)) > 78 {
			t.Fatalf("a long error must not stretch the box beyond a small terminal (%d wide): %q", len([]rune(line)), line)
		}
	}
	if fake.count("memory.stats") != 0 {
		t.Fatal("a service that is not answering is not asked a second question")
	}
	if m.statusErr != "" {
		t.Fatalf("the panel's own status must be unaffected: %q", m.statusErr)
	}

	press(m, "esc", "r")
	if m.overlay != overlayNone || !strings.Contains(m.View(), "chat_ai") {
		t.Fatalf("the panel must keep working after a memory failure:\n%s", m.View())
	}
}

func TestADegradedMemoryExplainsItAndOffersTheReindex(t *testing.T) {
	m, fake := memoryTUI(t)
	fake.responses["memory.status"]["embedder_reachable"] = false
	fake.responses["memory.status"]["needs_reindex"] = 2.0

	press(m, "m")

	view := m.View()
	for _, want := range []string{"similaridade indisponível: previsões são só o baseline", "2 experiências precisam de embedding neste modelo: x"} {
		if !strings.Contains(view, want) {
			t.Errorf("missing %q:\n%s", want, view)
		}
	}

	fake.responses["memory.status"]["embeddings"] = false
	press(m, "r")
	if view := m.View(); !strings.Contains(view, "similaridade desligada: previsões são só o baseline") {
		t.Fatalf("expected the embeddings-off wording:\n%s", view)
	}
}

func TestXReindexesOneBatchAndRefreshesTheOverlay(t *testing.T) {
	m, fake := memoryTUI(t)
	press(m, "m")
	before := fake.count("memory.status")

	press(m, "x")

	if req := fake.last("memory.reindex"); req.eventType == "" || len(req.payload) != 0 {
		t.Fatalf("expected a reindex with no limit, got %#v", req)
	}
	if fake.count("memory.status") != before+1 {
		t.Fatal("the overlay must refresh after a reindex")
	}
	if !strings.Contains(m.notice, "2 embeddings gerados, 1 restantes") {
		t.Fatalf("the notice should say how many remain, got %q", m.notice)
	}
	if len(m.history) != 1 || m.history[0].Label != "reindexar memória" {
		t.Fatalf("a reindex is an action and belongs in the history: %#v", m.history)
	}
}

func TestRefreshAndCloseKeysWorkAndTheOverlayIsNotPolled(t *testing.T) {
	m, fake := memoryTUI(t)
	press(m, "m")

	press(m, "r")
	if fake.count("memory.status") != 2 {
		t.Fatalf("r should reload the memory, asked %d times", fake.count("memory.status"))
	}

	polls := fake.count("ningo.status")
	_, cmd := m.Update(tickMsg(panelNow))
	pump(m, cmd)
	if fake.count("ningo.status") != polls {
		t.Fatal("the panel poll must not run while the overlay is open")
	}

	press(m, "q")
	if m.overlay != overlayNone {
		t.Fatal("q closes the overlay, it does not quit")
	}
	press(m, "m", "esc")
	if m.overlay != overlayNone {
		t.Fatal("esc closes the overlay")
	}
}

func TestMDoesNotOpenAnOverlayWhileAnotherRequestRuns(t *testing.T) {
	m, fake := memoryTUI(t)
	m.busy, m.busyLabel = true, "outra coisa"

	press(m, "m")

	if m.overlay != overlayNone || fake.count("memory.status") != 0 {
		t.Fatal("nothing should open or be asked while a request is in flight")
	}
	if !strings.Contains(m.notice, "aguarde") {
		t.Fatalf("expected the usual wait notice, got %q", m.notice)
	}
}

func TestTheMemoryOverlayAndTheHintsExistInEnglishToo(t *testing.T) {
	withLanguage(t, "en")
	m, _ := memoryTUI(t)
	if !strings.Contains(m.View(), "m memory") {
		t.Fatalf("the panel hint should mention the new key:\n%s", m.View())
	}

	press(m, "m")

	view := m.View()
	for _, want := range []string{
		"Experience memory",
		"40 experiences · 3 pending · 35 resolved · 2 expired",
		"Predictions for hashtag.suggest against the baseline",
		"skill +0.70: beats the baseline",
		"r refresh · x reindex · esc close",
	} {
		if !strings.Contains(view, want) {
			t.Errorf("English overlay is missing %q:\n%s", want, view)
		}
	}
}

// portugueseIn (see the i18n tests) finds words that skipped tr(): the data the fake orchestrator
// returns is English, so any hit in the English interface is text that was not translated.
func TestNoMemoryScreenLeaksPortugueseIntoTheEnglishInterface(t *testing.T) {
	withLanguage(t, "en")
	screens := map[string]func(*fakeClient){
		"memory": func(*fakeClient) {},
		"memory degraded": func(fake *fakeClient) {
			fake.responses["memory.status"]["embedder_reachable"] = false
			fake.responses["memory.status"]["needs_reindex"] = 2.0
			fake.responses["memory.stats"]["enough_data"] = false
		},
		"memory without embeddings": func(fake *fakeClient) { fake.responses["memory.status"]["embeddings"] = false },
		"memory nothing scored": func(fake *fakeClient) {
			fake.responses["memory.stats"] = map[string]any{"scored": 0.0, "unscored": 2.0}
		},
		"memory off": func(fake *fakeClient) {
			fake.responses["memory.status"] = map[string]any{"skipped": true, "reason": "service memory is disabled"}
		},
		"memory error": func(fake *fakeClient) {
			fake.errs["memory.status"] = errors.New("dispatch memory: resource temporarily unavailable")
		},
		"memory score error": func(fake *fakeClient) {
			fake.errs["memory.stats"] = errors.New("score unavailable")
		},
	}
	for name, arrange := range screens {
		m, fake := memoryTUI(t)
		arrange(fake)
		press(m, "m")

		view := m.View()
		if !strings.Contains(view, "Experience memory") {
			t.Errorf("%s: the overlay did not render:\n%s", name, view)
		}
		if leaked := portugueseIn(view); len(leaked) > 0 {
			t.Errorf("%s shows Portuguese words in English mode %v:\n%s", name, leaked, view)
		}
	}
}
