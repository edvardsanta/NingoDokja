package price

import (
	"context"
	"reflect"
	"testing"
	"time"
)

// history writes daily points ending today for one product at one merchant.
func history(t *testing.T, repo *MemoryRepository, product string, reais ...int64) {
	t.Helper()
	points := series(reais...)
	if _, err := repo.Upsert(context.Background(), Observation{
		Product: product, Title: "SSD " + product, Merchant: "store-a", Series: SeriesMerchant, Kind: KindPix, Points: points,
	}); err != nil {
		t.Fatal(err)
	}
}

func TestSearchByOpportunityPutsRealDiscountsFirstAndWarningsLast(t *testing.T) {
	repo := NewMemoryRepository()
	history(t, repo, "a-plain", append(flat(60, 500), 495)...)                            // 1% under: ordinary
	history(t, repo, "b-deep", append(flat(60, 500), 350)...)                             // 30% under: real, deepest
	history(t, repo, "c-real", append(flat(60, 500), 440)...)                             // 12% under: real
	history(t, repo, "d-new", 900)                                                        // one day: still gathering
	history(t, repo, "e-markup", append(append(flat(80, 500), flat(10, 650)...), 495)...) // markup
	history(t, repo, "f-new-cheap", 100)                                                  // one day, cheaper
	domain := New(repo).WithClock(func() time.Time { return today })

	got, err := domain.SearchProducts(context.Background(), Search{Query: "ssd", Order: OrderOpportunity})
	if err != nil {
		t.Fatal(err)
	}
	keys := []string{}
	for _, product := range got.Products {
		keys = append(keys, product.Product.Key)
	}
	want := []string{"b-deep", "c-real", "a-plain", "f-new-cheap", "d-new", "e-markup"}
	if !reflect.DeepEqual(keys, want) {
		t.Fatalf("order %v, want %v", keys, want)
	}

	plain, _ := domain.SearchProducts(context.Background(), Search{Query: "ssd"})
	if plain.Products[0].Product.Key != "a-plain" {
		t.Fatalf("without an order the answer stays in key order: %s", plain.Products[0].Product.Key)
	}
	if _, err := domain.SearchProducts(context.Background(), Search{Order: "price"}); err == nil {
		t.Fatal("an unknown order was accepted")
	}
}

func TestRankingLooksPastTheFirstPage(t *testing.T) {
	repo := NewMemoryRepository()
	for i := 0; i < maxSearchProducts+2; i++ {
		history(t, repo, "p"+string(rune('a'+i)), append(flat(60, 500), 498)...)
	}
	history(t, repo, "z-real", append(flat(60, 500), 300)...)
	domain := New(repo).WithClock(func() time.Time { return today })
	got, _ := domain.SearchProducts(context.Background(), Search{Order: OrderOpportunity})
	if got.Products[0].Product.Key != "z-real" || got.Matched != maxSearchProducts+3 {
		t.Fatalf("first %s matched %d", got.Products[0].Product.Key, got.Matched)
	}
}

func TestTheMerchantsAreListedForAPersonToPickAStore(t *testing.T) {
	repo := NewMemoryRepository()
	ctx := context.Background()
	repo.Upsert(ctx, describedObservation("p", "store-b", "", "", 1))
	repo.Upsert(ctx, describedObservation("p", "store-a", "", "", 1))
	got, err := New(repo).Handle(ctx, Request{Action: ActionListMerchants, Event: Event{}})
	if err != nil || !reflect.DeepEqual(got["merchants"], []any{"store-a", "store-b"}) {
		t.Fatalf("merchants %v %v", got, err)
	}
	ordered, err := New(repo).WithClock(func() time.Time { return today }).Handle(ctx, Request{Action: ActionSearchPrices,
		Event: Event{Payload: map[string]any{"order": "opportunity"}}})
	if err != nil || ordered["matched"] != 1 {
		t.Fatalf("ordered %v %v", ordered, err)
	}
}
