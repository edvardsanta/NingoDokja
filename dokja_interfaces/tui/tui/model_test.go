package tui

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

type call struct {
	eventType string
	payload   map[string]any
}

type fakeClient struct {
	calls     []call
	responses map[string]map[string]any
	errs      map[string]error
}

func (f *fakeClient) Request(_ context.Context, eventType string, payload map[string]any) (map[string]any, error) {
	f.calls = append(f.calls, call{eventType, payload})
	if err := f.errs[eventType]; err != nil {
		return nil, err
	}
	return f.responses[eventType], nil
}

func (f *fakeClient) count(eventType string) int {
	n := 0
	for _, c := range f.calls {
		if c.eventType == eventType {
			n++
		}
	}
	return n
}

func (f *fakeClient) last(eventType string) call {
	for i := len(f.calls) - 1; i >= 0; i-- {
		if f.calls[i].eventType == eventType {
			return f.calls[i]
		}
	}
	return call{}
}

func job(name string, enabled bool, interval string, override bool, next, last, outcome, announced string) map[string]any {
	return map[string]any{
		"name": name, "enabled": enabled, "interval": interval, "interval_override": override,
		"next_at": next, "last_at": last, "last_outcome": outcome, "announced_at": announced,
	}
}

func newFake() *fakeClient {
	return &fakeClient{
		errs: map[string]error{},
		responses: map[string]map[string]any{
			"ningo.status": {
				"services": map[string]any{
					"meme":      map[string]any{"status": "ok", "enabled": true},
					"chat_ai":   map[string]any{"status": "error", "enabled": true, "error": "connection refused"},
					"book":      map[string]any{"status": "disabled", "enabled": false},
					"scheduler": map[string]any{"status": "ok", "enabled": true},
				},
				"jobs": []any{
					job("meme.refresh", true, "45m0s", false, "2026-09-18T15:10:00Z", "2026-09-18T14:25:00Z", "ran", "2026-09-18T15:05:00Z"),
					job("meme.dispatch", false, "2h0m0s", true, "2026-09-18T17:00:00Z", "2026-09-18T15:00:00Z", "skipped", "2026-09-18T15:05:00Z"),
				},
				"channels": map[string]any{
					"meme":      []any{"bot-chan", "hook-chan"},
					"safe_only": []any{"hook-chan"},
				},
				"chat_profiles": map[string]any{
					"active": "hosted",
					"profiles": []any{
						map[string]any{"name": "backup", "base_url": "https://backup.example/v1", "model": "m-backup", "key_hint": "…bbbb", "active": false},
						map[string]any{"name": "hosted", "base_url": "https://api.example.com/v1", "model": "llama", "key_hint": "…aaaa", "active": true},
					},
				},
			},
			"meme.status": {"unsent_count": float64(42), "sent_count": float64(394)},
			"meme.list": {
				"total": float64(2), "offset": float64(0), "scope": "unsent", "count": float64(2),
				"memes": []any{
					map[string]any{"url": "https://x/a.jpeg", "title": "Primeiro meme", "tags": "a,b"},
					map[string]any{"url": "https://x/b.jpeg", "title": "Segundo meme", "tags": "c"},
				},
			},
			"meme.screen":             {"url": "https://x/a.jpeg", "safe": false, "reason": "blocked word 'torava' in image text", "text": "que voce torava e", "detections": []any{}},
			"discord.send":            {"sent_to": []any{"bot-chan"}, "marked_sent": true},
			"meme.dispatch.scheduled": {"delivered_count": float64(3)},
			"meme.pool.refresh":       {"status": "refreshed"},
			"meme.hashtag.tag":        {"hashtag": "#TioDoPave"},
			"meme.hashtag.suggest":    {"hashtag": "#TioDoPave", "relevant": true},
			"meme.hashtag.list": {
				"total": float64(2), "offset": float64(0),
				"examples": []any{
					map[string]any{"source_url": "https://x/tagged-a.jpeg", "text": "first tagged meme text", "hashtag": "#Trabalho", "embedded": false, "updated_at": "2026-09-27T12:54:03Z"},
					map[string]any{"source_url": "https://x/tagged-b.jpeg", "text": "second tagged meme text", "hashtag": "#Cafe", "embedded": true, "updated_at": "2026-09-27T12:55:03Z"},
				},
			},
			"meme.hashtag.untag": {"deleted": true},
		},
	}
}

