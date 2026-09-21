package tui

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
)

var (
	styleTitle  = lipgloss.NewStyle().Bold(true)
	styleActive = lipgloss.NewStyle().Bold(true).Reverse(true).Padding(0, 1)
	styleTab    = lipgloss.NewStyle().Padding(0, 1)
	styleDim    = lipgloss.NewStyle().Foreground(lipgloss.Color("8"))
	styleOK     = lipgloss.NewStyle().Foreground(lipgloss.Color("2"))
	styleWarn   = lipgloss.NewStyle().Foreground(lipgloss.Color("3"))
	styleBad    = lipgloss.NewStyle().Foreground(lipgloss.Color("1"))
	styleCursor = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("6"))
	styleBox    = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).Padding(1, 2)
)

func (m *Model) View() string {
	var body string
	switch m.overlay {
	case overlayConfirm:
		body = m.viewConfirm()
	case overlayPicker:
		body = m.viewPicker()
	case overlayDispatch:
		body = m.viewDispatch()
	case overlayInterval:
		body = m.viewInterval()
	case overlayProfiles:
		body = m.viewProfiles()
	case overlayProfileForm:
		body = m.viewProfileForm()
	default:
		switch m.tab {
		case tabPanel:
			body = m.viewPanel()
		case tabMemes:
			body = m.viewMemes()
		case tabDiscord:
			body = m.viewDiscord()
		case tabHistory:
			body = m.viewHistory()
		}
	}
	return strings.Join([]string{m.viewHeader(), "", body, "", m.viewFooter()}, "\n")
}

func (m *Model) viewHeader() string {
	titles := tabTitles()
	tabs := make([]string, len(titles))
	for i, name := range titles {
		label := fmt.Sprintf("%d %s", i+1, name)
		if tab(i) == m.tab {
			tabs[i] = styleActive.Render(label)
		} else {
			tabs[i] = styleTab.Render(label)
		}
	}
	line := lipgloss.JoinHorizontal(lipgloss.Top, tabs...)
	if m.busy {
		line += "  " + styleWarn.Render("⏳ "+m.busyLabel+"…")
	}
	return line
}

func (m *Model) viewFooter() string {
	hints := map[tab]string{
		tabPanel:   tr("↑↓ move · space toggle · x run job · i interval · p chat profiles · r refresh · tab/1-4 tabs · q quit"),
		tabMemes:   tr("↑↓ move · s NSFW filter · enter send this one · d dispatch · t queue/sent · n/p page · R refresh pool · q quit"),
		tabDiscord: tr("↑↓ field · space toggle channel · ctrl+s send · F1-F4 tabs · ctrl+c quit"),
		tabHistory: tr("tab/1-4 tabs · q quit"),
	}
	hint := hints[m.tab]
	switch m.overlay {
	case overlayConfirm:
		hint = tr("y/enter confirm · n/esc cancel")
	case overlayPicker:
		hint = tr("↑↓ move · space toggle · a all · enter continue · esc cancel")
	case overlayDispatch:
		hint = tr("enter continue · esc cancel")
	case overlayInterval:
		hint = tr("enter apply · esc cancel")
	case overlayProfiles:
		hint = tr("↑↓ move · enter activate · n new (local) · d delete (local) · esc close")
	case overlayProfileForm:
		hint = tr("tab/↑↓ field · ctrl+s save · esc back")
	}
	footer := styleDim.Render(hint)
	if m.notice != "" {
		style := styleOK
		if m.noticeErr {
			style = styleBad
		}
		footer = style.Render(m.notice) + "\n" + footer
	}
	return footer
}

func (m *Model) channelLabel(id string) string {
	if m.status != nil && m.status.Channels.isSafeOnly(id) {
		return id + styleWarn.Render(tr("  safe-only (NSFW filter)"))
	}
	return id + styleDim.Render(tr("  no filter"))
}

