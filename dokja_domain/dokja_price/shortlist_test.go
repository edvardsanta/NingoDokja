package price

import "testing"

func TestShortlistKeepsDistinctOptionsAndACompetingBrand(t *testing.T) {
	item := func(id, brand, socket, relationship string, price float64) map[string]any {
		return map[string]any{
			"product": id, "brand": brand, "price_cents": price,
			"specifications":   map[string]any{"socket": socket},
			"model_assessment": map[string]any{"relationship": relationship, "market_role": "direct_peer"},
		}
	}
	result := map[string]any{
		"product":  map[string]any{"brand": "brand-a", "specifications": map[string]any{"socket": "s1"}},
		"decision": map[string]any{"selected_cents": 170000.0, "budget_cents": 200000.0},
		"alternatives": []any{
			item("old-cheap", "brand-a", "s0", "inferior", 90000),
			item("close", "brand-a", "s1", "comparable", 180000),
			item("economy", "brand-a", "s1", "inferior", 130000),
			item("other-brand", "brand-b", "s2", "comparable", 190000),
			item("expensive", "brand-a", "s1", "superior", 350000),
		},
	}
	ShortlistAlternatives(result)
	selected := result["alternatives"].([]any)
	if len(selected) != 5 {
		t.Fatalf("shortlist %v", selected)
	}
	for i, want := range []string{"close", "economy", "other-brand"} {
		if selected[i].(map[string]any)["product"] != want {
			t.Fatalf("shortlist %v", selected)
		}
	}
	if result["alternatives_considered"] != 5 {
		t.Fatal(result)
	}
}

func TestShortlistSupportsMissingReviewsAndDoesNotDuplicateCandidates(t *testing.T) {
	result := map[string]any{"alternatives": []any{
		map[string]any{"product": "one", "price_cents": 10000.0, "classification": "technically_comparable"},
		map[string]any{"product": "two", "price_cents": 12000.0, "classification": "technically_comparable"},
	}}
	ShortlistAlternatives(result)
	selected := result["alternatives"].([]any)
	if len(selected) != 2 || selected[0].(map[string]any)["product"] == selected[1].(map[string]any)["product"] {
		t.Fatal(selected)
	}
}

func TestAlternativesKeepAllPeersAndExcludeOlderHigherTierOptions(t *testing.T) {
	rows := []any{}
	for _, id := range []string{"variant-a", "variant-b", "peer-a", "peer-b", "peer-c", "old-higher-tier"} {
		role := "direct_peer"
		if id == "old-higher-tier" {
			role = "older_option"
		}
		rows = append(rows, map[string]any{"product": id, "price_cents": 150000.0,
			"model_assessment": map[string]any{"relationship": "superior", "market_role": role}})
	}
	result := map[string]any{"alternatives": rows}
	ShortlistAlternatives(result)
	selected := result["alternatives"].([]any)
	if len(selected) != 5 {
		t.Fatalf("eligible peers were hidden: %v", selected)
	}
	for _, raw := range selected {
		if raw.(map[string]any)["product"] == "old-higher-tier" {
			t.Fatalf("older generation promoted: %v", selected)
		}
	}
}
