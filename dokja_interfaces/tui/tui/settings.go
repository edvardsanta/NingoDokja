package tui

import (
	"context"
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"dokja_interfaces/cli/cli"
)

type settingsMsg struct {
	view cli.SettingsView
	err  error
}

// settingLabel names a setting for people.
func settingLabel(id string) string {
	switch id {
	case "discord.meme_channels":
		return tr("setting_meme_channels")
	case "discord.safe_only_channels":
		return tr("setting_safe_only_channels")
	case "discord.answer_channels":
		return tr("setting_answer_channels")
	case "discord.direct_messages":
		return tr("setting_direct_messages")
	case "whatsapp.people":
		return tr("setting_people")
	case "whatsapp.groups":
		return tr("setting_groups")
	case "whatsapp.own_chat":
		return tr("setting_own_chat")
	case "models.embedding":
		return tr("setting_model_embedding")
	case "models.book_classifier":
		return tr("setting_model_book_classifier")
	case "models.book_overview":
		return tr("setting_model_book_overview")
	case "models.speech_to_text":
		return tr("setting_model_speech_to_text")
	case "models.ocr":
		return tr("setting_model_ocr")
	}
	return id
}

func groupLabel(group string) string {
	switch group {
	case "discord":
		return "Discord"
	case "whatsapp":
		return "WhatsApp"
	case "models":
		return tr("setting_group_models")
	}
	return group
}

// settingRows are the settings in the order they are shown and selected.
func (m *Model) settingRows() []cli.SettingRow {
	if m.settings == nil {
		return nil
	}
	var rows []cli.SettingRow
	for _, group := range cli.SettingGroups {
		for _, row := range m.settings.Settings {
			if row.Group == group {
				rows = append(rows, row)
			}
		}
	}
	return rows
}

func (m *Model) openSettings() tea.Cmd {
	if m.runtime == nil {
		m.setNotice(tr("modules_unavailable")+m.modulesErr, true)
		return nil
	}
	m.settings, m.settingsErr, m.settingsCursor, m.overlay = nil, "", 0, overlaySettings
	return m.cmdSettings()
}

func (m *Model) cmdSettings() tea.Cmd {
	if !m.begin(tr("settings_reading")) {
		return nil
	}
	runtime := m.runtime
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), min(m.timeout, 15*time.Second))
		defer cancel()
		view, err := runtime.Settings(ctx)
		return settingsMsg{view: view, err: err}
	}
}

func (m *Model) keySettings(key string) (tea.Model, tea.Cmd) {
	rows := m.settingRows()
	switch key {
	case "esc", "q":
		m.overlay = overlayNone
	case "up", "k":
		if m.settingsCursor > 0 {
			m.settingsCursor--
		}
	case "down", "j":
		if m.settingsCursor < len(rows)-1 {
			m.settingsCursor++
		}
	case "enter", "e":
		if m.settingsCursor < len(rows) {
			row := rows[m.settingsCursor]
			m.settingEditing = row.ID
			m.settingInput.SetValue(row.Value)
			m.settingInput.CursorEnd()
			m.settingInput.Focus()
			m.overlay = overlaySettingEdit
		}
	}
	return m, nil
}

func (m *Model) keySettingEdit(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.overlay = overlaySettings
		return m, nil
	case "enter":
		m.askSaveSetting(m.settingEditing, strings.TrimSpace(m.settingInput.Value()))
		return m, nil
	}
	var cmd tea.Cmd
	m.settingInput, cmd = m.settingInput.Update(msg)
	return m, cmd
}

// askSaveSetting confirms a change after saying which modules it recreates and what follows.
func (m *Model) askSaveSetting(id, value string) {
	running := map[string]bool{}
	if m.modules != nil {
		for _, module := range m.modules.Modules {
			running[module.Name] = !module.Stopped()
		}
	}
	lines := []string{fmt.Sprintf("%s = %s", settingLabel(id), value), ""}
	if m.settings != nil {
		impact := cli.ImpactOf(*m.settings, []string{id}, running)
		if len(impact.Now) > 0 {
			lines = append(lines, tr("setting_recreated_now", strings.Join(impact.Now, ", ")))
		}
		if len(impact.Later) > 0 {
			lines = append(lines, tr("setting_recreated_later", strings.Join(impact.Later, ", ")))
		}
		if impact.Sessions {
			lines = append(lines, tr("setting_sessions_end"))
		}
		if impact.Reindex {
			lines = append(lines, tr("setting_reindex"))
		}
	}
	runtime := m.runtime
	m.askConfirm(&pendingAction{
		label: tr("setting_save_label", settingLabel(id)),
		lines: lines,
		local: func() (string, error) {
			ctx, cancel := context.WithTimeout(context.Background(), m.timeout)
			defer cancel()
			view, err := runtime.SetSettings(ctx, map[string]string{id: value})
			for err == nil && view.Busy {
				select {
				case <-ctx.Done():
					return "", ctx.Err()
				case <-time.After(1200 * time.Millisecond):
				}
				view, err = runtime.Settings(ctx)
			}
			if err != nil {
				return "", err
			}
			if view.Error != "" {
				return "", fmt.Errorf("%s", view.Error)
			}
			return tr("setting_saved"), nil
		},
		onSuccess: func(m *Model, _ map[string]any) tea.Cmd { return m.cmdSettings() },
		reload:    true,
		back:      overlaySettings,
	})
}

func (m *Model) settingValue(row cli.SettingRow) string {
	if row.Value == "" {
		switch row.ID {
		case "discord.answer_channels":
			return tr("setting_every_channel")
		case "models.embedding", "models.book_classifier", "models.book_overview", "models.speech_to_text", "models.ocr":
			return tr("setting_default")
		}
		return tr("setting_none")
	}
	return cli.SettingValueText(*m.settings, row)
}

func (m *Model) viewSettings() string {
	var b strings.Builder
	b.WriteString(styleTitle.Render(tr("settings_title")) + "\n")
	if m.settingsErr != "" {
		b.WriteString(styleBad.Render(m.settingsErr) + "\n")
	}
	if m.settings == nil {
		return b.String() + styleDim.Render(tr("loading"))
	}
	if m.settings.Error != "" {
		b.WriteString(styleWarn.Render(tr("settings_last_change")+m.settings.Error) + "\n")
	}
	group := ""
	for i, row := range m.settingRows() {
		if row.Group != group {
			group = row.Group
			b.WriteString("\n" + styleTitle.Render(groupLabel(group)) + "\n")
		}
		marker := "  "
		if i == m.settingsCursor {
			marker = styleCursor.Render("▸ ")
		}
		b.WriteString(fmt.Sprintf("%s%-28s %s\n", marker, settingLabel(row.ID), m.settingValue(row)))
	}
	if len(m.settings.WebhookChannels) > 0 {
		b.WriteString("\n" + styleDim.Render(tr("settings_webhooks_by_hand")) + "\n")
	}
	return b.String()
}

func (m *Model) viewSettingEdit() string {
	hint := tr("setting_hint_list")
	for _, row := range m.settingRows() {
		if row.ID == m.settingEditing && len(row.Options) > 0 {
			hint = tr("setting_hint_options", strings.Join(row.Options, ", "))
		} else if row.ID == m.settingEditing && row.Kind == "text" {
			hint = tr("setting_hint_model")
		}
	}
	return styleBox.Render(styleTitle.Render(settingLabel(m.settingEditing)) + "\n\n" + hint + "\n" + m.settingInput.View())
}
