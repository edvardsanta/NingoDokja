package price

import (
	"sort"
	"strings"
)

// Decision is the deterministic purchase conclusion attached to a comparison.
// The language model may explain it, but it does not calculate it.
type Decision struct {
	Action         string   `json:"action"`
	SelectedOffer  string   `json:"selected_merchant"`
	ReasonCodes    []string `json:"reason_codes"`
	Confidence     string   `json:"confidence"`
	Alternatives   int      `json:"alternatives"`
	RequiredBudget int64    `json:"budget_cents"`
	SelectedCents  int64    `json:"selected_cents"`
}

const (
	ActionBuyOriginal      = "buy_original"
	ActionSeekAlternative  = "seek_alternative"
	ActionWait             = "wait_for_offer"
	ActionNoRecommendation = "no_recommendation"

	ReasonOverBudget       = "over_budget"
	ReasonWithinBudget     = "within_budget"
	ReasonInsufficientData = "insufficient_data"
	ReasonNoAlternatives   = "no_alternatives"
	ReasonFreshOffer       = "current_offer"
)

// DecisionInput contains only facts already collected by the price domain.
type DecisionInput struct {
	Product     Product
	Offers      []Offer
	BudgetCents int64
	Products    []Product
}

// Decide evaluates a product without inventing technical specifications. Products in the
// same category are counted as possible alternatives, but are never selected without a
// current offer and a compatible identity.
func Decide(input DecisionInput) Decision {
	decision := Decision{
		Action:         ActionNoRecommendation,
		Confidence:     "low",
		RequiredBudget: input.BudgetCents,
	}
	if len(input.Offers) == 0 {
		decision.ReasonCodes = []string{ReasonInsufficientData}
		return decision
	}

	offers := make([]Offer, 0, len(input.Offers))
	for _, offer := range input.Offers {
		if offer.Assessment.Key.Series == SeriesMarketMin || offer.Assessment.Latest.Cents <= 0 {
			continue
		}
		offers = append(offers, offer)
	}
	if len(offers) == 0 {
		decision.ReasonCodes = []string{ReasonInsufficientData}
		return decision
	}
	sort.SliceStable(offers, func(i, j int) bool {
		return offers[i].Assessment.Latest.Cents < offers[j].Assessment.Latest.Cents
	})
	cheapest := offers[0]
	decision.SelectedOffer = cheapest.Assessment.Key.Merchant
	decision.SelectedCents = cheapest.Assessment.Latest.Cents
	if cheapest.Assessment.HistoryDays >= 30 {
		decision.Confidence = "medium"
	}
	if cheapest.Assessment.HistoryDays >= 90 {
		decision.Confidence = "high"
	}

	if input.BudgetCents > 0 && cheapest.Assessment.Latest.Cents > input.BudgetCents {
		decision.Action = ActionSeekAlternative
		decision.ReasonCodes = []string{ReasonOverBudget}
	} else {
		decision.Action = ActionBuyOriginal
		decision.ReasonCodes = []string{ReasonWithinBudget, ReasonFreshOffer}
	}

	for _, product := range input.Products {
		if product.Key == input.Product.Key || !sameCategory(product, input.Product) {
			continue
		}
		decision.Alternatives++
	}
	if decision.Action == ActionSeekAlternative && decision.Alternatives == 0 {
		decision.ReasonCodes = append(decision.ReasonCodes, ReasonNoAlternatives)
	}
	return decision
}

func sameCategory(a, b Product) bool {
	if a.Category == "" || b.Category == "" {
		return false
	}

	return categoryToken(a.Category) == categoryToken(b.Category)
}

func comparableScore(profile CategoryProfile, original, candidate Product) (int, int, bool) {
	shared, better := 0, 0
	for _, attribute := range profile.Attributes {
		left, leftOK := numericSpec(original.Specifications, attribute.Key)
		right, rightOK := numericSpec(candidate.Specifications, attribute.Key)
		if leftOK && rightOK {
			shared++
			if attribute.HigherIsBetter && right > left || !attribute.HigherIsBetter && right < left {
				better++
			}
			continue
		}
		if attribute.Required {
			leftText, leftTextOK := stringSpec(original.Specifications, attribute.Key)
			rightText, rightTextOK := stringSpec(candidate.Specifications, attribute.Key)
			if leftTextOK && rightTextOK && leftText != rightText {
				return shared, better, false
			}
		}
	}
	return shared, better, true
}

func numericSpec(specs map[string]any, key string) (float64, bool) {
	value, ok := specs[key]
	switch typed := value.(type) {
	case float64:
		return typed, ok
	case int:
		return float64(typed), ok
	case int64:
		return float64(typed), ok
	default:
		return 0, false
	}
}

func stringSpec(specs map[string]any, key string) (string, bool) {
	value, ok := specs[key].(string)
	return value, ok
}

func categoryToken(value string) string {
	words := strings.Fields(strings.ToLower(value))
	for _, word := range words {
		switch word {
		case "gpu", "placa", "video", "placas":
			return "gpu"
		case "cpu", "processador", "processadores":
			return "cpu"
		case "ssd", "hd", "armazenamento":
			return "storage"
		case "monitor", "televisao", "televisor":
			return "display"
		case "celular", "smartphone", "tablet":
			return "mobile"
		}
	}
	return strings.Join(words, " ")
}

func (d Decision) Map() map[string]any {
	reasons := make([]any, len(d.ReasonCodes))
	for i, reason := range d.ReasonCodes {
		reasons[i] = reason
	}
	return map[string]any{
		"action": d.Action, "selected_merchant": d.SelectedOffer,
		"reason_codes": reasons, "confidence": d.Confidence,
		"alternatives": d.Alternatives, "budget_cents": d.RequiredBudget,
		"selected_cents": d.SelectedCents,
	}
}
