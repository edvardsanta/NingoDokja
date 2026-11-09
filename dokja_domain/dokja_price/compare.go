package price

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"unicode/utf8"
)

// ActionCompareProduct finds one watched product and lays its offers side by side.
const ActionCompareProduct = "compare-product"

// maxCandidates bounds the products offered back when a comparison matches several.
const maxCandidates = 5

// Compare asks for one product: by its manufacturer's part number when known, else by words.
type Compare struct {
	PartNumber string
	Query      string
}

// Offer is one history of the compared product, as a person reads it.
type Offer struct {
	Assessment Assessment
	URL        string
}

// Comparison is the result of a Compare. Found is false when nothing matched; Candidates holds
// the products to pick from when several matched and none stood out.
type Comparison struct {
	Found      bool
	Product    Product
	Offers     []Offer
	Candidates []Product
	// Growable reports that the person may have the product looked up when it was not found.
	Growable bool
}

func (c Compare) validate() error {
	for name, value := range map[string]string{"part_number": c.PartNumber, "query": c.Query} {
		if !utf8.ValidString(value) || utf8.RuneCountInString(value) > maxSearchText {
			return fmt.Errorf("%s must be text of at most %d characters", name, maxSearchText)
		}
	}
	if strings.TrimSpace(c.PartNumber) == "" && strings.TrimSpace(c.Query) == "" {
		return fmt.Errorf("a part number or words are required")
	}
	return nil
}

// alnum folds a part number to lowercase letters and digits, so "AB-12C3D4E/XY" and "ab12c3d4e"
// name the same product.
func alnum(text string) string {
	return strings.Join(words(text), "")
}

// CompareProduct finds the product and returns every history it has, cheapest first.
func (d *Domain) CompareProduct(ctx context.Context, compare Compare) (Comparison, error) {
	if err := compare.validate(); err != nil {
		return Comparison{}, err
	}
	products, err := d.repository.Products(ctx)
	if err != nil {
		return Comparison{}, err
	}
	var chosen *Product
	var matched []Product
	if part := alnum(compare.PartNumber); len(part) >= 6 {
		// An exact part number wins. A part number read from an address can be cut short and name
		// a whole series: one variant starting with it is the product, several are candidates.
		var prefixed []Product
		for i := range products {
			known := alnum(products[i].PartNumber)
			switch {
			case known == "":
			case known == part:
				chosen = &products[i]
			case strings.HasPrefix(known, part) || strings.HasPrefix(part, known):
				prefixed = append(prefixed, products[i])
			}
			if chosen != nil {
				break
			}
		}
		if chosen == nil && len(prefixed) == 1 {
			chosen = &prefixed[0]
		}
		matched = prefixed
	}
	if chosen == nil && len(matched) == 0 && strings.TrimSpace(compare.Query) != "" {
		wanted := words(compare.Query)
		for _, product := range products {
			described := words(strings.Join([]string{product.Title, product.Key, product.Category, product.Brand, product.PartNumber}, " "))
			if matches(described, wanted) {
				matched = append(matched, product)
			}
		}
		if len(matched) == 1 {
			chosen = &matched[0]
		}
	}
	if chosen == nil {
		if len(matched) > maxCandidates {
			matched = matched[:maxCandidates]
		}
		return Comparison{Candidates: matched}, nil
	}
	assessments, err := d.AssessProduct(ctx, chosen.Key)
	if err != nil {
		return Comparison{}, err
	}
	urls, err := d.repository.OfferURLs(ctx, chosen.Key)
	if err != nil {
		return Comparison{}, err
	}
	offers := make([]Offer, 0, len(assessments))
	for _, assessment := range assessments {
		offers = append(offers, Offer{Assessment: assessment, URL: urls[assessment.Key]})
	}
	// The market low is a reference, not a store to buy from: it goes last.
	sort.SliceStable(offers, func(i, j int) bool {
		a, b := offers[i].Assessment, offers[j].Assessment
		if (a.Key.Series == SeriesMarketMin) != (b.Key.Series == SeriesMarketMin) {
			return b.Key.Series == SeriesMarketMin
		}
		return a.Latest.Cents < b.Latest.Cents
	})
	return Comparison{Found: true, Product: *chosen, Offers: offers}, nil
}

func (c Comparison) Map() map[string]any {
	out := map[string]any{"found": c.Found, "growable": c.Growable}
	if c.Found {
		offers := make([]any, 0, len(c.Offers))
		for _, offer := range c.Offers {
			row := offer.Assessment.Map()
			row["url"] = offer.URL
			offers = append(offers, row)
		}
		out["product"] = map[string]any{
			"product": c.Product.Key, "title": c.Product.Title, "category": c.Product.Category,
			"brand": c.Product.Brand, "part_number": c.Product.PartNumber,
		}
		out["offers"] = offers
	}
	candidates := make([]any, 0, len(c.Candidates))
	for _, candidate := range c.Candidates {
		candidates = append(candidates, map[string]any{"product": candidate.Key, "title": candidate.Title})
	}
	out["candidates"] = candidates
	return out
}

// compareFromPayload reads a compare request.
func compareFromPayload(payload map[string]any) (Compare, error) {
	var compare Compare
	for name, target := range map[string]*string{"part_number": &compare.PartNumber, "query": &compare.Query} {
		raw, present := payload[name]
		if !present || raw == nil {
			continue
		}
		value, ok := raw.(string)
		if !ok {
			return Compare{}, fmt.Errorf("%s must be a string", name)
		}
		*target = value
	}
	return compare, nil
}
