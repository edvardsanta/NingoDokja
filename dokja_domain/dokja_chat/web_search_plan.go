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
	"voce": true, "você": true, "pensa": true, "opine": true,
	"algo": true, "coisa": true, "coisas": true, "relacionado": true, "relacionada": true,
	"read": true, "analyze": true, "think": true, "thoughts": true,
	"that": true, "this": true, "what": true, "tell": true, "about": true, "explain": true, "summarize": true,
	"summary": true, "with": true, "from": true, "have": true, "does": true, "your": true, "please": true, "page": true,
}

// researchIntentWords name an explicit request to search the web, as opposed to
// a question that merely hopes for an answer. Verb forms only: nouns like
// "busca" name a thing ("a busca binária", "qual a busca mais usada") far more
// often than they request one.
var researchIntentWords = map[string]bool{
	"pesquise": true, "pesquisar": true, "pesquisem": true,
	"busque": true, "buscar": true, "procure": true, "procurar": true,
	"googla": true, "googlar": true,
	"search": true, "research": true,
}

// WebSearchQuestionPlan plans a search from the question alone, for when the
// person explicitly asks for research ("pesquise X") and no reading exists to
// steer or trigger it. Asking for research is a deliberate act, not every
// factual question: without an intent word there is no search, and without a
// subject there is nothing to search for.
func WebSearchQuestionPlan(question string) (string, bool) {
	question = webLinkPattern.ReplaceAllString(question, " ")
	intent := false
	var terms []string
	seen := map[string]bool{}
	for _, word := range strings.FieldsFunc(strings.ToLower(question), func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) }) {
		if researchIntentWords[word] {
			intent = true
			continue
		}
		if len([]rune(word)) < 4 || askingWords[word] || seen[word] {
			continue
		}
		seen[word] = true
		terms = append(terms, word)
	}
	if !intent || len(terms) == 0 {
		return "", false
	}
	return strings.Join(terms, " "), true
}

// WebSearchPlan decides whether the pages read so far leave the question unanswered, and if so
// what to search. A link to a shop page says what the thing is called and nothing about the
// character a person asks about next: when none of the question's own words appears anywhere in
// the readings, the answer is not in them, and the first document's title carries the context
// that a bare name lacks. A question with no subject of its own ("resume isso") asks about the
// reading itself and never searches.
func WebSearchPlan(documents []WebDocument, question string) (string, bool) {
	if len(documents) == 0 {
		return "", false
	}
	question = webLinkPattern.ReplaceAllString(question, " ")
	var terms []string
	seen := map[string]bool{}
	for _, word := range strings.FieldsFunc(strings.ToLower(question), func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) }) {
		if len([]rune(word)) < 4 || askingWords[word] || seen[word] {
			continue
		}
		seen[word] = true
		terms = append(terms, word)
	}
	if len(terms) == 0 {
		return "", false
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
