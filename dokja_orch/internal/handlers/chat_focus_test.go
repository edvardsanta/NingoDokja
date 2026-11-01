package handlers

import (
	"context"
	"errors"
	"read_books/internal/core"
	"strings"
	"testing"
)

const bookPassage = "Basta ser mulher para baixar-me as asas, e digo isso sem\nqueixa alguma do Senhor."

// documentBase is a knowledge base with one book in it.
type documentBase struct {
	searches []string
	scoped   []string
	hits     []any
	err      error
}

func (b *documentBase) Search(_ context.Context, query string, _ int, _ float64) (map[string]any, error) {
	b.searches = append(b.searches, query)
	return map[string]any{"hits": b.hits}, b.err
}

func (b *documentBase) SearchDocument(_ context.Context, query, sourceID string, _ int) (map[string]any, error) {
	b.scoped = append(b.scoped, sourceID+"|"+query)
	return map[string]any{"hits": b.hits}, b.err
}

func bookHit(relevant bool) map[string]any {
	return map[string]any{"text": bookPassage, "source_id": "file:book", "title": "The book", "relevant": relevant}
}

func focusHandler(base *documentBase) *ChatDomainHandler {
	return NewChatDomainHandler(nil).WithKnowledge(base, nil)
}

func focusedTurn(handler *ChatDomainHandler, content string) knowledgeTurn {
	event := core.Event{Context: map[string]any{"session_key": "conversation"}}
	return handler.planKnowledgeTurn(event, content, false)
}

func citedBook(handler *ChatDomainHandler, question string) {
	turn := focusedTurn(handler, question)
	handler.focus.remember(turn, question, []knowledgeSource{{Number: 1, SourceID: "file:book#part1", Title: "The book"}})
}

func TestAFollowUpSearchesTheDocumentACitedReplyWasAbout(t *testing.T) {
	base := &documentBase{hits: []any{bookHit(true)}}
	handler := focusHandler(base)
	citedBook(handler, "what am I missing about the book of life")

	// Shorter than a general search accepts: only the focus gives it a subject.
	sources := handler.lookupForTurn(context.Background(), "visões", "", focusedTurn(handler, "visões"))

	if len(sources) != 1 || len(base.searches) != 0 || len(base.scoped) != 1 {
		t.Fatalf("sources=%v searches=%v scoped=%v", sources, base.searches, base.scoped)
	}
	if !strings.HasPrefix(base.scoped[0], "file:book|visões\nwhat am I missing") {
		t.Fatalf("scoped query: %q", base.scoped[0])
	}
}

func TestAShortAnswerContinuesARequestForText(t *testing.T) {
	handler := focusHandler(&documentBase{})
	citedBook(handler, "can you get me 5 quotes")
	if turn := focusedTurn(handler, "visões"); !turn.text {
		t.Fatal("the topic answers the request for quotes")
	}
	handler.focus.remember(focusedTurn(handler, "and what do you think of all that, honestly, after everything we have read together?"), "x", nil)
	if turn := focusedTurn(handler, "visões"); turn.text {
		t.Fatal("a turn away from the text ends the request")
	}
}

func TestAQuestionAboutTheTextTakesHitsInTheDocumentWhateverTheirScore(t *testing.T) {
	base := &documentBase{hits: []any{bookHit(false)}}
	handler := focusHandler(base)
	citedBook(handler, "the book")
	sources := handler.lookupForTurn(context.Background(), "me dá 5 trechos", "", focusedTurn(handler, "me dá 5 trechos"))
	if len(sources) != 1 || !sources[0].AskedForText {
		t.Fatalf("sources=%+v", sources)
	}
	if !strings.Contains(knowledgeReference(sources), "Quote word for word") {
		t.Fatal("a question about the text gets the text-first brief")
	}
}

func TestAConversationThatMovedOnGoesBackToSearchingEverything(t *testing.T) {
	base := &documentBase{hits: []any{bookHit(false)}}
	handler := focusHandler(base)
	citedBook(handler, "the book")
	sources := handler.lookupForTurn(context.Background(), "what about cooking rice properly", "", focusedTurn(handler, "what about cooking rice properly"))
	if len(sources) != 0 || len(base.searches) != 1 {
		t.Fatalf("sources=%+v searches=%v", sources, base.searches)
	}
}

func TestTheFocusEndsAfterRepliesWithoutTheDocument(t *testing.T) {
	handler := focusHandler(&documentBase{})
	citedBook(handler, "the book")
	for n := 0; n < knowledgeFocusIdleTurns; n++ {
		handler.focus.remember(focusedTurn(handler, "something else"), "something else", nil)
	}
	if turn := focusedTurn(handler, "something else"); turn.focus != nil {
		t.Fatalf("focus: %+v", turn.focus)
	}
}

