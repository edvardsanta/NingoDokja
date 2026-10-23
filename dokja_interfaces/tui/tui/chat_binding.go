package tui

import (
	"context"
	"strings"
	"time"

	"dokja_interfaces/cli/cli"
	tea "github.com/charmbracelet/bubbletea"
)

type chatBindingMsg struct {
	binding cli.ChatBinding
	err     error
}

func (m *Model) openChatBinding() tea.Cmd {
	if m.providers == nil || m.providers.Revision == 0 || m.runtime == nil {
		m.setNotice(tr("provider_configuration_unavailable"), true)
		return nil
	}
	m.overlay, m.chatCursor = overlayChatBinding, 0
	m.chatBinding, m.chatBindingErr, m.chatChoices = cli.ChatBinding{}, "", nil
	// Freeze the offered declarations for this edit. A late inventory response
	// cannot silently change the selected provider/model while confirmation is open.
	for _, row := range m.providers.Providers {
		if !row.Enabled {
			continue
		}
		for _, model := range row.DeclaredModels {
			m.chatChoices = append(m.chatChoices, cli.ChatBinding{Provider: row.ID, Model: model})
		}
	}
	if !m.begin(tr("settings_reading")) {
		return nil
	}
	runtime := m.runtime
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), min(m.timeout, 15*time.Second))
		defer cancel()
		binding, err := runtime.ChatBinding(ctx)
		return chatBindingMsg{binding, err}
	}
}

func (m *Model) keyChatBinding(key string) (tea.Model, tea.Cmd) {
	switch key {
	case "esc", "q":
		m.overlay = overlayProviders
	case "r":
		return m, m.openChatBinding()
	case "up", "k":
		m.chatCursor = max(0, m.chatCursor-1)
	case "down", "j":
		m.chatCursor = max(0, min(len(m.chatChoices)-1, m.chatCursor+1))
	case "enter":
		if m.chatBinding.Revision == 0 || m.chatBindingErr != "" || len(m.chatChoices) == 0 {
			return m, nil
		}
		choice, runtime := m.chatChoices[m.chatCursor], m.runtime
		choice.Revision = m.chatBinding.Revision
		m.askConfirm(&pendingAction{
			label: tr("chat_binding_save"),
			lines: []string{choice.Provider + " / " + choice.Model, tr("chat_binding_notice")},
			local: func() (string, error) {
				ctx, cancel := context.WithTimeout(context.Background(), m.timeout)
				defer cancel()
				if _, err := runtime.ConfigureChat(ctx, choice); err != nil {
					return "", err
				}
				return tr("chat_binding_saved"), nil
			},
			onSuccess: func(m *Model, _ map[string]any) tea.Cmd { return m.openChatBinding() },
			back:      overlayChatBinding,
		})
	}
	return m, nil
}

func (m *Model) viewChatBinding() string {
	var b strings.Builder
	b.WriteString(styleTitle.Render(tr("chat_binding_title")) + "\n\n" + tr("chat_binding_notice") + "\n\n")
	if m.chatBindingErr != "" {
		return b.String() + styleBad.Render(m.chatBindingErr)
	}
	if m.chatBinding.Revision == 0 {
		return b.String() + tr("loading")
	}
	current := tr("chat_binding_missing")
	if m.chatBinding.Provider != "" {
		current = m.chatBinding.Provider + " / " + m.chatBinding.Model
	}
	b.WriteString(current + "\n\n")
	if len(m.chatChoices) == 0 {
		return b.String() + tr("chat_binding_no_choices")
	}
	for i, choice := range m.chatChoices {
		marker := "  "
		if i == m.chatCursor {
			marker = "▸ "
		}
		b.WriteString(marker + choice.Provider + " / " + choice.Model + "\n")
	}
	return b.String()
}
