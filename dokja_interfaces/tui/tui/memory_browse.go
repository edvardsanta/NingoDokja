package tui

import (
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

const (
	memoryPageSize = 8
	// chancePause is how long the hashtag suggestion stops asking the memory for a chance after the
	// memory failed to answer, so a stopped service costs one wait, not one per suggestion.
	chancePause = time.Minute
	// predictTextLimit is the longest text the memory takes as a context.
	predictTextLimit = 4000
)

// memoryStates are the filters of the experience list, in the order t cycles through them.
var memoryStates = []string{"", "pending", "resolved"}

// memoryExperience is one experience in the list.
type memoryExperience struct {
	Ref, Action, Detail, Outcome, Snippet, CreatedAt, ResolvedAt string
	Predicted, Baseline                                          *float64
	// MatchedScore and Matched are what the choice rested on: how close the earlier example was and
	// the start of its text. Both are empty for an experience recorded without them.
	MatchedScore *float64
	Matched      string
}

type memoryExperiences struct {
	Items         []memoryExperience
	Total, Offset int
	Cursor        int
	State         string
	loaded        bool
	// off is why the orchestrator refused (the service is switched off); err is any other failure.
	off, err string
}

type memoryEvidence struct {
	Ref, Detail, Outcome, Snippet string
	Similarity                    float64
}

// memoryPrediction is the chance an action is accepted for a context, with what it rests on.
type memoryPrediction struct {
	Action                 string
	Predicted, Baseline    float64
	Support, Resolved      int
	Insufficient, Degraded bool
	Reason                 string
	Evidence               []memoryEvidence
}

// memoryPredictView is the prediction form's result area.
type memoryPredictView struct {
	prediction *memoryPrediction
	off, err   string
}

type memoryListMsg struct{ list memoryExperiences }

type memoryPredictionMsg struct{ view memoryPredictView }

// suggestionChanceMsg carries the chance asked for after a hashtag suggestion. base is the notice the
// chance is appended to, so a notice that has changed meanwhile is left alone.
type suggestionChanceMsg struct {
	prediction *memoryPrediction
	base       string
}

func parseMemoryExperiences(res map[string]any) memoryExperiences {
	list := memoryExperiences{Total: num(res, "total"), Offset: num(res, "offset")}
	items, _ := res["experiences"].([]any)
	for _, item := range items {
		fields, ok := item.(map[string]any)
		if !ok {
			continue
		}
		experience := memoryExperience{
			Ref: str(fields, "ref"), Action: str(fields, "action"), Detail: str(fields, "detail"),
			Outcome: str(fields, "outcome"), Snippet: str(fields, "context_snippet"),
			CreatedAt: str(fields, "created_at"), ResolvedAt: str(fields, "resolved_at"),
			Matched: str(fields, "matched_snippet"),
		}
		if value, ok := fields["matched_score"].(float64); ok {
			experience.MatchedScore = &value
		}
		if value, ok := fields["predicted_p"].(float64); ok {
			experience.Predicted = &value
		}
		if value, ok := fields["baseline_p"].(float64); ok {
			experience.Baseline = &value
		}
		list.Items = append(list.Items, experience)
	}
	return list
}

func parseMemoryPrediction(res map[string]any) memoryPrediction {
	prediction := memoryPrediction{
		Action: str(res, "action"), Predicted: flt(res, "predicted_p"), Baseline: flt(res, "baseline_p"),
		Support: num(res, "support"), Resolved: num(res, "resolved"), Reason: str(res, "reason"),
	}
	prediction.Insufficient, _ = res["insufficient"].(bool)
	prediction.Degraded, _ = res["degraded"].(bool)
	items, _ := res["evidence"].([]any)
	for _, item := range items {
		fields, ok := item.(map[string]any)
		if !ok {
			continue
		}
		prediction.Evidence = append(prediction.Evidence, memoryEvidence{
			Ref: str(fields, "ref"), Detail: str(fields, "detail"), Outcome: str(fields, "outcome"),
			Snippet: str(fields, "context_snippet"), Similarity: flt(fields, "similarity"),
		})
	}
	return prediction
}

func percent(chance float64) int { return int(math.Round(chance * 100)) }

// ---- commands -------------------------------------------------------------------------------

// cmdMemoryList reads one page of the experience list. The page, filter and cursor it is asked for
// become the list's only if the request goes out, so a busy TUI leaves the list as it was.
func (m *Model) cmdMemoryList(state string, offset, cursor int) tea.Cmd {
	if !m.begin(tr("reading_the_experiences")) {
		return nil
	}
	return func() tea.Msg {
		payload := map[string]any{"limit": memoryPageSize, "offset": offset, "include_context": true}
		if state != "" {
			payload["state"] = state
		}
		list := memoryExperiences{State: state, Offset: offset, Cursor: cursor, loaded: true}
		res, err := m.request("memory.list", payload)
		var skipped *skippedError
		switch {
		case errors.As(err, &skipped):
			list.off = skipped.reason
		case err != nil:
			list.err = err.Error()
		default:
			parsed := parseMemoryExperiences(res)
			list.Items, list.Total, list.Offset = parsed.Items, parsed.Total, parsed.Offset
		}
		return memoryListMsg{list: list}
	}
}

// cmdMemoryPredict asks for the chance a suggestion for this text is kept, with the earlier
// experiences it rests on.
func (m *Model) cmdMemoryPredict(text string) tea.Cmd {
	if !m.begin(tr("predicting_the_chance")) {
		return nil
	}
	return func() tea.Msg {
		res, err := m.request("memory.predict", map[string]any{
			"action": memoryScoredAction, "context": firstRunes(text, predictTextLimit), "include_context": true,
		})
		var skipped *skippedError
		switch {
		case errors.As(err, &skipped):
			return memoryPredictionMsg{view: memoryPredictView{off: skipped.reason}}
		case err != nil:
			return memoryPredictionMsg{view: memoryPredictView{err: err.Error()}}
		}
		prediction := parseMemoryPrediction(res)
		return memoryPredictionMsg{view: memoryPredictView{prediction: &prediction}}
	}
}

// cmdSuggestionChance follows a hashtag suggestion with the chance such a suggestion is kept. It is
// a second request on purpose: the suggestion is already on screen, so a slow or stopped memory costs
// the suggestion nothing, and after one failure it is not asked again for a while. Only a relevant
// suggestion, the kind the bot appends and the memory records, is worth a number.
func (m *Model) cmdSuggestionChance(suggestion map[string]any) tea.Cmd {
	text := strings.TrimSpace(str(suggestion, "query_text"))
	relevant, _ := suggestion["relevant"].(bool)
	if !relevant || str(suggestion, "hashtag") == "" || text == "" {
		return nil
	}
	if m.status != nil {
		if row, ok := m.status.service("memory"); ok && !row.Enabled {
			return nil
		}
	}
	if m.now().Before(m.chancePausedUntil) {
		return nil
	}
	if !m.begin(tr("predicting_the_chance")) {
		return nil
	}
	base := m.notice
	return func() tea.Msg {
		res, err := m.request("memory.predict", map[string]any{"action": memoryScoredAction, "context": firstRunes(text, predictTextLimit)})
		if err != nil {
			return suggestionChanceMsg{base: base}
		}
		prediction := parseMemoryPrediction(res)
		return suggestionChanceMsg{prediction: &prediction, base: base}
	}
}

func firstRunes(text string, limit int) string {
	runes := []rune(text)
	if len(runes) <= limit {
		return text
	}
	return string(runes[:limit])
}

// chanceNotice is the short wording appended to a suggestion's notice.
func chanceNotice(p memoryPrediction) string {
	if p.Insufficient {
		return tr("memory_chance_baseline_only", percent(p.Baseline))
	}
	return tr("memory_chance_kept_short", percent(p.Predicted), percent(p.Baseline))
}

// ---- keys -----------------------------------------------------------------------------------

func (m *Model) selectedMemoryExperience() (memoryExperience, bool) {
	list := m.memoryList
	if list.Cursor < 0 || list.Cursor >= len(list.Items) {
		return memoryExperience{}, false
	}
	return list.Items[list.Cursor], true
}

func (m *Model) keyMemoryList(key string) (tea.Model, tea.Cmd) {
	list := m.memoryList
	switch key {
	case "esc", "q":
		m.overlay = overlayMemory
	case "up", "k":
		if m.memoryList.Cursor > 0 {
			m.memoryList.Cursor--
		}
	case "down", "j":
		if m.memoryList.Cursor < len(list.Items)-1 {
			m.memoryList.Cursor++
		}
	case "n", "pgdown":
		if list.Offset+memoryPageSize < list.Total {
			return m, m.cmdMemoryList(list.State, list.Offset+memoryPageSize, 0)
		}
	case "p", "pgup":
		if list.Offset > 0 {
			return m, m.cmdMemoryList(list.State, max(0, list.Offset-memoryPageSize), 0)
		}
	case "t":
		next := 0
		for i, state := range memoryStates {
			if state == list.State {
				next = (i + 1) % len(memoryStates)
			}
		}
		return m, m.cmdMemoryList(memoryStates[next], 0, 0)
	case "r":
		return m, m.cmdMemoryList(list.State, list.Offset, list.Cursor)
	case "f":
		if item, ok := m.selectedMemoryExperience(); ok {
			m.askConfirm(&pendingAction{
				label:     tr("forget_experience"),
				eventType: "memory.forget",
				payload:   map[string]any{"ref": item.Ref},
				lines:     []string{trunc(experienceTitle(item), 70), "", tr("forget_experience_confirmation")},
				summarize: func(map[string]any) string { return tr("memory_forgotten") },
				onSuccess: func(m *Model, _ map[string]any) tea.Cmd {
					return m.cmdMemoryList(m.memoryList.State, m.memoryList.Offset, m.memoryList.Cursor)
				},
				back: overlayMemoryList,
			})
		}
	}
	return m, nil
}

func (m *Model) keyMemoryPredict(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.memoryInput.Blur()
		m.overlay = overlayMemory
		return m, nil
	case "enter":
		text := strings.TrimSpace(m.memoryInput.Value())
		if text == "" {
			m.setNotice(tr("memory_predict_type_first"), true)
			return m, nil
		}
		return m, m.cmdMemoryPredict(text)
	}
	var cmd tea.Cmd
	m.memoryInput, cmd = m.memoryInput.Update(msg)
	return m, cmd
}

