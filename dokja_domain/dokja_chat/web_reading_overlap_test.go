package chat

import "testing"

func TestReadingOverlap(t *testing.T) {
	docs := []WebDocument{{Title: "Fortnite Token Odds Calculator", Passages: []WebPassage{{Text: "One in 137,500 per chest for the Pirate Shop Access Token."}}}}
	cases := []struct {
		question string
		want     bool
	}{
		{"AAAAAAAAAAAAAAAA", false},
		{"gay", false},
		{"jo", false},
		{"burger", false},
		{"PARA DE FALAR DE ASSUNTO!!!", false},
		{"", false},
		{"qual a chance por chest?", true},
		{"resume o link", true},
		{"what does the article say?", true},
		{"https://example.com/other", false},
	}
	for _, c := range cases {
		if got := ReadingOverlap(c.question, docs); got != c.want {
			t.Errorf("ReadingOverlap(%q) = %v, want %v", c.question, got, c.want)
		}
	}
}
