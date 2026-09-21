package tui

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"

	"dokja_interfaces/cli/cli"
	store "read_books/dokja_store"
)

type tab int

const (
	tabPanel tab = iota
	tabMemes
	tabDiscord
	tabHistory
)

// tabTitles is a function, not a variable, so it follows the language chosen at start-up.
func tabTitles() []string { return []string{tr("Panel"), "Memes", "Discord", tr("History")} }

type overlayKind int

const (
	overlayNone overlayKind = iota
	overlayPicker
	overlayDispatch
	overlayConfirm
	overlayInterval
	overlayProfiles
	overlayProfileForm
)

const (
	pageSize         = 12
	maxDispatchBatch = 20
	maxHistory       = 200
)

type tickMsg time.Time

type statusMsg struct {
	data statusData
	err  error
}

type memesMsg struct {
	page memePage
	err  error
}

type screenMsg struct {
	data screenData
	err  error
}

type actionMsg struct {
	action *pendingAction
	res    map[string]any
	err    error
}

// pendingAction is a request waiting for (or past) confirmation.
type pendingAction struct {
	label     string
	eventType string
	payload   map[string]any
	lines     []string
	summarize func(map[string]any) string
	reload    bool

	// local runs instead of an orchestrator request, for actions that must stay on this machine.
	local func() (string, error)
}

type memeList struct {
	scope  string
	page   memePage
	cursor int
	loaded bool
	err    string
}

type picker struct {
	cursor   int
	selected map[string]bool
	item     memeItem
}

type discordForm struct {
	text     textinput.Model
	image    textinput.Model
	selected map[string]bool
	focus    int
}

// Model is the Bubble Tea model. At most one orchestrator request is in flight:
// the orchestrator answers one request at a time, and a dispatch that screens
// images can take a while.
type Model struct {
	client       Client
	now          func() time.Time
	timeout      time.Duration
	refreshEvery time.Duration

	width, height int
	tab           tab

	busy      bool
	busyLabel string
	notice    string
	noticeErr bool

	status    *statusData
	statusErr string
	memes     memeList
	screen    *screenData
	form      discordForm
	history   []historyEntry

	overlay       overlayKind
	picker        picker
	dispatchInput textinput.Model
	pending       *pendingAction

	panelCursor   int
	intervalInput textinput.Model
	intervalJob   string

	// Chat provider profiles. Selecting goes through the orchestrator; creating and
	// deleting write the local database directly, so a token never crosses the network.
	profiles      ProfileStore
	profileCursor int
	profileForm   [4]textinput.Model
	profileFocus  int

	// reloadMemes chains a pool reload after the status refresh that follows an action,
	// because each request must claim the single in-flight slot when it actually starts.
	reloadMemes bool

	images imageState

	// startOnMemes opens the Memes tab as soon as the first status arrives.
	startOnMemes bool
}

func NewModel(client Client, refreshEvery, timeout time.Duration) *Model {
	text := textinput.New()
	text.Placeholder = tr("message (optional when there is an image)")
	text.CharLimit = 1900
	text.Width = 60
	text.Focus()

	image := textinput.New()
	image.Placeholder = "https://... (opcional)"
	image.CharLimit = 2000
	image.Width = 60

	interval := textinput.New()
	interval.Placeholder = tr("45m, 6h or default")
	interval.CharLimit = 16
	interval.Width = 20

	dispatch := textinput.New()
	dispatch.SetValue("1")
	dispatch.CharLimit = 2
	dispatch.Width = 4

	var form [4]textinput.Model
	for i, placeholder := range []string{tr("name (e.g. hosted)"), "https://api.example.com/v1", tr("model"), tr("token (not shown on screen)")} {
		form[i] = textinput.New()
		form[i].Placeholder = placeholder
		form[i].Width = 46
		form[i].CharLimit = 512
	}
	form[3].EchoMode = textinput.EchoPassword
	form[3].EchoCharacter = '•'

	return &Model{
		profileForm:   form,
		client:        client,
		now:           time.Now,
		timeout:       timeout,
		refreshEvery:  refreshEvery,
		memes:         memeList{scope: "unsent"},
		form:          discordForm{text: text, image: image, selected: map[string]bool{}},
		dispatchInput: dispatch,
		intervalInput: interval,
		images:        newImageState(),
	}
}

// SetVideoFramer lets videos be previewed (needs ffmpeg for the real framer).
func (m *Model) SetVideoFramer(framer VideoFramer) { m.images.framer = framer }