// pump runs commands to completion, feeding results back, without waiting on ticks.
func pump(m *Model, cmd tea.Cmd) {
	queue := []tea.Cmd{cmd}
	for len(queue) > 0 {
		next := queue[0]
		queue = queue[1:]
		if next == nil {
			continue
		}
		switch msg := next().(type) {
		case tea.BatchMsg:
			queue = append(queue, msg...)
		case tickMsg, frameTickMsg:
			// Timers re-arm themselves forever; tests deliver them by hand.
		default:
			_, follow := m.Update(msg)
			queue = append(queue, follow)
		}
	}
}

func key(name string) tea.KeyMsg {
	switch name {
	case "enter":
		return tea.KeyMsg{Type: tea.KeyEnter}
	case "space":
		return tea.KeyMsg{Type: tea.KeySpace, Runes: []rune(" ")}
	case "down":
		return tea.KeyMsg{Type: tea.KeyDown}
	case "up":
		return tea.KeyMsg{Type: tea.KeyUp}
	case "esc":
		return tea.KeyMsg{Type: tea.KeyEsc}
	case "ctrl+s":
		return tea.KeyMsg{Type: tea.KeyCtrlS}
	case "f2":
		return tea.KeyMsg{Type: tea.KeyF2}
	case "tab":
		return tea.KeyMsg{Type: tea.KeyTab}
	}
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(name)}
}

func press(m *Model, names ...string) {
	for _, name := range names {
		_, cmd := m.Update(key(name))
		pump(m, cmd)
	}
}

func started(t *testing.T) (*Model, *fakeClient) {
	t.Helper()
	fake := newFake()
	m := NewModel(fake, time.Millisecond, time.Second)
	pump(m, m.Init())
	return m, fake
}

func TestPanelLoadsStatusAndMarksTheSafeOnlyChannel(t *testing.T) {
	m, _ := started(t)

	view := m.View()
	for _, want := range []string{"42 na fila", "394 já enviados", "chat_ai", "hook-chan", "só seguro"} {
		if !strings.Contains(view, want) {
			t.Fatalf("panel is missing %q:\n%s", want, view)
		}
	}
}

func TestMemesTabListsThePoolWithoutSendingAnything(t *testing.T) {
	m, fake := started(t)

	press(m, "2")

	list := fake.last("meme.list")
	if list.payload["scope"] != "unsent" || list.payload["limit"] != pageSize || list.payload["offset"] != 0 {
		t.Fatalf("unexpected list request %#v", list.payload)
	}
	if !strings.Contains(m.View(), "Segundo meme") {
		t.Fatalf("list not rendered:\n%s", m.View())
	}
	if fake.count("discord.send") != 0 {
		t.Fatal("browsing must not send anything")
	}
}

func TestScreenShowsTheVerdictTextAndCaptionIsForwarded(t *testing.T) {
	m, fake := started(t)
	press(m, "2", "s")

	req := fake.last("meme.screen")
	if req.payload["url"] != "https://x/a.jpeg" || req.payload["caption"] != "Primeiro meme" {
		t.Fatalf("unexpected screen request %#v", req.payload)
	}
	view := m.View()
	if !strings.Contains(view, "BARRADO") || !strings.Contains(view, "que voce torava e") {
		t.Fatalf("verdict not rendered:\n%s", view)
	}
}

