package tui

import (
	"fmt"
	"strings"
	"time"
	"unicode"

	"dokja_interfaces/cli/cli"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// This tab is one more door to the orchestrator's archive, the one the desktop, the phone and the CLI
// use: it keeps nothing of its own, so a conversation has the same id and turns wherever it is opened.

const (
	conversationPageSize  = 50
	conversationTurnsRead = 100
	conversationTextLimit = 12000
	conversationRows      = 12
	// conversationChatWait outlasts the orchestrator's own bound on a turn, so its answer is what is
	// shown and not a timeout here.
	conversationChatWait = 200 * time.Second
)

type conversationRow struct {
	ID, Kind, Title, Updated string
	Revision, Turns          int
}

// conversationTurn reads version 1 of the turn projection, pinned by the orchestrator's
// testdata/turn_view_v1.json. Provider and model are what answered, not what was selected.
type conversationTurn struct {
	View, Seq                                          int
	ID, Content, Reply, State, Reason, Provider, Model string
}

type conversationOpen struct {
	row   conversationRow
	turns []conversationTurn
}

// firstSeq is the place in the conversation of the first turn on screen, and 0 when that is unknown.
func (o *conversationOpen) firstSeq() int {
	if len(o.turns) == 0 {
		return 0
	}
	return o.turns[0].Seq
}

// older says the archive holds turns before the ones on screen.
func (o *conversationOpen) older() bool { return o.firstSeq() > 1 }

type conversationMode int

const (
	conversationBrowsing conversationMode = iota
	conversationComposing
	conversationRenaming
)

type conversationsView struct {
	loaded bool
	err    string
	items  []conversationRow
	cursor int
	next   string
	// open is the conversation on screen. A new one has no id until its first message.
	open *conversationOpen
	mode conversationMode
	// scroll is how many lines the transcript sits above its end.
	scroll int
}

func (c *conversationsView) writing() bool { return c.mode != conversationBrowsing }

type conversationsMsg struct {
	items []conversationRow
	next  string
	more  bool
	err   string
}

type conversationOpenMsg struct {
	open   *conversationOpen
	notice string
}

// conversationOlderMsg names its conversation: the owner may have left it while the page was read.
type conversationOlderMsg struct {
	id     string
	turns  []conversationTurn
	notice string
}

func (c *conversationsView) applyList(msg conversationsMsg) {
	c.loaded, c.err, c.next = true, msg.err, msg.next
	if msg.err != "" {
		return
	}
	if msg.more {
		c.items = append(c.items, msg.items...)
	} else {
		c.items, c.cursor = msg.items, 0
	}
	c.cursor = max(0, min(c.cursor, len(c.items)-1))
}

func (c *conversationsView) applyOpen(msg conversationOpenMsg) {
	if msg.open == nil {
		return
	}
	// Reading the latest page again keeps the earlier turns already read.
	if c.open != nil && c.open.row.ID == msg.open.row.ID && len(msg.open.turns) > 0 {
		var earlier []conversationTurn
		for _, turn := range c.open.turns {
			if turn.Seq > 0 && turn.Seq < msg.open.firstSeq() {
				earlier = append(earlier, turn)
			}
		}
		msg.open.turns = append(earlier, msg.open.turns...)
	}
	c.open, c.scroll = msg.open, 0
	for index := range c.items {
		if c.items[index].ID == msg.open.row.ID {
			c.items[index] = msg.open.row
		}
	}
}

// applyOlder puts a page above what is on screen. The scroll counts lines from the end, so what is on
// screen stays where it was.
func (c *conversationsView) applyOlder(msg conversationOlderMsg) {
	if c.open == nil || c.open.row.ID != msg.id {
		return
	}
	held := map[string]bool{}
	for _, turn := range c.open.turns {
		held[turn.ID] = true
	}
	var earlier []conversationTurn
	for _, turn := range msg.turns {
		if !held[turn.ID] {
			earlier = append(earlier, turn)
		}
	}
	c.open.turns = append(earlier, c.open.turns...)
}

func parseConversationRow(fields map[string]any) conversationRow {
	return conversationRow{
		ID: str(fields, "id"), Kind: str(fields, "kind"), Title: str(fields, "title"), Updated: str(fields, "updated_at"),
		Revision: num(fields, "revision"), Turns: num(fields, "turns"),
	}
}

func parseConversationTurn(fields map[string]any) conversationTurn {
	return conversationTurn{
		View: num(fields, "view"), Seq: num(fields, "seq"), ID: str(fields, "id"), Content: str(fields, "content"), Reply: str(fields, "reply"),
		State: str(fields, "state"), Reason: str(fields, "reason"), Provider: str(fields, "provider"), Model: str(fields, "model"),
	}
}

func parseConversationOpen(res map[string]any) *conversationOpen {
	fields, _ := res["conversation"].(map[string]any)
	open := &conversationOpen{row: parseConversationRow(fields)}
	turns, _ := res["turns"].([]any)
	for _, item := range turns {
		if turn, ok := item.(map[string]any); ok {
			open.turns = append(open.turns, parseConversationTurn(turn))
		}
	}
	return open
}

// retryableTurn finds the last turn left unanswered, the one the archive sends again.
func retryableTurn(turns []conversationTurn) (conversationTurn, bool) {
	for index := len(turns) - 1; index >= 0; index-- {
		switch turns[index].State {
		case "failed", "interrupted":
			return turns[index], true
		case "completed":
			return conversationTurn{}, false
		}
	}
	return conversationTurn{}, false
}

func (m *Model) cmdConversations(cursor string) tea.Cmd {
	if !m.begin(tr("reading_the_conversations")) {
		return nil
	}
	return func() tea.Msg {
		payload := map[string]any{"limit": conversationPageSize}
		if cursor != "" {
			payload["cursor"] = cursor
		}
		res, err := m.request("conversation.list", payload)
		if err != nil {
			return conversationsMsg{err: err.Error(), more: cursor != ""}
		}
		list := conversationsMsg{next: str(res, "next"), more: cursor != ""}
		items, _ := res["conversations"].([]any)
		for _, item := range items {
			if fields, ok := item.(map[string]any); ok {
				list.items = append(list.items, parseConversationRow(fields))
			}
		}
		return list
	}
}

func (m *Model) readConversation(id string) (*conversationOpen, error) {
	res, err := m.request("conversation.read", map[string]any{"conversation_id": id, "limit": conversationTurnsRead})
	if err != nil {
		return nil, err
	}
	return parseConversationOpen(res), nil
}

func (m *Model) cmdConversationOpen(id string) tea.Cmd {
	if !m.begin(tr("reading_the_conversation")) {
		return nil
	}
	return func() tea.Msg {
		open, err := m.readConversation(id)
		if err != nil {
			return conversationOpenMsg{notice: err.Error()}
		}
		return conversationOpenMsg{open: open}
	}
}

// cmdConversationOlder reads the page before the first turn on screen.
func (m *Model) cmdConversationOlder(open *conversationOpen) tea.Cmd {
	if !m.begin(tr("reading_the_conversation")) {
		return nil
	}
	id, before := open.row.ID, open.firstSeq()
	return func() tea.Msg {
		res, err := m.request("conversation.read", map[string]any{"conversation_id": id, "limit": conversationTurnsRead, "before_seq": before})
		if err != nil {
			return conversationOlderMsg{id: id, notice: err.Error()}
		}
		return conversationOlderMsg{id: id, turns: parseConversationOpen(res).turns}
	}
}

// cmdConversationTurn asks for a turn and shows what the archive holds afterwards, whether the turn
// was answered or not: a failed turn is on the transcript with its reason.
func (m *Model) cmdConversationTurn(id, title string, payload map[string]any) tea.Cmd {
	if !m.begin(tr("waiting_for_ningo")) {
		return nil
	}
	return func() tea.Msg {
		if id == "" {
			res, err := m.request("conversation.start", map[string]any{"title": title})
			if err != nil {
				return conversationOpenMsg{notice: err.Error()}
			}
			fields, _ := res["conversation"].(map[string]any)
			if id = str(fields, "id"); id == "" {
				return conversationOpenMsg{notice: tr("the_archive_named_no_conversation")}
			}
		}
		payload["conversation_id"] = id
		_, failure := m.requestWithin(conversationChatWait, "message.created", payload)
		open, err := m.readConversation(id)
		notice := ""
		switch {
		case failure != nil:
			notice = failure.Error()
		case err != nil:
			notice = err.Error()
		}
		return conversationOpenMsg{open: open, notice: notice}
	}
}

func (m *Model) cmdConversationSend(text string) tea.Cmd {
	sendID, err := cli.NewSendID()
	if err != nil {
		m.setNotice(err.Error(), true)
		return nil
	}
	return m.cmdConversationTurn(m.conversations.open.row.ID, cli.ConversationTitle(text),
		map[string]any{"content": text, "send_id": sendID})
}

func (m *Model) cmdConversationRename(row conversationRow, title string) tea.Cmd {
	if !m.begin(tr("renaming_the_conversation")) {
		return nil
	}
	return func() tea.Msg {
		_, err := m.request("conversation.rename", map[string]any{
			"conversation_id": row.ID, "revision": row.Revision, "title": title,
		})
		if err != nil {
			return conversationOpenMsg{notice: err.Error()}
		}
		open, err := m.readConversation(row.ID)
		if err != nil {
			return conversationOpenMsg{notice: err.Error()}
		}
		return conversationOpenMsg{open: open}
	}
}

// askDeleteConversation names the revision on screen, so a conversation that moved on since is
// refused by the archive instead of being deleted unseen.
func (m *Model) askDeleteConversation(row conversationRow) {
	m.askConfirm(&pendingAction{
		label:     tr("delete_conversation"),
		eventType: "conversation.delete",
		payload:   map[string]any{"conversation_id": row.ID, "revision": row.Revision},
		lines:     []string{trunc(conversationName(row), 70), "", tr("delete_conversation_confirmation")},
		summarize: func(map[string]any) string { return tr("conversation_deleted") },
		onSuccess: func(m *Model, _ map[string]any) tea.Cmd {
			m.conversations.open = nil
			return m.cmdConversations("")
		},
	})
}

func (m *Model) startWriting(mode conversationMode, value, placeholder string) tea.Cmd {
	m.conversations.mode = mode
	m.conversationInput.Placeholder = placeholder
	m.conversationInput.SetValue(value)
	return m.conversationInput.Focus()
}

func (m *Model) stopWriting() {
	m.conversations.mode = conversationBrowsing
	m.conversationInput.Blur()
	m.conversationInput.SetValue("")
}

func (m *Model) keyConversations(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	view := &m.conversations
	if view.writing() {
		return m.keyConversationWriting(msg)
	}
	key := msg.String()
	if key == "q" {
		return m, tea.Quit
	}
	if view.open != nil {
		return m.keyConversationOpen(key)
	}
	switch key {
	case "up", "k":
		view.cursor = max(0, view.cursor-1)
	case "down", "j":
		view.cursor = min(max(0, len(view.items)-1), view.cursor+1)
	case "enter":
		if len(view.items) > 0 {
			return m, m.cmdConversationOpen(view.items[view.cursor].ID)
		}
	case "n":
		view.open, view.scroll = &conversationOpen{}, 0
		return m, m.startWriting(conversationComposing, "", tr("write_to_ningo"))
	case "r":
		return m, m.cmdConversations("")
	case "m":
		if view.next != "" {
			return m, m.cmdConversations(view.next)
		}
	case "e":
		if len(view.items) > 0 {
			return m, m.startWriting(conversationRenaming, view.items[view.cursor].Title, tr("new_title"))
		}
	case "d":
		if len(view.items) > 0 {
			m.askDeleteConversation(view.items[view.cursor])
		}
	}
	return m, nil
}

func (m *Model) keyConversationOpen(key string) (tea.Model, tea.Cmd) {
	view := &m.conversations
	switch key {
	case "esc":
		view.open = nil
		return m, m.cmdConversations("")
	case "up", "k":
		view.scroll++
	case "down", "j":
		view.scroll = max(0, view.scroll-1)
	case "m":
		if view.open.older() {
			return m, m.cmdConversationOlder(view.open)
		}
	case "enter", "i":
		return m, m.startWriting(conversationComposing, "", tr("write_to_ningo"))
	case "e":
		if view.open.row.ID != "" {
			return m, m.startWriting(conversationRenaming, view.open.row.Title, tr("new_title"))
		}
	case "d":
		if view.open.row.ID != "" {
			m.askDeleteConversation(view.open.row)
		}
	case "R":
		turn, ok := retryableTurn(view.open.turns)
		if !ok {
			m.setNotice(tr("nothing_to_send_again"), true)
			return m, nil
		}
		return m, m.cmdConversationTurn(view.open.row.ID, "", map[string]any{"retry_turn_id": turn.ID})
	}
	return m, nil
}

func (m *Model) keyConversationWriting(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	view := &m.conversations
	switch msg.String() {
	case "esc":
		if view.open != nil && view.open.row.ID == "" {
			view.open = nil
		}
		m.stopWriting()
		return m, nil
	case "enter":
		text := strings.TrimSpace(m.conversationInput.Value())
		if text == "" {
			return m, nil
		}
		// Busy, the text stays in the field: leaving it would lose what was written.
		if !m.begin(tr("waiting_for_ningo")) {
			return m, nil
		}
		m.busy = false
		mode := view.mode
		m.stopWriting()
		if mode == conversationRenaming {
			if view.open != nil {
				return m, m.cmdConversationRename(view.open.row, text)
			}
			return m, m.cmdConversationRename(view.items[view.cursor], text)
		}
		return m, m.cmdConversationSend(text)
	}
	var cmd tea.Cmd
	m.conversationInput, cmd = m.conversationInput.Update(msg)
	return m, cmd
}

// plainText keeps what the archive holds from driving the terminal: a reply is the model's text, and
// a control sequence in it would otherwise reach the screen.
func plainText(text string) string {
	return strings.Map(func(r rune) rune {
		if r == '\n' || r == '\t' || !unicode.IsControl(r) {
			return r
		}
		return -1
	}, text)
}

func conversationName(row conversationRow) string {
	if row.Title == "" {
		return tr("untitled_conversation")
	}
	return plainText(row.Title)
}

func shortConversationID(id string) string {
	if len(id) > 8 {
		return id[:8]
	}
	return id
}

func (m *Model) conversationsHint() string {
	view := &m.conversations
	switch {
	case view.writing():
		return tr("conversation_writing_help")
	case view.open != nil:
		return tr("conversation_open_help")
	}
	return tr("conversation_list_help")
}

func (m *Model) viewConversations() string {
	view := &m.conversations
	if view.open != nil {
		return m.viewConversationOpen()
	}
	var b strings.Builder
	b.WriteString(styleTitle.Render(tr("conversations")) + "\n\n")
	switch {
	case view.err != "":
		b.WriteString(styleBad.Render(plainText(view.err)) + "\n")
	case !view.loaded:
		b.WriteString(styleDim.Render(tr("reading_the_conversations")) + "\n")
	case len(view.items) == 0:
		b.WriteString(styleDim.Render(tr("no_conversations_yet")) + "\n")
	}
	first := max(0, min(view.cursor-conversationRows/2, len(view.items)-conversationRows))
	for index := first; index < min(first+conversationRows, len(view.items)); index++ {
		row := view.items[index]
		updated, _, _ := strings.Cut(row.Updated, "T")
		line := fmt.Sprintf("%s  %s · %s · %s", shortConversationID(row.ID), trunc(conversationName(row), 50),
			tr("conversation_turns", row.Turns), updated)
		if row.Kind != "general" {
			line += " · " + tr("reading_conversation")
		}
		if index == view.cursor {
			b.WriteString(styleCursor.Render("› "+line) + "\n")
		} else {
			b.WriteString("  " + line + "\n")
		}
	}
	if view.next != "" {
		b.WriteString(styleDim.Render(tr("more_conversations")) + "\n")
	}
	if view.mode == conversationRenaming {
		b.WriteString("\n" + m.conversationInput.View() + "\n")
	}
	return b.String()
}

func turnLines(turn conversationTurn) []string {
	lines := []string{tr("conversation_you") + ": " + plainText(turn.Content)}
	switch turn.State {
	case "completed":
		lines = append(lines, "Ningo: "+plainText(turn.Reply))
		if answered := strings.Trim(turn.Provider+" / "+turn.Model, " /"); answered != "" {
			lines = append(lines, styleDim.Render(tr("answered_by", plainText(answered))))
		}
	case "failed":
		lines = append(lines, styleBad.Render(tr("turn_failed", plainText(firstOf(turn.Reason, tr("no_reason_recorded"))))))
	case "interrupted":
		lines = append(lines, styleWarn.Render(tr("turn_interrupted")))
	default:
		lines = append(lines, styleDim.Render(tr("turn_being_answered")))
	}
	return lines
}

func firstOf(text, fallback string) string {
	if text != "" {
		return text
	}
	return fallback
}

func (m *Model) viewConversationOpen() string {
	view := &m.conversations
	width := m.width - 2
	if width < 20 {
		width = 78
	}
	wrap := lipgloss.NewStyle().Width(width)
	lines := []string{}
	if view.open.older() {
		lines = append(lines, styleDim.Render(tr("earlier_turns")), "")
	}
	if len(view.open.turns) == 0 {
		lines = append(lines, styleDim.Render(tr("nothing_said_yet")))
	}
	for _, turn := range view.open.turns {
		for _, line := range turnLines(turn) {
			lines = append(lines, strings.Split(wrap.Render(line), "\n")...)
		}
		lines = append(lines, "")
	}
	room := m.height - 10
	if room < 5 {
		room = conversationRows
	}
	end := max(room, len(lines)-view.scroll)
	end = min(end, len(lines))
	start := max(0, end-room)
	view.scroll = len(lines) - end

	var b strings.Builder
	b.WriteString(styleTitle.Render(conversationName(view.open.row)) + "\n\n")
	b.WriteString(strings.Join(lines[start:end], "\n") + "\n")
	if view.writing() {
		b.WriteString("\n" + m.conversationInput.View() + "\n")
	}
	return b.String()
}
