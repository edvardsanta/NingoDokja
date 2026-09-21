package tui

import (
	"strings"
	"testing"
	"time"
)

var panelNow = time.Date(2026, 9, 18, 15, 6, 0, 0, time.UTC)

func panelModel(t *testing.T) (*Model, *fakeClient) {
	t.Helper()
	fake := newFake()
	m := NewModel(fake, time.Millisecond, time.Second)
	m.now = func() time.Time { return panelNow }
	pump(m, m.Init())
	return m, fake
}

func TestPanelListsServicesAndJobsWithTheirState(t *testing.T) {
	m, _ := panelModel(t)
	view := m.View()

	for _, want := range []string{
		"chat_ai", "connection refused", // a real error stays an error
		"desligado", // the switched-off book service
		"meme.dispatch", "pausado", "a cada 2h0m0s (alterado)",
		"a cada 45m0s", "rodou", "pulado",
		"em 4m", // meme.refresh is due at 15:10, the panel was read at 15:06
	} {
		if !strings.Contains(view, want) {
			t.Errorf("panel is missing %q:\n%s", want, view)
		}
	}
	if strings.Contains(view, "o scheduler ainda não anunciou") {
		t.Fatalf("the scheduler announced 1 minute ago, no warning expected:\n%s", view)
	}
}

func TestPanelWarnsWhenTheSchedulerIsSilent(t *testing.T) {
	m, fake := panelModel(t)
	for _, item := range fake.responses["ningo.status"]["jobs"].([]any) {
		item.(map[string]any)["announced_at"] = "2026-09-18T14:50:00Z"
	}
	press(m, "r")
	if !strings.Contains(m.View(), "último anúncio do scheduler há 16m") {
		t.Fatalf("a stale announcement should be flagged:\n%s", m.View())
	}

	for _, item := range fake.responses["ningo.status"]["jobs"].([]any) {
		item.(map[string]any)["announced_at"] = ""
	}
	press(m, "r")
	if !strings.Contains(m.View(), "ainda não anunciou nada") {
		t.Fatalf("a never-seen scheduler should be flagged:\n%s", m.View())
	}
}

func TestSpaceOnAServiceSwitchesItAndRefreshesTheStatus(t *testing.T) {
	m, fake := panelModel(t)
	before := fake.count("ningo.status")

	press(m, "space") // cursor starts on the first service, meme

	req := fake.last("services.set")
	if req.payload["name"] != "meme" || req.payload["enabled"] != false {
		t.Fatalf("expected meme to be switched off, got %#v", req.payload)
	}
	if fake.count("ningo.status") != before+1 {
		t.Fatal("the panel must refresh after a switch")
	}
	if len(m.history) != 1 || !strings.Contains(m.history[0].Label, "desligar serviço meme") {
		t.Fatalf("unexpected history %#v", m.history)
	}
}

func TestAServiceWithoutAHealthProbeSaysSo(t *testing.T) {
	m, fake := panelModel(t)
	fake.responses["ningo.status"]["services"].(map[string]any)["book"] = map[string]any{"status": "unchecked", "enabled": true}
	press(m, "r")
	if !strings.Contains(m.View(), "sem sonda de saúde") {
		t.Fatalf("expected an unchecked service to say it has no health probe:\n%s", m.View())
	}
}

func TestSpaceOnADisabledServiceTurnsItBackOn(t *testing.T) {
	m, fake := panelModel(t)
	press(m, "down", "down", "space") // book, which the fixture has switched off
	req := fake.last("services.set")
	if req.payload["name"] != "book" || req.payload["enabled"] != true {
		t.Fatalf("expected book to be switched on, got %#v", req.payload)
	}
}

func TestSpaceOnAJobPausesOrResumesIt(t *testing.T) {
	m, fake := panelModel(t)
	// four services come first (rows 0-3); the jobs start at row 4 = meme.refresh
	press(m, "down", "down", "down", "down", "space")
	req := fake.last("scheduler.jobs.set")
	if req.payload["name"] != "meme.refresh" || req.payload["enabled"] != false {
		t.Fatalf("expected meme.refresh to be paused, got %#v", req.payload)
	}

	press(m, "down", "space") // meme.dispatch is paused in the fixture
	req = fake.last("scheduler.jobs.set")
	if req.payload["name"] != "meme.dispatch" || req.payload["enabled"] != true {
		t.Fatalf("expected meme.dispatch to be resumed, got %#v", req.payload)
	}
}