func TestTheFocusIsTheWholeDocumentAndKeepsItsSubject(t *testing.T) {
	handler := focusHandler(&documentBase{})
	citedBook(handler, "the first question")
	citedBook(handler, "a follow-up")
	focus := focusedTurn(handler, "x").focus
	if focus == nil || focus.SourceID != "file:book" || focus.Subject != "the first question" {
		t.Fatalf("focus: %+v", focus)
	}
}

func TestASpokenTurnHasNoFocus(t *testing.T) {
	handler := focusHandler(&documentBase{})
	citedBook(handler, "the book")
	event := core.Event{Context: map[string]any{"session_key": "conversation"}}
	if turn := handler.planKnowledgeTurn(event, "cite the book", true); turn.focus != nil || turn.text {
		t.Fatalf("turn: %+v", turn)
	}
}

func textTurn() knowledgeTurn { return knowledgeTurn{text: true, planned: true} }

func TestATrueQuoteStandsAcrossALineBreak(t *testing.T) {
	sources := []knowledgeSource{{SourceID: "file:book", Text: bookPassage}}
	reply := `Ela diz: “baixar-me as asas, e digo isso sem queixa” [1].`
	if got := focusHandler(&documentBase{}).verifyQuotes(context.Background(), reply, sources, textTurn()); got != reply {
		t.Fatalf("got %q", got)
	}
}

func TestAnInventedQuoteIsTakenOut(t *testing.T) {
	sources := []knowledgeSource{{SourceID: "file:book", Text: bookPassage}}
	reply := `Ela diz: “vi um anjo com uma lança de ouro” [1].`
	got := focusHandler(&documentBase{}).verifyQuotes(context.Background(), reply, sources, textTurn())
	if want := `Ela diz: “[…]” [1].`; got != want {
		t.Fatalf("got %q", got)
	}
}

func TestAQuoteFromAnEarlierTurnIsFoundInTheDocument(t *testing.T) {
	base := &documentBase{hits: []any{bookHit(true)}}
	sources := []knowledgeSource{{SourceID: "file:book#part1", Text: "Another passage entirely."}}
	reply := `Ela diz: "baixar-me as asas, e digo isso" [1].`
	if got := focusHandler(base).verifyQuotes(context.Background(), reply, sources, textTurn()); got != reply {
		t.Fatalf("got %q", got)
	}
	if len(base.scoped) != 1 || !strings.HasPrefix(base.scoped[0], "file:book|") {
		t.Fatalf("scoped: %v", base.scoped)
	}
}

func TestEachSideOfAnEllipsisMustBeInTheText(t *testing.T) {
	sources := []knowledgeSource{{SourceID: "file:book", Text: bookPassage}}
	handler := focusHandler(&documentBase{})
	reply := `“Basta ser mulher […] sem queixa alguma” [1]`
	if got := handler.verifyQuotes(context.Background(), reply, sources, textTurn()); got != reply {
		t.Fatalf("got %q", got)
	}
	if got := handler.verifyQuotes(context.Background(), `“Basta ser mulher […] uma lança de ouro” [1]`, sources, textTurn()); got != `“[…]” [1]` {
		t.Fatalf("got %q", got)
	}
}

func TestQuotesAreOnlyCheckedWhenTheTurnIsAboutTheText(t *testing.T) {
	sources := []knowledgeSource{{SourceID: "file:book", Text: bookPassage}}
	reply := `Como você disse, “isso não faz sentido nenhum aqui” [1].`
	if got := focusHandler(&documentBase{}).verifyQuotes(context.Background(), reply, sources, knowledgeTurn{planned: true}); got != reply {
		t.Fatalf("got %q", got)
	}
}

func TestShortQuotesAreLeftAlone(t *testing.T) {
	sources := []knowledgeSource{{SourceID: "file:book", Text: bookPassage}}
	reply := `Ela diz “queime logo” [1].`
	if got := focusHandler(&documentBase{}).verifyQuotes(context.Background(), reply, sources, textTurn()); got != reply {
		t.Fatalf("got %q", got)
	}
}

func TestAQuoteThatCannotBeCheckedIsNotShown(t *testing.T) {
	base := &documentBase{err: errors.New("down")}
	sources := []knowledgeSource{{SourceID: "file:book", Text: "Another passage entirely."}}
	got := focusHandler(base).verifyQuotes(context.Background(), `“uma frase qualquer de quatro palavras” [1]`, sources, textTurn())
	if got != `“[…]” [1]` {
		t.Fatalf("got %q", got)
	}
}
