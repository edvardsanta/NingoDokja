package tui

import (
	"errors"
	"strings"
	"testing"

	store "read_books/dokja_store"
)

const tuiSecret = "sk-or-v1-SUPERSECRETTOKEN-tttt5555"

type fakeProfileStore struct {
	saved   []store.NewProfile
	deleted []struct {
		name  string
		force bool
	}
	saveErr error
}

func (f *fakeProfileStore) SaveProfile(p store.NewProfile) error {
	if f.saveErr != nil {
		return f.saveErr
	}
	f.saved = append(f.saved, p)
	return nil
}

func (f *fakeProfileStore) DeleteProfile(name string, force bool) error {
	f.deleted = append(f.deleted, struct {
		name  string
		force bool
	}{name, force})
	return nil
}

func profileTUI(t *testing.T) (*Model, *fakeClient, *fakeProfileStore) {
	t.Helper()
	m, fake := panelModel(t)
	fake.responses["ningo.status"]["services"].(map[string]any)["chat_ai"] = map[string]any{"status": "ok", "enabled": true}
	press(m, "r")
	writer := &fakeProfileStore{}
	m.SetProfileStore(writer)
	return m, fake, writer
}

func TestPanelShowsTheActiveChatProfileNextToChatAI(t *testing.T) {
	m, _, _ := profileTUI(t)
	view := m.View()
	if !strings.Contains(view, "perfil hosted") || !strings.Contains(view, "…aaaa") {
		t.Fatalf("the chat_ai row should name the active profile:\n%s", view)
	}
	if strings.Contains(view, "SUPERSECRET") {
		t.Fatal("no token may appear in the panel")
	}
}

func TestProfilePickerListsProfilesAndSelectsThroughTheOrchestratorByNameOnly(t *testing.T) {
	m, fake, writer := profileTUI(t)

	press(m, "p")
	view := m.View()
	for _, want := range []string{"Perfis do chat", "backup", "hosted", "…bbbb", "[*]"} {
		if !strings.Contains(view, want) {
			t.Errorf("picker is missing %q:\n%s", want, view)
		}
	}

	press(m, "up", "enter") // the cursor starts on the active profile; move to backup
	req := fake.last("chat.profile.use")
	if len(req.payload) != 1 || req.payload["name"] != "backup" {
		t.Fatalf("the orchestrator gets the profile name and nothing else, got %#v", req.payload)
	}
	if len(writer.saved) != 0 || len(writer.deleted) != 0 {
		t.Fatal("selecting must not touch the local database")
	}
}

func TestNewProfileIsWrittenLocallyAndTheTokenNeverShowsOrTravels(t *testing.T) {
	m, fake, writer := profileTUI(t)
	calls := len(fake.calls)

	press(m, "p", "n")
	press(m, "novo", "tab", "https://novo.example/v1", "tab", "modelo-x", "tab", tuiSecret)

	for _, view := range []string{m.View()} {
		if strings.Contains(view, "SUPERSECRET") || strings.Contains(view, tuiSecret) {
			t.Fatalf("the token must be masked while it is typed:\n%s", view)
		}
	}
	if !strings.Contains(m.View(), "••••") {
		t.Fatalf("expected bullets for the typed token:\n%s", m.View())
	}

	press(m, "ctrl+s")
	if len(writer.saved) != 1 {
		t.Fatalf("expected one local save, got %d (notice=%q)", len(writer.saved), m.notice)
	}
	saved := writer.saved[0]
	if saved.Name != "novo" || saved.BaseURL != "https://novo.example/v1" || saved.Model != "modelo-x" || saved.APIKey != tuiSecret {
		t.Fatalf("unexpected profile %+v", saved)
	}

	for _, call := range fake.calls[calls:] {
		if strings.Contains(strings.ToLower(call.eventType), "profile") && call.eventType != "ningo.status" {
			t.Fatalf("saving must not send anything profile-related over the orchestrator: %s", call.eventType)
		}
		if strings.Contains(strings.Join(payloadStrings(call.payload), "|"), "SUPERSECRET") {
			t.Fatalf("the token reached an orchestrator request: %#v", call)
		}
	}
	if m.profileForm[3].Value() != "" {
		t.Fatal("the token must be cleared from the form once handed to the store")
	}
	everywhere := m.View() + m.notice
	for _, entry := range m.history {
		everywhere += entry.Label + entry.Summary
	}
	if strings.Contains(everywhere, "SUPERSECRET") {
		t.Fatalf("the token leaked into the screen, notice or history: %s", everywhere)
	}
	if len(m.history) == 0 || !strings.Contains(m.history[0].Label, "criar perfil novo") {
		t.Fatalf("the action should be recorded without the token: %#v", m.history)
	}
}

