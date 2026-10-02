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
	case overlayHashtag:
		body = m.viewHashtag()
	case overlayHashtagList:
		body = m.viewHashtagList()
	case overlayMemory:
		body = m.viewMemory()
	case overlayMemoryList:
		body = m.viewMemoryList()
	case overlayMemoryPredict:
		body = m.viewMemoryPredict()
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
		tabPanel:   tr("panel_tab_help"),
		tabMemes:   tr("meme_tab_help"),
		tabDiscord: tr("field_space_toggle_channel_ctrl_s_send_f1_f4_tabs_ctrl_c_quit"),
		tabHistory: tr("tab_1_4_tabs_q_quit"),
	}
	hint := hints[m.tab]
	switch m.overlay {
	case overlayConfirm:
		hint = tr("y_enter_confirm_n_esc_cancel")
	case overlayPicker:
		hint = tr("channel_picker_help")
	case overlayDispatch:
		hint = tr("enter_continue_esc_cancel")
	case overlayInterval:
		hint = tr("enter_apply_esc_cancel")
	case overlayProfiles:
		hint = tr("move_enter_activate_n_new_local_d_delete_local_esc_close")
	case overlayProfileForm:
		hint = tr("tab_field_ctrl_s_save_esc_back")
	case overlayHashtag:
		hint = tr("enter_save_esc_cancel")
	case overlayHashtagList:
		hint = tr("hashtag_list_help")
	case overlayMemory:
		hint = tr("memory_overlay_help")
	case overlayMemoryList:
		hint = tr("memory_list_help")
	case overlayMemoryPredict:
		hint = tr("memory_predict_help")
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
		return id + styleWarn.Render(tr("safe_only_nsfw_filter"))
	}
	return id + styleDim.Render(tr("no_filter"))
}

func (m *Model) viewPanel() string {
	if m.status == nil {
		if m.statusErr != "" {
			return styleBad.Render(tr("no_answer_from_the_orchestrator") + m.statusErr)
		}
		return styleDim.Render(tr("loading"))
	}
	s := m.status
	var b strings.Builder

	b.WriteString(styleTitle.Render(tr("services")) + styleDim.Render(tr("service_toggle_help")) + "\n")
	for i, service := range s.Services {
		b.WriteString(m.panelLine(i, checkbox(service.Enabled), fmt.Sprintf("%-9s %s", service.Name, m.serviceState(service))))
	}

	b.WriteString("\n" + styleTitle.Render("Jobs") + styleDim.Render(tr("space_pauses_resumes_x_runs_now_i_interval")) + "\n")
	for i, job := range s.Jobs {
		b.WriteString(m.panelLine(len(s.Services)+i, checkbox(job.Enabled), fmt.Sprintf("%-25s %s", job.Name, m.jobState(job))))
	}
	if hint := schedulerHint(s); hint != "" {
		b.WriteString(styleWarn.Render("  "+hint) + "\n")
	}

	b.WriteString("\n" + styleTitle.Render(tr("meme_pool")) + "\n")
	if s.MemeOff {
		b.WriteString("  " + styleWarn.Render(tr("meme_service_switched_off_counts_unavailable")) + "\n")
	} else {
		b.WriteString(tr("d_queued_d_already_sent", s.Unsent, s.Sent))
	}
	b.WriteString("\n" + styleTitle.Render(tr("meme_channels")) + "\n")
	for _, id := range s.Channels.Meme {
		b.WriteString("  " + m.channelLabel(id) + "\n")
	}
	b.WriteString("\n" + styleDim.Render(tr("updated_at")+s.UpdatedAt.Format("15:04:05")))
	if m.statusErr != "" {
		b.WriteString("\n" + styleBad.Render(tr("last_refresh_failed")+m.statusErr))
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
			return styleWarn.Render(tr("switched_off")) + styleDim.Render("  ("+trunc(service.Detail, 50)+")")
		}
		return styleWarn.Render(tr("switched_off"))
	case service.Status == "error":
		return styleBad.Render("error") + styleDim.Render("  "+trunc(service.Detail, 60))
	case service.Status == "stopped":
		return styleWarn.Render(tr("stopped")) + styleDim.Render("  "+trunc(service.Detail, 60))
	case service.Status == "ok" && service.Name == "chat_ai" && m.status != nil:
		if profile, ok := m.status.activeProfile(); ok {
			return styleOK.Render("ok") + styleDim.Render(tr("profile_status_prefix")+profile.Name+" "+profile.KeyHint)
		}
		return styleOK.Render("ok")
	case service.Status == "ok":
		if service.Detail != "" {
			return styleOK.Render("ok") + styleDim.Render("  "+trunc(service.Detail, 50))
		}
		return styleOK.Render("ok")
	}
	return styleDim.Render(tr("no_health_probe"))
}

