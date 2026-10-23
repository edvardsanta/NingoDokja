package tui

import (
	"context"
	"strings"

	"dokja_interfaces/cli/cli"
	tea "github.com/charmbracelet/bubbletea"
)

func (m *Model) keyProviderEdit(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.overlay = overlayProviders
		return m, nil
	case "ctrl+t":
		m.providerEditing.Enabled = !m.providerEditing.Enabled
		return m, nil
	case "enter":
		m.providerEditing.Models = cli.DeclaredProviderModels(m.settingInput.Value())
		m.askSaveProvider()
		return m, nil
	}
	var cmd tea.Cmd
	m.settingInput, cmd = m.settingInput.Update(msg)
	return m, cmd
}
func (m *Model) viewProviderEdit() string {
	state := tr("provider_disabled")
	if m.providerEditing.Enabled {
		state = tr("provider_discovery_enabled")
	}
	return styleBox.Render(styleTitle.Render(m.providerEditing.ID) + "\n\n" + state + "\n" + tr("provider_declared_models") + "\n" + m.settingInput.View() + "\n" + tr("provider_models_hint") + "\n\n" + tr("provider_registration_notice"))
}
func (m *Model) askSaveProvider() {
	if m.providers == nil || m.providerEditingRevision == 0 {
		m.setNotice(tr("provider_configuration_unavailable"), true)
		return
	}
	revision, registration, runtime := m.providerEditingRevision, m.providerEditing, m.runtime
	names := strings.Join(registration.Models, ", ")
	if names == "" {
		names = tr("provider_no_declarations")
	}
	state := tr("provider_disabled")
	if registration.Enabled {
		state = tr("provider_discovery_enabled")
	}
	m.askConfirm(&pendingAction{
		label: tr("provider_save_registration"),
		lines: []string{registration.ID + ": " + state, tr("provider_declared_models") + ": " + names, tr("provider_registration_notice")},
		local: func() (string, error) {
			ctx, cancel := context.WithTimeout(context.Background(), m.timeout)
			defer cancel()
			if _, err := runtime.ConfigureProvider(ctx, revision, registration); err != nil {
				return "", err
			}
			return tr("provider_registration_saved"), nil
		},
		onSuccess: func(m *Model, _ map[string]any) tea.Cmd { return m.cmdProviders() },
		back:      overlayProviders,
	})
}
