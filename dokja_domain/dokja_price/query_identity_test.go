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
