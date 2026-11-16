package price

import "context"

// genericReference picks a priced reference so a family query can proceed without another
// prompt. Current merchant quotes take precedence over stale quotes and market history.
// A product whose title leads with one of the words asked for is the thing that was
// asked for: merchandise that merely mentions them ("Caneca Red Dead Redemption") must
// not become the reference just because it is cheaper than the game. Those products
// form the first pool; when none leads with a query word the pool stays as matched.
func (d *Domain) genericReference(ctx context.Context, products []Product, query string) (*Product, error) {
	if leading := nameLeadingMatches(products, query); len(leading) > 0 {
		products = leading
	}
	selected := &products[0]
	bestRank := 3
	var bestPrice int64
	for i := range products {
		assessments, err := d.AssessProduct(ctx, products[i].Key)
		if err != nil {
			return nil, err
		}
		for _, assessment := range assessments {
			if assessment.Key.Series == SeriesMarketMin || assessment.Latest.Cents <= 0 {
				continue
			}
			rank := 0
			if assessment.Verdict == VerdictStale {
				rank = 1
			}
			if rank < bestRank || rank == bestRank && assessment.Latest.Cents < bestPrice {
				selected, bestRank, bestPrice = &products[i], rank, assessment.Latest.Cents
			}
		}
	}
	return selected, nil
}

// nameLeadingMatches keeps the products whose title opens with a word the query asked
// for, so a reference is the named product rather than an accessory that name-drops it.
func nameLeadingMatches(products []Product, query string) []Product {
	wanted := map[string]bool{}
	for _, word := range words(query) {
		wanted[word] = true
	}
	if len(wanted) == 0 {
		return nil
	}
	var matched []Product
	for _, product := range products {
		titleWords := words(product.Title)
		if len(titleWords) > 0 && wanted[titleWords[0]] {
			matched = append(matched, product)
		}
	}
	return matched
}
