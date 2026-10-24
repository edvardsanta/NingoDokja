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
func tabTitles() []string { return []string{tr("panel"), "Memes", "Discord", tr("history")} }

type overlayKind int

const (
	overlayNone overlayKind = iota
	overlayPicker
	overlayDispatch
	overlayConfirm
	overlayInterval
	overlayProfiles
	overlayProfileForm
	overlayHashtag
	overlayHashtagList
	overlayMemory
	overlayMemoryList
	overlayMemoryPredict
	overlayMemoryFeedback
	overlayLink
	overlayProviders
	overlayProviderEdit
	overlayChatBinding
	overlaySettings
	overlaySettingEdit
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
	// The modules come from the local runtime controller, which answers even when the
	// orchestrator is stopped.
	modules    *cli.RuntimeSnapshot
	modulesErr string
}

// Runtime is the local runtime controller: it starts and stops the lite stack's containers.
type Runtime interface {
	ChatBinding(context.Context) (cli.ChatBinding, error)
	ConfigureChat(context.Context, cli.ChatBinding) (cli.ChatBinding, error)
	Providers(context.Context) (cli.ProvidersView, error)
	ConfigureProvider(context.Context, uint64, cli.ProviderRegistration) (cli.ProviderSaved, error)
	Request(ctx context.Context, eventType string, payload map[string]any) (cli.RuntimeSnapshot, error)
	Settings(ctx context.Context) (cli.SettingsView, error)
	SetSettings(ctx context.Context, values map[string]string) (cli.SettingsView, error)
}

type memoryMsg struct{ view memoryView }

type linkMsg struct {
	view cli.LinkView
	err  error
}

type linkTickMsg struct{}

// linkPoll is how often the link window reads the state while it is open.
const linkPoll = 2 * time.Second

// memoryScoredAction is the action the overlay scores: the hashtag suggestion is the only
// one that records experiences so far.
const memoryScoredAction = "hashtag.suggest"

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
	onSuccess func(*Model, map[string]any) tea.Cmd
	reload    bool

	// local runs instead of an orchestrator request, for actions that must stay on this machine.
	local func() (string, error)

	// back is the overlay to return to once the confirmation is answered, either way. Zero means
	// none: back to the screen underneath.
	back overlayKind
}

type memeList struct {
	scope  string
	page   memePage
	cursor int
	loaded bool
	err    string
}

type picker struct {
	cursor    int
	selected  map[string]bool
	item      memeItem
	forceNSFW bool
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

	runtime    Runtime
	modules    *cli.RuntimeSnapshot
	modulesErr string

	// The WhatsApp link window: what the orchestrator last said, and whether the window is open
	// (it stays open behind the unlink confirmation).
	link     cli.LinkView
	linkErr  string
	linkOpen bool

	// The settings window: what the controller last said, the selected row and the one edited.
	providers               *cli.ProvidersView
	providersErr            string
	chatBinding             cli.ChatBinding
	chatBindingErr          string
	chatChoices             []cli.ChatBinding
	chatCursor              int
	providersCursor         int
	providerEditing         cli.ProviderRegistration
	providerEditingRevision uint64
	settings                *cli.SettingsView
	settingsErr             string
	settingsCursor          int
	settingEditing          string
	settingInput            textinput.Model
	memes                   memeList
	screen                  *screenData
	// suggestion is the last hashtag suggestion asked for with g; it is shown with the meme it is about.
	suggestion *suggestionEvidence
	form       discordForm
	history    []historyEntry

	overlay       overlayKind
	picker        picker
	dispatchInput textinput.Model
	pending       *pendingAction

	panelCursor   int
	intervalInput textinput.Model
	intervalJob   string

	// Chat provider profiles. Selecting goes through the orchestrator; creating and
	// deleting write the local database directly, so a token never crosses the network.
	profiles             ProfileStore
	profileCursor        int
	profileForm          [4]textinput.Model
	profileFocus         int
	hashtagInput         textinput.Model
	hashtagList          hashtagExamples
	memory               memoryView
	memoryList           memoryExperiences
	memoryInput          textinput.Model
	memoryResult         memoryPredictView
	memoryFeedbackTarget memoryExperience
	memoryFeedbackInput  textinput.Model

	// chancePausedUntil is when the hashtag suggestion may ask the memory for a chance again, after
	// a failure.
	chancePausedUntil time.Time

	// reloadMemes chains a pool reload after the status refresh that follows an action,
	// because each request must claim the single in-flight slot when it actually starts.
	reloadMemes bool

	images imageState

	// startOnMemes opens the Memes tab as soon as the first status arrives.
	startOnMemes bool
}