// EnableImages turns on meme previews. sink receives the raw terminal sequences that
// upload images (kitty mode); fetch downloads them.
func (m *Model) EnableImages(mode ImageMode, sink TerminalSink, fetch ImageFetcher) {
	m.images.mode, m.images.sink, m.images.fetch = mode, sink, fetch
}

// OpenTab makes the TUI start on the named tab (panel, memes, discord, history; the
// Portuguese names painel and historico are accepted too).
func (m *Model) OpenTab(name string) {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "memes":
		m.tab, m.startOnMemes = tabMemes, true
	case "discord":
		m.tab = tabDiscord
	case "history", "historico", "histórico":
		m.tab = tabHistory
	}
}

// ImageIDs lists the kitty images this session uploaded, for cleanup on exit.
func (m *Model) ImageIDs() []uint32 { return m.images.ids() }

func (m *Model) Init() tea.Cmd {
	return tea.Batch(m.cmdStatus(), m.tickCmd())
}

func (m *Model) tickCmd() tea.Cmd {
	return tea.Tick(m.refreshEvery, func(t time.Time) tea.Msg { return tickMsg(t) })
}

// begin claims the single in-flight slot.
func (m *Model) begin(label string) bool {
	if m.busy {
		m.setNotice("aguarde: "+m.busyLabel+tr(" in progress"), true)
		return false
	}
	m.busy, m.busyLabel = true, label
	return true
}

func (m *Model) setNotice(text string, isErr bool) {
	m.notice, m.noticeErr = text, isErr
}

// skippedError means the orchestrator understood the request but a switch is off.
type skippedError struct{ reason string }

func (e *skippedError) Error() string { return tr("refused by the orchestrator: ") + e.reason }

func (m *Model) request(eventType string, payload map[string]any) (map[string]any, error) {
	ctx, cancel := context.WithTimeout(context.Background(), m.timeout)
	defer cancel()
	res, err := m.client.Request(ctx, eventType, payload)
	if err != nil {
		return nil, err
	}
	if skipped, _ := res["skipped"].(bool); skipped {
		return nil, &skippedError{reason: str(res, "reason")}
	}
	return res, nil
}

func (m *Model) cmdStatus() tea.Cmd {
	if !m.begin(tr("refreshing panel")) {
		return nil
	}
	return func() tea.Msg {
		system, err := m.request("ningo.status", map[string]any{})
		if err != nil {
			return statusMsg{err: err}
		}
		meme, err := m.request("meme.status", map[string]any{})
		memeOff := false
		var skipped *skippedError
		switch {
		case errors.As(err, &skipped):
			meme, memeOff = map[string]any{}, true // switched off: not an outage
		case err != nil:
			return statusMsg{err: err}
		}
		data := parseStatus(system, meme, m.now())
		data.MemeOff = memeOff
		return statusMsg{data: data}
	}
}

func (m *Model) cmdMemes() tea.Cmd {
	if !m.begin(tr("loading memes")) {
		return nil
	}
	scope, offset := m.memes.scope, m.memes.page.Offset
	return func() tea.Msg {
		res, err := m.request("meme.list", map[string]any{"scope": scope, "limit": pageSize, "offset": offset})
		if err != nil {
			return memesMsg{err: err}
		}
		return memesMsg{page: parseMemePage(res)}
	}
}

func (m *Model) cmdScreen(item memeItem) tea.Cmd {
	if !m.begin(tr("checking the NSFW filter")) {
		return nil
	}
	return func() tea.Msg {
		res, err := m.request("meme.screen", map[string]any{"url": item.URL, "caption": item.Title})
		if err != nil {
			return screenMsg{err: err}
		}
		return screenMsg{data: parseScreen(res)}
	}
}

func (m *Model) exec(action *pendingAction) tea.Cmd {
	if !m.begin(action.label) {
		return nil
	}
	if action.local != nil {
		return func() tea.Msg {
			summary, err := action.local()
			return actionMsg{action: action, res: map[string]any{"summary": summary}, err: err}
		}
	}
	return func() tea.Msg {
		res, err := m.request(action.eventType, action.payload)
		return actionMsg{action: action, res: res, err: err}
	}
}

func (m *Model) addHistory(label, summary string, ok bool) {
	m.history = append([]historyEntry{{At: m.now(), Label: label, Summary: summary, OK: ok}}, m.history...)
	if len(m.history) > maxHistory {
		m.history = m.history[:maxHistory]
	}
}

