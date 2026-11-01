package handlers

import (
	"context"
	"errors"
	"read_books/internal/core"
	"strings"
	"testing"
	"unicode/utf8"
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

const malthusReply = "Sem as leis dos pobres, tal conjunto de pessoas felizes seria maior."

// From a real conversation about Malthus: neither the question nor the first question of the
// conversation names the poor laws, so the search reached the book's birth tables and Ningo said
// the book did not hold the argument. Only its own previous reply had the words. The text is in
// Portuguese because that is the case being reproduced.
func TestAFollowUpSearchesWithWhatWasJustSaid(t *testing.T) {
	base := &documentBase{hits: []any{bookHit(true)}}
	handler := focusHandler(base)
	citedBook(handler, "Viu Ensaio sobre o Princípio da População?")

	question := "Como ele explica essa ideia do estimulo aos nascimentos?"
	turn := focusedTurn(handler, question)
	turn.previousReply = malthusReply
	handler.lookupForTurn(context.Background(), question, "", turn)

	if len(base.scoped) != 1 || !strings.Contains(base.scoped[0], question+"\nSem as leis dos pobres") {
		t.Fatalf("scoped query: %v", base.scoped)
	}
	if strings.Contains(base.scoped[0], "Viu Ensaio") {
		t.Fatalf("the old subject outweighed the reply: %q", base.scoped[0])
	}
}

// documentAfterRestart is a base where only the words of the previous reply find the book.
type documentAfterRestart struct{ documentBase }

func (b *documentAfterRestart) Search(_ context.Context, query string, _ int, _ float64) (map[string]any, error) {
	b.searches = append(b.searches, query)
	if strings.Contains(query, "leis dos pobres") {
		return map[string]any{"hits": []any{bookHit(true)}}, nil
	}
	return map[string]any{"hits": []any{}}, nil
}

// The same conversation, four seconds after a restart: the focus was gone, and without it
// "de fragmentos" found nothing, so the model got no passages and asked for them.
func TestAFollowUpFindsTheDocumentAgainAfterARestart(t *testing.T) {
	base := &documentAfterRestart{}
	handler := NewChatDomainHandler(nil).WithKnowledge(base, nil)
	turn := focusedTurn(handler, "de fragmentos")
	turn.previousReply = malthusReply

	sources := handler.lookupForTurn(context.Background(), "de fragmentos", "", turn)

	if len(sources) != 1 || !sources[0].AskedForText {
		t.Fatalf("sources=%v searches=%v", sources, base.searches)
	}
	if len(base.searches) != 3 || !strings.Contains(base.searches[2], "de fragmentos\nSem as leis dos pobres") {
		t.Fatalf("searches=%q", base.searches)
	}
	if turn.focus != nil {
		t.Fatal("the test needs a turn without a focus")
	}
}

func TestANewQuestionKeepsSearchingOnItsOwnWords(t *testing.T) {
	base := &documentAfterRestart{}
	handler := NewChatDomainHandler(nil).WithKnowledge(base, nil)
	question := strings.Repeat("uma pergunta nova sobre outro assunto que nada tem a ver ", 2)
	turn := focusedTurn(handler, question)
	turn.previousReply = malthusReply

	handler.lookupForTurn(context.Background(), question, "", turn)

	if len(base.searches) != 1 || strings.Contains(base.searches[0], "leis dos pobres") {
		t.Fatalf("searches=%q", base.searches)
	}
}

func TestAReadingAnchorIsNotReplacedByTheReply(t *testing.T) {
	base := &documentAfterRestart{}
	handler := NewChatDomainHandler(nil).WithKnowledge(base, nil)
	turn := focusedTurn(handler, "e isso?")
	turn.previousReply = malthusReply

	handler.lookupForTurn(context.Background(), "e isso?", "the reading", turn)

	for _, query := range base.searches {
		if strings.Contains(query, "leis dos pobres") {
			t.Fatalf("searches=%q", base.searches)
		}
	}
}

func TestPreviousReplyComesFromTheSessionAndIsBounded(t *testing.T) {
	history := []chatMessage{
		{Role: "assistant", Content: "older"},
		{Role: "user", Content: "question"},
		{Role: "assistant", Content: " " + strings.Repeat("é", knowledgeMaxAnchorLen+50)},
		{Role: "user", Content: "de fragmentos"},
	}
	if reply := previousReply(history); utf8.RuneCountInString(reply) != knowledgeMaxAnchorLen || !strings.HasPrefix(reply, "é") {
		t.Fatal(utf8.RuneCountInString(reply))
	}
	if reply := previousReply([]chatMessage{{Role: "user", Content: "first"}}); reply != "" {
		t.Fatal(reply)
	}
}

func TestAskingForFragmentsIsAskingForTheText(t *testing.T) {
	for _, content := range []string{"de fragmentos", "me dá um fragmento"} {
		if !focusedTurn(focusHandler(&documentBase{}), content).text {
			t.Fatal(content)
		}
	}
	if focusedTurn(focusHandler(&documentBase{}), "isso causa fragmentação de memória?").text {
		t.Fatal("a word sharing the root is not a request for text")
	}
}

func TestBriefsNeverAskForWhatTheLibraryHolds(t *testing.T) {
	for _, brief := range []string{knowledgeReferenceBrief, knowledgeTextBrief} {
		if !strings.Contains(brief, "never ask them to send you a passage") {
			t.Fatal(brief)
		}
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

func TestOutsideATurnAboutTheTextOnlyACitedQuoteIsChecked(t *testing.T) {
	sources := []knowledgeSource{{SourceID: "file:book", Text: bookPassage}}
	handler := focusHandler(&documentBase{})
	turn := knowledgeTurn{planned: true}
	reply := `Como você disse, “isso não faz sentido nenhum aqui”.`
	if got := handler.verifyQuotes(context.Background(), reply, sources, turn); got != reply {
		t.Fatalf("got %q", got)
	}
	// A stitched quote: both ends are the author's, the sentence is not.
	stitched := `Ela escreve: "se não for conforme à fé, queime logo" [1].`
	if got := handler.verifyQuotes(context.Background(), stitched, sources, turn); got != `Ela escreve: "[…]" [1].` {
		t.Fatalf("got %q", got)
	}
}

func TestASpokenTurnKeepsItsQuotes(t *testing.T) {
	sources := []knowledgeSource{{SourceID: "file:book", Text: bookPassage}}
	reply := `“uma frase qualquer de quatro palavras” [1]`
	if got := focusHandler(&documentBase{}).verifyQuotes(context.Background(), reply, sources, knowledgeTurn{}); got != reply {
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