func payloadStrings(payload map[string]any) []string {
	out := []string{}
	for key, value := range payload {
		out = append(out, key)
		if text, ok := value.(string); ok {
			out = append(out, text)
		}
	}
	return out
}

func TestAStoreErrorKeepsTheFormOpenWithoutEchoingTheToken(t *testing.T) {
	m, _, writer := profileTUI(t)
	writer.saveErr = errors.New("base_url must be an http(s) URL")

	press(m, "p", "n")
	press(m, "x", "tab", "nao-e-url", "tab", "m", "tab", tuiSecret, "ctrl+s")

	if m.overlay != overlayProfileForm || !strings.Contains(m.notice, "base_url") {
		t.Fatalf("the form should stay open with the reason, overlay=%v notice=%q", m.overlay, m.notice)
	}
	if strings.Contains(m.notice+m.View(), "SUPERSECRET") {
		t.Fatal("the error path must not echo the token")
	}
}

func TestCreatingOrDeletingNeedsLocalAccessToTheDatabase(t *testing.T) {
	m, _, _ := profileTUI(t)
	m.SetProfileStore(nil)

	press(m, "p", "n")
	if m.overlay != overlayProfiles || !strings.Contains(m.notice, "DOKJA_DB_FILE") {
		t.Fatalf("expected an explanation, overlay=%v notice=%q", m.overlay, m.notice)
	}
	press(m, "d")
	if m.overlay != overlayProfiles || !strings.Contains(m.notice, "DOKJA_DB_FILE") {
		t.Fatalf("deleting also needs the local database, notice=%q", m.notice)
	}
}

func TestDeletingAProfileAsksFirstAndWarnsWhenItIsTheActiveOne(t *testing.T) {
	m, _, writer := profileTUI(t)

	press(m, "p", "d") // the cursor starts on the active profile (hosted)
	view := m.View()
	if !strings.Contains(view, "Apagar o perfil hosted") || !strings.Contains(view, "ATIVO") || strings.Contains(view, "posta no Discord") {
		t.Fatalf("unexpected confirmation:\n%s", view)
	}
	if len(writer.deleted) != 0 {
		t.Fatal("nothing may be deleted before the user confirms")
	}

	press(m, "n")
	if len(writer.deleted) != 0 {
		t.Fatal("cancelling must not delete")
	}

	press(m, "p", "d", "y")
	if len(writer.deleted) != 1 || writer.deleted[0].name != "hosted" || !writer.deleted[0].force {
		t.Fatalf("expected a forced delete of the active profile, got %#v", writer.deleted)
	}
	if len(m.history) == 0 || m.history[0].Summary != "apagado" {
		t.Fatalf("unexpected history %#v", m.history)
	}
}

func TestProfilePickerRefusesToOpenBeforeTheStatusLoads(t *testing.T) {
	fake := newFake()
	fake.errs["ningo.status"] = errors.New("down")
	m := NewModel(fake, 1, 1000000000)
	pump(m, m.Init())

	press(m, "p")
	if m.overlay != overlayNone || !strings.Contains(m.notice, "aguarde") {
		t.Fatalf("expected a wait notice, overlay=%v notice=%q", m.overlay, m.notice)
	}
}