func (m *Model) jobState(job jobRow) string {
	every := tr("unknown_interval")
	if job.Interval != "" {
		every = tr("every") + job.Interval
		if job.Override {
			every += tr("changed")
		}
	}
	parts := []string{styleDim.Render(fmt.Sprintf("%-24s", every))}
	if !job.Enabled {
		parts = append(parts, styleWarn.Render(tr("paused")))
	}
	if !job.LastAt.IsZero() {
		outcome := map[string]string{"ran": tr("ran"), "skipped": tr("skipped"), "error": tr("error")}[job.LastOutcome]
		text := tr("last") + clock(job.LastAt, m.status.UpdatedAt)
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
		parts = append(parts, styleDim.Render(tr("next")+clock(job.NextAt, m.status.UpdatedAt)+" ("+until(job.NextAt.Sub(m.status.UpdatedAt))+")"))
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
		return tr("in_1m")
	case d < time.Hour:
		return tr("in_dm", int(d.Minutes()))
	}
	return tr("in_dh_02dm", int(d.Hours()), int(d.Minutes())%60)
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
		return tr("scheduler_missing_announcement")
	case s.UpdatedAt.Sub(newest) > 3*time.Minute:
		return tr("last_scheduler_announce_dm_ago_stopped_the_times_below_may_be_stale", int(s.UpdatedAt.Sub(newest).Minutes()))
	}
	return ""
}

const memeListWidth = 72

