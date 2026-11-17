package chat

import "testing"

func shopPage() []WebDocument {
	return []WebDocument{{Title: "Ordem Paranormal — O Espreitador e Outras Histórias", Passages: []WebPassage{
		{Label: "page", Text: "Livro oficial de Ordem Paranormal. Preço, autores e ficha técnica."},
	}}}
}

func TestSearchesWhenTheReadingNeverMentionsWhatWasAsked(t *testing.T) {
	query, ok := WebSearchPlan(shopPage(), "me fale do kian")
	if !ok || query != "kian Ordem Paranormal — O Espreitador e Outras Histórias" {
		t.Fatalf("query=%q ok=%v", query, ok)
	}
}

func TestDoesNotSearchWhenTheReadingCoversTheQuestion(t *testing.T) {
	if q, ok := WebSearchPlan(shopPage(), "quem são os autores?"); ok {
		t.Fatalf("the page mentions autores, searched %q", q)
	}
	if q, ok := WebSearchPlan(shopPage(), "fala do espreitador"); ok {
		t.Fatalf("the title mentions the subject, searched %q", q)
	}
}

func TestDoesNotSearchForAQuestionAboutTheReadingItself(t *testing.T) {
	for _, question := range []string{"resume isso", "explique essa página", "https://example.com/x fale sobre isso"} {
		if q, ok := WebSearchPlan(shopPage(), question); ok {
			t.Fatalf("%q searched %q", question, q)
		}
	}
	if _, ok := WebSearchPlan(nil, "me fale do kian"); ok {
		t.Fatal("without a reading there is no context to search from")
	}
}