func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		return m, nil

	case tickMsg:
		// Skip the poll while anything is in flight or on screen; just re-arm.
		if !m.busy && m.overlay == overlayNone && m.tab == tabPanel {
			return m, tea.Batch(m.cmdStatus(), m.tickCmd())
		}
		return m, m.tickCmd()

	case statusMsg:
		m.busy = false
		if msg.err != nil {
			m.statusErr = msg.err.Error()
			return m, nil
		}
		m.statusErr = ""
		m.status = &msg.data
		if m.startOnMemes {
			m.startOnMemes = false
			return m, m.cmdMemes()
		}
		if m.reloadMemes {
			m.reloadMemes = false
			return m, m.reloadMemesIfLoaded()
		}
		return m, nil

	case memesMsg:
		m.busy = false
		if msg.err != nil {
			m.memes.err = msg.err.Error()
			return m, nil
		}
		m.memes.err, m.memes.loaded = "", true
		m.memes.page, m.memes.cursor = msg.page, 0
		return m, m.onSelectionChanged()

	case imageMsg:
		m.applyImage(msg)
		if item, ok := m.selectedMeme(); ok && item.URL == msg.url {
			return m, m.cmdAnimate()
		}
		return m, nil

	case frameTickMsg:
		item, ok := m.selectedMeme()
		if !ok || msg.gen != m.images.animGen || item.URL != msg.url || m.tab != tabMemes {
			return m, nil
		}
		m.images.frame++
		return m, m.frameTick(msg.url)

	case screenMsg:
		m.busy = false
		if msg.err != nil {
			m.setNotice(tr("NSFW filter failed: ")+msg.err.Error(), true)
			return m, nil
		}
		m.screen = &msg.data
		return m, nil

	case actionMsg:
		m.busy = false
		if msg.err != nil {
			m.addHistory(msg.action.label, msg.err.Error(), false)
			m.setNotice(msg.action.label+tr(" failed: ")+msg.err.Error(), true)
			return m, nil
		}
		summary := "ok"
		if msg.action.local != nil {
			summary = str(msg.res, "summary")
		} else if msg.action.summarize != nil {
			summary = msg.action.summarize(msg.res)
		}
		m.addHistory(msg.action.label, summary, true)
		m.setNotice(msg.action.label+": "+summary, false)
		if msg.action.reload {
			m.reloadMemes = true
			return m, m.cmdStatus()
		}
		return m, nil

	case tea.KeyMsg:
		return m.handleKey(msg)
	}
	return m, nil
}

func (m *Model) reloadMemesIfLoaded() tea.Cmd {
	if !m.memes.loaded {
		return nil
	}
	return m.cmdMemes()
}

func (m *Model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	key := msg.String()
	if key == "ctrl+c" {
		return m, tea.Quit
	}
	switch m.overlay {
	case overlayConfirm:
		return m.keyConfirm(key)
	case overlayPicker:
		return m.keyPicker(key)
	case overlayDispatch:
		return m.keyDispatch(msg)
	case overlayInterval:
		return m.keyInterval(msg)
	case overlayProfiles:
		return m.keyProfiles(key)
	case overlayProfileForm:
		return m.keyProfileForm(msg)
	}

	if next, ok := tabForKey(key, m.typing()); ok {
		return m, m.gotoTab(next)
	}
	switch key {
	case "tab":
		return m, m.gotoTab(tab((int(m.tab) + 1) % len(tabTitles())))
	case "shift+tab":
		return m, m.gotoTab(tab((int(m.tab) + len(tabTitles()) - 1) % len(tabTitles())))
	}

	switch m.tab {
	case tabPanel:
		return m.keyPanel(key)
	case tabMemes:
		return m.keyMemes(key)
	case tabDiscord:
		return m.keyDiscord(msg)
	case tabHistory:
		if key == "q" {
			return m, tea.Quit
		}
	}
	return m, nil
}

// typing reports whether keystrokes belong to a text field, so digits and q stay text.
func (m *Model) typing() bool {
	return m.tab == tabDiscord && m.form.focus <= 1
}

func tabForKey(key string, typing bool) (tab, bool) {
	switch key {
	case "f1":
		return tabPanel, true
	case "f2":
		return tabMemes, true
	case "f3":
		return tabDiscord, true
	case "f4":
		return tabHistory, true
	}
	if !typing && len(key) == 1 && key[0] >= '1' && key[0] <= '4' {
		return tab(key[0] - '1'), true
	}
	return 0, false
}

func (m *Model) gotoTab(next tab) tea.Cmd {
	m.tab = next
	m.setNotice("", false)
	if next == tabMemes && !m.memes.loaded {
		return m.cmdMemes()
	}
	return m.cmdAnimate()
}

// onSelectionChanged fetches the newly selected meme's preview and restarts playback.
func (m *Model) onSelectionChanged() tea.Cmd {
	return tea.Batch(m.cmdSelectedImage(), m.cmdAnimate())
}

