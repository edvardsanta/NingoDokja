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

func TestGenericComparisonPrefersTheNamedProductOverCheaperMentions(t *testing.T) {
	repo := NewMemoryRepository()
	for _, o := range []Observation{
		{Product: "mug", Title: "Caneca Red Dead Redemption (Mod.2)", Merchant: "store-a", Series: SeriesMerchant, Kind: KindPix, Points: []Point{{On: day(0), Cents: 3591}}},
		{Product: "game", Title: "Red Dead Redemption 2 Ps4 Midia Fisica", Merchant: "store-b", Series: SeriesMerchant, Kind: KindPix, Points: []Point{{On: day(0), Cents: 26700}}},
		{Product: "game", Title: "Red Dead Redemption 2", Merchant: "steam", Series: SeriesMerchant, Kind: KindUnknown, Points: []Point{{On: day(0), Cents: 29990}}},
	} {
		if _, err := repo.Upsert(context.Background(), o); err != nil {
			t.Fatal(err)
		}
	}
	domain := New(repo).WithClock(func() time.Time { return today })
	result, err := domain.CompareProduct(context.Background(), Compare{Query: "red dead redemption 2"})
	if err != nil || !result.Found {
		t.Fatalf("comparison failed: %+v %v", result, err)
	}
	if result.Product.Key != "game" && result.Product.Key != "game-pc" {
		t.Fatalf("the mug must not lead a game query, got %q", result.Product.Key)
	}
	merchants := map[string]bool{}
	for _, offer := range result.Offers {
		merchants[offer.Assessment.Key.Merchant] = true
	}
	if !merchants["steam"] {
		t.Fatalf("the game reference must carry the steam offer: %+v", result.Offers)
	}
}

func TestGenericComparisonKeepsPriceOrderWhenNoTitleLeadsWithTheQuery(t *testing.T) {
	repo := NewMemoryRepository()
	for _, o := range []Observation{
		{Product: "cheap-mention", Title: "Caneca Red Dead Redemption (Mod.2)", Merchant: "store-a", Series: SeriesMerchant, Kind: KindPix, Points: []Point{{On: day(0), Cents: 3591}}},
		{Product: "other-mention", Title: "Poster Red Dead Redemption 2", Merchant: "store-b", Series: SeriesMerchant, Kind: KindPix, Points: []Point{{On: day(0), Cents: 9000}}},
	} {
		if _, err := repo.Upsert(context.Background(), o); err != nil {
			t.Fatal(err)
		}
	}
	domain := New(repo).WithClock(func() time.Time { return today })
	result, err := domain.CompareProduct(context.Background(), Compare{Query: "red dead redemption 2"})
	if err != nil || !result.Found {
		t.Fatalf("comparison failed: %+v %v", result, err)
	}
	if result.Product.Key != "cheap-mention" {
		t.Fatalf("with no title leading, the cheapest current quote leads as before, got %q", result.Product.Key)
	}
}
