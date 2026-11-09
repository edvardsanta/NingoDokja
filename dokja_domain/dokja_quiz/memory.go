package quiz

import (
	"encoding/json"
	"fmt"
	"time"
	"unicode/utf8"
)

// What Ningo remembers of the questionnaires he took. A run is kept only when it finished with a
// result link; its items are the questions the site actually asked, with the option he chose.
const (
	// RememberedRuns is how many recent runs a conversation sees.
	RememberedRuns = 3
	// MaxRememberedItems bounds the questions kept for one run.
	MaxRememberedItems = 80
	MaxCommentRunes    = 1200
	maxSiteChars       = 253
)

// AnsweredItem is one question the site asked and the option Ningo chose.
type AnsweredItem struct {
	Question string `json:"question"`
	Answer   string `json:"answer"`
}

// Run is a finished questionnaire as Ningo remembers it.
type Run struct {
	Site       string         `json:"site"`
	Title      string         `json:"title,omitempty"`
	Label      string         `json:"label,omitempty"`
	URL        string         `json:"url"`
	Comment    string         `json:"comment,omitempty"`
	FinishedAt time.Time      `json:"finished_at"`
	Items      []AnsweredItem `json:"items,omitempty"`
}

// ParseAsked reads a runner's optional list of the question ids the site actually showed. Absent
// means unknown and is not an error; anything else must be distinct ids of this quiz.
func ParseAsked(value any, quiz Quiz) ([]string, error) {
	if value == nil {
		return nil, nil
	}
	raw, ok := value.([]any)
	if !ok || len(raw) > MaxQuestions {
		return nil, fmt.Errorf("asked must be a list of at most %d question ids", MaxQuestions)
	}
	known := map[string]bool{}
	for _, question := range quiz.Questions {
		known[question.ID] = true
	}
	seen := map[string]bool{}
	asked := make([]string, 0, len(raw))
	for _, item := range raw {
		id, _ := item.(string)
		if !known[id] || seen[id] {
			return nil, fmt.Errorf("asked must list distinct ids of the quiz's questions")
		}
		seen[id] = true
		asked = append(asked, id)
	}
	return asked, nil
}

// NewRun is what is kept of a finished run: the result, Ningo's comment, and the asked questions
// with his answers, in the order the site showed them.
func NewRun(quizLink string, quiz Quiz, result Result, answers map[string]string, asked []string, comment string, finishedAt time.Time) Run {
	link, _ := ParseLink(quizLink)
	site := ""
	if link != "" {
		site = hostOf(link)
	}
	run := Run{
		Site: site, Title: quiz.Title, Label: result.Label, URL: result.URL,
		Comment: clip(singleLine(comment), MaxCommentRunes), FinishedAt: finishedAt.UTC(),
	}
	byID := map[string]Question{}
	for _, question := range quiz.Questions {
		byID[question.ID] = question
	}
	for _, id := range asked {
		if len(run.Items) == MaxRememberedItems {
			break
		}
		if answer, ok := answers[id]; ok {
			run.Items = append(run.Items, AnsweredItem{Question: byID[id].Text, Answer: answer})
		}
	}
	return run
}

// ValidateRun checks a run read back from storage, which is as untrusted as the site it came from.
func ValidateRun(run Run) error {
	if run.Site == "" || len(run.Site) > maxSiteChars || run.FinishedAt.IsZero() {
		return fmt.Errorf("remembered run needs a site and a finish time")
	}
	if _, err := ParseResult("https://"+run.Site+"/", map[string]any{"url": run.URL}); err != nil {
		return fmt.Errorf("remembered run has an invalid result link")
	}
	if utf8.RuneCountInString(run.Title) > MaxTitleRunes || utf8.RuneCountInString(run.Label) > MaxLabelRunes ||
		utf8.RuneCountInString(run.Comment) > MaxCommentRunes || len(run.Items) > MaxRememberedItems {
		return fmt.Errorf("remembered run exceeds its bounds")
	}
	for _, item := range run.Items {
		if item.Question == "" || utf8.RuneCountInString(item.Question) > MaxTextRunes ||
			item.Answer == "" || utf8.RuneCountInString(item.Answer) > MaxOptionRunes {
			return fmt.Errorf("remembered run has an invalid item")
		}
	}
	return nil
}

// MemoryData renders the recent runs, newest first, as the JSON a conversation receives. Only the
// newest run carries its answers: it is the one people ask about, and older ones stay short.
// withAnswers false leaves every run's answers out, for voice turns that must stay small.
func MemoryData(runs []Run, withAnswers bool) string {
	if len(runs) == 0 {
		return ""
	}
	if len(runs) > RememberedRuns {
		runs = runs[:RememberedRuns]
	}
	type view struct {
		Site     string         `json:"site"`
		Title    string         `json:"title,omitempty"`
		Date     string         `json:"date"`
		Label    string         `json:"label,omitempty"`
		URL      string         `json:"result_link"`
		Comment  string         `json:"your_comment,omitempty"`
		Answered int            `json:"questions_asked,omitempty"`
		Items    []AnsweredItem `json:"your_answers,omitempty"`
	}
	views := make([]view, 0, len(runs))
	for index, run := range runs {
		v := view{Site: run.Site, Title: run.Title, Date: run.FinishedAt.Format("2006-01-02"), Label: run.Label,
			URL: run.URL, Comment: run.Comment, Answered: len(run.Items)}
		if index == 0 && withAnswers {
			v.Items = run.Items
		}
		views = append(views, v)
	}
	encoded, _ := json.Marshal(views)
	return string(encoded)
}

func hostOf(link string) string {
	parsed, _ := ParseLink(link)
	if parsed == "" {
		return ""
	}
	start := len("https://")
	if len(parsed) > 7 && parsed[:7] == "http://" {
		start = len("http://")
	}
	rest := parsed[start:]
	for i, r := range rest {
		if r == '/' || r == '?' || r == '#' || r == ':' {
			return rest[:i]
		}
	}
	return rest
}