func (m *Model) viewPanel() string {
	if m.status == nil {
		if m.statusErr != "" {
			return styleBad.Render(tr("No answer from the orchestrator: ") + m.statusErr)
		}
		return styleDim.Render(tr("Loading…"))
	}
	s := m.status
	var b strings.Builder

	b.WriteString(styleTitle.Render(tr("Services")) + styleDim.Render(tr("   space toggles (the container keeps running; switching the scheduler off pauses every job)")) + "\n")
	for i, service := range s.Services {
		b.WriteString(m.panelLine(i, checkbox(service.Enabled), fmt.Sprintf("%-9s %s", service.Name, m.serviceState(service))))
	}

	b.WriteString("\n" + styleTitle.Render("Jobs") + styleDim.Render(tr("   space pauses/resumes · x runs now · i interval")) + "\n")
	for i, job := range s.Jobs {
		b.WriteString(m.panelLine(len(s.Services)+i, checkbox(job.Enabled), fmt.Sprintf("%-25s %s", job.Name, m.jobState(job))))
	}
	if hint := schedulerHint(s); hint != "" {
		b.WriteString(styleWarn.Render("  "+hint) + "\n")
	}

	b.WriteString("\n" + styleTitle.Render(tr("Meme pool")) + "\n")
	if s.MemeOff {
		b.WriteString("  " + styleWarn.Render(tr("meme service switched off: counts unavailable")) + "\n")
	} else {
		b.WriteString(tr("  %d queued · %d already sent\n", s.Unsent, s.Sent))
	}
	b.WriteString("\n" + styleTitle.Render(tr("Meme channels")) + "\n")
	for _, id := range s.Channels.Meme {
		b.WriteString("  " + m.channelLabel(id) + "\n")
	}
	b.WriteString("\n" + styleDim.Render(tr("updated at ")+s.UpdatedAt.Format("15:04:05")))
	if m.statusErr != "" {
		b.WriteString("\n" + styleBad.Render(tr("last refresh failed: ")+m.statusErr))
	}
	return b.String()
}

func checkbox(on bool) string {
	if on {
		return "[x]"
	}
	return "[ ]"
}

func (m *Model) panelLine(index int, box, text string) string {
	marker := "  "
	if index == m.panelCursor {
		marker = styleCursor.Render("▸ ")
	}
	return marker + box + " " + text + "\n"
}

func (m *Model) serviceState(service serviceRow) string {
	switch {
	case service.Status == "disabled":
		if service.Detail != "" {
			return styleWarn.Render(tr("switched off")) + styleDim.Render("  ("+trunc(service.Detail, 50)+")")
		}
		return styleWarn.Render(tr("switched off"))
	case service.Status == "error":
		return styleBad.Render("error") + styleDim.Render("  "+trunc(service.Detail, 60))
	case service.Status == "stopped":
		return styleWarn.Render(tr("stopped")) + styleDim.Render("  "+trunc(service.Detail, 60))
	case service.Status == "ok" && service.Name == "chat_ai" && m.status != nil:
		if profile, ok := m.status.activeProfile(); ok {
			return styleOK.Render("ok") + styleDim.Render(tr("  profile ")+profile.Name+" "+profile.KeyHint)
		}
		return styleOK.Render("ok")
	case service.Status == "ok":
		if service.Detail != "" {
			return styleOK.Render("ok") + styleDim.Render("  "+trunc(service.Detail, 50))
		}
		return styleOK.Render("ok")
	}
	return styleDim.Render(tr("no health probe"))
}

func (m *Model) jobState(job jobRow) string {
	every := tr("unknown interval")
	if job.Interval != "" {
		every = tr("every ") + job.Interval
		if job.Override {
			every += tr(" (changed)")
		}
	}
	parts := []string{styleDim.Render(fmt.Sprintf("%-24s", every))}
	if !job.Enabled {
		parts = append(parts, styleWarn.Render(tr("paused")))
	}
	if !job.LastAt.IsZero() {
		outcome := map[string]string{"ran": tr("ran"), "skipped": tr("skipped"), "error": tr("error")}[job.LastOutcome]
		text := tr("last ") + clock(job.LastAt, m.status.UpdatedAt)
		if outcome != "" {
			text += " " + outcome
		}
		style := styleDim
		if job.LastOutcome == "error" {
			style = styleBad
		}
		parts = append(parts, style.Render(text))
	}
	if job.Enabled && !job.NextAt.IsZero() {
		parts = append(parts, styleDim.Render(tr("next ")+clock(job.NextAt, m.status.UpdatedAt)+" ("+until(job.NextAt.Sub(m.status.UpdatedAt))+")"))
	}
	return strings.Join(parts, "  ")
}

// clock prints a time in the local zone, with the date only when it is not today.
func clock(t, now time.Time) string {
	t, now = t.Local(), now.Local()
	if t.YearDay() == now.YearDay() && t.Year() == now.Year() {
		return t.Format("15:04")
	}
	return t.Format("02/01 15:04")
}

func until(d time.Duration) string {
	switch {
	case d < time.Minute:
		return tr("in <1m")
	case d < time.Hour:
		return tr("in %dm", int(d.Minutes()))
	}
	return tr("in %dh%02dm", int(d.Hours()), int(d.Minutes())%60)
}

// schedulerHint warns when the scheduler has stopped announcing itself; it does so about
// once a minute, so a few minutes of silence means it is not running.
func schedulerHint(s *statusData) string {
	var newest time.Time
	for _, job := range s.Jobs {
		if job.AnnouncedAt.After(newest) {
			newest = job.AnnouncedAt
		}
	}
	switch {
	case len(s.Jobs) == 0:
		return ""
	case newest.IsZero():
		return tr("the scheduler has not announced anything yet (stopped?): intervals and next runs appear once it runs")
	case s.UpdatedAt.Sub(newest) > 3*time.Minute:
		return tr("last scheduler announce %dm ago (stopped?): the times below may be stale", int(s.UpdatedAt.Sub(newest).Minutes()))
	}
	return ""
}

