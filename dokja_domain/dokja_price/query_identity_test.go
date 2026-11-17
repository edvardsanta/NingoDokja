package price

import (
	"context"
	"testing"
	"time"
)

func TestCompareModelIdentityIgnoresPollutedCategoryAndNumericPrefixes(t *testing.T) {
	repo := NewMemoryRepository()
	for _, p := range []struct {
		key, title string
		cents      int64
	}{
		{"wrong", "GPU RX 550", 61990},
		{"wrong-family", "GPU GT 7600", 70000},
		{"prefix", "GPU RX 76000", 90000},
		{"suffix", "GPU RX 7600XT", 110000},
		{"wanted", "GPU RX 7600 Challenger", 189999},
	} {
		_, err := repo.Upsert(context.Background(), Observation{
			Product: p.key, Title: p.title, Category: "RX 7600", PartNumber: "EXAMPLE-" + p.key,
			Merchant: "store-a", Series: SeriesMerchant, Kind: KindPix, Points: []Point{{On: day(0), Cents: p.cents}},
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	domain := New(repo).WithClock(func() time.Time { return today })
	for _, query := range []string{"RX 7600", "RX7600", "rx-7600"} {
		got, err := domain.CompareProduct(context.Background(), Compare{Query: query})
		if err != nil || !got.Found || got.Product.Key != "wanted" {
			t.Fatalf("query %q selected wrong model: %+v, error %v", query, got, err)
		}
		if got.Profile.ID != "gpu" {
			t.Fatalf("model-family category lost the GPU profile: %+v", got.Profile)
		}
	}
}

func TestCompareFindsAVariantOnlyModelByItsBaseNumber(t *testing.T) {
	repo := NewMemoryRepository()
	_, err := repo.Upsert(context.Background(), Observation{
		Product: "5600xt", Title: "Processador AMD Ryzen 5 5600XT, Cache 32MB, 6 Núcleos, AM4",
		Category: "ryzen 5 5600", PartNumber: "100-100001585BOX",
		Merchant: "kabum", Series: SeriesMerchant, Kind: KindPix,
		Points: []Point{{On: day(0), Cents: 105999}},
	})
	if err != nil {
		t.Fatal(err)
	}
	domain := New(repo).WithClock(func() time.Time { return today })
	got, err := domain.CompareProduct(context.Background(), Compare{Query: "ryzen 5 5600"})
	if err != nil || !got.Found || got.Product.Key != "5600xt" {
		t.Fatalf("a base model must find its variant listings: %+v, error %v", got, err)
	}
	// The strict matcher never matches the variant for the base model on its own.
	product := Product{Key: "5600xt", Title: "Processador AMD Ryzen 5 5600XT, Cache 32MB, AM4"}
	if comparisonMatches(product, "ryzen 5 5600") {
		t.Fatal("the strict matcher must keep base and variant apart")
	}
	// False prefixes stay rejected: a longer number is a different model.
	if comparisonMatchesVariant(product, "ryzen 5 56000") {
		t.Fatal("a digit token must not match a title token whose continuation is not purely alphabetic")
	}
}