func (m *Model) selectedMeme() (memeItem, bool) {
	items := m.memes.page.Items
	if m.memes.cursor < 0 || m.memes.cursor >= len(items) {
		return memeItem{}, false
	}
	return items[m.memes.cursor], true
}

func (m *Model) keyMemes(key string) (tea.Model, tea.Cmd) {
	items := m.memes.page.Items
	switch key {
	case "q":
		return m, tea.Quit
	case "up", "k":
		if m.memes.cursor > 0 {
			m.memes.cursor--
		}
		return m, m.onSelectionChanged()
	case "down", "j":
		if m.memes.cursor < len(items)-1 {
			m.memes.cursor++
		}
		return m, m.onSelectionChanged()
	case "n", "pgdown":
		if next := m.memes.page.Offset + pageSize; next < m.memes.page.Total {
			m.memes.page.Offset = next
			return m, m.cmdMemes()
		}
	case "p", "pgup":
		if m.memes.page.Offset > 0 {
			m.memes.page.Offset = max(0, m.memes.page.Offset-pageSize)
			return m, m.cmdMemes()
		}
	case "t":
		if m.memes.scope == "unsent" {
			m.memes.scope = "sent"
		} else {
			m.memes.scope = "unsent"
		}
		m.memes.page.Offset = 0
		return m, m.cmdMemes()
	case "r":
		return m, m.cmdMemes()
	case "s":
		if item, ok := m.selectedMeme(); ok {
			return m, m.cmdScreen(item)
		}
	case "enter":
		if item, ok := m.selectedMeme(); ok {
			m.openPicker(item)
		}
	case "d":
		m.overlay = overlayDispatch
		m.dispatchInput.SetValue("1")
		m.dispatchInput.CursorEnd()
		m.dispatchInput.Focus()
	case "R":
		return m, m.exec(&pendingAction{
			label:     tr("refresh pool"),
			eventType: "meme.pool.refresh",
			payload:   map[string]any{"max_items_per_scraper": 20},
			summarize: func(map[string]any) string { return tr("pool refreshed") },
			reload:    true,
		})
	}
	return m, nil
}

func (m *Model) channelsOrNotice() []string {
	if m.status == nil {
		m.setNotice(tr("channels not loaded yet; open the Panel and wait"), true)
		return nil
	}
	channels := m.status.Channels.destinations()
	if len(channels) == 0 {
		m.setNotice(tr("no channel configured in the orchestrator"), true)
	}
	return channels
}

func (m *Model) openPicker(item memeItem) {
	if len(m.channelsOrNotice()) == 0 {
		return
	}
	m.picker = picker{selected: map[string]bool{}, item: item}
	m.overlay = overlayPicker
}

func (m *Model) keyPicker(key string) (tea.Model, tea.Cmd) {
	channels := m.status.Channels.destinations()
	switch key {
	case "esc", "q":
		m.overlay = overlayNone
	case "up", "k":
		if m.picker.cursor > 0 {
			m.picker.cursor--
		}
	case "down", "j":
		if m.picker.cursor < len(channels)-1 {
			m.picker.cursor++
		}
	case " ", "space", "x":
		id := channels[m.picker.cursor]
		m.picker.selected[id] = !m.picker.selected[id]
	case "a":
		for _, id := range channels {
			m.picker.selected[id] = true
		}
	case "enter":
		chosen := chosenChannels(channels, m.picker.selected)
		if len(chosen) == 0 {
			m.setNotice(tr("select at least one channel with space"), true)
			return m, nil
		}
		item := m.picker.item
		payload := map[string]any{"channel_ids": chosen, "content": item.Title, "attachment_url": item.URL}
		if m.memes.scope == "unsent" {
			payload["mark_sent"] = true
		}
		m.askConfirm(&pendingAction{
			label:     tr("send meme"),
			eventType: "discord.send",
			payload:   payload,
			lines:     append([]string{"Meme: " + trunc(item.Title, 60), "URL:  " + trunc(item.URL, 70), ""}, m.destinationLines(chosen)...),
			summarize: summarizeSend,
			reload:    true,
		})
	}
	return m, nil
}

func chosenChannels(all []string, selected map[string]bool) []string {
	out := []string{}
	for _, id := range all {
		if selected[id] {
			out = append(out, id)
		}
	}
	return out
}