func TestMemesTabCanLearnAndSuggestAHashtag(t *testing.T) {
	m, fake := started(t)
	press(m, "2", "h")
	if m.overlay != overlayHashtag {
		t.Fatalf("expected hashtag overlay, got %v", m.overlay)
	}
	press(m, "TioDoPave", "enter")
	if req := fake.last("meme.hashtag.tag"); req.payload["url"] != "https://x/a.jpeg" || req.payload["hashtag"] != "TioDoPave" {
		t.Fatalf("unexpected tag request %#v", req.payload)
	}
	press(m, "g")
	if req := fake.last("meme.hashtag.suggest"); req.payload["url"] != "https://x/a.jpeg" {
		t.Fatalf("unexpected suggest request %#v", req.payload)
	}
	if !strings.Contains(m.notice, "#TioDoPave") {
		t.Fatalf("suggestion was not shown: %q", m.notice)
	}
}

func TestSuggestionShowsWhyItWasMadeAndStaysWithTheMemeItIsAbout(t *testing.T) {
	m, fake := started(t)
	fake.responses["meme.hashtag.suggest"] = map[string]any{
		"hashtag": "#TioDoPave", "relevant": true, "score": 0.8312, "threshold": 0.6,
		"query_text": "texto lido da imagem", "matched_text": "texto do meme marcado",
	}

	press(m, "2", "g")

	view := m.View()
	for _, want := range []string{
		"Sugestão de hashtag", "sugerido: #TioDoPave",
		"proximidade: 0.83 (relevante a partir de 0.60)",
		"texto lido: texto lido da imagem",
		"texto marcado mais parecido: texto do meme marcado",
	} {
		if !strings.Contains(view, want) {
			t.Fatalf("the suggestion should say why it was made, missing %q:\n%s", want, view)
		}
	}

	press(m, "down")
	if strings.Contains(m.View(), "Sugestão de hashtag") {
		t.Fatalf("the evidence is about the meme it was asked for, not the next one:\n%s", m.View())
	}
	press(m, "up")
	if !strings.Contains(m.View(), "texto lido da imagem") {
		t.Fatal("coming back to the meme shows its suggestion again")
	}
}

func TestSuggestionBelowTheThresholdOrWithoutOneSaysSoAndKeepsWhatItKnows(t *testing.T) {
	for name, tc := range map[string]struct {
		response map[string]any
		want     []string
		absent   []string
	}{
		"below the threshold": {
			response: map[string]any{
				"hashtag": "#Cafe", "relevant": false, "score": 0.41, "threshold": 0.6,
				"query_text": "lido", "matched_text": "parecido",
			},
			want:   []string{"mais próximo: #Cafe (abaixo do limiar de relevância)", "proximidade: 0.41 (relevante a partir de 0.60)", "texto lido: lido"},
			absent: []string{"sugerido: #Cafe"},
		},
		"nothing tagged yet": {
			response: map[string]any{"hashtag": nil, "relevant": false, "degraded": false, "reason": "no tagged examples with an embedding from this model yet", "query_text": "lido"},
			want:     []string{"Sugestão de hashtag", "no tagged examples with an embedding", "texto lido: lido"},
			absent:   []string{"proximidade", "texto marcado mais parecido"},
		},
	} {
		m, fake := started(t)
		fake.responses["meme.hashtag.suggest"] = tc.response

		press(m, "2", "g")

		view := m.View()
		for _, want := range tc.want {
			if !strings.Contains(view, want) {
				t.Errorf("%s: missing %q:\n%s", name, want, view)
			}
		}
		for _, absent := range tc.absent {
			if strings.Contains(view, absent) {
				t.Errorf("%s: %q must not be shown:\n%s", name, absent, view)
			}
		}
	}
}

func TestSuggestionEvidenceExistsInEnglishAndLeaksNoPortuguese(t *testing.T) {
	withLanguage(t, "en")
	m, fake := started(t)
	fake.responses["meme.hashtag.suggest"] = map[string]any{
		"hashtag": "#TioDoPave", "relevant": true, "score": 0.83, "threshold": 0.6,
		"query_text": "words read", "matched_text": "words of the match",
	}

	press(m, "2", "g")

	view := m.View()
	for _, want := range []string{"Hashtag suggestion", "suggested #TioDoPave", "closeness: 0.83 (relevant from 0.60)", "text read: words read", "closest tagged text: words of the match"} {
		if !strings.Contains(view, want) {
			t.Errorf("missing %q:\n%s", want, view)
		}
	}
	for _, portuguese := range []string{"Sugestão", "proximidade", "texto lido", "parecido"} {
		if strings.Contains(view, portuguese) {
			t.Errorf("Portuguese %q leaked into the English interface:\n%s", portuguese, view)
		}
	}
}

