// Package quiz owns how Ningo takes an online questionnaire: which questions a runner may hand
// over, how they are put to the model, which answers count, and which result link is accepted.
// It does no I/O. The orchestrator asks a Feeds quiz runner and the model, in that order.
package quiz

import (
	"fmt"
	"net/url"
	"regexp"
	"strings"
	"unicode/utf8"
)

// Capability is what a quiz runner provides in Feeds.
const Capability = "quiz.run.v1"

// ActionTake is the workflow action of a quiz.take request.
const ActionTake = "take-quiz"

// Runner actions.
const (
	RunnerQuestions = "questions"
	RunnerSubmit    = "submit"
)

// Bounds on what a runner may hand over. They keep a hostile or broken page from flooding the
// model's context.
const (
	MaxQuestions   = 400
	MaxTextRunes   = 600
	MaxOptionRunes = 160
	MinOptions     = 2
	MaxOptions     = 12
	MaxLinkChars   = 2048
	MaxTitleRunes  = 120
	MaxLabelRunes  = 120
)

// Refusal codes. Interfaces word them; a runner's own text never reaches a person.
const (
	RefusedInvalidLink = "invalid_link"
	RefusedBusy        = "busy"
	RefusedNoRunner    = "no_runner"
	RefusedUnsupported = "unsupported"
	RefusedUnavailable = "unavailable"
	RefusedUnanswered  = "unanswered"
	RefusedFailed      = "failed"
)

var idPattern = regexp.MustCompile(`^[A-Za-z0-9._:-]{1,64}$`)

// Question is one item of a questionnaire, as plain text from a third-party site.
type Question struct {
	ID      string   `json:"id"`
	Text    string   `json:"text"`
	Options []string `json:"options"`
}

// Quiz is everything a runner says the site may ask.
type Quiz struct {
	Title     string     `json:"title"`
	Questions []Question `json:"questions"`
}

// Result is the site's verdict: its result link and the short label it shows.
type Result struct {
	URL   string `json:"url"`
	Label string `json:"label,omitempty"`
}

// ParseLink accepts an http(s) link without credentials, the only thing a person supplies.
func ParseLink(raw string) (string, error) {
	link := strings.TrimSpace(raw)
	parsed, err := url.Parse(link)
	if err != nil || link == "" || len(link) > MaxLinkChars || parsed.User != nil || parsed.Hostname() == "" ||
		(parsed.Scheme != "http" && parsed.Scheme != "https") {
		return "", fmt.Errorf("quiz link must be an http(s) address without credentials")
	}
	return link, nil
}

// ParseQuiz validates a runner's quiz. Any question out of bounds rejects the whole quiz rather
// than answering a different questionnaire than the site will ask.
func ParseQuiz(value any) (Quiz, error) {
	raw, _ := value.(map[string]any)
	if raw == nil {
		return Quiz{}, fmt.Errorf("quiz must be an object")
	}
	title, _ := raw["title"].(string)
	quiz := Quiz{Title: clip(singleLine(title), MaxTitleRunes)}
	items, _ := raw["questions"].([]any)
	if len(items) == 0 || len(items) > MaxQuestions {
		return Quiz{}, fmt.Errorf("quiz must have 1 to %d questions", MaxQuestions)
	}
	seen := map[string]bool{}
	for index, item := range items {
		question, err := parseQuestion(item)
		if err != nil {
			return Quiz{}, fmt.Errorf("question %d: %w", index+1, err)
		}
		if seen[question.ID] {
			return Quiz{}, fmt.Errorf("question %d: duplicate id", index+1)
		}
		seen[question.ID] = true
		quiz.Questions = append(quiz.Questions, question)
	}
	return quiz, nil
}

func parseQuestion(value any) (Question, error) {
	raw, _ := value.(map[string]any)
	id, _ := raw["id"].(string)
	text, _ := raw["text"].(string)
	text = singleLine(text)
	if !idPattern.MatchString(id) {
		return Question{}, fmt.Errorf("id must be 1 to 64 letters, digits, '.', '_', ':' or '-'")
	}
	if text == "" || utf8.RuneCountInString(text) > MaxTextRunes {
		return Question{}, fmt.Errorf("text must be 1 to %d characters", MaxTextRunes)
	}
	options, _ := raw["options"].([]any)
	if len(options) < MinOptions || len(options) > MaxOptions {
		return Question{}, fmt.Errorf("must have %d to %d options", MinOptions, MaxOptions)
	}
	question := Question{ID: id, Text: text}
	seen := map[string]bool{}
	for _, option := range options {
		text, _ := option.(string)
		text = singleLine(text)
		if text == "" || utf8.RuneCountInString(text) > MaxOptionRunes || seen[normalize(text)] {
			return Question{}, fmt.Errorf("options must be distinct texts of 1 to %d characters", MaxOptionRunes)
		}
		seen[normalize(text)] = true
		question.Options = append(question.Options, text)
	}
	return question, nil
}

// ParseResult accepts the site's result link only on the quiz's own host, so a runner cannot
// make Ningo post an arbitrary link.
func ParseResult(quizLink string, value any) (Result, error) {
	raw, _ := value.(map[string]any)
	link, _ := raw["url"].(string)
	link, err := ParseLink(link)
	if err != nil {
		return Result{}, err
	}
	start, _ := url.Parse(quizLink)
	got, _ := url.Parse(link)
	if start == nil || !strings.EqualFold(start.Hostname(), got.Hostname()) {
		return Result{}, fmt.Errorf("result link must be on the quiz's host")
	}
	label, _ := raw["label"].(string)
	return Result{URL: link, Label: clip(singleLine(label), MaxLabelRunes)}, nil
}

// singleLine folds any whitespace, line breaks included, into single spaces and drops control
// characters.
func singleLine(s string) string {
	return strings.Join(strings.FieldsFunc(s, func(r rune) bool {
		return r == ' ' || r == '\t' || r == '\n' || r == '\r' || r < 0x20 || r == 0x7f
	}), " ")
}

func clip(s string, limit int) string {
	if utf8.RuneCountInString(s) <= limit {
		return s
	}
	return string([]rune(s)[:limit])
}
