package price

import "testing"

func TestShortlistKeepsDistinctOptionsAndACompetingBrand(t *testing.T) {
	item := func(id, brand, socket, relationship string, price float64) map[string]any {
		return map[string]any{
			"product": id, "brand": brand, "price_cents": price,
			"specifications":   map[string]any{"socket": socket},
			"model_assessment": map[string]any{"relationship": relationship},
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
	if len(selected) != 3 {
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
		map[string]any{"product": "one", "price_cents": 10000.0},
		map[string]any{"product": "two", "price_cents": 12000.0},
	}}
	ShortlistAlternatives(result)
	selected := result["alternatives"].([]any)
	if len(selected) != 2 || selected[0].(map[string]any)["product"] == selected[1].(map[string]any)["product"] {
		t.Fatal(selected)
	}
}