func TestHashtagListShowsTaggedMemesAndEmbeddingState(t *testing.T) {
	m, fake := started(t)
	press(m, "2", "l")
	if req := fake.last("meme.hashtag.list"); req.payload["limit"] != 200 || req.payload["offset"] != 0 {
		t.Fatalf("unexpected hashtag list request: %#v", req.payload)
	}
	if m.overlay != overlayHashtagList {
		t.Fatalf("expected hashtag list overlay, got %v", m.overlay)
	}
	view := m.View()
	for _, want := range []string{"Memes marcados", "#Trabalho", "sem embedding", "first tagged meme text", "tagged-a.jpeg"} {
		if !strings.Contains(view, want) {
			t.Fatalf("tagged meme list is missing %q:\n%s", want, view)
		}
	}
	if item, ok := m.selectedPreviewItem(); !ok || item.URL != "https://x/tagged-a.jpeg" {
		t.Fatalf("wrong selected preview item: %#v, %v", item, ok)
	}
	press(m, "down")
	if item, ok := m.selectedPreviewItem(); !ok || item.URL != "https://x/tagged-b.jpeg" {
		t.Fatalf("cursor did not select the second tagged meme: %#v, %v", item, ok)
	}
	if !strings.Contains(m.View(), "pronto") || !strings.Contains(m.View(), "second tagged meme text") {
		t.Fatalf("second tagged meme details were not rendered:\n%s", m.View())
	}
	press(m, "esc")
	if m.overlay != overlayNone {
		t.Fatal("escape must close the tagged meme list")
	}
}

func TestHashtagRemovalNeedsConfirmation(t *testing.T) {
	m, fake := started(t)
	press(m, "2", "u")
	if m.overlay != overlayConfirm || fake.count("meme.hashtag.untag") != 0 {
		t.Fatal("removal needs confirmation")
	}
	press(m, "y")
	if fake.last("meme.hashtag.untag").payload["url"] != "https://x/a.jpeg" {
		t.Fatal("wrong meme removed")
	}
}

func TestSendingAPickedMemeNeedsConfirmationAndMarksItSent(t *testing.T) {
	m, fake := started(t)
	press(m, "2", "down", "enter")

	press(m, "enter") // nothing ticked yet
	if m.overlay != overlayPicker || !strings.Contains(m.notice, "ao menos um canal") {
		t.Fatalf("expected the picker to insist on a channel, notice=%q", m.notice)
	}

	press(m, "down", "space", "enter") // hook-chan
	if m.overlay != overlayConfirm {
		t.Fatalf("expected confirmation, overlay=%v", m.overlay)
	}
	if !strings.Contains(m.View(), "Segundo meme") || !strings.Contains(m.View(), "filtro NSFW") {
		t.Fatalf("confirmation should name the meme and the filter:\n%s", m.View())
	}
	if fake.count("discord.send") != 0 {
		t.Fatal("nothing may be sent before the user confirms")
	}

	press(m, "n")
	if fake.count("discord.send") != 0 || m.overlay != overlayNone {
		t.Fatal("cancelling must not send")
	}

	press(m, "enter", "down", "space", "enter", "y")
	send := fake.last("discord.send")
	channels, _ := send.payload["channel_ids"].([]string)
	if len(channels) != 1 || channels[0] != "hook-chan" ||
		send.payload["content"] != "Segundo meme" || send.payload["attachment_url"] != "https://x/b.jpeg" ||
		send.payload["mark_sent"] != true {
		t.Fatalf("unexpected send %#v", send.payload)
	}
	if len(m.history) != 1 || !m.history[0].OK || !strings.Contains(m.history[0].Summary, "marcado como enviado") {
		t.Fatalf("unexpected history %#v", m.history)
	}
	if fake.count("meme.status") < 2 || fake.count("meme.list") < 2 {
		t.Fatalf("status and list should reload after a send: status=%d list=%d", fake.count("meme.status"), fake.count("meme.list"))
	}
}

