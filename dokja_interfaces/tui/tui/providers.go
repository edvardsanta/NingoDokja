package tui

import (
	"context"
	"strings"
	"time"

	"dokja_interfaces/cli/cli"
	tea "github.com/charmbracelet/bubbletea"
)

type providersMsg struct {
	view cli.ProvidersView
	err  error
}

func (m *Model) cmdProviders() tea.Cmd {
	if m.runtime == nil || !m.begin(tr("settings_reading")) {
		return nil
	}
	runtime := m.runtime
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), min(m.timeout, 15*time.Second))
		defer cancel()
		view, err := runtime.Providers(ctx)
		return providersMsg{view: view, err: err}
	}
}
func (m *Model) keyProviders(key string) (tea.Model, tea.Cmd) {
	switch key {
	case "up", "k":
		m.providersCursor = max(0, m.providersCursor-1)
	case "down", "j":
		if m.providers != nil {
			m.providersCursor = min(len(m.providers.Providers)-1, m.providersCursor+1)
		}
	case "e", "enter":
		if m.providers == nil || len(m.providers.Providers) == 0 {
			return m, nil
		}
		if m.providers.Revision == 0 {
			m.setNotice(tr("provider_configuration_unavailable"), true)
			return m, nil
		}
		row := m.providers.Providers[m.providersCursor]
		m.providerEditingRevision = m.providers.Revision
		m.providerEditing = cli.ProviderRegistration{ID: row.ID, Enabled: row.Enabled, Models: row.DeclaredModels}
		m.settingInput.CharLimit = 2200
		m.settingInput.SetValue(strings.Join(row.DeclaredModels, ", "))
		m.settingInput.CursorEnd()
		m.settingInput.Focus()
		m.overlay = overlayProviderEdit

	case "esc", "q":
		m.overlay = overlaySettings
	case "r":
		return m, m.cmdProviders()
	}
	return m, nil
}
func (m *Model) viewProviders() string {
	var b strings.Builder
	b.WriteString(styleTitle.Render(tr("providers_title")) + "\n\n" + tr("providers_discovery_only") + "\n")
	if m.providersErr != "" {
		return b.String() + styleBad.Render(m.providersErr)
	}
	if m.providers == nil {
		return b.String() + tr("loading")
	}
	for i, row := range m.providers.Providers {
		state := tr("provider_unavailable")
		switch row.State {
		case "installed":
			state = tr("provider_installed")
		case "reachable":
			state = tr("provider_reachable")
		}
		if row.Reason == "service_stopped" {
			state = tr("provider_service_stopped")
		}
		if row.Reason == "disabled" {
			state = tr("provider_disabled")
		}
		marker := "  "
		if i == m.providersCursor {
			marker = "▸ "
		}
		b.WriteString("\n" + marker + row.Name + ": " + state + "\n")
		if m.providers.Revision != 0 {
			if len(row.DeclaredModels) == 0 {
				b.WriteString(tr("provider_no_declarations") + "\n")
			} else {
				b.WriteString(tr("provider_declared_models") + ": " + strings.Join(row.DeclaredModels, ", ") + "\n")
			}
		}

		if row.ModelDiscovery == "listed" {
			if len(row.Models) == 0 {
				b.WriteString(tr("provider_no_models"))
			} else {
				b.WriteString(strings.Join(row.Models, ", "))
			}
		} else {
			b.WriteString(tr("provider_models_unavailable"))
		}
		if row.Truncated {
			b.WriteString("\n" + tr("provider_models_truncated"))
		}
		b.WriteString("\n")
	}
	return b.String()
}
