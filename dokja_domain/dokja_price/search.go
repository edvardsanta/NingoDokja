package price

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"
)

// OrderOpportunity sorts a search by how good a buy each product is now.
const OrderOpportunity = "opportunity"

const (
	// maxRanked bounds how many matching products are assessed to be ranked by opportunity.
	maxRanked = 300
	// maxSearchText bounds what a person may type in a search field.
	maxSearchText = 100
	// maxSearchProducts bounds one answer; a chat reply cannot hold more.
	maxSearchProducts = 10
	// prefixMinRunes: a search word this long also matches the start of a longer word
	// ("zen" finds "zenith"); shorter ones must match a whole word, so "hd" does not
	// find "hdmi".
	prefixMinRunes = 3
)

// Search narrows the watched products. Every field is optional; an empty Search matches
// every product.
type Search struct {
	// Product is an exact product key.
	Product string
	// Query holds words that must all appear in the product's title or key, such as "hd"
	// or "gx 5000".
	Query string
	// Brand must match the product's brand, or its title when the collector gave no brand.
	Brand string
	// Merchant keeps only the histories of that merchant key, such as one store.
	Merchant string
	// Order is "" (product key) or "opportunity": real discounts first, deepest first, then
	// ordinary prices by how far under their median they are, then products still gathering
	// history, cheapest first, and warnings last.
	Order string
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
	// URLs are the offers' pages, by history, for a person to open.
	URLs map[Key]string
}

// SearchResult is what a search found, at most maxSearchProducts products.
type SearchResult struct {
	Products []ProductAssessments
	// Matched counts every matching product, including those beyond the limit.
	Matched int
	// Merchants lists every merchant key known, for a person to pick a store from.
	Merchants []string
	// Watch reports what the search did to the watched searches, when it could change them.
	Watch *WatchOutcome
}

func (s Search) validate() error {
	if s.Product != "" && !keyPattern.MatchString(s.Product) {
		return fmt.Errorf("product must be 1 to 80 lowercase letters, digits, '.', '_', ':' or '-'")
	}
	if s.Order != "" && s.Order != OrderOpportunity {
		return fmt.Errorf("order must be empty or %q", OrderOpportunity)
	}
	if s.Merchant != "" && !keyPattern.MatchString(s.Merchant) {
		return fmt.Errorf("merchant must be 1 to 80 lowercase letters, digits, '.', '_', ':' or '-'")
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
	query, brand := words(search.Query), words(search.Brand)
	var result SearchResult
	var matched []Product
	for _, product := range products {
		if search.Product != "" && product.Key != search.Product {
			continue
		}
		described := words(strings.Join([]string{product.Title, product.Key, product.Category, product.Brand, product.PartNumber}, " "))
		branded := described
		if product.Brand != "" {
			branded = words(product.Brand)
		}
		if !matches(described, query) || !matches(branded, brand) {
			continue
		}
		if search.Merchant != "" {
			keys, err := d.repository.Keys(ctx, product.Key)
			if err != nil {
				return SearchResult{}, err
			}
			sold := false
			for _, key := range keys {
				sold = sold || key.Merchant == search.Merchant
			}
			if !sold {
				continue
			}
		}
		matched = append(matched, product)
	}
	result.Matched = len(matched)

	// Each candidate is assessed once; ranking needs every assessment, a plain answer only the
	// first page's.
	candidates := matched
	if search.Order != OrderOpportunity && len(candidates) > maxSearchProducts {
		candidates = candidates[:maxSearchProducts]
	}
	if len(candidates) > maxRanked {
		candidates = candidates[:maxRanked]
	}
	assessed := make([]ProductAssessments, 0, len(candidates))
	for _, product := range candidates {
		assessments, err := d.AssessProduct(ctx, product.Key)
		if err != nil {
			return SearchResult{}, err
		}
		if search.Merchant != "" {
			kept := assessments[:0]
			for _, assessment := range assessments {
				if assessment.Key.Merchant == search.Merchant {
					kept = append(kept, assessment)
				}
			}
			assessments = kept
		}
		assessed = append(assessed, ProductAssessments{Product: product, Assessments: assessments})
	}
	if search.Order == OrderOpportunity {
		sort.SliceStable(assessed, func(i, j int) bool { return opportunityLess(assessed[i], assessed[j]) })
	}
	if len(assessed) > maxSearchProducts {
		assessed = assessed[:maxSearchProducts]
	}
	for _, found := range assessed {
		urls, err := d.repository.OfferURLs(ctx, found.Product.Key)
		if err != nil {
			return SearchResult{}, err
		}
		found.URLs = urls
		if search.History {
			if found.Market, err = d.marketHistory(ctx, found.Assessments); err != nil {
				return SearchResult{}, err
			}
		}
		result.Products = append(result.Products, found)
	}
	merchants, err := d.repository.Merchants(ctx)
	if err != nil {
		return SearchResult{}, err
	}
	result.Merchants = merchants
	return result, nil
}

// opportunityRank places a product by its assessments: 0 a real discount, 1 an ordinary price,
// 2 still gathering history, 3 a warning (a fake discount or a markup outranks everything else,
// as on every screen). Within a rank, a deeper discount, then a lower price, comes first.
func opportunityRank(found ProductAssessments) (rank int, discount int64, cheapest int64) {
	rank, discount, cheapest = 2, -1<<62, 1<<62
	warned, real, plain := false, false, false
	for _, assessment := range found.Assessments {
		switch assessment.Verdict {
		case VerdictFakeDiscount, VerdictInflatedBeforeSale:
			warned = true
		case VerdictRealDiscount:
			real = true
		case VerdictNoDiscount, VerdictStale:
			plain = true
		}
		discount = max(discount, assessment.DiscountVs30Bps)
		cheapest = min(cheapest, assessment.Latest.Cents)
	}
	switch {
	case warned:
		rank = 3
	case real:
		rank = 0
	case plain:
		rank = 1
	}
	return rank, discount, cheapest
}

func opportunityLess(a, b ProductAssessments) bool {
	rankA, discountA, cheapA := opportunityRank(a)
	rankB, discountB, cheapB := opportunityRank(b)
	if rankA != rankB {
		return rankA < rankB
	}
	if rankA <= 1 && discountA != discountB {
		return discountA > discountB
	}
	return cheapA < cheapB
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
			row := assessment.Map()
			row["url"] = product.URLs[assessment.Key]
			rows = append(rows, row)
		}
		entry := map[string]any{
			"product": product.Product.Key, "title": product.Product.Title, "assessments": rows,
			"category": product.Product.Category, "brand": product.Product.Brand, "part_number": product.Product.PartNumber,
		}
		if product.Market != nil {
			market := make([]any, 0, len(product.Market))
			for _, point := range product.Market {
				market = append(market, map[string]any{"on": point.On, "cents": point.Cents})
			}
			entry["market"] = market
		}
		products = append(products, entry)
	}
	merchants := make([]any, 0, len(r.Merchants))
	for _, merchant := range r.Merchants {
		merchants = append(merchants, merchant)
	}
	out := map[string]any{"products": products, "matched": r.Matched, "count": len(products), "merchants": merchants}
	if r.Watch != nil {
		out["watch"] = r.Watch.Map()
	}
	return out
}
