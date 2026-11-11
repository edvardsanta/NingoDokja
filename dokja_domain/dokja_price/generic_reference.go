package price

import "context"

// genericReference picks a priced reference so a family query can proceed without another prompt.
// Current merchant quotes take precedence over stale quotes and market history.
func (d *Domain) genericReference(ctx context.Context, products []Product) (*Product, error) {
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