func selectJob(m *Model, index int) {
	// four services come first, then the jobs
	for i := 0; i < 4+index; i++ {
		press(m, "down")
	}
}

func TestRunNowAsksForConfirmationAndWarnsAboutAPausedJob(t *testing.T) {
	m, fake := panelModel(t)
	selectJob(m, 1) // meme.dispatch, paused

	press(m, "x")
	view := m.View()
	for _, want := range []string{"Rodar agora: meme.dispatch", "pausado", "ignora a pausa", "bot-chan", "só seguro"} {
		if !strings.Contains(view, want) {
			t.Errorf("confirmation is missing %q:\n%s", want, view)
		}
	}
	if fake.count("meme.dispatch.scheduled") != 0 {
		t.Fatal("nothing may run before the user confirms")
	}

	press(m, "n")
	if fake.count("meme.dispatch.scheduled") != 0 {
		t.Fatal("cancelling must not run the job")
	}

	press(m, "x", "y")
	req := fake.last("meme.dispatch.scheduled")
	if req.payload["limit"] != 1 {
		t.Fatalf("a manual dispatch sends exactly one meme, got %#v", req.payload)
	}
	if _, stamped := req.payload["schedule"]; stamped {
		t.Fatal("a manual run must not look like a scheduled one")
	}
}

func TestRunNowWarnsWhenTheJobsServiceIsOff(t *testing.T) {
	m, fake := panelModel(t)
	fake.responses["ningo.status"]["services"].(map[string]any)["meme"] = map[string]any{"status": "disabled", "enabled": false}
	press(m, "r")
	selectJob(m, 0) // meme.refresh

	press(m, "x")
	if !strings.Contains(m.View(), "serviço meme está desligado") {
		t.Fatalf("expected a warning about the disabled service:\n%s", m.View())
	}
}

func TestRunNowOnAServiceRowIsRefused(t *testing.T) {
	m, fake := panelModel(t)
	press(m, "x")
	if m.overlay != overlayNone || !strings.Contains(m.notice, "só jobs") || fake.count("meme.pool.refresh") != 0 {
		t.Fatalf("expected a hint, got overlay=%v notice=%q", m.overlay, m.notice)
	}
}

func TestIntervalOverlayValidatesAndSends(t *testing.T) {
	m, fake := panelModel(t)
	selectJob(m, 0)

	press(m, "i")
	if m.overlay != overlayInterval || !strings.Contains(m.View(), "Intervalo de meme.refresh") {
		t.Fatalf("expected the interval prompt:\n%s", m.View())
	}

	m.intervalInput.SetValue("10s")
	press(m, "enter")
	if m.overlay != overlayInterval || !strings.Contains(m.notice, "between") || fake.count("scheduler.jobs.set") != 0 {
		t.Fatalf("a too-short interval must keep the prompt open, notice=%q", m.notice)
	}

	m.intervalInput.SetValue("45m")
	press(m, "enter")
	req := fake.last("scheduler.jobs.set")
	if req.payload["name"] != "meme.refresh" || req.payload["interval"] != "45m0s" || m.overlay != overlayNone {
		t.Fatalf("unexpected interval request %#v", req.payload)
	}

	press(m, "i")
	m.intervalInput.SetValue("default")
	press(m, "enter")
	if fake.last("scheduler.jobs.set").payload["interval"] != "default" {
		t.Fatalf("'default' must clear the override, got %#v", fake.last("scheduler.jobs.set").payload)
	}
}

func TestIntervalOnAServiceRowIsRefusedAndEscCancels(t *testing.T) {
	m, _ := panelModel(t)
	press(m, "i")
	if m.overlay != overlayNone || !strings.Contains(m.notice, "só jobs") {
		t.Fatalf("services have no interval, got overlay=%v notice=%q", m.overlay, m.notice)
	}

	selectJob(m, 0)
	press(m, "i", "esc")
	if m.overlay != overlayNone {
		t.Fatal("esc must close the prompt")
	}
}

