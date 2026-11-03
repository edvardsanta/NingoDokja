package chat

import (
	"net/url"
	"regexp"
	"sort"
	"strings"
	"unicode"
)

const MaxWebLinks = 4
const MaxWebPassages = 8
const ActionConverseLinks = "converse-links"

var webLinkPattern = regexp.MustCompile(`https?://[^\s<>]+`)

// WebLinks recognizes raw, Discord-wrapped and Markdown links without treating
// any page-supplied address as a new request. Fragments remain part of identity.
func WebLinks(message string) []string {
	var links []string
	seen := map[string]bool{}
	for _, raw := range webLinkPattern.FindAllString(message, -1) {
		raw = strings.TrimRight(raw, ".,;!?\"'")
		for strings.HasSuffix(raw, ")") && strings.Count(raw, ")") > strings.Count(raw, "(") {
			raw = strings.TrimSuffix(raw, ")")
		}
		u, err := url.Parse(raw)
		if err != nil || len(raw) > 2048 || u.Hostname() == "" || u.User != nil || seen[raw] {
			continue
		}
		seen[raw] = true
		links = append(links, raw)
	}
	return links
}

type WebPassage struct {
	Text  string `json:"text"`
	Label string `json:"label"`
	URL   string `json:"url"`
}

type WebDocument struct {
	URL         string       `json:"url"`
	Title       string       `json:"title"`
	Published   string       `json:"published"`
	RetrievedAt string       `json:"retrieved_at"`
	Partial     bool         `json:"partial"`
	Passages    []WebPassage `json:"passages"`
}

// SelectWebPassages balances the pages before ranking extra passages by the
// current question. It can revisit text not included in the preceding reply.
func SelectWebPassages(documents []WebDocument, question string) []WebDocument {
	words := strings.FieldsFunc(strings.ToLower(question), func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) })
	type candidate struct{ doc, index, score int }
	var rest []candidate
	selected := map[int]map[int]bool{}
	budget := MaxWebPassages
	for d, document := range documents {
		if len(document.Passages) == 0 {
			continue
		}
		best := 0
		bestScore := -1
		for i, p := range document.Passages {
			score := 0
			hay := strings.ToLower(p.Label + " " + p.Text)
			for _, word := range words {
				if len([]rune(word)) >= 4 && strings.Contains(hay, word) {
					score++
				}
			}
			rest = append(rest, candidate{d, i, score})
			if score > bestScore {
				best, bestScore = i, score
			}
		}
		selected[d] = map[int]bool{best: true}
		budget--
	}
	sort.SliceStable(rest, func(i, j int) bool { return rest[i].score > rest[j].score })
	for _, c := range rest {
		if budget <= 0 {
			break
		}
		if selected[c.doc][c.index] {
			continue
		}
		selected[c.doc][c.index] = true
		budget--
	}
	var out []WebDocument
	for d, document := range documents {
		all := document.Passages
		document.Passages = nil
		for i, p := range all {
			if selected[d][i] {
				document.Passages = append(document.Passages, p)
			}
		}
		if len(document.Passages) > 0 {
			out = append(out, document)
		}
	}
	return out
}
