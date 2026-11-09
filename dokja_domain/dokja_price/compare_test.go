package price

import (
	"context"
	"reflect"
	"strings"
	"testing"
	"time"
)

func compareRepo(t *testing.T) *Domain {
	t.Helper()
	repo := NewMemoryRepository()
	ctx := context.Background()
	add := func(product, title, part, merchant string, series Series, kind Kind, cents int64, url string) {
		repo.Upsert(ctx, Observation{Product: product, Title: title, PartNumber: part, Merchant: merchant, Series: series, Kind: kind,
			URL: url, Points: []Point{{On: day(0), Cents: cents}}})
	}
	add("pn:ab-1000x1", "SSD Example 1TB", "AB-1000X1", "store-a", SeriesMerchant, KindPix, 1685_25, "https://a.example/1")
	add("pn:ab-1000x1", "SSD Example 1TB", "AB-1000X1", "store-a", SeriesMerchant, KindCard, 1872_50, "https://a.example/1")
	add("pn:ab-1000x1", "SSD Example 1TB", "AB-1000X1", "store-b", SeriesMerchant, KindUnknown, 1599_00, "https://b.example/1")
	add("pn:ab-1000x1", "SSD Example 1TB", "AB-1000X1", "aggregator", SeriesMarketMin, KindUnknown, 1500_00, "https://m.example/1")
	add("pn:ab-2000x1", "SSD Example 2TB", "AB-2000X1", "store-a", SeriesMerchant, KindPix, 2900_00, "")
	return New(repo).WithClock(func() time.Time { return today })
}

func TestCompareFindsAProductByPartNumberAndListsItsOffersCheapestFirst(t *testing.T) {
	domain := compareRepo(t)
	got, err := domain.CompareProduct(context.Background(), Compare{PartNumber: "ab1000x1/xy"})
	if err != nil || !got.Found || got.Product.Key != "pn:ab-1000x1" {
		t.Fatalf("got %+v %v", got, err)
	}
	order := []string{}
	for _, offer := range got.Offers {
		order = append(order, offer.Assessment.Key.Merchant+"/"+string(offer.Assessment.Key.Kind))
	}
	// the market low is a reference and goes last, whatever its price
	want := []string{"store-b/unknown", "store-a/pix", "store-a/card", "aggregator/unknown"}
	if !reflect.DeepEqual(order, want) {
		t.Fatalf("order %v, want %v", order, want)
	}
	if got.Offers[0].URL != "https://b.example/1" {
		t.Fatalf("url %q", got.Offers[0].URL)
	}
}

func TestCompareByWordsPicksOneOrOffersCandidates(t *testing.T) {
	domain := compareRepo(t)
	one, _ := domain.CompareProduct(context.Background(), Compare{Query: "example 2tb"})
	if !one.Found || one.Product.Key != "pn:ab-2000x1" {
		t.Fatalf("one %+v", one)
	}
	several, _ := domain.CompareProduct(context.Background(), Compare{Query: "ssd example"})
	if several.Found || len(several.Candidates) != 2 {
		t.Fatalf("several %+v", several)
	}
	none, _ := domain.CompareProduct(context.Background(), Compare{Query: "rtx 5090"})
	if none.Found || len(none.Candidates) != 0 {
		t.Fatalf("none %+v", none)
	}
	if _, err := domain.CompareProduct(context.Background(), Compare{}); err == nil {
		t.Fatal("an empty comparison was accepted")
	}
}

func TestCompareActionSaysWhetherTheOwnerMayLookAMissingProductUp(t *testing.T) {
	domain := compareRepo(t)
	ask := func(owner bool, payload map[string]any) map[string]any {
		got, err := domain.Handle(context.Background(), Request{Action: ActionCompareProduct, Event: Event{
			Source: "discord", Payload: payload, Context: map[string]any{"is_owner": owner}}})
		if err != nil {
			t.Fatal(err)
		}
		return got
	}
	if got := ask(true, map[string]any{"query": "rtx 5090"}); got["found"] != false || got["growable"] != true {
		t.Fatalf("owner %v", got)
	}
	if got := ask(false, map[string]any{"query": "rtx 5090"}); got["growable"] != false {
		t.Fatalf("guest %v", got)
	}
	found := ask(false, map[string]any{"part_number": "AB-1000X1"})
	offers := found["offers"].([]any)
	if found["found"] != true || len(offers) != 4 || offers[0].(map[string]any)["url"] != "https://b.example/1" {
		t.Fatalf("found %v", found)
	}
	if _, err := domain.Handle(context.Background(), Request{Action: ActionCompareProduct, Event: Event{Payload: map[string]any{"query": 3}}}); err == nil {
		t.Fatal("a numeric query was accepted")
	}
}

func TestACutShortPartNumberNamingSeveralVariantsOffersThemAll(t *testing.T) {
	repo := NewMemoryRepository()
	ctx := context.Background()
	for _, part := range []string{"XY650SS-240GT-R", "XY650SS-240GT-CA"} {
		repo.Upsert(ctx, Observation{Product: "pn:" + strings.ToLower(part), Title: "SSD " + part, PartNumber: part, Merchant: "store-a",
			Series: SeriesMerchant, Kind: KindPix, Points: []Point{{On: day(0), Cents: 100}}})
	}
	domain := New(repo).WithClock(func() time.Time { return today })
	several, _ := domain.CompareProduct(ctx, Compare{PartNumber: "XY650SS"})
	if several.Found || len(several.Candidates) != 2 {
		t.Fatalf("several %+v", several)
	}
	exact, _ := domain.CompareProduct(ctx, Compare{PartNumber: "xy650ss-240gt-r"})
	if !exact.Found || exact.Product.PartNumber != "XY650SS-240GT-R" {
		t.Fatalf("exact %+v", exact)
	}
}