func (m *Model) destinationLines(chosen []string) []string {
	lines := []string{tr("Send to:")}
	restricted := false
	for _, id := range chosen {
		label := id
		if m.status != nil && m.status.Channels.isSafeOnly(id) {
			label += tr("  (safe-only: the image goes through the NSFW filter first)")
			restricted = true
		}
		lines = append(lines, "  • "+label)
	}
	if restricted {
		lines = append(lines, "", tr("If the filter blocks it, this channel is skipped and the rest receive it."))
	}
	return lines
}

func (m *Model) askConfirm(action *pendingAction) {
	m.pending = action
	m.overlay = overlayConfirm
}

func (m *Model) keyConfirm(key string) (tea.Model, tea.Cmd) {
	switch key {
	case "y", "Y", "enter":
		if m.busy {
			// Keep the confirmation open instead of silently dropping the action.
			m.setNotice("aguarde: "+m.busyLabel+tr(" in progress"), true)
			return m, nil
		}
		action := m.pending
		m.pending, m.overlay = nil, overlayNone
		return m, m.exec(action)
	case "n", "N", "esc":
		m.pending, m.overlay = nil, overlayNone
		m.setNotice("cancelado", false)
	}
	return m, nil
}

func (m *Model) keyDispatch(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.overlay = overlayNone
		return m, nil
	case "enter":
		count, err := strconv.Atoi(strings.TrimSpace(m.dispatchInput.Value()))
		if err != nil || count < 1 || count > maxDispatchBatch {
			m.setNotice(tr("enter a number between 1 and %d", maxDispatchBatch), true)
			return m, nil
		}
		channels := []string{}
		if m.status != nil {
			channels = m.status.Channels.Meme
		}
		m.overlay = overlayNone
		m.askConfirm(&pendingAction{
			label:     tr("dispatch %d meme(s)", count),
			eventType: "meme.dispatch.scheduled",
			payload:   map[string]any{"limit": count},
			lines: append([]string{tr("Pick %d meme(s) from the top of the queue and send to:", count)},
				m.destinationLines(channels)[1:]...),
			summarize: summarizeDispatch,
			reload:    true,
		})
		return m, nil
	}
	var cmd tea.Cmd
	m.dispatchInput, cmd = m.dispatchInput.Update(msg)
	return m, cmd
}

func (m *Model) keyDiscord(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	key := msg.String()
	channels := []string{}
	if m.status != nil {
		channels = m.status.Channels.destinations()
	}
	sendRow := 2 + len(channels)

	switch key {
	case "ctrl+s":
		return m, m.confirmDiscordSend(channels)
	case "down":
		m.moveFormFocus(1, sendRow)
		return m, nil
	case "up":
		m.moveFormFocus(-1, sendRow)
		return m, nil
	case "enter":
		switch {
		case m.form.focus <= 1:
			m.moveFormFocus(1, sendRow)
		case m.form.focus == sendRow:
			return m, m.confirmDiscordSend(channels)
		default:
			m.toggleFormChannel(channels)
		}
		return m, nil
	case " ", "space":
		if m.form.focus > 1 && m.form.focus < sendRow {
			m.toggleFormChannel(channels)
			return m, nil
		}
	case "q":
		if !m.typing() {
			return m, tea.Quit
		}
	}

	var cmd tea.Cmd
	switch m.form.focus {
	case 0:
		m.form.text, cmd = m.form.text.Update(msg)
	case 1:
		m.form.image, cmd = m.form.image.Update(msg)
	}
	return m, cmd
}

func (m *Model) moveFormFocus(delta, sendRow int) {
	m.form.focus = min(max(m.form.focus+delta, 0), sendRow)
	m.form.text.Blur()
	m.form.image.Blur()
	switch m.form.focus {
	case 0:
		m.form.text.Focus()
	case 1:
		m.form.image.Focus()
	}
}

func (m *Model) toggleFormChannel(channels []string) {
	index := m.form.focus - 2
	if index >= 0 && index < len(channels) {
		m.form.selected[channels[index]] = !m.form.selected[channels[index]]
	}
}

func (m *Model) confirmDiscordSend(channels []string) tea.Cmd {
	text := strings.TrimSpace(m.form.text.Value())
	image := strings.TrimSpace(m.form.image.Value())
	chosen := chosenChannels(channels, m.form.selected)
	switch {
	case text == "" && image == "":
		m.setNotice(tr("write a message and/or provide an image"), true)
		return nil
	case len(chosen) == 0:
		m.setNotice(tr("select at least one channel (space)"), true)
		return nil
	}
	payload := map[string]any{"channel_ids": chosen}
	lines := []string{}
	if text != "" {
		payload["content"] = text
		lines = append(lines, tr("Text:  ")+trunc(text, 70))
	}
	if image != "" {
		payload["attachment_url"] = image
		lines = append(lines, tr("Image: ")+trunc(image, 70))
	}
	m.askConfirm(&pendingAction{
		label:     tr("send message"),
		eventType: "discord.send",
		payload:   payload,
		lines:     append(append(lines, ""), m.destinationLines(chosen)...),
		summarize: summarizeSend,
	})
	return nil
}