func TestPickedMemeCanForceSafeOnlyDeliveryFromTUI(t *testing.T) {
	m, fake := started(t)
	press(m, "2", "enter", "down", "space", "f", "enter")

	if m.overlay != overlayConfirm || !strings.Contains(m.View(), "--force ignora o bloqueio NSFW") {
		t.Fatalf("forced send needs an explicit warning:\n%s", m.View())
	}
	if fake.count("discord.send") != 0 {
		t.Fatal("forced send must still wait for confirmation")
	}
	press(m, "y")
	if fake.last("discord.send").payload["force_nsfw"] != true {
		t.Fatalf("expected force_nsfw in TUI request, got %#v", fake.last("discord.send").payload)
	}

	press(m, "enter")
	if m.picker.forceNSFW {
		t.Fatal("--force must reset whenever the picker opens")
	}
}

func TestForceIsNotSentForOpenOnlyDestination(t *testing.T) {
	m, fake := started(t)
	press(m, "2", "enter", "space", "f", "enter", "y")
	if _, present := fake.last("discord.send").payload["force_nsfw"]; present {
		t.Fatalf("open-only send must not carry force_nsfw: %#v", fake.last("discord.send").payload)
	}
}

func TestResendingFromTheSentListDoesNotMarkAgain(t *testing.T) {
	m, fake := started(t)
	press(m, "2", "t", "enter", "space", "enter", "y")

	if _, present := fake.last("discord.send").payload["mark_sent"]; present {
		t.Fatalf("sent memes must not be re-marked: %#v", fake.last("discord.send").payload)
	}
}

func TestDispatchAsksForCountAndConfirmationAndCapsTheBatch(t *testing.T) {
	m, fake := started(t)
	press(m, "2", "d")
	if m.overlay != overlayDispatch {
		t.Fatalf("expected the dispatch prompt, overlay=%v", m.overlay)
	}

	m.dispatchInput.SetValue("99")
	press(m, "enter")
	if m.overlay != overlayDispatch || !strings.Contains(m.notice, "entre 1 e") {
		t.Fatalf("oversized batch must be refused, notice=%q", m.notice)
	}

	m.dispatchInput.SetValue("3")
	press(m, "enter")
	if m.overlay != overlayConfirm || fake.count("meme.dispatch.scheduled") != 0 {
		t.Fatal("dispatch must wait for confirmation")
	}
	press(m, "y")
	if fake.last("meme.dispatch.scheduled").payload["limit"] != 3 {
		t.Fatalf("unexpected dispatch %#v", fake.last("meme.dispatch.scheduled").payload)
	}
	if !strings.Contains(m.history[0].Summary, "3 meme(s) entregue(s)") {
		t.Fatalf("unexpected history %#v", m.history)
	}
}

func TestOnlyOneRequestIsInFlightAndPollsAreSkippedWhileBusy(t *testing.T) {
	m, fake := started(t)
	before := len(fake.calls)

	m.busy, m.busyLabel = true, "disparando"
	_, cmd := m.Update(tickMsg(time.Now()))
	pump(m, cmd)
	press(m, "r")

	if len(fake.calls) != before {
		t.Fatalf("no request may start while busy, got %d new", len(fake.calls)-before)
	}
	if !strings.Contains(m.notice, "aguarde") {
		t.Fatalf("expected a wait notice, got %q", m.notice)
	}
}

func TestConfirmationStaysOpenWhenABusyRequestIsRunning(t *testing.T) {
	m, fake := started(t)
	press(m, "2", "enter", "space", "enter")
	if m.overlay != overlayConfirm {
		t.Fatal("expected confirmation")
	}

	m.busy, m.busyLabel = true, "atualizando painel"
	press(m, "y")

	if m.overlay != overlayConfirm || fake.count("discord.send") != 0 {
		t.Fatal("the action must wait, not be dropped or duplicated")
	}
}

