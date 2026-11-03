package chat

import (
	"strings"
	"testing"
)

func TestWebLinksRecognizesMultipleLinksAndKeepsFragments(t *testing.T) {
	links := WebLinks("Read <https://example.com/news> and [the law](https://example.com/law#art140). https://example.com/news")
	if len(links) != 2 || links[1] != "https://example.com/law#art140" {
		t.Fatalf("%#v", links)
	}
	if got := WebLinks("file:///etc/passwd https://user:secret@example.com/ https://"); len(got) != 0 {
		t.Fatalf("%#v", got)
	}
}
func TestWebSelectionBalancesPagesAndFindsFollowupPassages(t *testing.T) {
	docs := []WebDocument{{URL: "https://example.com/a", Passages: []WebPassage{{Text: "Opening"}, {Text: "Calculation assumptions are explicit."}}}, {URL: "https://example.com/b", Passages: []WebPassage{{Text: "Other page"}}}}
	for i := 0; i < 20; i++ {
		docs[0].Passages = append(docs[0].Passages, WebPassage{Text: "Other filler"})
	}
	selected := SelectWebPassages(docs, "calculation assumptions")
	if len(selected) != 2 {
		t.Fatalf("lost page: %#v", selected)
	}
	count := 0
	found := false
	for _, d := range selected {
		for _, p := range d.Passages {
			count++
			found = found || strings.Contains(p.Text, "assumptions")
		}
	}
	if count != MaxWebPassages || !found {
		t.Fatalf("count=%d found=%t", count, found)
	}
}