const memeListWidth = 72

func (m *Model) viewMemes() string {
	var list strings.Builder
	scope := tr("queued")
	if m.memes.scope == "sent" {
		scope = tr("already sent")
	}
	page := m.memes.page
	from := 0
	if len(page.Items) > 0 {
		from = page.Offset + 1
	}
	list.WriteString(styleTitle.Render(fmt.Sprintf("Memes %s", scope)) +
		styleDim.Render(fmt.Sprintf("  %d–%d de %d", from, page.Offset+len(page.Items), page.Total)) + "\n\n")

	switch {
	case m.memes.err != "":
		return list.String() + styleBad.Render(tr("Failed to list: ")+m.memes.err)
	case !m.memes.loaded:
		return list.String() + styleDim.Render(tr("Loading…"))
	case len(page.Items) == 0:
		return list.String() + styleDim.Render(tr("Nothing here."))
	}

	for i, item := range page.Items {
		title := strings.TrimSpace(item.Title)
		if title == "" {
			title = tr("(untitled)")
		}
		line := fmt.Sprintf("%-52s %s", trunc(title, 50), styleDim.Render(fileName(item.URL)))
		if i == m.memes.cursor {
			list.WriteString(styleCursor.Render("▸ ") + line + "\n")
		} else {
			list.WriteString("  " + line + "\n")
		}
	}

	var detail strings.Builder
	if item, ok := m.selectedMeme(); ok {
		detail.WriteString("\n" + styleTitle.Render(tr("Selected")) + "\n")
		detail.WriteString(tr("  title: ") + trunc(item.Title, 80) + "\n")
		detail.WriteString("  tags:   " + trunc(item.Tags, 80) + "\n")
		detail.WriteString("  url:    " + trunc(item.URL, 90) + "\n")
		if item.DateSent != "" {
			detail.WriteString(tr("  sent at: ") + item.DateSent + "\n")
		}
		if m.screen != nil && m.screen.URL == item.URL {
			detail.WriteString("\n" + m.viewScreen(*m.screen))
		}
	}

	top := strings.TrimRight(list.String(), "\n")
	image := m.viewPreview()
	switch {
	case image == "":
	case m.width >= memeListWidth+4+previewMaxCols:
		top = lipgloss.JoinHorizontal(lipgloss.Top, lipgloss.NewStyle().Width(memeListWidth).Render(top), "  ", image)
		image = ""
	default:
		image = "\n" + image
	}
	return top + "\n" + detail.String() + image
}

// viewPreview draws the selected meme's image, or says why there is none.
func (m *Model) viewPreview() string {
	if !m.images.enabled() {
		return ""
	}
	item, ok := m.selectedMeme()
	if !ok {
		return ""
	}
	p := m.images.cache[item.URL]
	switch {
	case p == nil && m.images.inflight[item.URL]:
		return styleDim.Render(tr("loading image…"))
	case p == nil:
		return ""
	case p.err != "":
		return styleDim.Render(tr("(no preview: ") + trunc(p.err, 60) + ")")
	}
	return p.textAt(m.images.frame)
}

func (m *Model) viewScreen(s screenData) string {
	var b strings.Builder
	if s.Safe {
		b.WriteString(styleOK.Render(tr("NSFW filter: safe")) + "\n")
	} else {
		b.WriteString(styleBad.Render(tr("NSFW filter: BLOCKED — ")+s.Reason) + "\n")
	}
	if s.Text != "" {
		b.WriteString(tr("  text read: ") + trunc(s.Text, 90) + "\n")
	}
	if len(s.Detections) > 0 {
		b.WriteString(tr("  detections:  ") + strings.Join(s.Detections, ", ") + "\n")
	}
	return b.String()
}

func fileName(url string) string {
	if i := strings.LastIndex(url, "/"); i >= 0 {
		return url[i+1:]
	}
	return url
}

func (m *Model) viewDiscord() string {
	var b strings.Builder
	b.WriteString(styleTitle.Render(tr("Send message")) + "\n\n")
	b.WriteString(m.formRow(0, tr("Text "), m.form.text.View()))
	b.WriteString(m.formRow(1, tr("Image"), m.form.image.View()))
	b.WriteString("\n" + styleTitle.Render(tr("Channels")) + "\n")

	channels := []string{}
	if m.status != nil {
		channels = m.status.Channels.destinations()
	}
	if len(channels) == 0 {
		b.WriteString(styleDim.Render(tr("  (channels not loaded yet)")) + "\n")
	}
	for i, id := range channels {
		box := "[ ]"
		if m.form.selected[id] {
			box = "[x]"
		}
		b.WriteString(m.formRow(2+i, box, m.channelLabel(id)))
	}
	sendRow := 2 + len(channels)
	b.WriteString("\n" + m.formRow(sendRow, "", styleTitle.Render(tr("[ Send ]"))))
	return b.String()
}

