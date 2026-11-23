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

func TestAnExplicitResearchAskSearchesEvenWithoutReadings(t *testing.T) {
	query, ok := WebSearchPlan(nil, "ningo, pesquisa para mim livros sobre sound design")
	if !ok || query != "livros sound design" {
		t.Fatalf("query=%q ok=%v", query, ok)
	}
	query, ok = WebSearchPlan(nil, "search for quantum thermodynamics books")
	if !ok || query != "quantum thermodynamics books" {
		t.Fatalf("query=%q ok=%v", query, ok)
	}
}

func TestAPlainQuestionWithoutReadingsStillDoesNotSearch(t *testing.T) {
	for _, question := range []string{"me fale do kian", "o que voce acha do lula", "quanto custa um ssd"} {
		if q, ok := WebSearchPlan(nil, question); ok {
			t.Fatalf("%q searched %q without an explicit ask", question, q)
		}
	}
}

func TestAnExplicitResearchAskSearchesEvenWhenTheReadingCoversIt(t *testing.T) {
	query, ok := WebSearchPlan(shopPage(), "pesquisa o espreitador")
	if !ok || query != "espreitador" {
		t.Fatalf("an explicit ask must search with its own words only: query=%q ok=%v", query, ok)
	}
}

func TestAnExplicitResearchAskNeverBorrowsTheReadingsTitle(t *testing.T) {
	query, ok := WebSearchPlan(shopPage(), "Pesquise o mistfall hunters")
	if !ok || query != "mistfall hunters" {
		t.Fatalf("query=%q ok=%v", query, ok)
	}
}