func NewModel(client Client, refreshEvery, timeout time.Duration) *Model {
	text := textinput.New()
	text.Placeholder = tr("message_optional_when_there_is_an_image")
	text.CharLimit = 1900
	text.Width = 60
	text.Focus()

	image := textinput.New()
	image.Placeholder = "https://... (opcional)"
	image.CharLimit = 2000
	image.Width = 60

	settingText := textinput.New()
	settingText.CharLimit = 2000
	settingText.Width = 60

	interval := textinput.New()
	interval.Placeholder = tr("45m_6h_or_default")
	interval.CharLimit = 16
	interval.Width = 20

	dispatch := textinput.New()
	dispatch.SetValue("1")
	dispatch.CharLimit = 2
	dispatch.Width = 4

	hashtag := textinput.New()
	hashtag.Placeholder = "TioDoPave"
	hashtag.CharLimit = 100
	hashtag.Width = 40

	feedbackText := textinput.New()
	feedbackText.CharLimit = 200
	feedbackText.Width = 52

	memoryText := textinput.New()
	memoryText.CharLimit = 1000
	memoryText.Width = 52

	var form [4]textinput.Model
	for i, placeholder := range []string{tr("name_e_g_hosted"), "https://api.example.com/v1", tr("model_placeholder"), tr("token_not_shown_on_screen")} {
		form[i] = textinput.New()
		form[i].Placeholder = placeholder
		form[i].Width = 46
		form[i].CharLimit = 512
	}
	form[3].EchoMode = textinput.EchoPassword
	form[3].EchoCharacter = '•'

	return &Model{
		profileForm:         form,
		client:              client,
		now:                 time.Now,
		timeout:             timeout,
		refreshEvery:        refreshEvery,
		memes:               memeList{scope: "unsent"},
		form:                discordForm{text: text, image: image, selected: map[string]bool{}},
		dispatchInput:       dispatch,
		intervalInput:       interval,
		settingInput:        settingText,
		hashtagInput:        hashtag,
		memoryInput:         memoryText,
		memoryFeedbackInput: feedbackText,
		images:              newImageState(),
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
		m.setNotice("aguarde: "+m.busyLabel+tr("in_progress"), true)
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

func (e *skippedError) Error() string { return tr("refused_by_the_orchestrator") + e.reason }

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

// SetRuntime gives the panel module controls. Without one, unavailable says why there are none.
func (m *Model) SetRuntime(runtime Runtime, unavailable string) {
	m.runtime, m.modulesErr = runtime, unavailable
}

func (m *Model) readModules() (*cli.RuntimeSnapshot, string) {
	if m.runtime == nil {
		return nil, m.modulesErr
	}
	ctx, cancel := context.WithTimeout(context.Background(), min(m.timeout, 15*time.Second))
	defer cancel()
	snapshot, err := m.runtime.Request(ctx, "runtime.modules", nil)
	if err != nil {
		return nil, err.Error()
	}
	return &snapshot, ""
}

func (m *Model) cmdStatus() tea.Cmd {
	if !m.begin(tr("refreshing_panel")) {
		return nil
	}
	return func() tea.Msg {
		modules, modulesErr := m.readModules()
		system, err := m.request("ningo.status", map[string]any{})
		if err != nil {
			return statusMsg{err: err, modules: modules, modulesErr: modulesErr}
		}
		meme, err := m.request("meme.status", map[string]any{})
		memeOff := false
		var skipped *skippedError
		switch {
		case errors.As(err, &skipped):
			meme, memeOff = map[string]any{}, true // switched off: not an outage
		case err != nil:
			return statusMsg{err: err, modules: modules, modulesErr: modulesErr}
		}
		data := parseStatus(system, meme, m.now())
		data.MemeOff = memeOff
		return statusMsg{data: data, modules: modules, modulesErr: modulesErr}
	}
}

// cmdMemory reads the experience memory for the overlay: its state and, if that answers, the
// score of the stored predictions. A failure of either stays inside the overlay.
func (m *Model) cmdMemory() tea.Cmd {
	if !m.begin(tr("reading_the_experience_memory")) {
		return nil
	}
	return func() tea.Msg {
		view := memoryView{loaded: true}
		res, err := m.request("memory.status", map[string]any{})
		var skipped *skippedError
		switch {
		case errors.As(err, &skipped):
			view.off = skipped.reason
			return memoryMsg{view: view}
		case err != nil:
			// The service is down or failing: do not ask it a second question and wait again.
			view.statusErr = err.Error()
			return memoryMsg{view: view}
		}
		status := parseMemoryStatus(res)
		view.status = &status

		res, err = m.request("memory.stats", map[string]any{"action": memoryScoredAction})
		if err != nil {
			view.scoreErr = err.Error()
			return memoryMsg{view: view}
		}
		score := parseMemoryScore(res)
		view.score = &score
		return memoryMsg{view: view}
	}
}

func (m *Model) cmdMemes() tea.Cmd {
	if !m.begin(tr("loading_memes")) {
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
	if !m.begin(tr("checking_the_nsfw_filter")) {
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
		m.modules, m.modulesErr = msg.modules, msg.modulesErr
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

	case memoryMsg:
		m.busy = false
		m.memory = msg.view
		return m, nil

	case chatBindingMsg:
		m.busy = false
		if m.overlay != overlayChatBinding {
			return m, nil
		}
		if msg.err != nil {
			m.chatBindingErr = msg.err.Error()
			return m, nil
		}
		m.chatBinding, m.chatBindingErr = msg.binding, ""
		for i, choice := range m.chatChoices {
			if choice.Provider == msg.binding.Provider && choice.Model == msg.binding.Model {
				m.chatCursor = i
			}
		}
		return m, nil
	case providersMsg:
		m.busy = false
		if msg.err != nil {
			m.providersErr = msg.err.Error()
			return m, nil
		}
		m.providers, m.providersErr = &msg.view, ""
		m.providersCursor = max(0, min(m.providersCursor, len(msg.view.Providers)-1))
		return m, nil
	case settingsMsg:
		m.busy = false
		if msg.err != nil {
			m.settingsErr = msg.err.Error()
			return m, nil
		}
		m.settings, m.settingsErr = &msg.view, ""
		m.settingsCursor = max(0, min(m.settingsCursor, len(m.settingRows())-1))
		return m, nil

	case linkMsg:
		m.busy = false
		if msg.err != nil {
			m.linkErr = msg.err.Error()
			return m, nil
		}
		m.link, m.linkErr = msg.view, ""
		return m, nil

	case linkTickMsg:
		if !m.linkOpen {
			return m, nil
		}
		if m.overlay == overlayLink && !m.busy {
			return m, tea.Batch(m.cmdLink("communications.link.status"), m.linkTick())
		}
		return m, m.linkTick()

	case memoryListMsg:
		m.busy = false
		list := msg.list
		if len(list.Items) == 0 && list.Total > 0 && list.Offset > 0 {
			// The last experience of the last page was forgotten: step back a page.
			return m, m.cmdMemoryList(list.State, max(0, list.Offset-memoryPageSize), 0)
		}
		list.Cursor = max(0, min(list.Cursor, len(list.Items)-1))
		m.memoryList = list
		return m, nil

	case memoryPredictionMsg:
		m.busy = false
		m.memoryResult = msg.view
		return m, nil

	case suggestionChanceMsg:
		m.busy = false
		if msg.prediction == nil {
			m.chancePausedUntil = m.now().Add(chancePause)
			return m, nil
		}
		m.chancePausedUntil = time.Time{}
		if m.notice == msg.base && !m.noticeErr {
			m.setNotice(msg.base+" · "+chanceNotice(*msg.prediction), false)
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
		if item, ok := m.selectedPreviewItem(); ok && item.URL == msg.url {
			return m, m.cmdAnimate()
		}
		return m, nil

	case frameTickMsg:
		item, ok := m.selectedPreviewItem()
		if !ok || msg.gen != m.images.animGen || item.URL != msg.url || m.tab != tabMemes {
			return m, nil
		}
		m.images.frame++
		return m, m.frameTick(msg.url)

	case screenMsg:
		m.busy = false
		if msg.err != nil {
			m.setNotice(tr("nsfw_filter_failed")+msg.err.Error(), true)
			return m, nil
		}
		m.screen = &msg.data
		return m, nil

	case actionMsg:
		m.busy = false
		if msg.err != nil {
			m.addHistory(msg.action.label, msg.err.Error(), false)
			message := msg.action.label + tr("failed") + msg.err.Error()
			if msg.action.eventType == "memory.resolve" {
				message += " " + tr("memory_feedback_check")
			}
			m.setNotice(message, true)
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
		var follow tea.Cmd
		if msg.action.onSuccess != nil {
			follow = msg.action.onSuccess(m, msg.res)
		}
		if msg.action.reload {
			m.reloadMemes = true
			return m, tea.Batch(follow, m.cmdStatus())
		}
		return m, follow

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
	case overlayHashtag:
		return m.keyHashtag(msg)
	case overlayHashtagList:
		return m.keyHashtagList(key)
	case overlayMemory:
		return m.keyMemory(key)
	case overlayMemoryList:
		return m.keyMemoryList(key)
	case overlayMemoryFeedback:
		return m.keyMemoryFeedback(msg)
	case overlayMemoryPredict:
		return m.keyMemoryPredict(msg)
	case overlayLink:
		return m.keyLink(key)
	case overlayChatBinding:
		return m.keyChatBinding(key)
	case overlayProviderEdit:
		return m.keyProviderEdit(msg)
	case overlayProviders:
		return m.keyProviders(key)
	case overlaySettings:
		return m.keySettings(key)
	case overlaySettingEdit:
		return m.keySettingEdit(msg)
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

func (m *Model) selectedHashtagExample() (hashtagExample, bool) {
	items := m.hashtagList.Items
	if m.hashtagList.Cursor < 0 || m.hashtagList.Cursor >= len(items) {
		return hashtagExample{}, false
	}
	return items[m.hashtagList.Cursor], true
}

func (m *Model) selectedPreviewItem() (memeItem, bool) {
	if m.overlay == overlayHashtagList {
		example, ok := m.selectedHashtagExample()
		return memeItem{URL: example.SourceURL, Title: example.Hashtag}, ok
	}
	return m.selectedMeme()
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
			label:     tr("refresh_pool"),
			eventType: "meme.pool.refresh",
			payload:   map[string]any{"max_items_per_scraper": 20},
			summarize: func(map[string]any) string { return tr("pool_refreshed") },
			reload:    true,
		})
	case "h":
		if _, ok := m.selectedMeme(); ok {
			m.hashtagInput.SetValue("")
			m.hashtagInput.Focus()
			m.overlay = overlayHashtag
		}
	case "g":
		if item, ok := m.selectedMeme(); ok {
			return m, m.exec(&pendingAction{
				label:     tr("suggest_hashtag"),
				eventType: "meme.hashtag.suggest",
				payload:   map[string]any{"url": item.URL},
				summarize: summarizeHashtagSuggestion,
				onSuccess: func(m *Model, res map[string]any) tea.Cmd {
					evidence := parseSuggestionEvidence(item.URL, res)
					m.suggestion = &evidence
					return m.cmdSuggestionChance(res)
				},
			})
		}
	case "l":
		return m, m.exec(&pendingAction{
			label:     tr("list_hashtag_examples"),
			eventType: "meme.hashtag.list",
			payload:   map[string]any{"limit": 200, "offset": 0},
			summarize: summarizeHashtagList,
			onSuccess: func(m *Model, res map[string]any) tea.Cmd {
				m.hashtagList = parseHashtagExamples(res)
				m.overlay = overlayHashtagList
				return tea.Batch(m.cmdSelectedImage(), m.cmdAnimate())
			},
		})
	case "u":
		if item, ok := m.selectedMeme(); ok {
			m.askConfirm(&pendingAction{
				label:     tr("remove_hashtag_example"),
				eventType: "meme.hashtag.untag",
				payload:   map[string]any{"url": item.URL},
				summarize: func(map[string]any) string { return tr("hashtag_example_removed") },
			})
		}
	}
	return m, nil
}

func (m *Model) keyHashtag(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.overlay = overlayNone
		m.hashtagInput.Blur()
		return m, nil
	case "enter":
		item, ok := m.selectedMeme()
		if !ok || strings.TrimSpace(m.hashtagInput.Value()) == "" {
			m.setNotice(tr("enter_a_hashtag"), true)
			return m, nil
		}
		hashtag := strings.TrimSpace(m.hashtagInput.Value())
		m.overlay = overlayNone
		m.hashtagInput.Blur()
		return m, m.exec(&pendingAction{
			label:     tr("tag_meme_hashtag"),
			eventType: "meme.hashtag.tag",
			payload:   map[string]any{"url": item.URL, "hashtag": hashtag},
			summarize: summarizeHashtagTag,
		})
	}
	var cmd tea.Cmd
	m.hashtagInput, cmd = m.hashtagInput.Update(msg)
	return m, cmd
}

func (m *Model) keyHashtagList(key string) (tea.Model, tea.Cmd) {
	switch key {
	case "esc", "q":
		m.overlay = overlayNone
		return m, m.cmdAnimate()
	case "up", "k":
		if m.hashtagList.Cursor > 0 {
			m.hashtagList.Cursor--
		}
	case "down", "j":
		if m.hashtagList.Cursor < len(m.hashtagList.Items)-1 {
			m.hashtagList.Cursor++
		}
	default:
		return m, nil
	}
	return m, tea.Batch(m.cmdSelectedImage(), m.cmdAnimate())
}

func (m *Model) channelsOrNotice() []string {
	if m.status == nil {
		m.setNotice(tr("channels_not_loaded_yet_open_the_panel_and_wait"), true)
		return nil
	}
	channels := m.status.Channels.destinations()
	if len(channels) == 0 {
		m.setNotice(tr("no_channel_configured_in_the_orchestrator"), true)
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
	case "f":
		m.picker.forceNSFW = !m.picker.forceNSFW
	case "enter":
		chosen := chosenChannels(channels, m.picker.selected)
		if len(chosen) == 0 {
			m.setNotice(tr("select_at_least_one_channel_with_space"), true)
			return m, nil
		}
		item := m.picker.item
		payload := map[string]any{"channel_ids": chosen, "content": item.Title, "attachment_url": item.URL}
		if m.memes.scope == "unsent" {
			payload["mark_sent"] = true
		}
		lines := append([]string{"Meme: " + trunc(item.Title, 60), "URL:  " + trunc(item.URL, 70), ""}, m.destinationLines(chosen)...)
		if m.picker.forceNSFW && m.includesSafeOnly(chosen) {
			payload["force_nsfw"] = true
			lines = append(lines, "", tr("force_nsfw_warning"))
		}
		m.askConfirm(&pendingAction{
			label:     tr("send_meme"),
			eventType: "discord.send",
			payload:   payload,
			lines:     lines,
			summarize: summarizeSend,
			reload:    true,
		})
	}
	return m, nil
}

func (m *Model) includesSafeOnly(channels []string) bool {
	if m.status == nil {
		return false
	}
	for _, id := range channels {
		if m.status.Channels.isSafeOnly(id) {
			return true
		}
	}
	return false
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
	lines := []string{tr("send_to")}
	restricted := false
	for _, id := range chosen {
		label := id
		if m.status != nil && m.status.Channels.isSafeOnly(id) {
			label += tr("safe_only_the_image_goes_through_the_nsfw_filter_first")
			restricted = true
		}
		lines = append(lines, "  • "+label)
	}
	if restricted {
		lines = append(lines, "", tr("if_the_filter_blocks_it_this_channel_is_skipped_and_the_rest_receive_it"))
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
			m.setNotice("aguarde: "+m.busyLabel+tr("in_progress"), true)
			return m, nil
		}
		action := m.pending
		m.pending, m.overlay = nil, action.back
		return m, m.exec(action)
	case "n", "N", "esc":
		back := overlayNone
		if m.pending != nil {
			back = m.pending.back
		}
		m.pending, m.overlay = nil, back
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
			m.setNotice(tr("enter_a_number_between_1_and_d", maxDispatchBatch), true)
			return m, nil
		}
		channels := []string{}
		if m.status != nil {
			channels = m.status.Channels.Meme
		}
		m.overlay = overlayNone
		m.askConfirm(&pendingAction{
			label:     tr("dispatch_d_meme_s", count),
			eventType: "meme.dispatch.scheduled",
			payload:   map[string]any{"limit": count},
			lines: append([]string{tr("pick_d_meme_s_from_the_top_of_the_queue_and_send_to", count)},
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
		m.setNotice(tr("write_a_message_and_or_provide_an_image"), true)
		return nil
	case len(chosen) == 0:
		m.setNotice(tr("select_at_least_one_channel_space"), true)
		return nil
	}
	payload := map[string]any{"channel_ids": chosen}
	lines := []string{}
	if text != "" {
		payload["content"] = text
		lines = append(lines, tr("text_summary_prefix")+trunc(text, 70))
	}
	if image != "" {
		payload["attachment_url"] = image
		lines = append(lines, tr("image_summary_prefix")+trunc(image, 70))
	}
	m.askConfirm(&pendingAction{
		label:     tr("send_message_action"),
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

// cmdSelectedImage fetches the selected pool meme or tagged example's preview.
// Downloads do not use the orchestrator request slot: they are plain HTTP and
// independent of the REQ socket.
func (m *Model) cmdSelectedImage() tea.Cmd {
	item, ok := m.selectedPreviewItem()
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
		m.images.remember(&preview{url: url, err: tr("video_no_preview_ffmpeg_unavailable")})
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
	item, ok := m.selectedPreviewItem()
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
		p.err = tr("terminal_refused_the_image") + err.Error()
	}
	p.uploaded, p.transmit = true, nil
}

// panelRow is one selectable line of the panel: a module, a service or a job.
type panelRow struct {
	isModule       bool
	isJob          bool
	name           string
	enabled        bool
	hasRouting     bool
	routingEnabled bool
}

func (m *Model) panelRows() []panelRow {
	var rows []panelRow
	if m.modules != nil {
		for _, module := range m.modules.Modules {
			row := panelRow{isModule: true, name: module.Name, enabled: !module.Stopped()}
			if m.status != nil {
				if service, ok := m.status.service(module.Name); ok {
					row.hasRouting = true
					row.routingEnabled = service.Enabled
				}
			}
			rows = append(rows, row)
		}
	}
	if m.status == nil {
		return rows
	}
	for _, service := range m.status.Services {
		if m.hasRuntimeModule(service.Name) {
			continue
		}
		rows = append(rows, panelRow{name: service.Name, enabled: service.Enabled})
	}
	for _, job := range m.status.Jobs {
		rows = append(rows, panelRow{isJob: true, name: job.Name, enabled: job.Enabled})
	}
	return rows
}

func (m *Model) hasRuntimeModule(name string) bool {
	if m.modules == nil {
		return false
	}
	for _, module := range m.modules.Modules {
		if module.Name == name {
			return true
		}
	}
	return false
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
		row, ok := m.selectedPanelRow()
		switch {
		case ok && row.isModule && m.modules != nil && m.modules.Busy:
			m.setNotice(tr("modules_busy"), true)
		case ok && row.isModule && !row.enabled && cli.DeliveryModules[row.name]:
			// Discord answers and the scheduler posts once running: say what will happen first.
			action := m.moduleToggle(row)
			action.lines = m.deliveryLines(row.name)
			m.askConfirm(action)
		case ok && row.isModule:
			return m, m.exec(m.moduleToggle(row))
		case ok:
			return m, m.exec(m.togglePending(row))
		}
	case "x":
		if row, ok := m.selectedPanelRow(); ok && row.isJob {
			m.askRunJob(row)
		} else if ok {
			m.setNotice(tr("only_jobs_can_be_run_select_a_job"), true)
		}
	case "e":
		if row, ok := m.selectedPanelRow(); ok && row.isModule && row.hasRouting {
			routing := panelRow{name: row.name, enabled: row.routingEnabled}
			return m, m.exec(m.togglePending(routing))
		}
		m.setNotice(tr("select_a_communication_service"), true)
	case "p":
		m.openProfiles()
	case "m":
		cmd := m.cmdMemory()
		if cmd == nil {
			return m, nil // another request is running; begin() already said so
		}
		m.memory = memoryView{}
		m.overlay = overlayMemory
		return m, cmd
	case "c":
		return m, m.openSettings()
	case "l":
		if row, ok := m.selectedPanelRow(); ok && row.name == "whatsapp" {
			return m, m.openLink()
		}
		m.setNotice(tr("link_select_whatsapp"), true)
	case "i":
		if row, ok := m.selectedPanelRow(); ok && row.isJob {
			m.openInterval(row.name)
		} else if ok {
			m.setNotice(tr("only_jobs_have_an_interval_select_a_job"), true)
		}
	}
	return m, nil
}

func (m *Model) keyMemory(key string) (tea.Model, tea.Cmd) {
	switch key {
	case "esc", "q":
		m.overlay = overlayNone
	case "r":
		return m, m.cmdMemory()
	case "l":
		cmd := m.cmdMemoryList("", 0, 0)
		if cmd == nil {
			return m, nil // another request is running; begin() already said so
		}
		m.memoryList = memoryExperiences{}
		m.overlay = overlayMemoryList
		return m, cmd
	case "p":
		m.memoryInput.SetValue("")
		m.memoryInput.Focus()
		m.memoryResult = memoryPredictView{}
		m.overlay = overlayMemoryPredict
	case "x":
		// One bounded batch per press: the service embeds a few dozen at a time, and the
		// summary says how many remain.
		return m, m.exec(&pendingAction{
			label:     tr("reindex_memory"),
			eventType: "memory.reindex",
			payload:   map[string]any{},
			summarize: func(res map[string]any) string {
				return tr("d_embedded_d_remaining", num(res, "embedded"), num(res, "remaining"))
			},
			onSuccess: func(m *Model, _ map[string]any) tea.Cmd { return m.cmdMemory() },
		})
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

// moduleToggle starts or stops a module's containers. It changes no service switch or job, so
// starting a module never makes Ningo post anything by itself.
func (m *Model) moduleToggle(row panelRow) *pendingAction {
	label := tr("start_module", row.name)
	if row.enabled {
		label = tr("stop_module", row.name)
	}
	runtime := m.runtime
	return &pendingAction{
		label: label,
		local: func() (string, error) {
			ctx, cancel := context.WithTimeout(context.Background(), min(m.timeout, 15*time.Second))
			defer cancel()
			if _, err := runtime.Request(ctx, "runtime.set", map[string]any{"module": row.name, "enabled": !row.enabled}); err != nil {
				return "", err
			}
			return tr("module_accepted"), nil
		},
		reload: true,
	}
}

// deliveryLines says what starting discord or the scheduler will do with the switches and jobs
// already saved. Nothing is changed on the way.
func (m *Model) deliveryLines(module string) []string {
	lines := []string{tr("start_module", module), ""}
	if m.status == nil {
		return append(lines, tr("delivery_status_unknown"))
	}
	on := func(name string) bool {
		row, ok := m.status.service(name)
		return !ok || row.Enabled
	}
	switch module {
	case "discord":
		lines = append(lines, tr("delivery_discord_voice"))
		if on("discord") {
			return append(lines, tr("delivery_discord_answers"))
		}
		return append(lines, tr("delivery_discord_switch_off"))
	case "scheduler":
		if !on("scheduler") {
			return append(lines, tr("delivery_scheduler_switch_off"))
		}
		running := 0
		for _, job := range m.status.Jobs {
			if !job.Enabled {
				continue
			}
			running++
			line := tr("delivery_job_every", job.Name, job.Interval)
			if !job.NextAt.IsZero() {
				line += tr("delivery_job_next", clock(job.NextAt, m.now()))
			}
			switch job.Name {
			case "meme.dispatch":
				line += tr("delivery_job_posts_memes", len(m.status.Channels.Meme))
			case "meme.refresh":
				line += tr("delivery_job_refreshes")
			}
			lines = append(lines, line)
		}
		if running == 0 {
			lines = append(lines, tr("delivery_scheduler_all_paused"))
		}
	}
	return lines
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
	lines := []string{tr("run_now") + row.name, ""}
	if !row.enabled {
		lines = append(lines, tr("the_job_is_paused_a_manual_run_ignores_the_pause"), "")
	}
	if service, ok := m.status.service(jobService(row.name)); ok && !service.Enabled {
		lines = append(lines, tr("warning_service_s_is_switched_off_and_the_orchestrator_will_refuse_it", service.Name), "")
	}
	summarize := func(map[string]any) string { return "executado" }
	switch row.name {
	case "meme.dispatch":
		lines = append(lines, tr("sends_1_meme_goes_through_the_nsfw_filter_on_safe_only_channels_to"))
		lines = append(lines, m.destinationLines(m.status.Channels.Meme)[1:]...)
		summarize = summarizeDispatch
	}
	m.askConfirm(&pendingAction{
		label:     tr("run_job") + row.name,
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
			label:     tr("interval_action_prefix") + m.intervalJob,
			eventType: "scheduler.jobs.set",
			payload:   payload,
			summarize: func(res map[string]any) string {
				if interval := str(res, "interval"); interval != "" {
					return tr("every") + interval
				}
				return tr("back_to_the_default")
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
		m.setNotice(tr("wait_for_the_panel_to_load"), true)
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
				label:     tr("use_chat_profile") + row.Name,
				eventType: "chat.profile.use",
				payload:   map[string]any{"name": row.Name},
				summarize: func(map[string]any) string { return tr("active_applies_from_the_next_message") },
				reload:    true,
			})
		}
	case "n":
		if m.profiles == nil {
			m.setNotice(tr("no_local_access_to_the_database_set_dokja_db_file_to_create_profiles"), true)
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
			m.setNotice(tr("no_local_access_to_the_database_set_dokja_db_file_to_delete_profiles"), true)
			return m, nil
		}
		if m.profileCursor < len(rows) {
			m.askDeleteProfile(rows[m.profileCursor])
		}
	}
	return m, nil
}

func (m *Model) askDeleteProfile(row profileRow) {
	lines := []string{tr("delete_profile_confirmation") + row.Name + " (" + row.Model + ")?", ""}
	if row.Active {
		lines = append(lines, tr("default_profile_warning"), "")
	}
	lines = append(lines, tr("this_removes_the_token_from_the_local_database_it_cannot_be_undone"))
	m.overlay = overlayConfirm
	m.pending = &pendingAction{
		label: tr("delete_profile_action") + row.Name,
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
	m.addHistory(tr("create_profile")+profile.Name, tr("saved_in_the_local_database"), true)
	m.setNotice(tr("profile_notice_prefix")+profile.Name+tr("saved_press_enter_on_the_profile_to_activate_it"), false)
	return m.cmdStatus()
}

func (m *Model) linkTick() tea.Cmd {
	return tea.Tick(linkPoll, func(time.Time) tea.Msg { return linkTickMsg{} })
}

// openLink shows the WhatsApp link window, which reads the state now and every couple of seconds.
func (m *Model) openLink() tea.Cmd {
	m.link, m.linkErr, m.linkOpen, m.overlay = cli.LinkView{}, "", true, overlayLink
	return tea.Batch(m.cmdLink("communications.link.status"), m.linkTick())
}

func (m *Model) cmdLink(eventType string) tea.Cmd {
	if !m.begin(tr("link_reading")) {
		return nil
	}
	return func() tea.Msg {
		res, err := m.request(eventType, map[string]any{"interface": "whatsapp"})
		if err != nil {
			return linkMsg{err: err}
		}
		return linkMsg{view: cli.ParseLink(res)}
	}
}

func (m *Model) keyLink(key string) (tea.Model, tea.Cmd) {
	switch key {
	case "esc", "q":
		m.linkOpen, m.overlay = false, overlayNone
	case "n":
		return m, m.cmdLink("communications.link.start")
	case "u":
		m.askConfirm(&pendingAction{
			label:     tr("link_unlink_label"),
			eventType: "communications.link.reset",
			payload:   map[string]any{"interface": "whatsapp"},
			lines:     []string{tr("link_unlink_label"), "", tr("link_unlink_warning")},
			summarize: func(res map[string]any) string { return cli.LinkStateText(str(res, "state")) },
			onSuccess: func(m *Model, res map[string]any) tea.Cmd {
				m.link = cli.ParseLink(res)
				return nil
			},
			back: overlayLink,
		})
	}
	return m, nil
}
