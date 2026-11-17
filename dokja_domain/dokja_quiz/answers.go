package quiz

import (
	"encoding/json"
	"fmt"
	promptfiles "read_books/dokja_store/prompts"
	"strings"
	"unicode"
)

// BatchSize is how many questions one model call answers. A whole pool of a few hundred items
// takes a handful of calls; a batch stays small enough to answer as one JSON object.
const BatchSize = 60

// Batches splits a quiz into the model calls that answer it, in order.
func Batches(questions []Question, size int) [][]Question {
	if size <= 0 {
		size = BatchSize
	}
	var batches [][]Question
	for start := 0; start < len(questions); start += size {
		end := min(start+size, len(questions))
		batches = append(batches, questions[start:end])
	}
	return batches
}

// AnswerInstructions tells Ningo how to answer a batch. The questions follow as data.
func AnswerInstructions() string { return promptfiles.MustRead("quiz_answer_instructions") }

// BatchPrompt is the user message for one batch: the instructions, then the items as JSON.
func BatchPrompt(batch []Question) string {
	items, _ := json.Marshal(batch)
	return fmt.Sprintf("%s\n\nItems (JSON data):\n%s", AnswerInstructions(), items)
}

// ParseAnswers reads the model's reply for a batch. An answer counts only when its option is one
// of the question's own, compared without case or spacing differences; the canonical option text is
// kept so the runner can click it. It returns the answers and the ids still unanswered.
func ParseAnswers(reply string, batch []Question) (map[string]string, []string) {
	answers := map[string]string{}
	var parsed struct {
		Answers []struct {
			ID     string `json:"id"`
			Option string `json:"option"`
		} `json:"answers"`
	}
	if object := jsonObject(reply); object != "" {
		_ = json.Unmarshal([]byte(object), &parsed)
	}
	byID := map[string]Question{}
	for _, question := range batch {
		byID[question.ID] = question
	}
	for _, answer := range parsed.Answers {
		question, ok := byID[answer.ID]
		if !ok {
			continue
		}
		if option, ok := matchOption(question, answer.Option); ok {
			answers[answer.ID] = option
		}
	}
	var missing []string
	for _, question := range batch {
		if _, ok := answers[question.ID]; !ok {
			missing = append(missing, question.ID)
		}
	}
	return answers, missing
}

// Pending keeps the questions of a batch that are still unanswered, for one retry.
func Pending(batch []Question, missing []string) []Question {
	wanted := map[string]bool{}
	for _, id := range missing {
		wanted[id] = true
	}
	var pending []Question
	for _, question := range batch {
		if wanted[question.ID] {
			pending = append(pending, question)
		}
	}
	return pending
}

func matchOption(question Question, chosen string) (string, bool) {
	want := normalize(chosen)
	if want == "" {
		return "", false
	}
	for _, option := range question.Options {
		if normalize(option) == want {
			return option, true
		}
	}
	return "", false
}

// normalize compares option texts the way a person reads them: case, spacing and surrounding
// punctuation do not matter.
func normalize(s string) string {
	s = strings.ToLower(singleLine(s))
	return strings.TrimFunc(s, func(r rune) bool { return unicode.IsPunct(r) || unicode.IsSpace(r) })
}

// jsonObject finds the reply's JSON object, tolerating a code fence or a stray sentence around it.
func jsonObject(reply string) string {
	start := strings.Index(reply, "{")
	end := strings.LastIndex(reply, "}")
	if start < 0 || end <= start {
		return ""
	}
	return reply[start : end+1]
}

// CommentPrompt asks Ningo for a short in-character reaction to the site's verdict. The label is
// the site's text, quoted as data.
func CommentPrompt(result Result, language string) string {
	label := result.Label
	if label == "" {
		label = promptfiles.MustRead("quiz_empty_verdict")
	}
	encoded, _ := json.Marshal(label)
	return fmt.Sprintf(promptfiles.MustRead("answers_commentprompt1"), encoded, language)
}

// Refusal maps a runner's refusal to a fixed code; anything unknown is a failure.
func Refusal(reason string) string {
	switch reason {
	case RefusedUnsupported, RefusedUnavailable, RefusedUnanswered:
		return reason
	}
	return RefusedFailed
}
