package tui

import (
	"strings"
	"testing"

	"dokja_interfaces/cli/cli"
	tea "github.com/charmbracelet/bubbletea"
)

func editableProviders(t *testing.T) (*Model, *fakeRuntime) {
	m, r := settingsModel(t)
	r.providers = cli.ProvidersView{Revision: 4, Providers: []cli.ProviderRow{{ID: "codex", Name: "Codex", Enabled: true, DeclaredModels: []string{"fixture/old"}, State: "installed", InferenceStatus: "not_tested"}}}
	press(m, "c", "p")
	return m, r
}
func providerWriteCalls(r *fakeRuntime) []call {
	out := []call{}
	for _, c := range r.calls {
		if c.eventType == "runtime.providers.configure" {
			out = append(out, c)
		}
	}
	return out
}
func TestTUIProviderEditRequiresConfirmationAndKeepsRevision(t *testing.T) {
	m, r := editableProviders(t)
	press(m, "e")
	m.settingInput.SetValue("fixture/new")
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyCtrlT})
	pump(m, cmd)
	press(m, "enter")
	if m.overlay != overlayConfirm || len(providerWriteCalls(r)) != 0 {
		t.Fatal("registration saved before confirmation")
	}
	if !strings.Contains(m.View(), "fixture/new") || !strings.Contains(m.View(), tr("provider_disabled")) {
		t.Fatal(m.View())
	}
	press(m, "y")
	writes := providerWriteCalls(r)
	if len(writes) != 1 || writes[0].payload["configuration_revision"] != uint64(4) || writes[0].payload["enabled"] != false || m.overlay != overlayProviders {
		t.Fatal(writes, m.overlay)
	}
}
func TestTUIProviderEditCancellationAndConflictDoNotRetry(t *testing.T) {
	m, r := editableProviders(t)
	press(m, "e", "enter", "n")
	if len(providerWriteCalls(r)) != 0 {
		t.Fatal("cancel saved registration")
	}
	press(m, "e")
	r.providers.Revision++
	press(m, "enter", "y")
	if len(providerWriteCalls(r)) != 1 || !m.noticeErr || !strings.Contains(m.notice, "reload") {
		t.Fatal(r.calls, m.notice)
	}
}
func TestOldControllerProviderInventoryStaysReadOnly(t *testing.T) {
	m, r := editableProviders(t)
	m.providers.Revision = 0
	press(m, "e")
	if m.overlay != overlayProviders || len(providerWriteCalls(r)) != 0 {
		t.Fatal(m.overlay, r.calls)
	}
}

func TestProviderDraftKeepsItsOriginalRevisionAfterLateRefresh(t *testing.T) {
	m, r := editableProviders(t)
	press(m, "e")
	m.settingInput.SetValue("fixture/draft")
	r.providers.Revision++
	_, cmd := m.Update(providersMsg{view: r.providers})
	pump(m, cmd)
	press(m, "enter", "y")
	writes := providerWriteCalls(r)
	if len(writes) != 1 || writes[0].payload["configuration_revision"] != uint64(4) || !m.noticeErr {
		t.Fatal(writes, m.notice)
	}
}
