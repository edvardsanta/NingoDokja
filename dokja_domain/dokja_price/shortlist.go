package price

import (
	"math"
	"sort"
	"strings"
)

// ShortlistAlternatives orders distinct purchase options after the current model review.
func ShortlistAlternatives(result map[string]any) {
	rows, _ := result["alternatives"].([]any)
	product, _ := result["product"].(map[string]any)
	decision, _ := result["decision"].(map[string]any)
	budget, _ := numericSpec(decision, "budget_cents")
	originalPrice, _ := numericSpec(decision, "selected_cents")
	brand, _ := product["brand"].(string)
	title, _ := product["title"].(string)
	brand = CPUBrand(brand, title)
	candidates := []map[string]any{}
	for _, row := range rows {
		if item, ok := row.(map[string]any); ok {
			if !eligibleAlternative(product, item) {
				continue
			}
			candidates = append(candidates, item)
		}
	}
	price := func(item map[string]any) float64 {
		value, _ := numericSpec(item, "price_cents")
		return value
	}
	rank := func(item map[string]any) int {
		review, _ := item["model_assessment"].(map[string]any)
		switch review["relationship"] {
		case "comparable", "superior":
			return 0
		case "inferior":
			return 2
		}
		classification, _ := item["classification"].(string)
		if classification == "technically_comparable" || classification == "technically_superior" {
			return 0
		}
		if classification == "similar_cpu_specs" {
			return 1
		}
		return technicalRank(classification) + 1
	}
	platform := func(item map[string]any) int {
		left, _ := product["specifications"].(map[string]any)
		right, _ := item["specifications"].(map[string]any)
		a, _ := stringSpec(left, "socket")
		b, _ := stringSpec(right, "socket")
		if a == "" || b == "" {
			return 1
		}
		if strings.EqualFold(strings.ReplaceAll(a, " ", ""), strings.ReplaceAll(b, " ", "")) {
			return 0
		}
		return 2
	}
	sort.SliceStable(candidates, func(i, j int) bool {
		a, b := candidates[i], candidates[j]
		if budget > 0 && (price(a) > budget) != (price(b) > budget) {
			return price(a) <= budget
		}
		if rank(a) != rank(b) {
			return rank(a) < rank(b)
		}
		if platform(a) != platform(b) {
			return platform(a) < platform(b)
		}
		return math.Abs(price(a)-originalPrice) < math.Abs(price(b)-originalPrice)
	})
	selected := []any{}
	used := map[string]bool{}
	add := func(item map[string]any, role string) {
		id, _ := item["product"].(string)
		if id == "" || used[id] {
			return
		}
		item["selection_reason"] = role
		item["over_budget"] = budget > 0 && price(item) > budget
		selected = append(selected, item)
		used[id] = true
	}
	for _, item := range candidates {
		if rank(item) == 0 {
			add(item, "performance")
			break
		}
	}
	var economy map[string]any
	for _, item := range candidates {
		id, _ := item["product"].(string)
		if used[id] {
			continue
		}
		if price(item) <= 0 || originalPrice <= 0 || price(item) >= originalPrice {
			continue
		}
		if economy == nil || platform(item) < platform(economy) ||
			platform(item) == platform(economy) && price(item) < price(economy) {
			economy = item
		}
	}
	if economy != nil {
		add(economy, "economy")
	}
	for _, item := range candidates {
		b, _ := item["brand"].(string)
		t, _ := item["title"].(string)
		other := CPUBrand(b, t)
		if brand != "" && other != "" && other != brand {
			add(item, "different_brand")
			break
		}
	}
	for _, item := range candidates {
		review, _ := item["model_assessment"].(map[string]any)
		if advantage, _ := review["advantage"].(string); advantage != "" {
			add(item, "advantage")
		}
	}
	for _, item := range candidates {
		add(item, "alternative")
	}
	result["alternatives"] = selected
	result["alternatives_considered"] = len(rows)
}