func TestPollingOnlyRunsOnThePanelTab(t *testing.T) {
	m, fake := started(t)
	press(m, "2")
	before := fake.count("ningo.status")

	_, cmd := m.Update(tickMsg(time.Now()))
	pump(m, cmd)

	if fake.count("ningo.status") != before {
		t.Fatal("the panel poll must not run on other tabs")
	}
}

func TestDiscordFormValidatesConfirmsAndSendsText(t *testing.T) {
	m, fake := started(t)
	press(m, "f3")

	press(m, "1", "2", "a")
	if m.form.text.Value() != "12a" || m.tab != tabDiscord {
		t.Fatalf("digits must be typed, not switch tabs: value=%q tab=%v", m.form.text.Value(), m.tab)
	}

	press(m, "ctrl+s")
	if !strings.Contains(m.notice, "ao menos um canal") {
		t.Fatalf("expected a channel validation error, got %q", m.notice)
	}

	press(m, "down", "down", "space", "ctrl+s") // first channel: bot-chan
	if m.overlay != overlayConfirm || fake.count("discord.send") != 0 {
		t.Fatal("expected confirmation before sending")
	}
	press(m, "y")

	send := fake.last("discord.send")
	if send.payload["content"] != "12a" || send.payload["attachment_url"] != nil {
		t.Fatalf("unexpected send %#v", send.payload)
	}
	if channels, _ := send.payload["channel_ids"].([]string); len(channels) != 1 || channels[0] != "bot-chan" {
		t.Fatalf("unexpected channels %#v", send.payload["channel_ids"])
	}
}

func TestDiscordFormRequiresContent(t *testing.T) {
	m, fake := started(t)
	press(m, "f3", "down", "down", "space", "ctrl+s")

	if m.overlay != overlayNone || !strings.Contains(m.notice, "mensagem") || fake.count("discord.send") != 0 {
		t.Fatalf("empty send must be refused, notice=%q", m.notice)
	}
}

func TestFailuresAreShownAndRecordedInTheHistory(t *testing.T) {
	m, fake := started(t)
	fake.errs["discord.send"] = errors.New("nothing sent: image is not safe for hook-chan (blocked word 'torava')")

	press(m, "2", "enter", "down", "space", "enter", "y")

	if len(m.history) != 1 || m.history[0].OK || !strings.Contains(m.history[0].Summary, "torava") {
		t.Fatalf("unexpected history %#v", m.history)
	}
	if !m.noticeErr || !strings.Contains(m.View(), "falhou") {
		t.Fatalf("failure should be visible:\n%s", m.View())
	}
}

func TestPanelShowsAnErrorWhenTheOrchestratorIsDown(t *testing.T) {
	fake := newFake()
	fake.errs["ningo.status"] = errors.New("context deadline exceeded")
	m := NewModel(fake, time.Millisecond, time.Second)
	pump(m, m.Init())

	if !strings.Contains(m.View(), "Sem resposta do orquestrador") {
		t.Fatalf("expected an unreachable message:\n%s", m.View())
	}
	press(m, "2", "enter")
	if m.overlay != overlayNone {
		t.Fatal("cannot pick channels without status")
	}
}

func TestUnwrapResult(t *testing.T) {
	envelope := map[string]any{
		"event_id": "e1",
		"workflow": "meme",
		"result":   map[string]any{"meme": map[string]any{"unsent_count": float64(3)}},
	}
	res, err := unwrapResult(envelope)
	if err != nil || res["unsent_count"] != float64(3) {
		t.Fatalf("unexpected unwrap %#v / %v", res, err)
	}

	if _, err := unwrapResult("nope"); err == nil {
		t.Fatal("expected an error for a non-map response")
	}
	if _, err := unwrapResult(map[string]any{"workflow": "meme"}); err == nil {
		t.Fatal("expected an error when result is missing")
	}
}
