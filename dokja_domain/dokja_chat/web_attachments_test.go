package chat

import (
	"fmt"
	"strings"
	"testing"
)

func TestPDFReadingPlanUsesTheLinkBudgetAndRejectsInvalidMetadata(t *testing.T) {
	attachments := []WebAttachment{
		{URL: "https://example.com/a.pdf", Filename: "a.pdf", Size: 1600000},
		{URL: "https://example.com/large.pdf", Filename: "large.pdf", Size: MaxWebDocumentBytes + 1},
		{URL: "file:///etc/passwd", Filename: "fake.pdf"},
		{URL: "https://example.com/other", Filename: "file", ContentType: "application/pdf"},
		{URL: "https://example.com/photo.png", Filename: "photo.png"},
	}
	requests := PlanWebReadings("https://example.com/a.pdf", attachments)
	if len(requests) != 4 || requests[0].URL != "https://example.com/a.pdf" || requests[1].Error != "too_large" || requests[2].Error != "unreadable" || requests[3].Error != "" {
		t.Fatal(requests)
	}
	requests = PlanWebReadings("", []WebAttachment{{URL: strings.Repeat("x", 2049), Filename: "x.pdf"}})
	if len(requests) != 1 || requests[0].Error != "unreadable" {
		t.Fatal("unbounded attachment address")
	}
}

func TestPDFFollowupCanSelectAnOriginalPageBeyondTheFirstReply(t *testing.T) {
	document := WebDocument{URL: "https://example.com/file.pdf"}
	for i := 1; i <= 30; i++ {
		document.Passages = append(document.Passages, WebPassage{Label: fmt.Sprintf("Page %d", i), Text: "A page of text.", URL: fmt.Sprintf("https://example.com/file.pdf#page=%d", i)})
	}
	for _, question := range []string{"What does page 29 say?", "O que diz a página 29?"} {
		selected := SelectWebPassages([]WebDocument{document}, question)
		found := false
		for _, p := range selected[0].Passages {
			if p.Label == "Page 29" {
				found = true
			}
		}
		if !found || !selected[0].Partial || len(selected[0].Passages) > MaxWebPassages {
			t.Fatal("requested page omitted or partial reading hidden", selected)
		}
	}
}