func trunc(text string, limit int) string {
	runes := []rune(strings.ReplaceAll(text, "\n", " "))
	if len(runes) <= limit {
		return string(runes)
	}
	return string(runes[:limit-1]) + "…"
}

type frameTickMsg struct {
	url string
	gen int
}

func isVideo(url string) bool {
	lower := strings.ToLower(url)
	for _, ext := range []string{".mp4", ".webm", ".mov", ".mkv"} {
		if strings.HasSuffix(lower, ext) {
			return true
		}
	}
	return false
}

// cmdSelectedImage fetches the selected meme's preview. Downloads do not use the
// orchestrator request slot: they are plain HTTP and independent of the REQ socket.
func (m *Model) cmdSelectedImage() tea.Cmd {
	item, ok := m.selectedMeme()
	if !ok || !m.images.enabled() || item.URL == "" {
		return nil
	}
	url := item.URL
	if _, cached := m.images.cache[url]; cached || m.images.inflight[url] {
		return nil
	}
	m.images.inflight[url] = true
	mode, fetch, framer := m.images.mode, m.images.fetch, m.images.framer
	video := isVideo(url)
	if video && framer == nil {
		delete(m.images.inflight, url)
		m.images.remember(&preview{url: url, err: tr("video: no preview (ffmpeg unavailable)")})
		return nil
	}
	firstID, span := uint32(0), uint32(1)
	if mode == ImagesKitty {
		firstID = m.images.nextID
		if video {
			span = videoMaxFrames
		}
		m.images.nextID += span
	}
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()
		data, err := fetch(ctx, url)
		if err != nil {
			return imageMsg{url: url, err: err}
		}
		if video {
			frames, err := framer(ctx, data)
			if err != nil {
				return imageMsg{url: url, err: err}
			}
			built, transmit, err := buildVideoPreview(mode, url, frames, firstID)
			return imageMsg{url: url, preview: built, transmit: transmit, err: err}
		}
		built, transmit, err := buildPreview(mode, url, data, firstID)
		return imageMsg{url: url, preview: built, transmit: transmit, err: err}
	}
}

// cmdAnimate (re)starts the playback loop for the selected meme when it is a video.
// Bumping the generation makes ticks from an earlier selection harmless.
func (m *Model) cmdAnimate() tea.Cmd {
	m.images.animGen++
	m.images.frame = 0
	item, ok := m.selectedMeme()
	if !ok || m.tab != tabMemes {
		return nil
	}
	p := m.images.cache[item.URL]
	m.upload(p)
	if p == nil || !p.animated() {
		return nil
	}
	return m.frameTick(item.URL)
}

func (m *Model) frameTick(url string) tea.Cmd {
	gen := m.images.animGen
	return tea.Tick(frameInterval, func(time.Time) tea.Msg { return frameTickMsg{url: url, gen: gen} })
}

func (m *Model) applyImage(msg imageMsg) {
	delete(m.images.inflight, msg.url)
	if msg.err != nil {
		m.images.remember(&preview{url: msg.url, err: msg.err.Error()})
		return
	}
	msg.preview.transmit = msg.transmit
	if cleanup := m.images.remember(msg.preview); len(cleanup) > 0 && m.images.sink != nil {
		_ = m.images.sink.WriteRaw(cleanup)
	}
}

// upload sends a preview's kitty images to the terminal, once, when it is shown.
func (m *Model) upload(p *preview) {
	if p == nil || p.uploaded || len(p.transmit) == 0 || m.images.sink == nil {
		return
	}
	if err := m.images.sink.WriteRaw(p.transmit); err != nil {
		p.err = tr("terminal refused the image: ") + err.Error()
	}
	p.uploaded, p.transmit = true, nil
}

// panelRow is one selectable line of the panel: a service or a job.
type panelRow struct {
	isJob   bool
	name    string
	enabled bool
}

func (m *Model) panelRows() []panelRow {
	if m.status == nil {
		return nil
	}
	rows := make([]panelRow, 0, len(m.status.Services)+len(m.status.Jobs))
	for _, service := range m.status.Services {
		rows = append(rows, panelRow{name: service.Name, enabled: service.Enabled})
	}
	for _, job := range m.status.Jobs {
		rows = append(rows, panelRow{isJob: true, name: job.Name, enabled: job.Enabled})
	}
	return rows
}