func TestSwitchedOffMemeServiceIsNotAnOutage(t *testing.T) {
	m, fake := panelModel(t)
	fake.responses["meme.status"] = map[string]any{"skipped": true, "reason": "service meme is disabled"}
	press(m, "r")

	view := m.View()
	if strings.Contains(view, "Sem resposta do orquestrador") || strings.Contains(view, "última atualização falhou") {
		t.Fatalf("a switch that is off is not a failure:\n%s", view)
	}
	if !strings.Contains(view, "serviço de meme desligado") {
		t.Fatalf("the panel should say why the counts are missing:\n%s", view)
	}
}

func TestMemesTabExplainsARefusalFromADisabledService(t *testing.T) {
	m, fake := panelModel(t)
	fake.responses["meme.list"] = map[string]any{"skipped": true, "reason": "service meme is disabled"}
	press(m, "2")

	if !strings.Contains(m.View(), "recusado pelo orquestrador: service meme is disabled") {
		t.Fatalf("the list should explain the refusal:\n%s", m.View())
	}
}

func TestAnActionRefusedBecauseAServiceIsOffShowsTheReason(t *testing.T) {
	m, fake := panelModel(t)
	fake.responses["meme.dispatch.scheduled"] = map[string]any{"skipped": true, "reason": "service meme is disabled"}
	selectJob(m, 1)
	press(m, "x", "y")

	if len(m.history) == 0 || m.history[0].OK || !strings.Contains(m.history[0].Summary, "service meme is disabled") {
		t.Fatalf("a refused run must be recorded as a failure with its reason: %#v", m.history)
	}
}

func TestPanelSelectionStaysInRangeWhenTheRowsShrink(t *testing.T) {
	m, _ := panelModel(t)
	for i := 0; i < 30; i++ {
		press(m, "down")
	}
	if rows := m.panelRows(); m.panelCursor != len(rows)-1 {
		t.Fatalf("the cursor should stop on the last row, at %d of %d", m.panelCursor, len(rows))
	}
	for i := 0; i < 30; i++ {
		press(m, "up")
	}
	if m.panelCursor != 0 {
		t.Fatalf("the cursor should stop on the first row, at %d", m.panelCursor)
	}
}

func TestParseStatusFollowsTheCanonicalServiceOrder(t *testing.T) {
	system := map[string]any{"services": map[string]any{
		"book": map[string]any{"status": "ok", "enabled": true},
		"meme": map[string]any{"status": "ok", "enabled": true},
		"junk": map[string]any{"status": "ok", "enabled": true},
	}}
	data := parseStatus(system, map[string]any{}, panelNow)
	names := []string{}
	for _, service := range data.Services {
		names = append(names, service.Name)
	}
	if strings.Join(names, ",") != "meme,book" {
		t.Fatalf("expected canonical order without unknown services, got %v", names)
	}
}

func TestSchedulerRowSwitchesEverythingAndShowsWhetherItIsRunning(t *testing.T) {
	m, fake := panelModel(t)
	press(m, "down", "down", "down", "space") // the scheduler is the fourth service

	req := fake.last("services.set")
	if req.payload["name"] != "scheduler" || req.payload["enabled"] != false {
		t.Fatalf("expected the scheduler to be switched off, got %#v", req.payload)
	}

	fake.responses["ningo.status"]["services"].(map[string]any)["scheduler"] = map[string]any{
		"status": "stopped", "enabled": true, "detail": "never announced (stopped?)",
	}
	press(m, "r")
	view := m.View()
	if !strings.Contains(view, "scheduler") || !strings.Contains(view, "parado") || !strings.Contains(view, "never announced") {
		t.Fatalf("a scheduler that is not announcing must read as stopped:\n%s", view)
	}

	fake.responses["ningo.status"]["services"].(map[string]any)["scheduler"] = map[string]any{"status": "disabled", "enabled": false}
	press(m, "r")
	if !strings.Contains(m.View(), "desligado") {
		t.Fatalf("a switched-off scheduler must read as off:\n%s", m.View())
	}
}
