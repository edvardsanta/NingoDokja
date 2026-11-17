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

func TestShortlistAcceptsSameCoresThreadsCandidates(t *testing.T) {
	// Without a price.technical.v1 provider, benchmark-backed classes never fire;
	// same cores and threads extracted from the listings are the grounded signal.
	result := map[string]any{
		"product":  map[string]any{"title": "Processador AMD Ryzen 5 5600XT, 6 Núcleos, 12 Threads"},
		"decision": map[string]any{"selected_cents": 105999.0},
		"alternatives": []any{
			map[string]any{"product": "intel-peer", "title": "Processador Intel Core i5 12400F, 6 Cores, 12 Threads", "brand": "intel", "price_cents": 104999.0, "classification": "similar_cpu_specs"},
			map[string]any{"product": "ungrounded", "title": "Processador Example", "price_cents": 50000.0, "classification": "same_category_candidate"},
		},
	}
	ShortlistAlternatives(result)
	got := result["alternatives"].([]any)
	if len(got) == 0 || got[0].(map[string]any)["product"] != "intel-peer" {
		t.Fatalf("a same-cores-threads candidate was rejected: %v", got)
	}
	if len(got) != 1 {
		t.Fatalf("an ungrounded candidate was promoted: %v", got)
	}
}
