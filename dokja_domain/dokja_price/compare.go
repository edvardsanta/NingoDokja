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
	PartNumber  string
	Query       string
	BudgetCents int64
}

// Offer is one history of the compared product, as a person reads it.
type Offer struct {
	Assessment Assessment
	URL        string
}

// Comparison is the result of a Compare. Found is false when nothing matched; Candidates holds
// the products to pick from when several matched and none stood out.
type Comparison struct {
	Found        bool
	Product      Product
	Offers       []Offer
	Decision     Decision
	Profile      CategoryProfile
	Alternatives []Alternative
	Candidates   []Product
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
	allProducts := make([]Product, 0, len(products))
	allProducts = append(allProducts, products...)
	alternatives := d.alternatives(ctx, *chosen, offers, products, compare.BudgetCents)
	return Comparison{
		Found: true, Product: *chosen, Offers: offers,
		Profile: CategoryProfileFor(chosen.Category), Alternatives: alternatives,
		Decision: Decide(DecisionInput{
			Product: *chosen, Offers: offers, BudgetCents: compare.BudgetCents, Products: allProducts,
		}),
	}, nil
}

func (d *Domain) alternatives(ctx context.Context, original Product, originalOffers []Offer, products []Product, budget int64) []Alternative {
	originalPrice := cheapestMerchant(originalOffers)
	result := make([]Alternative, 0, maxCandidates)
	for _, product := range products {
		if product.Key == original.Key || !sameCategory(product, original) {
			continue
		}
		assessments, err := d.AssessProduct(ctx, product.Key)
		if err != nil {
			continue
		}
		urls, err := d.repository.OfferURLs(ctx, product.Key)
		if err != nil {
			continue
		}
		var cheapest *Offer
		for _, assessment := range assessments {
			if assessment.Key.Series == SeriesMarketMin || assessment.Latest.Cents <= 0 {
				continue
			}
			offer := Offer{Assessment: assessment, URL: urls[assessment.Key]}
			if cheapest == nil || offer.Assessment.Latest.Cents < cheapest.Assessment.Latest.Cents {
				copy := offer
				cheapest = &copy
			}
		}
		if cheapest == nil {
			continue
		}
		classification := "same_category_candidate"
		reasons := []string{"same_category"}
		profile := CategoryProfileFor(original.Category)
		shared, better, compatible := comparableScore(profile, original, product)
		confidence := CandidateConfidence(product)
		if profile.ID == "cpu" {
			classification, confidence, reasons = compareCPUs(original, product, reasons)
		} else if !profile.Generic && compatible && shared > 0 {
			confidence = "medium"
			if shared >= 2 && better == shared {
				classification = "technically_superior"
				reasons = append(reasons, "shared_attributes_superior")
			} else if shared >= 2 && better*2 >= shared {
				classification = "technically_comparable"
				reasons = append(reasons, "shared_attributes_comparable")
			}
		}
		// Price facts are reasons, never the classification: a cheaper candidate that is
		// technically comparable stays comparable, so its equivalence can be kept.
		if originalPrice > 0 && cheapest.Assessment.Latest.Cents < originalPrice {
			reasons = append(reasons, "lower_current_price")
		}
		if budget > 0 && cheapest.Assessment.Latest.Cents <= budget {
			reasons = append(reasons, "within_budget")
		}
		result = append(result, Alternative{
			Product: product, Offer: *cheapest, Classification: classification,
			ReasonCodes: reasons, Confidence: confidence, SharedAttributes: shared,
		})
	}
	// Technically closer candidates first, then the cheaper.
	sort.SliceStable(result, func(i, j int) bool {
		left, right := technicalRank(result[i].Classification), technicalRank(result[j].Classification)
		if left != right {
			return left < right
		}
		return result[i].Offer.Assessment.Latest.Cents < result[j].Offer.Assessment.Latest.Cents
	})
	if len(result) > maxCandidates {
		if CategoryProfileFor(original.Category).ID == "cpu" {
			result = includeCompetingCPU(original, result)
		}
		result = result[:maxCandidates]
	}
	return result
}

// technicalRank orders classifications from the closest match to an unconfirmed one.
func technicalRank(classification string) int {
	switch classification {
	case "technically_superior":
		return 0
	case "technically_comparable":
		return 1
	case "similar_cpu_specs":
		return 2
	default:
		return 3
	}
}

func cheapestMerchant(offers []Offer) int64 {
	var cheapest int64
	for _, offer := range offers {
		if offer.Assessment.Key.Series == SeriesMarketMin || offer.Assessment.Latest.Cents <= 0 {
			continue
		}
		if cheapest == 0 || offer.Assessment.Latest.Cents < cheapest {
			cheapest = offer.Assessment.Latest.Cents
		}
	}
	return cheapest
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
			"specifications": c.Product.Specifications,
		}
		out["offers"] = offers
		out["decision"] = c.Decision.Map()
		out["profile"] = map[string]any{
			"id": c.Profile.ID, "generic": c.Profile.Generic,
		}
		alternatives := make([]any, 0, len(c.Alternatives))
		for _, alternative := range c.Alternatives {
			alternatives = append(alternatives, alternative.Map())
		}
		out["alternatives"] = alternatives
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
	if raw, present := payload["budget_cents"]; present && raw != nil {
		value, ok := raw.(int64)
		if !ok {
			if number, numberOK := raw.(int); numberOK {
				value, ok = int64(number), true
			}
		}
		if number, numberOK := raw.(float64); numberOK && number >= 0 && number == float64(int64(number)) {
			value, ok = int64(number), true
		}
		if !ok || value < 0 {
			return Compare{}, fmt.Errorf("budget_cents must be a non-negative integer")
		}
		compare.BudgetCents = value
	}
	return compare, nil
}
