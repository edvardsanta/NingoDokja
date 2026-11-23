package handlers

import (
	"context"
	"fmt"
	chat "read_books/dokja_domain/dokja_chat"
	"strings"
	"testing"
)

// shopService answers a shop page that never mentions the character, and a search that does.
type shopService struct {
	searches   []string
	searchFail bool
}

func (s *shopService) Dispatch(_ context.Context, kind string, payload map[string]any) (map[string]any, error) {
	switch kind {
	case "feeds.web.read":
		address := payload["url"].(string)
		return map[string]any{"url": address, "title": "Ordem Paranormal — O Espreitador", "retrieved_at": "2026-11-03T12:00:00Z",
			"passages": []chat.WebPassage{{Text: "Livro oficial. Preço, autores e ficha técnica.", Label: "page", URL: address}}}, nil
	case "feeds.web.search":
		s.searches = append(s.searches, payload["query"].(string))
		if s.searchFail {
			return nil, fmt.Errorf("web_timeout: search instance did not answer")
		}
		return map[string]any{"retrieved_at": "2026-11-03T12:00:01Z", "results": []map[string]any{
			{"title": "Kian", "url": "https://example.com/kian", "snippet": "Kian is a character of the franchise."},
		}}, nil
	}
	return nil, fmt.Errorf("unexpected event %s", kind)
}

func evidenceSent(generator *fakeChatGenerator) string {
	var sent []string
	for _, m := range generator.messages {
		if strings.Contains(m["content"], "UNTRUSTED_WEB_EVIDENCE") || m["role"] == "user" {
			sent = append(sent, m["content"])
		}
	}
	return strings.Join(sent, "\n")
}

func TestAFollowUpTheShopPageCannotAnswerSearchesTheWebOnce(t *testing.T) {
	service := &shopService{}
	generator := &fakeChatGenerator{result: map[string]any{"reply": "ok"}}
	h := NewChatDomainHandler(&triagingGenerator{triageModel: generator, verdict: true}).WithWebReading(service, nil)
	if _, err := h.Handle(context.Background(), chatEvent("https://example.com/book"), chatStep); err != nil {
		t.Fatal(err)
	}
	if len(service.searches) != 0 {
		t.Fatalf("a bare link must not search: %v", service.searches)
	}
	if _, err := h.Handle(context.Background(), chatEvent("me fale do kian"), chatStep); err != nil {
		t.Fatal(err)
	}
	if len(service.searches) != 1 || !strings.HasPrefix(service.searches[0], "kian Ordem Paranormal") {
		t.Fatalf("expected one search carrying the page's context: %v", service.searches)
	}
	if sent := evidenceSent(generator); !strings.Contains(sent, "Kian is a character") || !strings.Contains(sent, webSearchHeading) {
		t.Fatalf("the search snippet must reach the model labelled as a search result: %s", sent)
	}
	if _, err := h.Handle(context.Background(), chatEvent("e a Lívia?"), chatStep); err != nil {
		t.Fatal(err)
	}
	if len(service.searches) != 2 {
		t.Fatalf("a different query is a different search: %v", service.searches)
	}
	if _, err := h.Handle(context.Background(), chatEvent("me fale do kian"), chatStep); err != nil {
		t.Fatal(err)
	}
	if len(service.searches) != 2 {
		t.Fatalf("the same query must reuse the answer: %v", service.searches)
	}
}

func TestAFailedSearchLeavesTheReadingTurnAlone(t *testing.T) {
	service := &shopService{searchFail: true}
	generator := &fakeChatGenerator{result: map[string]any{"reply": "ok"}}
	h := NewChatDomainHandler(generator).WithWebReading(service, nil)
	if _, err := h.Handle(context.Background(), chatEvent("https://example.com/book"), chatStep); err != nil {
		t.Fatal(err)
	}
	if _, err := h.Handle(context.Background(), chatEvent("me fale do kian"), chatStep); err != nil {
		t.Fatalf("a search failure must not fail the turn: %v", err)
	}
	if strings.Contains(evidenceSent(generator), "Kian is a character") {
		t.Fatal("no search result exists to show")
	}
}

func TestAnExplicitResearchAskSearchesTheWebWithNothingReadYet(t *testing.T) {
	service := &shopService{}
	generator := &fakeChatGenerator{result: map[string]any{"reply": "ok"}}
	h := NewChatDomainHandler(generator).WithWebReading(service, nil)
	if _, err := h.Handle(context.Background(), chatEvent("pesquisa livros sobre sound design"), chatStep); err != nil {
		t.Fatal(err)
	}
	if len(service.searches) != 1 || service.searches[0] != "livros sound design" {
		t.Fatalf("an explicit research ask must search with its own content words: %v", service.searches)
	}
	if sent := evidenceSent(generator); !strings.Contains(sent, "Kian is a character") || !strings.Contains(sent, webSearchHeading) {
		t.Fatalf("the search snippet must reach the model labelled as a search result: %s", sent)
	}
}

func TestAPlainQuestionWithNothingReadDoesNotSearchTheWeb(t *testing.T) {
	service := &shopService{}
	generator := &fakeChatGenerator{result: map[string]any{"reply": "ok"}}
	h := NewChatDomainHandler(generator).WithWebReading(service, nil)
	if _, err := h.Handle(context.Background(), chatEvent("quais livros de sound design voce conhece?"), chatStep); err != nil {
		t.Fatal(err)
	}
	if len(service.searches) != 0 {
		t.Fatalf("a plain question must not search: %v", service.searches)
	}
}

func TestAnExplicitResearchAskSearchesDespiteAKeptReadingAndLeavesItOut(t *testing.T) {
	service := &shopService{}
	generator := &fakeChatGenerator{result: map[string]any{"reply": "ok"}}
	h := NewChatDomainHandler(&triagingGenerator{triageModel: generator, verdict: true}).WithWebReading(service, nil)
	if _, err := h.Handle(context.Background(), chatEvent("https://example.com/book"), chatStep); err != nil {
		t.Fatal(err)
	}
	if _, err := h.Handle(context.Background(), chatEvent("Pesquise o mistfall hunters"), chatStep); err != nil {
		t.Fatal(err)
	}
	if len(service.searches) != 1 || service.searches[0] != "mistfall hunters" {
		t.Fatalf("the ask must search its own subject, not the earlier reading's: %v", service.searches)
	}
	if sent := evidenceSent(generator); strings.Contains(sent, "Preço, autores e ficha técnica") {
		t.Fatalf("the earlier reading must stay out of an unrelated research ask: %s", sent)
	}
}

func TestAnExplicitResearchAskSearchesEvenWhenTheReadingCoversTheWords(t *testing.T) {
	service := &shopService{}
	generator := &fakeChatGenerator{result: map[string]any{"reply": "ok"}}
	h := NewChatDomainHandler(&triagingGenerator{triageModel: generator, verdict: true}).WithWebReading(service, nil)
	if _, err := h.Handle(context.Background(), chatEvent("https://example.com/book"), chatStep); err != nil {
		t.Fatal(err)
	}
	if _, err := h.Handle(context.Background(), chatEvent("pesquisa quem sao os autores do espreitador"), chatStep); err != nil {
		t.Fatal(err)
	}
	if len(service.searches) != 1 {
		t.Fatalf("an explicit ask searches even when a reading mentions the words: %v", service.searches)
	}
}
