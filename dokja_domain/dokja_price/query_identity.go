package price

import (
	"strings"
	"unicode"
)

// comparisonWords accepts common model spelling variants such as RX7600 and RX 7600.
// Numeric suffixes remain attached so a base model does not match a different variant.
func comparisonWords(text string) []string {
	var tokens []string
	for _, word := range words(text) {
		index := strings.IndexFunc(word, unicode.IsDigit)
		if index > 0 {
			tokens = append(tokens, word[:index], word[index:])
		} else {
			tokens = append(tokens, word)
		}
	}
	return tokens
}

func comparisonMatches(product Product, query string) bool {
	if part := alnum(product.PartNumber); len(part) >= 6 && alnum(query) == part {
		return true
	}
	if strings.Contains(query, ":") && strings.EqualFold(strings.TrimSpace(query), product.Key) {
		return true
	}
	identity := comparisonWords(product.Title)
	described := comparisonWords(strings.Join([]string{product.Title, product.Category, product.Brand}, " "))
	if strings.IndexFunc(query, unicode.IsDigit) >= 0 {
		described = comparisonWords(product.Title + " " + product.Brand)
	}
	wanted := comparisonWords(query)
	if len(wanted) == 0 {
		return false
	}
	for _, token := range wanted {
		if strings.IndexFunc(token, unicode.IsDigit) >= 0 {
			found := false
			for _, word := range identity {
				if word == token {
					found = true
					break
				}
			}
			if !found {
				return false
			}
		} else if !matches(described, []string{token}) {
			return false
		}
	}
	return true
}