func (m *Model) selectedPanelRow() (panelRow, bool) {
	rows := m.panelRows()
	if m.panelCursor < 0 || m.panelCursor >= len(rows) {
		return panelRow{}, false
	}
	return rows[m.panelCursor], true
}

func (m *Model) keyPanel(key string) (tea.Model, tea.Cmd) {
	rows := m.panelRows()
	switch key {
	case "q":
		return m, tea.Quit
	case "r":
		return m, m.cmdStatus()
	case "up", "k":
		if m.panelCursor > 0 {
			m.panelCursor--
		}
	case "down", "j":
		if m.panelCursor < len(rows)-1 {
			m.panelCursor++
		}
	case " ", "space", "enter", "t":
		if row, ok := m.selectedPanelRow(); ok {
			return m, m.exec(m.togglePending(row))
		}
	case "x":
		if row, ok := m.selectedPanelRow(); ok && row.isJob {
			m.askRunJob(row)
		} else if ok {
			m.setNotice(tr("only jobs can be run; select a job"), true)
		}
	case "p":
		m.openProfiles()
	case "i":
		if row, ok := m.selectedPanelRow(); ok && row.isJob {
			m.openInterval(row.name)
		} else if ok {
			m.setNotice(tr("only jobs have an interval; select a job"), true)
		}
	}
	return m, nil
}

// togglePending flips a service or job. It needs no confirmation: it posts nothing and
// is undone by pressing the same key.
func (m *Model) togglePending(row panelRow) *pendingAction {
	eventType, kind := "services.set", tr("service")
	if row.isJob {
		eventType, kind = "scheduler.jobs.set", "job"
	}
	verb := "desligar"
	if !row.enabled {
		verb = "ligar"
	}
	return &pendingAction{
		label:     fmt.Sprintf("%s %s %s", verb, kind, row.name),
		eventType: eventType,
		payload:   map[string]any{"name": row.name, "enabled": !row.enabled},
		summarize: func(map[string]any) string { return "ok" },
		reload:    true,
	}
}

// jobServices lists the service each job depends on, to warn before a run that will be refused.
func jobService(job string) string {
	switch {
	case strings.HasPrefix(job, "meme."):
		return "meme"
	}
	return ""
}

func (m *Model) askRunJob(row panelRow) {
	eventType, payload, err := cli.JobEvent(row.name, 1)
	if err != nil {
		m.setNotice(err.Error(), true)
		return
	}
	lines := []string{tr("Run now: ") + row.name, ""}
	if !row.enabled {
		lines = append(lines, tr("The job is paused; a manual run ignores the pause."), "")
	}
	if service, ok := m.status.service(jobService(row.name)); ok && !service.Enabled {
		lines = append(lines, tr("Warning: service %s is switched off and the orchestrator will refuse it.", service.Name), "")
	}
	summarize := func(map[string]any) string { return "executado" }
	switch row.name {
	case "meme.dispatch":
		lines = append(lines, tr("Sends 1 meme (goes through the NSFW filter on safe-only channels) to:"))
		lines = append(lines, m.destinationLines(m.status.Channels.Meme)[1:]...)
		summarize = summarizeDispatch
	}
	m.askConfirm(&pendingAction{
		label:     tr("run job ") + row.name,
		eventType: eventType,
		payload:   payload,
		lines:     lines,
		summarize: summarize,
		reload:    true,
	})
}

func (m *Model) openInterval(job string) {
	m.intervalJob = job
	m.intervalInput.SetValue("")
	m.intervalInput.CursorEnd()
	m.intervalInput.Focus()
	m.overlay = overlayInterval
}

func (m *Model) keyInterval(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.overlay = overlayNone
		return m, nil
	case "enter":
		payload, err := cli.BuildJobIntervalPayload(m.intervalJob, m.intervalInput.Value())
		if err != nil {
			m.setNotice(err.Error(), true)
			return m, nil
		}
		m.overlay = overlayNone
		return m, m.exec(&pendingAction{
			label:     tr("interval of ") + m.intervalJob,
			eventType: "scheduler.jobs.set",
			payload:   payload,
			summarize: func(res map[string]any) string {
				if interval := str(res, "interval"); interval != "" {
					return tr("every ") + interval
				}
				return tr("back to the default")
			},
			reload: true,
		})
	}
	var cmd tea.Cmd
	m.intervalInput, cmd = m.intervalInput.Update(msg)
	return m, cmd
}

// ProfileStore is the local write side of the profiles: what the TUI may do directly.
type ProfileStore interface {
	SaveProfile(store.NewProfile) error
	DeleteProfile(name string, force bool) error
}

