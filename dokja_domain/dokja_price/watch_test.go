package price

import (
	"context"
	"fmt"
	"reflect"
	"testing"
	"time"
)

func TestAnObservationCarriesItsDescriptionAndOfferPage(t *testing.T) {
	text := body(t, []map[string]any{point(day(0), "10")}, func(m map[string]any) {
		m["category"], m["brand"], m["part_number"], m["url"] = " SSD  SATA ", "Brandx", "AB-1000X1", "https://store.example/p/1"
	})
	got, err := ParseObservation(text, today)
	if err != nil {
		t.Fatal(err)
	}
	if got.Category != "SSD SATA" || got.Brand != "Brandx" || got.PartNumber != "AB-1000X1" || got.URL != "https://store.example/p/1" {
		t.Fatalf("observation %+v", got)
	}
	for _, bad := range []string{"ftp://store.example/p", "/relative", "https://user:secret@store.example/p", "https://"} {
		text := body(t, []map[string]any{point(day(0), "10")}, func(m map[string]any) { m["url"] = bad })
		if _, err := ParseObservation(text, today); err == nil {
			t.Errorf("url %q accepted", bad)
		}
	}
}

func describedObservation(product, merchant, brand, address string, cents int64) Observation {
	return Observation{
		Product: product, Title: "SSD " + product, Merchant: merchant, Series: SeriesMerchant, Kind: KindPix,
		Category: "ssd sata", Brand: brand, URL: address, Points: []Point{{On: day(0), Cents: cents}},
	}
}

func TestTheRepositoryKeepsTheLatestNonEmptyDescriptionAndPagePerOffer(t *testing.T) {
	repo := NewMemoryRepository()
	ctx := context.Background()
	repo.Upsert(ctx, describedObservation("ab-1000x1", "store-a", "brandx", "https://a.example/1", 100))
	later := describedObservation("ab-1000x1", "store-a", "", "", 90)
	later.Category = ""
	repo.Upsert(ctx, later)
	repo.Upsert(ctx, describedObservation("ab-1000x1", "store-b", "brandx", "https://b.example/1", 95))

	products, _ := repo.Products(ctx)
	if len(products) != 1 || products[0].Brand != "brandx" || products[0].Category != "ssd sata" {
		t.Fatalf("an empty description must not erase a known one: %+v", products)
	}
	urls, _ := repo.OfferURLs(ctx, "ab-1000x1")
	if len(urls) != 2 || urls[Key{Product: "ab-1000x1", Merchant: "store-b", Series: SeriesMerchant, Kind: KindPix}] != "https://b.example/1" {
		t.Fatalf("urls %v", urls)
	}
}

func TestSearchFiltersByStoreAndBrandAndHandsBackPages(t *testing.T) {
	repo := NewMemoryRepository()
	ctx := context.Background()
	repo.Upsert(ctx, describedObservation("ssd-a", "store-a", "brandx", "https://a.example/a", 100))
	repo.Upsert(ctx, describedObservation("ssd-a", "store-b", "brandx", "https://b.example/a", 90))
	repo.Upsert(ctx, describedObservation("ssd-b", "store-b", "brandy", "https://b.example/b", 80))
	// a product with no brand from its collector still matches by its title
	untagged := describedObservation("ssd-c", "store-a", "", "", 70)
	untagged.Title = "SSD Brandx Old"
	repo.Upsert(ctx, untagged)
	domain := New(repo).WithClock(func() time.Time { return today })

	got, err := domain.SearchProducts(ctx, Search{Query: "ssd", Brand: "brandx"})
	if err != nil {
		t.Fatal(err)
	}
	keys := []string{}
	for _, product := range got.Products {
		keys = append(keys, product.Product.Key)
	}
	if !reflect.DeepEqual(keys, []string{"ssd-a", "ssd-c"}) || !reflect.DeepEqual(got.Merchants, []string{"store-a", "store-b"}) {
		t.Fatalf("keys %v merchants %v", keys, got.Merchants)
	}

	onlyB, _ := domain.SearchProducts(ctx, Search{Query: "ssd", Merchant: "store-b"})
	if onlyB.Matched != 2 || len(onlyB.Products[0].Assessments) != 1 || onlyB.Products[0].Assessments[0].Key.Merchant != "store-b" {
		t.Fatalf("store filter %+v", onlyB)
	}
	mapped := onlyB.Map()["products"].([]any)[0].(map[string]any)
	row := mapped["assessments"].([]any)[0].(map[string]any)
	if row["url"] != "https://b.example/a" || mapped["brand"] != "brandx" || mapped["category"] != "ssd sata" {
		t.Fatalf("mapped %v", mapped)
	}
	if _, err := domain.SearchProducts(ctx, Search{Merchant: "Not A Key"}); err == nil {
		t.Fatal("a bad merchant key was accepted")
	}
}

func searchAs(t *testing.T, domain *Domain, source string, owner bool, payload map[string]any) map[string]any {
	t.Helper()
	got, err := domain.Handle(context.Background(), Request{Action: ActionSearchPrices, Event: Event{
		Source: source, Payload: payload, Context: map[string]any{"is_owner": owner},
	}})
	if err != nil {
		t.Fatal(err)
	}
	return got
}