// ---- views ----------------------------------------------------------------------------------

func memoryFilterLabel(state string) string {
	switch state {
	case "pending":
		return tr("memory_filter_pending")
	case "resolved":
		return tr("memory_filter_resolved")
	}
	return tr("memory_filter_all")
}

// outcomeLabel names an outcome in the interface language. An experience with no outcome yet is
// pending.
func outcomeLabel(outcome string) string {
	switch outcome {
	case "":
		return tr("memory_outcome_pending")
	case "accepted":
		return tr("memory_outcome_accepted")
	case "replaced":
		return tr("memory_outcome_replaced")
	case "ignored":
		return tr("memory_outcome_ignored")
	case "expired":
		return tr("memory_outcome_expired")
	}
	return outcome
}

func outcomeStyle(outcome string) func(...string) string {
	switch outcome {
	case "accepted":
		return styleOK.Render
	case "replaced", "ignored":
		return styleBad.Render
	}
	return styleDim.Render
}

// experienceTitle is what an experience is called on screen: what the bot did, and for what.
func experienceTitle(e memoryExperience) string {
	title := firstNonEmpty(e.Detail, e.Action)
	if e.Detail != "" && e.Action != "" {
		title += " · " + e.Action
	}
	return title
}

func (m *Model) viewMemoryList() string {
	l := m.memoryList
	var b strings.Builder
	b.WriteString(styleTitle.Render(tr("memory_experiences_title")) +
		styleDim.Render("  "+memoryFilterLabel(l.State)+" · "+strconv.Itoa(l.Total)) + "\n\n")
	switch {
	case !l.loaded:
		b.WriteString(styleDim.Render(tr("loading")))
	case l.off != "":
		b.WriteString(styleWarn.Render(tr("memory_switched_off")) + styleDim.Render("  ("+trunc(l.off, 60)+")"))
	case l.err != "":
		b.WriteString(styleBad.Render(strings.TrimRight(tr("no_answer_from_the_memory_service"), " ")) + "\n")
		b.WriteString("  " + styleDim.Render(trunc(l.err, 68)))
	case len(l.Items) == 0:
		b.WriteString(styleDim.Render(tr("memory_no_experiences")))
	default:
		for i, e := range l.Items {
			chance := "   -   "
			if e.Predicted != nil && e.Baseline != nil {
				chance = fmt.Sprintf("%.2f/%.2f", *e.Predicted, *e.Baseline)
			}
			line := outcomeStyle(e.Outcome)(fmt.Sprintf("%-9s", outcomeLabel(e.Outcome))) +
				fmt.Sprintf(" %-14s %s %s", trunc(firstNonEmpty(e.Detail, e.Action), 14), chance, styleDim.Render(trunc(e.Snippet, 30)))
			if i == l.Cursor {
				b.WriteString(styleCursor.Render("▸ ") + line + "\n")
			} else {
				b.WriteString("  " + line + "\n")
			}
		}
		if e, ok := m.selectedMemoryExperience(); ok {
			b.WriteString("\n" + viewMemoryExperience(e))
		}
	}
	return styleBox.Render(strings.TrimRight(b.String(), "\n"))
}

