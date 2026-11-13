package chat

import (
	"strings"
	"unicode"
)

// A turn has two kinds of context. The live context is the session's own history and its summary:
// it is always sent, because it is the conversation itself. The macro context is what Ningo kept
// from earlier discussions, durable and searched on request: it costs a lookup and a slice of the
// prompt, and it repeats what the live context already says when the talk is still about it. So the
// macro context is sent only when the live context cannot carry the turn.

// Reasons a turn recalls, or does not recall, the macro context.
const (
	RecallEmptyQuestion  = "empty_question"
	RecallAsksForPast    = "asks_for_the_past"
	RecallNoLiveContext  = "no_live_context"
	RecallShortFollowUp  = "short_follow_up"
	RecallLiveCovers     = "live_context_covers_it"
	RecallBeyondLive     = "beyond_the_live_context"
	liveCoverageRequired = 0.5
	// A question with fewer content words than this leans on what was just said.
	followUpWords = 3
	minWordRunes  = 4
)

// RecallDecision says whether to look in the macro context for this turn, and why.
type RecallDecision struct {
	Use    bool
	Reason string
}

// DecideMacroRecall decides from the question and the texts of the live context (history and
// summary, without the question itself). It is deterministic: the same turn always decides the same.
//
//  1. A question that asks for the past ("lembra", "last time", "we discussed") recalls.
//  2. With no live context (a new or expired session) the macro context is the only continuity.
//  3. A short follow-up leans on the live context and recalls nothing.
//  4. Otherwise it recalls only when the live context does not already talk about the question:
//     most of the question's content words are absent from it.
func DecideMacroRecall(question string, live []string) RecallDecision {
	question = strings.TrimSpace(question)
	if question == "" {
		return RecallDecision{Reason: RecallEmptyQuestion}
	}
	if asksForThePast(question) {
		return RecallDecision{Use: true, Reason: RecallAsksForPast}
	}
	words := contentWords(question)
	liveText := strings.Join(live, "\n")
	if strings.TrimSpace(liveText) == "" {
		return RecallDecision{Use: true, Reason: RecallNoLiveContext}
	}
	if len(words) < followUpWords {
		return RecallDecision{Reason: RecallShortFollowUp}
	}
	known := map[string]bool{}
	for _, word := range contentWords(liveText) {
		known[word] = true
	}
	covered := 0
	for _, word := range words {
		if known[word] {
			covered++
		}
	}
	if float64(covered)/float64(len(words)) >= liveCoverageRequired {
		return RecallDecision{Reason: RecallLiveCovers}
	}
	return RecallDecision{Use: true, Reason: RecallBeyondLive}
}

// pastCues are phrases that point at an earlier discussion. They only decide that the past is wanted;
// what comes back is still chosen by the macro search itself.
var pastCues = []string{
	"lembra", "lembro", "lembrar", "como falamos", "como a gente falou", "como conversamos", "a gente falou",
	"a gente conversou", "já discutimos", "ja discutimos", "última vez", "ultima vez", "outro dia", "semana passada",
	"antes você disse", "antes voce disse", "você disse que", "voce disse que", "o que decidimos", "o que combinamos",
	"remember", "last time", "we discussed", "we talked about", "we decided", "you said earlier", "you told me",
	"earlier you", "the other day", "last week", "as we agreed",
}

func asksForThePast(question string) bool {
	lower := strings.ToLower(question)
	for _, cue := range pastCues {
		if strings.Contains(lower, cue) {
			return true
		}
	}
	return false
}

// stopWords are too common to show that two texts are about the same thing.
var stopWords = map[string]bool{}

func init() {
	for _, word := range strings.Fields(`
		para como mais mas muito muita isso isto esse essa esses essas aquilo aquele aquela porque pois então entao
		sobre entre quando onde quem qual quais você voce vocês voces tenho tinha temos tem estou está esta estão
		estao fazer feito faz pode podem posso dizer sobre ainda também tambem só depois antes aqui ali agora
		that this these those with from have has had been being were what when where which while would could should
		about there their them they then than your yours just like into also more most some very much only over`) {
		stopWords[word] = true
	}
}

// contentWords lowercases and splits text into distinct words long enough to carry a subject.
func contentWords(text string) []string {
	seen := map[string]bool{}
	var words []string
	for _, word := range strings.FieldsFunc(strings.ToLower(text), func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) }) {
		if len([]rune(word)) < minWordRunes || stopWords[word] || seen[word] {
			continue
		}
		seen[word] = true
		words = append(words, word)
	}
	return words
}
