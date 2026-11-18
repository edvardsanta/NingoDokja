package chat

import (
	"strings"
	"unicode"
)

// maxWebSearchTitleRunes keeps the page's title short enough to steer a search without drowning
// the words the person actually asked about.
const maxWebSearchTitleRunes = 80

// askingWords are request words that say how to answer, not what to answer about.
var askingWords = map[string]bool{
	"fale": true, "falar": true, "fala": true, "falem": true, "sobre": true, "conte": true, "conta": true, "contar": true,
	"explique": true, "explica": true, "explicar": true, "resuma": true, "resumo": true, "resumir": true,
	"isso": true, "esse": true, "essa": true, "este": true, "esta": true, "aquilo": true, "aqui": true,
	"qual": true, "quais": true, "quem": true, "como": true, "porque": true, "pois": true, "mais": true, "menos": true,
	"muito": true, "pouco": true, "para": true, "pela": true, "pelo": true, "com": true, "sem": true, "uma": true,
	"uns": true, "umas": true, "dele": true, "dela": true, "deles": true, "delas": true, "pode": true, "poderia": true,
	"gostaria": true, "queria": true, "quero": true, "sabe": true, "saber": true, "tambem": true, "também": true,
	"ainda": true, "entao": true, "então": true, "link": true, "pagina": true, "página": true, "livro": true,
	"resume": true, "leia": true, "analise": true, "analisa": true, "comente": true, "comenta": true, "acha": true,
	"voce": true, "você": true, "pensa": true, "opine": true, "ningo": true,
	"read": true, "analyze": true, "think": true, "thoughts": true,
	"that": true, "this": true, "what": true, "tell": true, "about": true, "explain": true, "summarize": true,
	"summary": true, "with": true, "from": true, "have": true, "does": true, "your": true, "please": true, "page": true,
}

// researchIntentWords are the verbs a person commands a search with, in the languages the owner
// asks in. They name the act of researching, never its subject, so they are dropped from the
// query terms exactly like the asking words.
var researchIntentWords = map[string]bool{
	"pesquisa": true, "pesquise": true, "pesquisar": true,
	"busca": true, "busque": true, "buscar": true,
	"procura": true, "procure": true, "procurar": true,
	"googla": true, "googlar": true,
	"search": true, "research": true, "google": true, "find": true,
}

// WebSearchPlan decides whether the question calls for a web search, and if so what to search.
// Two shapes ask for one:
//
//   - a follow-up the readings leave unanswered — when none of the question's own words appears
//     anywhere in the pages read so far, the answer is not in them, and the first document's
//     title carries the context a bare name lacks;
//   - an explicit research command ("pesquisa livros sobre sound design"). With no readings in
//     the conversation there is nothing to complement, so only the command to go look sends the
//     search out, with the question's own content words as the query.
//
// A question with no subject of its own ("resume isso") asks about the reading itself and never
// searches; a question the readings already answer does not search again.
func WebSearchPlan(documents []WebDocument, question string) (string, bool) {
	question = webLinkPattern.ReplaceAllString(question, " ")
	terms := searchTerms(question)
	if len(terms) == 0 {
		return "", false
	}
	if len(documents) == 0 {
		if !asksForResearch(question) {
			return "", false
		}
		return strings.Join(terms, " "), true
	}
	for _, document := range documents {
		for _, passage := range document.Passages {
			hay := strings.ToLower(passage.Label + " " + passage.Text + " " + document.Title)
			for _, term := range terms {
				if strings.Contains(hay, term) {
					return "", false
				}
			}
		}
	}
	query := strings.Join(terms, " ")
	if title := []rune(strings.Join(strings.Fields(documents[0].Title), " ")); len(title) > 0 {
		if len(title) > maxWebSearchTitleRunes {
			title = title[:maxWebSearchTitleRunes]
		}
		query += " " + string(title)
	}
	return query, true
}

// searchTerms is the question's own content words, in order, without request or research verbs.
func searchTerms(question string) []string {
	var terms []string
	seen := map[string]bool{}
	for _, word := range strings.FieldsFunc(strings.ToLower(question), func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) }) {
		if len([]rune(word)) < 4 || askingWords[word] || researchIntentWords[word] || seen[word] {
			continue
		}
		seen[word] = true
		terms = append(terms, word)
	}
	return terms
}

// asksForResearch is true when the question itself commands a search.
func asksForResearch(question string) bool {
	for _, word := range strings.FieldsFunc(strings.ToLower(question), func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) }) {
		if researchIntentWords[word] {
			return true
		}
	}
	return false
}