func (m *Model) SetProfileStore(profiles ProfileStore) { m.profiles = profiles }

func (m *Model) openProfiles() {
	if m.status == nil {
		m.setNotice(tr("wait for the panel to load"), true)
		return
	}
	m.profileCursor = 0
	for i, row := range m.status.Profiles {
		if row.Active {
			m.profileCursor = i
		}
	}
	m.overlay = overlayProfiles
}

func (m *Model) keyProfiles(key string) (tea.Model, tea.Cmd) {
	rows := m.status.Profiles
	switch key {
	case "esc", "q":
		m.overlay = overlayNone
	case "up", "k":
		if m.profileCursor > 0 {
			m.profileCursor--
		}
	case "down", "j":
		if m.profileCursor < len(rows)-1 {
			m.profileCursor++
		}
	case "enter":
		if m.profileCursor < len(rows) {
			row := rows[m.profileCursor]
			m.overlay = overlayNone
			return m, m.exec(&pendingAction{
				label:     tr("use chat profile ") + row.Name,
				eventType: "chat.profile.use",
				payload:   map[string]any{"name": row.Name},
				summarize: func(map[string]any) string { return tr("active (applies from the next message)") },
				reload:    true,
			})
		}
	case "n":
		if m.profiles == nil {
			m.setNotice(tr("no local access to the database: set DOKJA_DB_FILE to create profiles"), true)
			return m, nil
		}
		for i := range m.profileForm {
			m.profileForm[i].SetValue("")
			m.profileForm[i].Blur()
		}
		m.profileFocus = 0
		m.profileForm[0].Focus()
		m.overlay = overlayProfileForm
	case "d":
		if m.profiles == nil {
			m.setNotice(tr("no local access to the database: set DOKJA_DB_FILE to delete profiles"), true)
			return m, nil
		}
		if m.profileCursor < len(rows) {
			m.askDeleteProfile(rows[m.profileCursor])
		}
	}
	return m, nil
}

func (m *Model) askDeleteProfile(row profileRow) {
	lines := []string{tr("Delete profile ") + row.Name + " (" + row.Model + ")?", ""}
	if row.Active {
		lines = append(lines, tr("This is the ACTIVE profile: chat goes back to the CHAT_AI_* environment variables."), "")
	}
	lines = append(lines, tr("This removes the token from the local database. It cannot be undone."))
	m.overlay = overlayConfirm
	m.pending = &pendingAction{
		label: tr("delete profile ") + row.Name,
		lines: lines,
		local: func() (string, error) {
			if err := m.profiles.DeleteProfile(row.Name, row.Active); err != nil {
				return "", err
			}
			return tr("deleted"), nil
		},
		reload: true,
	}
}

func (m *Model) keyProfileForm(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.overlay = overlayProfiles
		return m, nil
	case "tab", "down", "enter":
		if msg.String() == "enter" && m.profileFocus == len(m.profileForm)-1 {
			return m, m.saveProfileForm()
		}
		m.moveProfileFocus(1)
		return m, nil
	case "shift+tab", "up":
		m.moveProfileFocus(-1)
		return m, nil
	case "ctrl+s":
		return m, m.saveProfileForm()
	}
	var cmd tea.Cmd
	m.profileForm[m.profileFocus], cmd = m.profileForm[m.profileFocus].Update(msg)
	return m, cmd
}

func (m *Model) moveProfileFocus(delta int) {
	m.profileForm[m.profileFocus].Blur()
	m.profileFocus = min(max(m.profileFocus+delta, 0), len(m.profileForm)-1)
	m.profileForm[m.profileFocus].Focus()
}

// saveProfileForm writes the profile locally and clears the token from memory
// and from the screen as soon as it has been handed to the store.
func (m *Model) saveProfileForm() tea.Cmd {
	profile := store.NewProfile{
		Name:    strings.TrimSpace(m.profileForm[0].Value()),
		BaseURL: strings.TrimSpace(m.profileForm[1].Value()),
		Model:   strings.TrimSpace(m.profileForm[2].Value()),
		APIKey:  strings.TrimSpace(m.profileForm[3].Value()),
	}
	if err := m.profiles.SaveProfile(profile); err != nil {
		m.setNotice(err.Error(), true)
		return nil
	}
	m.profileForm[3].SetValue("")
	m.overlay = overlayProfiles
	m.addHistory(tr("create profile ")+profile.Name, tr("saved in the local database"), true)
	m.setNotice(tr("profile ")+profile.Name+tr(" saved; press enter on the profile to activate it"), false)
	return m.cmdStatus()
}