func (m *Model) viewMemes() string {
	var list strings.Builder
	scope := tr("queued")
	if m.memes.scope == "sent" {
		scope = tr("already_sent")
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
		return list.String() + styleBad.Render(tr("failed_to_list")+m.memes.err)
	case !m.memes.loaded:
		return list.String() + styleDim.Render(tr("loading"))
	case len(page.Items) == 0:
		return list.String() + styleDim.Render(tr("nothing_here"))
	}

	for i, item := range page.Items {
		title := strings.TrimSpace(item.Title)
		if title == "" {
			title = tr("untitled")
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
		detail.WriteString("\n" + styleTitle.Render(tr("selected")) + "\n")
		detail.WriteString(tr("title") + trunc(item.Title, 80) + "\n")
		detail.WriteString("  tags:   " + trunc(item.Tags, 80) + "\n")
		detail.WriteString("  url:    " + trunc(item.URL, 90) + "\n")
		if item.DateSent != "" {
			detail.WriteString(tr("sent_at") + item.DateSent + "\n")
		}
		if m.screen != nil && m.screen.URL == item.URL {
			detail.WriteString("\n" + m.viewScreen(*m.screen))
		}
		if m.suggestion != nil && m.suggestion.URL == item.URL {
			detail.WriteString("\n" + viewSuggestion(*m.suggestion))
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

// viewSuggestion says why a hashtag was suggested: how close the closest tagged meme was and what
// was read from this meme and from that one, so a wrong suggestion can be traced to the text OCR read.
func viewSuggestion(s suggestionEvidence) string {
	var b strings.Builder
	b.WriteString(styleTitle.Render(tr("hashtag_suggestion")) + "\n")
	switch {
	case s.Hashtag == "":
		b.WriteString("  " + styleDim.Render(trunc(firstNonEmpty(s.Reason, tr("no_hashtag_suggestion")), 90)) + "\n")
	case s.Relevant:
		b.WriteString("  " + tr("suggested") + s.Hashtag + "\n")
	default:
		b.WriteString("  " + styleWarn.Render(tr("closest_is")+s.Hashtag+tr("below_relevance_threshold")) + "\n")
	}
	if s.HasScore {
		b.WriteString(tr("suggestion_closeness", s.Score, s.Threshold) + "\n")
	}
	if s.TextRead != "" {
		b.WriteString(tr("text_read") + trunc(s.TextRead, 100) + "\n")
	}
	if s.Matched != "" {
		b.WriteString(tr("suggestion_closest_text") + trunc(s.Matched, 100) + "\n")
	}
	return b.String()
}

// viewPreview draws the selected meme's image, or says why there is none.
func (m *Model) viewPreview() string {
	if !m.images.enabled() {
		return ""
	}
	item, ok := m.selectedPreviewItem()
	if !ok {
		return ""
	}
	p := m.images.cache[item.URL]
	switch {
	case p == nil && m.images.inflight[item.URL]:
		return styleDim.Render(tr("loading_image"))
	case p == nil:
		return ""
	case p.err != "":
		return styleDim.Render(tr("no_preview") + trunc(p.err, 60) + ")")
	}
	return p.textAt(m.images.frame)
}

func (m *Model) viewScreen(s screenData) string {
	var b strings.Builder
	if s.Safe {
		b.WriteString(styleOK.Render(tr("nsfw_filter_safe")) + "\n")
	} else {
		b.WriteString(styleBad.Render(tr("nsfw_filter_blocked")+s.Reason) + "\n")
	}
	if s.Text != "" {
		b.WriteString(tr("text_read") + trunc(s.Text, 90) + "\n")
	}
	if len(s.Detections) > 0 {
		b.WriteString(tr("detections") + strings.Join(s.Detections, ", ") + "\n")
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
	b.WriteString(styleTitle.Render(tr("send_message_heading")) + "\n\n")
	b.WriteString(m.formRow(0, tr("text_field"), m.form.text.View()))
	b.WriteString(m.formRow(1, tr("image_field"), m.form.image.View()))
	b.WriteString("\n" + styleTitle.Render(tr("channels")) + "\n")

	channels := []string{}
	if m.status != nil {
		channels = m.status.Channels.destinations()
	}
	if len(channels) == 0 {
		b.WriteString(styleDim.Render(tr("channels_not_loaded_yet")) + "\n")
	}
	for i, id := range channels {
		box := "[ ]"
		if m.form.selected[id] {
			box = "[x]"
		}
		b.WriteString(m.formRow(2+i, box, m.channelLabel(id)))
	}
	sendRow := 2 + len(channels)
	b.WriteString("\n" + m.formRow(sendRow, "", styleTitle.Render(tr("send"))))
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
		return styleDim.Render(tr("nothing_has_been_triggered_in_this_session_yet"))
	}
	var b strings.Builder
	b.WriteString(styleTitle.Render(tr("session_history")) + "\n\n")
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
	body := styleTitle.Render(tr("confirm")+m.pending.label) + "\n\n" + strings.Join(m.pending.lines, "\n")
	if m.pending.local == nil && strings.HasPrefix(m.pending.eventType, "discord.") {
		body += "\n\n" + styleWarn.Render(tr("this_really_posts_to_discord"))
	}
	return styleBox.Render(body)
}

func (m *Model) viewPicker() string {
	var b strings.Builder
	b.WriteString(styleTitle.Render(tr("send_to_which_channels")) + "\n")
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
	force := tr("force_nsfw_off")
	if m.picker.forceNSFW {
		force = styleBad.Render(tr("force_nsfw_on"))
	}
	b.WriteString("\n" + tr("force_nsfw_label") + force + "\n")
	return styleBox.Render(b.String())
}

func (m *Model) viewDispatch() string {
	return styleBox.Render(
		styleTitle.Render(tr("dispatch_memes_from_the_queue")) + "\n\n" +
			"Quantos? (1–" + fmt.Sprint(maxDispatchBatch) + ")  " + m.dispatchInput.View())
}

func (m *Model) viewHashtag() string {
	item, _ := m.selectedMeme()
	return styleBox.Render(
		styleTitle.Render(tr("learn_hashtag_from_selected_meme")) + "\n\n" +
			"Meme: " + trunc(item.Title, 70) + "\n" +
			tr("the_image_text_is_read_by_ocr_automatically") + "\n\n" +
			"Hashtag: " + m.hashtagInput.View())
}

func (m *Model) viewHashtagList() string {
	var list strings.Builder
	list.WriteString(styleTitle.Render(tr("tagged_memes")) +
		styleDim.Render(fmt.Sprintf("  %d", m.hashtagList.Total)) + "\n\n")
	if len(m.hashtagList.Items) == 0 {
		return styleBox.Render(list.String() + styleDim.Render(tr("no_tagged_memes")))
	}

	visible := 8
	if m.height > 0 {
		visible = max(3, min(12, m.height-14))
	}
	start := max(0, m.hashtagList.Cursor-visible+1)
	end := min(len(m.hashtagList.Items), start+visible)
	for i := start; i < end; i++ {
		example := m.hashtagList.Items[i]
		state := styleOK.Render(fmt.Sprintf("%-16s", tr("embedding_ready")))
		if !example.Embedded {
			state = styleWarn.Render(fmt.Sprintf("%-16s", tr("embedding_missing")))
		}
		line := fmt.Sprintf("%-22s %s %s", trunc(example.Hashtag, 20), state, styleDim.Render(fileName(example.SourceURL)))
		if i == m.hashtagList.Cursor {
			list.WriteString(styleCursor.Render("▸ ") + line + "\n")
		} else {
			list.WriteString("  " + line + "\n")
		}
	}

	var detail strings.Builder
	if example, ok := m.selectedHashtagExample(); ok {
		detail.WriteString("\n" + styleTitle.Render(example.Hashtag) + "\n")
		detail.WriteString(tr("text_read") + trunc(example.Text, 100) + "\n")
		detail.WriteString("  URL: " + trunc(example.SourceURL, 100) + "\n")
		if example.UpdatedAt != "" {
			detail.WriteString(tr("updated_at") + example.UpdatedAt + "\n")
		}
		if !example.Embedded {
			detail.WriteString(styleWarn.Render(tr("embedding_missing_help")) + "\n")
		}
	}

	top := strings.TrimRight(list.String(), "\n")
	image := m.viewPreview()
	if image != "" && m.width >= memeListWidth+4+previewMaxCols {
		top = lipgloss.JoinHorizontal(lipgloss.Top, lipgloss.NewStyle().Width(memeListWidth).Render(top), "  ", image)
		image = ""
	} else if image != "" {
		image = "\n" + image
	}
	return styleBox.Render(top + "\n" + detail.String() + image)
}

// viewMemory shows the experience memory's state and how its predictions are doing. A score is
// never called a win before there are enough scored predictions to say so.
func (m *Model) viewMemory() string {
	var b strings.Builder
	b.WriteString(styleTitle.Render(tr("experience_memory")) + "\n\n")
	v := m.memory
	switch {
	case !v.loaded:
		b.WriteString(styleDim.Render(tr("loading")))
	case v.off != "":
		b.WriteString(styleWarn.Render(tr("memory_switched_off")))
		b.WriteString(styleDim.Render("  (" + trunc(v.off, 60) + ")"))
	case v.statusErr != "":
		// The reason goes on its own line so a long error does not stretch the box.
		b.WriteString(styleBad.Render(strings.TrimRight(tr("no_answer_from_the_memory_service"), " ")) + "\n")
		b.WriteString("  " + styleDim.Render(trunc(v.statusErr, 68)))
	default:
		b.WriteString(viewMemoryStatus(*v.status))
		b.WriteString("\n" + viewMemoryScore(v))
	}
	return styleBox.Render(strings.TrimRight(b.String(), "\n"))
}

func viewMemoryStatus(s memoryStatus) string {
	var b strings.Builder
	b.WriteString("  " + tr("memory_counts", s.Experiences, s.Pending, s.Resolved, s.Expired) + "\n")
	switch {
	case !s.Embeddings:
		b.WriteString("  " + styleWarn.Render(tr("memory_embeddings_off")) + "\n")
	case !s.EmbedderReachable:
		b.WriteString("  " + styleWarn.Render(tr("memory_embedder_down")) + "\n")
	default:
		b.WriteString("  " + styleOK.Render(tr("memory_similarity_on", s.EmbedModel)) + "\n")
	}
	if s.NeedsReindex > 0 {
		b.WriteString("  " + styleWarn.Render(tr("memory_needs_reindex", s.NeedsReindex)) + "\n")
	}
	return b.String()
}

func viewMemoryScore(v memoryView) string {
	var b strings.Builder
	b.WriteString(styleTitle.Render(tr("memory_score_title", memoryScoredAction)) + "\n")
	switch {
	case v.scoreErr != "":
		b.WriteString("  " + styleBad.Render(strings.TrimRight(tr("memory_score_failed"), " ")) + "\n")
		b.WriteString("    " + styleDim.Render(trunc(v.scoreErr, 66)) + "\n")
	case v.score == nil || v.score.Scored == 0:
		b.WriteString("  " + styleDim.Render(tr("memory_nothing_scored")) + "\n")
	default:
		s := v.score
		if !s.EnoughData {
			b.WriteString("  " + styleWarn.Render(tr("memory_too_few_to_judge", s.Scored, s.MinScored)) + "\n")
		}
		b.WriteString("  " + tr("memory_scored_counts", s.Scored, s.Unscored) + "\n")
		b.WriteString("  " + tr("memory_brier", s.BrierPrediction, s.BrierBaseline) + "\n")
		switch {
		case !s.EnoughData:
			// No verdict until there are enough scored predictions to give one.
		case s.BeatsBaseline:
			b.WriteString("  " + styleOK.Render(tr("memory_beats_baseline", s.Skill)) + "\n")
		default:
			b.WriteString("  " + styleWarn.Render(tr("memory_does_not_beat_baseline", s.Skill)) + "\n")
		}
	}
	return b.String()
}

func (m *Model) viewInterval() string {
	return styleBox.Render(
		styleTitle.Render(tr("interval_heading_prefix")+m.intervalJob) + "\n\n" +
			tr("new_interval_e_g_45m_6h_min_1m_max_720h") +
			tr("or_default_to_go_back_to_the_default") + m.intervalInput.View())
}

func (m *Model) viewProfiles() string {
	var b strings.Builder
	b.WriteString(styleTitle.Render(tr("chat_profiles_provider_model_token")) + "\n")
	b.WriteString(styleDim.Render(tr("the_token_is_never_shown_only_its_last_4_characters")) + "\n\n")
	if len(m.status.Profiles) == 0 {
		b.WriteString(styleDim.Render(tr("no_profiles_yet_chat_uses_the_chat_ai_environment_variables")) + tr("n_creates_the_first_one"))
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
	labels := []string{tr("name"), "URL   ", tr("model_field"), "Token "}
	var b strings.Builder
	b.WriteString(styleTitle.Render(tr("new_chat_profile")) + "\n")
	b.WriteString(styleDim.Render(tr("stored_on_this_computer_only_it_never_goes_through_the_orchestrator")) + "\n\n")
	for i, label := range labels {
		marker := "  "
		if i == m.profileFocus {
			marker = styleCursor.Render("▸ ")
		}
		b.WriteString(fmt.Sprintf("%s%s %s\n", marker, label, m.profileForm[i].View()))
	}
	return styleBox.Render(b.String())
}
