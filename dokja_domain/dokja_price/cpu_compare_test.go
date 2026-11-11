package price

import (
	"context"
	"fmt"
	"slices"
	"testing"
	"time"
)

func TestCPUComparisonUsesBenchmarkEvidenceAcrossPlatforms(t *testing.T) {
	original := Product{Specifications: map[string]any{
		"cores": 8.0, "threads": 16.0, "socket": "S1",
		"benchmark": "example-suite-v1-multicore", "performance_score": 100.0,
	}}
	tests := []struct {
		name           string
		specs          map[string]any
		classification string
	}{
		{"same counts", map[string]any{"cores": 8.0, "threads": 16.0, "socket": "S2"}, "similar_cpu_specs"},
		{"different counts", map[string]any{"cores": 12.0, "threads": 20.0, "socket": "S2"}, "same_category_candidate"},
		{"comparable benchmark", map[string]any{"socket": "S2", "benchmark": "example-suite-v1-multicore", "performance_score": 95.0}, "technically_comparable"},
		{"superior benchmark", map[string]any{"socket": "S2", "benchmark": "example-suite-v1-multicore", "performance_score": 115.0}, "technically_superior"},
		{"lower benchmark", map[string]any{"cores": 8.0, "threads": 16.0, "socket": "S2", "benchmark": "example-suite-v1-multicore", "performance_score": 80.0}, "same_category_candidate"},
		{"different benchmarks", map[string]any{"socket": "S2", "benchmark": "other-suite", "performance_score": 200.0}, "same_category_candidate"},
		{"unknown benchmark", map[string]any{"socket": "S2", "performance_score": 200.0}, "same_category_candidate"},
		{"missing specs", nil, "same_category_candidate"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			classification, _, reasons := compareCPUs(original, Product{Specifications: test.specs}, []string{"same_category"})
			if classification != test.classification {
				t.Fatalf("classification %s, want %s", classification, test.classification)
			}
			if test.specs != nil && !slices.Contains(reasons, "different_cpu_socket") {
				t.Fatalf("missing platform change: %v", reasons)
			}
		})
	}
}

func TestCPUComparisonKeepsCompetingBrandVisible(t *testing.T) {
	repo := NewMemoryRepository()
	for i := 0; i < 8; i++ {
		brand := "brand-a"
		if i == 7 {
			brand = "brand-b"
		}
		_, err := repo.Upsert(context.Background(), Observation{
			Product: fmt.Sprintf("cpu-%d", i), Title: fmt.Sprintf("CPU Example %d", i),
			Category: "cpu", Brand: brand, Merchant: "store-a", Series: SeriesMerchant, Kind: KindPix,
			Points: []Point{{On: day(0), Cents: int64(10000 + i*100)}},
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	domain := New(repo).WithClock(func() time.Time { return today })
	result, err := domain.CompareProduct(context.Background(), Compare{Query: "CPU Example 0"})
	if err != nil || len(result.Alternatives) != 5 {
		t.Fatalf("comparison %+v, error %v", result, err)
	}
	if last := result.Alternatives[4]; last.Product.Brand != "brand-b" || last.Classification != "same_category_candidate" {
		t.Fatalf("missing competing candidate: %+v", result.Alternatives)
	}
}
