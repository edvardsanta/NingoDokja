package price

import (
	"context"
	"testing"
	"time"
)

func TestGenericComparisonChoosesCurrentVariantAndKeepsOtherVariants(t *testing.T) {
	repo := NewMemoryRepository()
	for _, o := range []Observation{
		{Product: "variant-old", Title: "GPU Example Challenger Old", Category: "gpu", Merchant: "store-a", Series: SeriesMerchant, Kind: KindPix, Points: []Point{{On: day(-10), Cents: 100000}}},
		{Product: "variant-current", Title: "GPU Example Challenger", Category: "gpu", Merchant: "store-b", Series: SeriesMerchant, Kind: KindPix, Points: []Point{{On: day(0), Cents: 190000}}},
		{Product: "variant-oc", Title: "GPU Example Challenger OC", Category: "gpu", Merchant: "store-c", Series: SeriesMerchant, Kind: KindPix, Points: []Point{{On: day(0), Cents: 200000}}},
	} {
		if _, err := repo.Upsert(context.Background(), o); err != nil {
			t.Fatal(err)
		}
	}
	domain := New(repo).WithClock(func() time.Time { return today })
	result, err := domain.CompareProduct(context.Background(), Compare{Query: "GPU Example Challenger", BudgetCents: 200000})
	if err != nil || !result.Found || result.Product.Key != "variant-current" || result.ReferenceMatches != 3 {
		t.Fatalf("reference %+v error %v", result, err)
	}
	if len(result.Alternatives) != 2 || len(result.Candidates) != 0 {
		t.Fatalf("comparison interrupted: %+v", result)
	}
	if result.Map()["reference_selection"] == nil {
		t.Fatal("choice was not explained")
	}
}
