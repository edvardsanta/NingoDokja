package chat

import (
	"net/url"
	"path"
	"regexp"
	"sort"
	"strings"
	"unicode"
)

const MaxWebLinks = 4
const MaxWebPassages = 8
const ActionConverseLinks = "converse-links"
const MaxWebDocumentBytes = 2 << 20

// WebAttachment is platform metadata, never file bytes, a path or instructions.
type WebAttachment struct {
	URL         string `json:"url"`
	Filename    string `json:"filename"`
	ContentType string `json:"content_type"`
	Size        int64  `json:"size"`
}

type WebReadingRequest struct{ URL, Error string }

// PlanWebReadings shares the existing per-turn link budget with PDF attachments.
func PlanWebReadings(message string, attachments []WebAttachment) []WebReadingRequest {
	var requests []WebReadingRequest
	seen := map[string]bool{}
	for _, link := range WebLinks(message) {
		requests = append(requests, WebReadingRequest{URL: link})
		seen[link] = true
	}
	for _, attachment := range attachments {
		if seen[attachment.URL] {
			continue
		}
		if !strings.EqualFold(path.Ext(attachment.Filename), ".pdf") && !strings.EqualFold(attachment.ContentType, "application/pdf") {
			continue
		}
		request := WebReadingRequest{URL: attachment.URL}
		u, err := url.Parse(attachment.URL)
		if err != nil || len(attachment.URL) > 2048 || u.Hostname() == "" || u.User != nil || (u.Scheme != "https" && u.Scheme != "http") {
			request.Error = "unreadable"
		} else if attachment.Size < 0 || attachment.Size > MaxWebDocumentBytes {
			request.Error = "too_large"
		}
		seen[attachment.URL] = true
		requests = append(requests, request)
	}
	return requests
}

var webLinkPattern = regexp.MustCompile(`https?://[^\s<>]+`)
var requestedReadingPage = regexp.MustCompile(`(?i)\b(?:pages?|p[aá]ginas?|p[aá]g\.?)\s+([1-9][0-9]{0,4})\b`)

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
	wantedPages := map[string]bool{}
	for _, match := range requestedReadingPage.FindAllStringSubmatch(question, -1) {
		wantedPages["page "+match[1]] = true
	}
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
			if wantedPages[strings.ToLower(p.Label)] {
				score += 1000
			}
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
			document.Partial = document.Partial || len(document.Passages) < len(all)
			out = append(out, document)
		}
	}
	return out
}
