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

func TestQuestionPlanSearchesOnAnExplicitResearchRequest(t *testing.T) {
	for _, test := range []struct {
		question string
		query    string
	}{
		{"pesquise termodinâmica quântica", "termodinâmica quântica"},
		{"pesquise algo relacionado a modelagem estatística", "modelagem estatística"},
		{"busque modelagem e inferência estatística", "modelagem inferência estatística"},
		{"nao quero banca, so quero que pesquise algo relacionado", "banca"},
		{"search for bayesian inference", "bayesian inference"},
	} {
		query, ok := WebSearchQuestionPlan(test.question)
		if !ok || query != test.query {
			t.Fatalf("%q: query=%q ok=%v, want %q", test.question, query, ok, test.query)
		}
	}
}

func TestQuestionPlanIgnoresQuestionsWithoutResearchIntent(t *testing.T) {
	for _, question := range []string{
		"me fale do kian",
		"o que é entropia",
		"qual a busca mais usada", // "busca" names a thing, not a request here — no subject intent
		"pesquise aí",
		"pesquise sobre",
	} {
		if query, ok := WebSearchQuestionPlan(question); ok {
			t.Fatalf("%q searched %q", question, query)
		}
	}
}
