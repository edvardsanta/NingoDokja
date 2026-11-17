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

// comparisonMatches reports whether a query names a known product: every query
// token must appear in the product's title words, with digit tokens matching
// exactly, so a base model never silently answers for a different variant.
func comparisonMatches(product Product, query string) bool {
	return comparisonMatchesWith(product, query, false)
}

// comparisonMatchesVariant relaxes the digit rule: a digit token also matches a
// title word that starts with it and continues with purely alphabetic suffixes,
// so "5600" finds "5600GT" and "5600XT", the way stores compound model numbers.
// It is the fallback for when the strict matcher found nothing: a base model
// whose listings only exist as variants is still findable. "5" never matches
// "5600GT" (the remainder is not purely alphabetic), and "5600" never matches
// "56000".
func comparisonMatchesVariant(product Product, query string) bool {
	return comparisonMatchesWith(product, query, true)
}

func comparisonMatchesWith(product Product, query string, variantSuffixes bool) bool {
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
				if word == token || variantSuffixes && strings.HasPrefix(word, token) && suffixIsAlpha(word, token) {
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

// suffixIsAlpha reports whether word continues token with purely alphabetic
// characters: "5600" → "5600XT" yes, "5" → "5600XT" no, "5600" → "56000" no.
func suffixIsAlpha(word, token string) bool {
	return strings.TrimLeftFunc(word[len(token):], unicode.IsLetter) == ""
}
