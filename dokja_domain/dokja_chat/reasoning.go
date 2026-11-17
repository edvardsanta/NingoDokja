package chat

import (
	"regexp"
	"strings"
	"unicode"
)

// ReasoningTurn contains facts about this turn, never a model's difficulty verdict.
type ReasoningTurn struct {
	Message         string
	SourceCount     int
	ReadingRelevant bool
	Owner           bool
	Voice           bool
	Preview         bool
}

var arithmeticTurn = regexp.MustCompile(`\d[\d.,]*\s*[%+*/×÷^−-]\s*\d`)
var reasoningMath = regexp.MustCompile(`\b(calculate|calculation|probability|equation|integral|derivative|percentage|calcule|calcula|cálculo|probabilidade|equação|derivada|porcentagem)\b`)
var reasoningCompare = regexp.MustCompile(`\b(compare|comparison|versus|vs|difference|differences|verify|verification|fact.check|check|contrast|comparar|comparação|diferença|diferenças|verifique|verifica|verificar|confira|confere|confronte)\b`)
var reasoningResearch = regexp.MustCompile(`\b(research|investigate|pesquise|pesquisa|pesquisar|investigue|investigar)\b`)
var reasoningBanter = regexp.MustCompile(`^(hi|hello|hey|thanks|thank you|good morning|oi|olá|ola|bom dia|boa tarde|boa noite|valeu|obrigado|obrigada|lol|haha|kkkk|kkk|burger)$`)
var reasoningNoise = regexp.MustCompile(`^(a+|k+|(?:ha)+|(?:he)+|lol|asdf[a-z]*|qwerty[a-z]*)$`)

// NeedsReasoning selects substantive turns before inference. Evidence alone must
// not turn noise into a question. Short arithmetic is an intentional exception
// to the short-message rule; relevant reading may support a short follow-up.
func NeedsReasoning(turn ReasoningTurn) bool {
	if turn.Voice || turn.Preview {
		return false
	}
	message := strings.ToLower(strings.TrimSpace(turn.Message))
	text := webLinkPattern.ReplaceAllString(message, " ")
	words := strings.FieldsFunc(text, func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	})
	plain := strings.Join(words, " ")
	if reasoningBanter.MatchString(plain) {
		return false
	}
	noise := len(words) > 0
	for _, word := range words {
		noise = noise && reasoningNoise.MatchString(word)
	}
	if noise {
		return false
	}
	if arithmeticTurn.MatchString(text) || mathBlock.MatchString(text) {
		return true
	}
	if len(words) == 0 {
		return turn.SourceCount >= 2 && len(WebLinks(message)) >= 2
	}
	// Repeated characters and isolated tokens carry no request, even with sources.
	if len(words) == 1 {
		return false
	}
	if len(words) < 4 && turn.SourceCount == 0 {
		return false
	}
	if reasoningMath.MatchString(plain) || reasoningCompare.MatchString(plain) {
		return true
	}
	return turn.ReadingRelevant || turn.SourceCount >= 2 || (turn.Owner && reasoningResearch.MatchString(plain))
}