// viewMemoryExperience shows the selected experience in full: what the bot did, the start of its
// context, what was predicted and when.
func viewMemoryExperience(e memoryExperience) string {
	var b strings.Builder
	b.WriteString(styleTitle.Render(trunc(experienceTitle(e), 68)) + "\n")
	if e.Snippet != "" {
		b.WriteString("  " + styleDim.Render("\""+trunc(e.Snippet, 64)+"\"") + "\n")
	}
	if matched := matchedEvidence(e); matched != "" {
		b.WriteString(tr("memory_detail_matched", matched) + "\n")
	}
	if e.Predicted != nil && e.Baseline != nil {
		b.WriteString("  " + tr("memory_detail_chance", *e.Predicted, *e.Baseline) + "\n")
	} else {
		b.WriteString("  " + styleDim.Render(tr("memory_detail_no_prediction")) + "\n")
	}
	created := dateTime(e.CreatedAt)
	if e.ResolvedAt != "" {
		b.WriteString("  " + tr("memory_detail_dates", created, dateTime(e.ResolvedAt)) + "\n")
	} else {
		b.WriteString("  " + tr("memory_detail_created", created) + "\n")
	}
	b.WriteString("  " + styleDim.Render("ref "+trunc(e.Ref, 60)))
	return b.String()
}

