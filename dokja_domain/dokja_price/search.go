package price

import (
	"context"
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"
)

const (
	// maxSearchText bounds what a person may type in a search field.
	maxSearchText = 100
	// maxSearchProducts bounds one answer; a chat reply cannot hold more.
	maxSearchProducts = 10
	// prefixMinRunes: a search word this long also matches the start of a longer word
	// ("ryz" finds "ryzen"); shorter ones must match a whole word, so "hd" does not
	// find "hdmi".
	prefixMinRunes = 3
)

// Search narrows the watched products. Every field is optional; an empty Search matches
// every product.
type Search struct {
	// Product is an exact product key.
	Product string
	// Query holds words that must all appear in the product's title or key, such as "hd"
	// or "rtx 5070".
	Query string
	// Brand is one more word that must appear, such as "amd".
	Brand string
	// History asks for each product's market-low points of the last year, for a chart. A chat
	// reply has no use for them, so they come only when asked.
	History bool
}

// ProductAssessments are the assessments of one product, and its market-low history when the
// search asked for it.
type ProductAssessments struct {
	Product     Product
	Assessments []Assessment
	Market      []Point
}

// SearchResult is what a search found, at most maxSearchProducts products.
type SearchResult struct {
	Products []ProductAssessments
	// Matched counts every matching product, including those beyond the limit.
	Matched int
}

func (s Search) validate() error {
	if s.Product != "" && !keyPattern.MatchString(s.Product) {
		return fmt.Errorf("product must be 1 to 80 lowercase letters, digits, '.', '_', ':' or '-'")
	}
	for name, value := range map[string]string{"query": s.Query, "brand": s.Brand} {
		if !utf8.ValidString(value) || utf8.RuneCountInString(value) > maxSearchText {
			return fmt.Errorf("%s must be text of at most %d characters", name, maxSearchText)
		}
	}
	return nil
}

// SearchProducts assesses the watched products that match the search, in key order.
func (d *Domain) SearchProducts(ctx context.Context, search Search) (SearchResult, error) {
	if err := search.validate(); err != nil {
		return SearchResult{}, err
	}
	products, err := d.repository.Products(ctx)
	if err != nil {
		return SearchResult{}, err
	}
	wanted := append(words(search.Query), words(search.Brand)...)
	var result SearchResult
	for _, product := range products {
		if search.Product != "" && product.Key != search.Product {
			continue
		}
		if !matches(words(product.Title+" "+product.Key), wanted) {
			continue
		}
		result.Matched++
		if len(result.Products) == maxSearchProducts {
			continue
		}
		assessments, err := d.AssessProduct(ctx, product.Key)
		if err != nil {
			return SearchResult{}, err
		}
		found := ProductAssessments{Product: product, Assessments: assessments}
		if search.History {
			if found.Market, err = d.marketHistory(ctx, assessments); err != nil {
				return SearchResult{}, err
			}
		}
		result.Products = append(result.Products, found)
	}
	return result, nil
}

// marketHistory is the first market-low history among a product's assessments, over the last
// year; a product followed at one store only has none.
func (d *Domain) marketHistory(ctx context.Context, assessments []Assessment) ([]Point, error) {
	for _, assessment := range assessments {
		if assessment.Key.Series != SeriesMarketMin {
			continue
		}
		from := d.now().UTC().AddDate(-1, 0, 0).Format(dateLayout)
		return d.repository.History(ctx, assessment.Key, from, "")
	}
	return nil, nil
}

// matches reports whether every wanted word is one of the product's words, or the start of
// one when the wanted word is long enough.
func matches(have, wanted []string) bool {
	for _, want := range wanted {
		found := false
		for _, word := range have {
			if word == want || (utf8.RuneCountInString(want) >= prefixMinRunes && strings.HasPrefix(word, want)) {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}

// words splits text into lowercase words without accents, so "Vídeo" matches "video".
func words(text string) []string {
	var builder strings.Builder
	for _, r := range strings.ToLower(text) {
		r = foldAccent(r)
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			builder.WriteRune(r)
		} else {
			builder.WriteRune(' ')
		}
	}
	return strings.Fields(builder.String())
}

// foldAccent maps the accented letters of Portuguese and Spanish to their base letter.
func foldAccent(r rune) rune {
	switch r {
	case 'á', 'à', 'â', 'ã', 'ä':
		return 'a'
	case 'é', 'è', 'ê', 'ë':
		return 'e'
	case 'í', 'ì', 'î', 'ï':
		return 'i'
	case 'ó', 'ò', 'ô', 'õ', 'ö':
		return 'o'
	case 'ú', 'ù', 'û', 'ü':
		return 'u'
	case 'ç':
		return 'c'
	case 'ñ':
		return 'n'
	}
	return r
}

// Map is the search result as the orchestrator passes it on.
func (r SearchResult) Map() map[string]any {
	products := make([]any, 0, len(r.Products))
	for _, product := range r.Products {
		rows := make([]any, 0, len(product.Assessments))
		for _, assessment := range product.Assessments {
			rows = append(rows, assessment.Map())
		}
		entry := map[string]any{"product": product.Product.Key, "title": product.Product.Title, "assessments": rows}
		if product.Market != nil {
			market := make([]any, 0, len(product.Market))
			for _, point := range product.Market {
				market = append(market, map[string]any{"on": point.On, "cents": point.Cents})
			}
			entry["market"] = market
		}
		products = append(products, entry)
	}
	return map[string]any{"products": products, "matched": r.Matched, "count": len(products)}
}