func (m *Model) formRow(index int, label, content string) string {
	marker := "  "
	if m.form.focus == index {
		marker = styleCursor.Render("▸ ")
	}
	return fmt.Sprintf("%s%-7s %s\n", marker, label, content)
}

func (m *Model) viewHistory() string {
	if len(m.history) == 0 {
		return styleDim.Render(tr("Nothing has been triggered in this session yet."))
	}
	var b strings.Builder
	b.WriteString(styleTitle.Render(tr("Session history")) + "\n\n")
	for _, entry := range m.history {
		mark := styleOK.Render("✔")
		if !entry.OK {
			mark = styleBad.Render("✘")
		}
		b.WriteString(fmt.Sprintf("%s %s  %-22s %s\n", mark, entry.At.Format("15:04:05"), trunc(entry.Label, 22), trunc(entry.Summary, 80)))
	}
	return b.String()
}

func (m *Model) viewConfirm() string {
	if m.pending == nil {
		return ""
	}
	body := styleTitle.Render(tr("Confirm: ")+m.pending.label) + "\n\n" + strings.Join(m.pending.lines, "\n")
	if m.pending.local == nil {
		body += "\n\n" + styleWarn.Render(tr("This really posts to Discord."))
	}
	return styleBox.Render(body)
}

func (m *Model) viewPicker() string {
	var b strings.Builder
	b.WriteString(styleTitle.Render(tr("Send to which channels?")) + "\n")
	b.WriteString(styleDim.Render(trunc(m.picker.item.Title, 60)) + "\n\n")
	for i, id := range m.status.Channels.destinations() {
		box := "[ ]"
		if m.picker.selected[id] {
			box = "[x]"
		}
		marker := "  "
		if i == m.picker.cursor {
			marker = styleCursor.Render("▸ ")
		}
		b.WriteString(fmt.Sprintf("%s%s %s\n", marker, box, m.channelLabel(id)))
	}
	return styleBox.Render(b.String())
}

func (m *Model) viewDispatch() string {
	return styleBox.Render(
		styleTitle.Render(tr("Dispatch memes from the queue")) + "\n\n" +
			"Quantos? (1–" + fmt.Sprint(maxDispatchBatch) + ")  " + m.dispatchInput.View())
}

func (m *Model) viewInterval() string {
	return styleBox.Render(
		styleTitle.Render(tr("Interval of ")+m.intervalJob) + "\n\n" +
			tr("New interval (e.g. 45m, 6h; min 1m, max 720h)\n") +
			tr("or \"default\" to go back to the default:  ") + m.intervalInput.View())
}

func (m *Model) viewProfiles() string {
	var b strings.Builder
	b.WriteString(styleTitle.Render(tr("Chat profiles (provider + model + token)")) + "\n")
	b.WriteString(styleDim.Render(tr("the token is never shown; only its last 4 characters")) + "\n\n")
	if len(m.status.Profiles) == 0 {
		b.WriteString(styleDim.Render(tr("no profiles yet: chat uses the CHAT_AI_* environment variables.\n")) + tr("n creates the first one.\n"))
	}
	for i, row := range m.status.Profiles {
		marker, active := "  ", "   "
		if i == m.profileCursor {
			marker = styleCursor.Render("▸ ")
		}
		if row.Active {
			active = styleOK.Render("[*]")
		}
		hint := row.KeyHint
		if hint == "" {
			hint = "(oculto)"
		}
		b.WriteString(fmt.Sprintf("%s%s %-14s %-24s %s  %s\n", marker, active, trunc(row.Name, 14), trunc(row.Model, 24), styleDim.Render(trunc(row.BaseURL, 36)), styleDim.Render(hint)))
	}
	return styleBox.Render(b.String())
}

func (m *Model) viewProfileForm() string {
	labels := []string{tr("Name  "), "URL   ", tr("Model"), "Token "}
	var b strings.Builder
	b.WriteString(styleTitle.Render(tr("New chat profile")) + "\n")
	b.WriteString(styleDim.Render(tr("stored on this computer only; it never goes through the orchestrator")) + "\n\n")
	for i, label := range labels {
		marker := "  "
		if i == m.profileFocus {
			marker = styleCursor.Render("▸ ")
		}
		b.WriteString(fmt.Sprintf("%s%s %s\n", marker, label, m.profileForm[i].View()))
	}
	return styleBox.Render(b.String())
}