// matchedEvidence is what an experience's choice rested on, as "0.83 "start of the text"", or empty
// when it was recorded without it.
func matchedEvidence(e memoryExperience) string {
	var parts []string
	if e.MatchedScore != nil {
		parts = append(parts, fmt.Sprintf("%.2f", *e.MatchedScore))
	}
	if e.Matched != "" {
		parts = append(parts, "\""+trunc(e.Matched, 50)+"\"")
	}
	return strings.Join(parts, " ")
}

// dateTime shortens a service timestamp such as 2026-10-01T03:27:52Z to 2026-10-01 03:27.
func dateTime(value string) string {
	if len(value) >= 16 {
		return value[:10] + " " + value[11:16]
	}
	return value
}

func (m *Model) viewMemoryPredict() string {
	var b strings.Builder
	b.WriteString(styleTitle.Render(tr("memory_predict_title", memoryScoredAction)) + "\n")
	b.WriteString(styleDim.Render(tr("memory_predict_intro")) + "\n\n")
	b.WriteString(tr("memory_predict_prompt") + m.memoryInput.View() + "\n")

	v := m.memoryResult
	switch {
	case v.off != "":
		b.WriteString("\n" + styleWarn.Render(tr("memory_switched_off")) + styleDim.Render("  ("+trunc(v.off, 60)+")"))
	case v.err != "":
		b.WriteString("\n" + styleBad.Render(strings.TrimRight(tr("no_answer_from_the_memory_service"), " ")) + "\n")
		b.WriteString("  " + styleDim.Render(trunc(v.err, 68)))
	case v.prediction != nil:
		b.WriteString("\n" + viewMemoryPrediction(*v.prediction))
	}
	return styleBox.Render(strings.TrimRight(b.String(), "\n"))
}

func viewMemoryPrediction(p memoryPrediction) string {
	var b strings.Builder
	b.WriteString(tr("memory_predict_chance", percent(p.Predicted), percent(p.Baseline)) + "\n")
	b.WriteString("  " + styleDim.Render(tr("memory_predict_support", p.Support, p.Resolved)) + "\n")
	if p.Degraded {
		b.WriteString("  " + styleWarn.Render(tr("memory_predict_degraded")) + "\n")
	}
	if p.Insufficient {
		b.WriteString("  " + styleWarn.Render(tr("memory_predict_insufficient", trunc(p.Reason, 50))) + "\n")
	}
	if len(p.Evidence) > 0 {
		b.WriteString("\n" + styleTitle.Render(tr("memory_predict_evidence")) + "\n")
		for _, e := range p.Evidence {
			line := fmt.Sprintf("[%.2f] ", e.Similarity) + outcomeStyle(e.Outcome)(fmt.Sprintf("%-9s", outcomeLabel(e.Outcome))) +
				fmt.Sprintf(" %-12s ", trunc(e.Detail, 12)) + styleDim.Render(trunc(e.Snippet, 28))
			b.WriteString("  " + line + "\n")
		}
	}
	return b.String()
}