func TestOnlyTheOwnerGrowsTheWatchedSearches(t *testing.T) {
	domain := New(NewMemoryRepository()).WithClock(func() time.Time { return today })
	status := func(got map[string]any) any {
		watch, _ := got["watch"].(map[string]any)
		return watch["status"]
	}

	if got := searchAs(t, domain, "discord", false, map[string]any{"watch": true, "query": "SSD Sata", "brand": "Brandx"}); status(got) != "not_owner" {
		t.Fatalf("a guest grew the list: %v", got["watch"])
	}
	if got := searchAs(t, domain, "discord", true, map[string]any{"watch": true, "query": "SSD Sata", "brand": "Brandx"}); status(got) != "added" {
		t.Fatalf("the owner did not grow the list: %v", got["watch"])
	}
	if got := searchAs(t, domain, "discord", true, map[string]any{"watch": true, "query": "ssd   SATA", "brand": "brandx"}); status(got) != "watched" {
		t.Fatalf("the same search, written differently, was added twice: %v", got["watch"])
	}
	if got := searchAs(t, domain, "desktop", false, map[string]any{"watch": true, "query": "hd", "brand": ""}); status(got) != "added" {
		t.Fatalf("the operator at the Desktop may grow the list: %v", got["watch"])
	}
	if got := searchAs(t, domain, "discord", true, map[string]any{"watch": true, "brand": "brandz"}); got["watch"] != nil {
		t.Fatalf("a brand without a category is not a store query: %v", got["watch"])
	}

	list, _ := domain.Handle(context.Background(), Request{Action: ActionListWatches, Event: Event{}})
	want := []any{map[string]any{"category": "hd", "brand": ""}, map[string]any{"category": "ssd sata", "brand": "brandx"}}
	if !reflect.DeepEqual(list["watches"], want) {
		t.Fatalf("watches %v", list["watches"])
	}

	if _, err := domain.Handle(context.Background(), Request{Action: ActionUnwatch, Event: Event{Source: "discord",
		Payload: map[string]any{"category": "hd"}}}); err == nil {
		t.Fatal("a guest removed a watched search")
	}
	removed, err := domain.Handle(context.Background(), Request{Action: ActionUnwatch, Event: Event{Source: "cli",
		Payload: map[string]any{"category": "HD"}}})
	if err != nil || removed["removed"] != true {
		t.Fatalf("removed %v %v", removed, err)
	}
}

func TestTheWatchedSearchesStopAtTheirLimit(t *testing.T) {
	repo := NewMemoryRepository()
	for i := 0; i < maxWatches; i++ {
		repo.AddWatch(context.Background(), Watch{Category: fmt.Sprintf("thing %d", i)})
	}
	domain := New(repo).WithClock(func() time.Time { return today })
	got := searchAs(t, domain, "cli", false, map[string]any{"watch": true, "query": "one more"})
	if got["watch"].(map[string]any)["status"] != "full" {
		t.Fatalf("watch %v", got["watch"])
	}
}

func TestTheCollectorIsToldWhichProductsLackAMarketHistory(t *testing.T) {
	repo := NewMemoryRepository()
	ctx := context.Background()
	repo.Upsert(ctx, describedObservation("ssd-a", "store-a", "brandx", "", 100))
	repo.Upsert(ctx, describedObservation("ssd-b", "store-a", "brandx", "", 100))
	market := describedObservation("ssd-b", "aggregator", "", "", 90)
	market.Series, market.Kind = SeriesMarketMin, KindUnknown
	repo.Upsert(ctx, market)
	repo.Upsert(ctx, describedObservation("ssd-c", "store-a", "brandx", "", 100))
	domain := New(repo)

	missing, err := domain.MissingMarket(ctx, 5)
	if err != nil || len(missing) != 2 || missing[0].Key != "ssd-a" || missing[1].Key != "ssd-c" {
		t.Fatalf("missing %+v %v", missing, err)
	}
	if one, _ := domain.MissingMarket(ctx, 1); len(one) != 1 {
		t.Fatalf("limit ignored: %+v", one)
	}
	repo.AddWatch(ctx, Watch{Category: "ssd"})
	if watches, _ := domain.Watches(ctx); len(watches) != 1 {
		t.Fatalf("watches %+v", watches)
	}
}

func TestASearchWithoutAskingToWatchLeavesTheListAlone(t *testing.T) {
	domain := New(NewMemoryRepository()).WithClock(func() time.Time { return today })
	got := searchAs(t, domain, "desktop", false, map[string]any{"query": "ss"})
	if got["watch"] != nil {
		t.Fatalf("a search typed on the way added a watch: %v", got["watch"])
	}
	list, _ := domain.Handle(context.Background(), Request{Action: ActionListWatches, Event: Event{}})
	if list["count"] != 0 {
		t.Fatalf("watches %v", list)
	}
}

func TestTheCollectorIsGivenTheKnownMarketPagesStalestFirst(t *testing.T) {
	repo := NewMemoryRepository()
	ctx := context.Background()
	market := func(product, page, on string) {
		o := describedObservation(product, "aggregator", "", page, 100)
		o.Series, o.Kind, o.Points = SeriesMarketMin, KindUnknown, []Point{{On: on, Cents: 100}}
		repo.Upsert(ctx, o)
	}
	market("ssd-fresh", "https://market.example/fresh", day(-1))
	market("ssd-stale", "https://market.example/stale", day(-9))
	market("ssd-no-page", "", day(-3))
	repo.Upsert(ctx, describedObservation("ssd-store-only", "store-a", "", "https://store.example/p", 100))
	domain := New(repo)

	pages, err := domain.MarketPages(ctx, 5)
	if err != nil {
		t.Fatal(err)
	}
	got := []string{}
	for _, page := range pages {
		got = append(got, page.Product.Key+"="+page.Page)
	}
	want := []string{"ssd-stale=https://market.example/stale", "ssd-fresh=https://market.example/fresh"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("pages %v, want %v", got, want)
	}
	if one, _ := domain.MarketPages(ctx, 1); len(one) != 1 || one[0].Product.Key != "ssd-stale" {
		t.Fatalf("limit %+v", one)
	}
}
