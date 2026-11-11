package price

import "testing"

func TestMissingTechnicalReviewKeepsChipVariantsAndRejectsUnrelatedCheapCards(t *testing.T) {
	result := map[string]any{
		"product":  map[string]any{"title": "GPU RX 7600 Challenger"},
		"decision": map[string]any{"selected_cents": 189999.0},
		"alternatives": []any{
			map[string]any{"product": "old-cheap", "title": "GPU RX 550", "price_cents": 61990.0, "classification": "same_category_candidate"},
			map[string]any{"product": "same-chip", "title": "GPU RX7600 Fighter", "price_cents": 199999.0, "classification": "same_category_candidate"},
			map[string]any{"product": "older-tier", "title": "GPU RTX 3060 Ti", "price_cents": 210999.0, "classification": "same_category_candidate"},
			map[string]any{"product": "other-model", "title": "GPU RX 7600 XT", "price_cents": 250000.0, "classification": "same_category_candidate"},
		},
	}
	ShortlistAlternatives(result)
	got := result["alternatives"].([]any)
	if len(got) != 1 || got[0].(map[string]any)["product"] != "same-chip" || got[0].(map[string]any)["classification"] != "same_model_variant" {
		t.Fatalf("unreviewed products promoted: %v", got)
	}
}

func TestGPUModelIdentityKeepsPerformanceAndMobileSuffixesDistinct(t *testing.T) {
	for _, pair := range [][2]string{{"RTX 4070 Ti", "RTX 4070 Ti Super"}, {"RX 7900", "RX 7900 GRE"}, {"RX 7600", "RX 7600 M"}} {
		if gpuModel(pair[0]) == gpuModel(pair[1]) {
			t.Fatalf("distinct models grouped: %v", pair)
		}
	}
}
