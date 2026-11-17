package chat

import "testing"

func TestNeedsReasoning(t *testing.T) {
	for _, tc := range []struct {
		name string
		turn ReasoningTurn
		want bool
	}{
		{"arithmetic", ReasoningTurn{Message: "12 * 7 + 4?"}, true},
		{"math question", ReasoningTurn{Message: "Calculate the probability of drawing two red balls"}, true},
		{"multiple sources", ReasoningTurn{Message: "What follows from these sources?", SourceCount: 2}, true},
		{"multiple links", ReasoningTurn{Message: "https://example.com/a https://example.com/b", SourceCount: 2}, true},
		{"comparison", ReasoningTurn{Message: "Compare these two proposed explanations"}, true},
		{"verification", ReasoningTurn{Message: "Verify whether this claim follows"}, true},
		{"kept reading", ReasoningTurn{Message: "What does the article conclude?", SourceCount: 1, ReadingRelevant: true}, true},
		{"short reading followup", ReasoningTurn{Message: "Why those assumptions?", SourceCount: 1, ReadingRelevant: true}, true},
		{"owner research", ReasoningTurn{Message: "Research the causes of this dispute", Owner: true}, true},
		{"other research", ReasoningTurn{Message: "Research the causes of this dispute"}, false},
		{"preview", ReasoningTurn{Message: "12 * 7", Preview: true}, false},
		{"voice", ReasoningTurn{Message: "12 * 7", Voice: true}, false},
		{"empty", ReasoningTurn{}, false},
		{"noise", ReasoningTurn{Message: "AAAAAAAAAAAAAAA", SourceCount: 2, ReadingRelevant: true}, false},
		{"repeated noise", ReasoningTurn{Message: "AAAA AAAA AAAA AAAA", SourceCount: 2, ReadingRelevant: true}, false},
		{"laughter", ReasoningTurn{Message: "haha haha haha haha", SourceCount: 2, ReadingRelevant: true}, false},
		{"URL is not arithmetic", ReasoningTurn{Message: "https://example.com/12-34"}, false},
		{"keyboard noise", ReasoningTurn{Message: "asdfghjkl"}, false},
		{"banter", ReasoningTurn{Message: "Hello!", SourceCount: 2, ReadingRelevant: true}, false},
		{"short", ReasoningTurn{Message: "verify this"}, false},
		{"short reading without evidence", ReasoningTurn{Message: "the article", ReadingRelevant: true}, false},
		{"ordinary conversation", ReasoningTurn{Message: "Tell me a funny story about a cat"}, false},
		// Portuguese fixtures exercise the language of the actual chat interface.
		{"Portuguese comparison", ReasoningTurn{Message: "compare as duas explicações propostas"}, true},
		{"Portuguese research", ReasoningTurn{Message: "pesquise as causas deste conflito", Owner: true}, true},
		{"Portuguese banter", ReasoningTurn{Message: "bom dia", SourceCount: 2}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := NeedsReasoning(tc.turn); got != tc.want {
				t.Fatalf("NeedsReasoning = %v, want %v", got, tc.want)
			}
		})
	}
}
