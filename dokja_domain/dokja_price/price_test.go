package price

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"
)

var today = time.Date(2026, 11, 20, 12, 0, 0, 0, time.UTC)

func day(offset int) string { return today.AddDate(0, 0, offset).Format(dateLayout) }

// body builds a price.observation.v1 body; edit changes the decoded map before encoding.
func body(t *testing.T, points []map[string]any, edit func(map[string]any)) string {
	t.Helper()
	raw := map[string]any{
		"contract": ContractVersion, "product": "ssd-example-1tb", "title": "SSD Example 1TB",
		"merchant": "store-a", "merchant_name": "Store A", "series": "merchant",
		"price_kind": "pix", "currency": "BRL", "points": points,
	}
	if edit != nil {
		edit(raw)
	}
	encoded, err := json.Marshal(raw)
	if err != nil {
		t.Fatal(err)
	}
	return string(encoded)
}

func point(on string, price string) map[string]any {
	return map[string]any{"on": on, "price": json.Number(price)}
}

func TestParseObservationReadsTheContract(t *testing.T) {
	text := body(t, []map[string]any{
		{"on": day(-1), "price": json.Number("499.9"), "list_price": json.Number("899.00")},
		{"on": day(0), "price": json.Number("459"), "seller": "Store B"},
	}, nil)
	got, err := ParseObservation(text, today)
	if err != nil {
		t.Fatal(err)
	}
	want := Observation{
		Product: "ssd-example-1tb", Title: "SSD Example 1TB", Merchant: "store-a", MerchantName: "Store A",
		Series: SeriesMerchant, Kind: KindPix,
		Points: []Point{{On: day(-1), Cents: 49990, ListCents: 89900}, {On: day(0), Cents: 45900, Seller: "Store B"}},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v\nwant %+v", got, want)
	}
}

func TestParseObservationTellsOtherBodiesApart(t *testing.T) {
	for _, text := range []string{"Plain article text", `{"title": "other json"}`} {
		if _, err := ParseObservation(text, today); !errors.Is(err, ErrNotObservation) {
			t.Errorf("%q: got %v, want ErrNotObservation", text, err)
		}
	}
}

func TestParseObservationRefusesBrokenContracts(t *testing.T) {
	cases := map[string]string{
		"unknown field":   body(t, []map[string]any{point(day(0), "10")}, func(m map[string]any) { m["extra"] = 1 }),
		"bad product":     body(t, []map[string]any{point(day(0), "10")}, func(m map[string]any) { m["product"] = "Has Space" }),
		"bad series":      body(t, []map[string]any{point(day(0), "10")}, func(m map[string]any) { m["series"] = "average" }),
		"bad kind":        body(t, []map[string]any{point(day(0), "10")}, func(m map[string]any) { m["price_kind"] = "boleto" }),
		"other currency":  body(t, []map[string]any{point(day(0), "10")}, func(m map[string]any) { m["currency"] = "USD" }),
		"no title":        body(t, []map[string]any{point(day(0), "10")}, func(m map[string]any) { m["title"] = " " }),
		"no points":       body(t, []map[string]any{}, nil),
		"future date":     body(t, []map[string]any{point(day(2), "10")}, nil),
		"bad date":        body(t, []map[string]any{point("2026-02-30", "10")}, nil),
		"repeated date":   body(t, []map[string]any{point(day(0), "10"), point(day(0), "11")}, nil),
		"zero price":      body(t, []map[string]any{point(day(0), "0")}, nil),
		"many decimals":   body(t, []map[string]any{point(day(0), "10.0000000000000001")}, nil),
		"rounds to zero":  body(t, []map[string]any{point(day(0), "0.004")}, nil),
		"exponent":        body(t, []map[string]any{point(day(0), "1e3")}, nil),
		"negative":        body(t, []map[string]any{point(day(0), "-5")}, nil),
		"absurd price":    body(t, []map[string]any{point(day(0), "99999999999")}, nil),
		"trailing object": body(t, []map[string]any{point(day(0), "10")}, nil) + `{}`,
	}
	for name, text := range cases {
		_, err := ParseObservation(text, today)
		if err == nil || errors.Is(err, ErrNotObservation) {
			t.Errorf("%s: got %v, want a contract error", name, err)
		}
	}
}

func TestParseObservationRoundsFloatingPointNoiseToCents(t *testing.T) {
	// Regression: aggregator histories print prices such as 1665.6100000000001, and the
	// first strict parser refused a whole year of points because of one of them.
	text := body(t, []map[string]any{
		point(day(-2), "1665.6100000000001"), point(day(-1), "1909.2399999"), point(day(0), "10.005"),
	}, nil)
	got, err := ParseObservation(text, today)
	if err != nil {
		t.Fatal(err)
	}
	cents := []int64{got.Points[0].Cents, got.Points[1].Cents, got.Points[2].Cents}
	if !reflect.DeepEqual(cents, []int64{166561, 190924, 1001}) {
		t.Fatalf("cents %v", cents)
	}
}

func TestParseObservationAcceptsTomorrowForTimeZones(t *testing.T) {
	if _, err := ParseObservation(body(t, []map[string]any{point(day(1), "10")}, nil), today); err != nil {
		t.Fatal(err)
	}
}

func TestParseObservationCleansText(t *testing.T) {
	text := body(t, []map[string]any{point(day(0), "10")}, func(m map[string]any) {
		m["title"] = "  SSD\n‮Example\t1TB  "
	})
	got, err := ParseObservation(text, today)
	if err != nil {
		t.Fatal(err)
	}
	if got.Title != "SSD Example 1TB" {
		t.Fatalf("title %q", got.Title)
	}
}

func TestMemoryRepositoryUpsertIsIdempotentAndLatestWins(t *testing.T) {
	repo := NewMemoryRepository()
	ctx := context.Background()
	first := Observation{Product: "p", Title: "P", Merchant: "m", Series: SeriesMerchant, Kind: KindPix,
		Points: []Point{{On: day(-1), Cents: 100}, {On: day(0), Cents: 90}}}
	if written, _ := repo.Upsert(ctx, first); written != 2 {
		t.Fatalf("first write %d", written)
	}

	if written, _ := repo.Upsert(ctx, first); written != 0 {
		t.Fatalf("repeated write %d", written)
	}

	later := first
	later.Points = []Point{{On: day(0), Cents: 80}}
	if written, _ := repo.Upsert(ctx, later); written != 1 {
		t.Fatalf("later write %d", written)
	}
	history, _ := repo.History(ctx, first.Key(), "", "")
	if len(history) != 2 || history[0].On != day(-1) || history[1].Cents != 80 {
		t.Fatalf("history %+v", history)
	}
	card := first
	card.Kind = KindCard
	repo.Upsert(ctx, card)
	keys, _ := repo.Keys(ctx, "p")
	if len(keys) != 2 || keys[0].Kind != KindCard || keys[1].Kind != KindPix {
		t.Fatalf("keys %+v", keys)
	}
	if bounded, _ := repo.History(ctx, first.Key(), day(0), day(0)); len(bounded) != 1 {
		t.Fatalf("bounded %+v", bounded)
	}
}

func TestMemoryRepositoryScopesDecisionMemoryByUserAndProduct(t *testing.T) {
	repo := NewMemoryRepository()
	ctx := context.Background()
	for _, item := range []DecisionMemory{
		{ID: "one", UserID: "u1", Product: Product{Key: "pn:a"}},
		{ID: "two", UserID: "u2", Product: Product{Key: "pn:a"}},
		{ID: "three", UserID: "u1", Product: Product{Key: "pn:b"}},
		{ID: "four", UserID: "u1", Product: Product{Key: "pn:a"}},
	} {
		if err := repo.SaveDecision(ctx, item); err != nil {
			t.Fatal(err)
		}
	}
	got, err := repo.RecentDecisions(ctx, "u1", "pn:a", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].ID != "four" || got[1].ID != "one" {
		t.Fatalf("got %+v", got)
	}
}

// series builds daily points ending today. prices[i] is in reais, oldest first.
func series(prices ...int64) []Point {
	points := make([]Point, len(prices))
	for i, reais := range prices {
		points[i] = Point{On: day(i - len(prices) + 1), Cents: reais * 100}
	}
	return points
}

func flat(days int, reais int64) []int64 {
	values := make([]int64, days)
	for i := range values {
		values[i] = reais
	}
	return values
}

func assess(points []Point) Assessment {
	return Assess(Key{Product: "p", Merchant: "m", Series: SeriesMerchant, Kind: KindPix}, points, today)
}

func TestAssessNeedsHistory(t *testing.T) {
	if got := assess(series(append(flat(5, 500), 400)...)); got.Verdict != VerdictInsufficientHistory {
		t.Fatalf("verdict %s", got.Verdict)
	}
	if got := assess(nil); got.Verdict != VerdictInsufficientHistory {
		t.Fatalf("empty verdict %s", got.Verdict)
	}
}

func TestAssessFindsARealDiscount(t *testing.T) {
	got := assess(series(append(flat(90, 500), 440)...))
	if got.Verdict != VerdictRealDiscount {
		t.Fatalf("verdict %s", got.Verdict)
	}
	if got.Median30Cents != 50000 || got.DiscountVs30Bps != 1200 || got.HistoryDays != 90 {
		t.Fatalf("assessment %+v", got)
	}
	if !reflect.DeepEqual(got.Signals, []Signal{SignalAllTimeLow}) {
		t.Fatalf("signals %v", got.Signals)
	}
}

func TestAssessIgnoresASmallDip(t *testing.T) {
	if got := assess(series(append(flat(60, 500), 490)...)); got.Verdict != VerdictNoDiscount {
		t.Fatalf("verdict %s", got.Verdict)
	}
}

func TestAssessDoesNotCallItRealWhenTheLongerRunWasCheaper(t *testing.T) {
	// Cheap for months, expensive for the last month, back to the old price today.
	prices := append(flat(60, 400), flat(30, 500)...)
	if got := assess(series(append(prices, 470)...)); got.Verdict == VerdictRealDiscount {
		t.Fatalf("verdict %s, median90 %d", got.Verdict, got.Median90Cents)
	}
}

func TestAssessCatchesAMarkupBeforeTheSale(t *testing.T) {
	// Usual price 500, raised to 650 in the last ten days, "sale" back to 495.
	prices := append(flat(80, 500), flat(10, 650)...)
	got := assess(series(append(prices, 495)...))
	if got.Verdict != VerdictInflatedBeforeSale {
		t.Fatalf("verdict %s (%+v)", got.Verdict, got)
	}
	if !contains(got.Signals, SignalRaisedBeforeDrop) {
		t.Fatalf("signals %v", got.Signals)
	}
}

func TestAssessKeepsARealDropAfterAMarkup(t *testing.T) {
	prices := append(flat(80, 500), flat(10, 650)...)
	if got := assess(series(append(prices, 400)...)); got.Verdict != VerdictRealDiscount {
		t.Fatalf("verdict %s", got.Verdict)
	}
}

func TestAssessCallsOutAnAdvertisedDiscountTheHistoryDoesNotShow(t *testing.T) {
	points := series(append(flat(60, 500), 495)...)
	points[len(points)-1].ListCents = 90000
	got := assess(points)
	if got.Verdict != VerdictFakeDiscount {
		t.Fatalf("verdict %s", got.Verdict)
	}
	if got.AdvertisedDiscountBps != 4500 || !contains(got.Signals, SignalListAboveObservedMax) {
		t.Fatalf("assessment %+v", got)
	}
}

func TestAssessReportsAStaleHistory(t *testing.T) {
	points := series(append(flat(60, 500), 400)...)
	for i := range points {
		on, _ := time.Parse(dateLayout, points[i].On)
		points[i].On = on.AddDate(0, 0, -5).Format(dateLayout)
	}
	if got := assess(points); got.Verdict != VerdictStale {
		t.Fatalf("verdict %s", got.Verdict)
	}
}

func TestAssessAcceptsAnAggregatorThatStopsAtYesterday(t *testing.T) {
	// Regression: a history ending yesterday in Brazil, judged late in the evening when the
	// UTC date is already tomorrow, was called stale.
	points := series(append(flat(60, 500), 400)...)
	for i := range points {
		on, _ := time.Parse(dateLayout, points[i].On)
		points[i].On = on.AddDate(0, 0, -1).Format(dateLayout)
	}
	lateEvening := today.Add(13 * time.Hour) // 01:00 UTC of the next day
	got := Assess(Key{Product: "p", Merchant: "m", Series: SeriesMarketMin, Kind: KindUnknown}, points, lateEvening)
	if got.Verdict != VerdictRealDiscount {
		t.Fatalf("verdict %s", got.Verdict)
	}
}

func contains(signals []Signal, want Signal) bool {
	for _, signal := range signals {
		if signal == want {
			return true
		}
	}
	return false
}

func feedItem(id, text string) map[string]any {
	return map[string]any{"id": id, "plugin": "price-collector", "title": "x", "body": text}
}

func TestDomainRecordsObservationsAndSkipsOtherItems(t *testing.T) {
	domain := New(NewMemoryRepository()).WithClock(func() time.Time { return today })
	pix := make([]map[string]any, 0, 31)
	for i := -30; i <= 0; i++ {
		price := "500"
		if i == 0 {
			price = "430"
		}
		pix = append(pix, point(day(i), price))
	}
	items := []any{
		feedItem("a", body(t, pix, nil)),
		feedItem("b", body(t, []map[string]any{point(day(0), "520")}, func(m map[string]any) { m["price_kind"] = "card" })),
		feedItem("c", "An article body"),
		map[string]any{"id": "d", "title": "no body"},
		feedItem("e", body(t, []map[string]any{point(day(0), "0")}, nil)),
		"not even an object",
	}
	result, err := domain.Handle(context.Background(), Request{
		Action: ActionRecordObservations, Event: Event{Payload: map[string]any{"items": items}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result["observations"] != 2 || result["points"] != 32 || result["written"] != 32 || result["items"] != 6 {
		t.Fatalf("result %v", result)
	}
	skipped := result["skipped"].(map[string]any)
	if skipped[SkipOther] != 1 || skipped[SkipNoBody] != 2 || skipped[SkipInvalid] != 1 {
		t.Fatalf("skipped %v", skipped)
	}
	invalid := result["invalid"].([]any)
	if len(invalid) != 1 || invalid[0].(map[string]any)["id"] != "e" {
		t.Fatalf("invalid %v", invalid)
	}

	again, _ := domain.Record(context.Background(), items[:2])
	if again.Written != 0 {
		t.Fatalf("replay wrote %d", again.Written)
	}

	assessed, err := domain.Handle(context.Background(), Request{
		Action: ActionAssessPrice, Event: Event{Payload: map[string]any{"product": "ssd-example-1tb"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	rows := assessed["assessments"].([]any)
	if len(rows) != 2 {
		t.Fatalf("rows %v", rows)
	}
	verdicts := map[string]any{}
	for _, row := range rows {
		values := row.(map[string]any)
		verdicts[values["price_kind"].(string)] = values["verdict"]
	}
	want := map[string]any{"card": string(VerdictInsufficientHistory), "pix": string(VerdictRealDiscount)}
	if !reflect.DeepEqual(verdicts, want) {
		t.Fatalf("verdicts %v", verdicts)
	}

	all, _ := domain.AssessProduct(context.Background(), "")
	if len(all) != 2 {
		t.Fatalf("all products %d", len(all))
	}
}

func TestDomainAcceptsItemsBuiltInGo(t *testing.T) {
	// Regression: a []map[string]any payload used to read as an empty batch with no error.
	domain := New(NewMemoryRepository()).WithClock(func() time.Time { return today })
	items := []map[string]any{feedItem("a", body(t, []map[string]any{point(day(0), "10")}, nil))}
	result, err := domain.Handle(context.Background(), Request{
		Action: ActionRecordObservations, Event: Event{Payload: map[string]any{"items": items}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result["observations"] != 1 {
		t.Fatalf("result %v", result)
	}
}

func TestDomainRefusesBadRequests(t *testing.T) {
	domain := New(NewMemoryRepository())
	if _, err := domain.Handle(context.Background(), Request{Action: "nope"}); err == nil {
		t.Fatal("unknown action accepted")
	}
	if _, err := domain.AssessProduct(context.Background(), "Bad Key"); err == nil {
		t.Fatal("bad product accepted")
	}
	for name, payload := range map[string]map[string]any{
		"items as text":     {"items": "a,b"},
		"items as one item": {"items": map[string]any{"body": "x"}},
	} {
		if _, err := domain.Handle(context.Background(), Request{
			Action: ActionRecordObservations, Event: Event{Payload: payload},
		}); err == nil {
			t.Errorf("%s accepted", name)
		}
	}
	if _, err := domain.Handle(context.Background(), Request{
		Action: ActionAssessPrice, Event: Event{Payload: map[string]any{"product": 42}},
	}); err == nil {
		t.Error("numeric product accepted as every product")
	}
	many := make([]any, maxItemsPerRecord+1)
	if _, err := domain.Record(context.Background(), many); err == nil {
		t.Fatal("oversized batch accepted")
	}
	if _, err := New(nil).Handle(context.Background(), Request{Action: ActionAssessPrice}); err == nil {
		t.Fatal("missing repository accepted")
	}
}

// failingRepository proves a storage failure stops the batch and is reported.
type failingRepository struct{ *MemoryRepository }

func (failingRepository) Upsert(context.Context, Observation) (int, error) {
	return 0, fmt.Errorf("disk full")
}

func TestDomainReportsStorageFailures(t *testing.T) {
	domain := New(failingRepository{NewMemoryRepository()}).WithClock(func() time.Time { return today })
	_, err := domain.Record(context.Background(), []any{feedItem("a", body(t, []map[string]any{point(day(0), "1")}, nil))})
	if err == nil || !strings.Contains(err.Error(), "disk full") {
		t.Fatalf("err %v", err)
	}
}

func searchDomain(t *testing.T) *Domain {
	t.Helper()
	repo := NewMemoryRepository()
	for _, product := range []struct{ key, title string }{
		{"hd-blue-2tb", "HD Brandv Blue 2TB SATA"},
		{"ssd-x900pro-1tb", "SSD Brandx X900 PRO 1TB NVMe"},
		{"cpu-zenith7-9000", "Processador Brandz Zenith 7 9000"},
		{"gpu-vx9000", "Placa de vídeo Brandz Vertex VX 9000"},
		{"gpu-gx5000", "Placa de vídeo Brandw GX 5000 HDMI"},
	} {
		repo.Upsert(context.Background(), Observation{
			Product: product.key, Title: product.title, Merchant: "store-a",
			Series: SeriesMerchant, Kind: KindPix, Points: []Point{{On: day(0), Cents: 100}},
		})
	}
	return New(repo).WithClock(func() time.Time { return today })
}

func searchKeys(t *testing.T, domain *Domain, search Search) []string {
	t.Helper()
	result, err := domain.SearchProducts(context.Background(), search)
	if err != nil {
		t.Fatal(err)
	}
	keys := []string{}
	for _, product := range result.Products {
		keys = append(keys, product.Product.Key)
	}
	return keys
}

func TestSearchFindsProductsByWordsAndBrand(t *testing.T) {
	domain := searchDomain(t)
	cases := []struct {
		search Search
		want   []string
	}{
		// "hd" is a whole word: the GX title's "HDMI" does not count.
		{Search{Query: "hd"}, []string{"hd-blue-2tb"}},
		{Search{Brand: "brandz"}, []string{"cpu-zenith7-9000", "gpu-vx9000"}},
		{Search{Query: "placa de video", Brand: "BRANDZ"}, []string{"gpu-vx9000"}},
		{Search{Query: "Vídeo"}, []string{"gpu-gx5000", "gpu-vx9000"}},
		{Search{Query: "zen"}, []string{"cpu-zenith7-9000"}},
		{Search{Query: "x900pro"}, []string{"ssd-x900pro-1tb"}},
		{Search{Product: "hd-blue-2tb"}, []string{"hd-blue-2tb"}},
		{Search{Brand: "brandq"}, []string{}},
		{Search{}, []string{"cpu-zenith7-9000", "gpu-gx5000", "gpu-vx9000", "hd-blue-2tb", "ssd-x900pro-1tb"}},
	}
	for _, c := range cases {
		if got := searchKeys(t, domain, c.search); !reflect.DeepEqual(got, c.want) {
			t.Errorf("%+v: got %v, want %v", c.search, got, c.want)
		}
	}
}

func TestSearchIsBoundedAndCountsWhatItLeftOut(t *testing.T) {
	repo := NewMemoryRepository()
	for i := 0; i < maxSearchProducts+3; i++ {
		repo.Upsert(context.Background(), Observation{
			Product: fmt.Sprintf("hd-%02d", i), Title: "HD Example", Merchant: "store-a",
			Series: SeriesMerchant, Kind: KindPix, Points: []Point{{On: day(0), Cents: 100}},
		})
	}
	result, err := New(repo).WithClock(func() time.Time { return today }).SearchProducts(context.Background(), Search{Query: "hd"})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Products) != maxSearchProducts || result.Matched != maxSearchProducts+3 {
		t.Fatalf("products %d matched %d", len(result.Products), result.Matched)
	}
}

func TestSearchActionReturnsTitlesAndRefusesBadFields(t *testing.T) {
	domain := searchDomain(t)
	got, err := domain.Handle(context.Background(), Request{
		Action: ActionSearchPrices, Event: Event{Payload: map[string]any{"query": "hd"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	products := got["products"].([]any)
	first := products[0].(map[string]any)
	if got["matched"] != 1 || first["title"] != "HD Brandv Blue 2TB SATA" || len(first["assessments"].([]any)) != 1 {
		t.Fatalf("result %v", got)
	}
	for _, payload := range []map[string]any{
		{"brand": 3},
		{"query": strings.Repeat("x", maxSearchText+1)},
		{"product": "Not A Key"},
	} {
		if _, err := domain.Handle(context.Background(), Request{Action: ActionSearchPrices, Event: Event{Payload: payload}}); err == nil {
			t.Errorf("%v accepted", payload)
		}
	}
}

func TestSearchHandsTheMarketHistoryOnlyWhenAsked(t *testing.T) {
	repo := NewMemoryRepository()
	ctx := context.Background()
	repo.Upsert(ctx, Observation{Product: "ssd-a", Title: "SSD A", Merchant: "aggregator", Series: SeriesMarketMin, Kind: KindUnknown,
		Points: []Point{{On: today.AddDate(-2, 0, 0).Format(dateLayout), Cents: 1}, {On: day(-2), Cents: 900}, {On: day(-1), Cents: 800}}})
	repo.Upsert(ctx, Observation{Product: "ssd-a", Title: "SSD A", Merchant: "store-a", Series: SeriesMerchant, Kind: KindPix,
		Points: []Point{{On: day(0), Cents: 850}}})
	repo.Upsert(ctx, Observation{Product: "hd-b", Title: "HD B", Merchant: "store-a", Series: SeriesMerchant, Kind: KindPix,
		Points: []Point{{On: day(0), Cents: 300}}})
	domain := New(repo).WithClock(func() time.Time { return today })

	plain, err := domain.Handle(ctx, Request{Action: ActionSearchPrices, Event: Event{Payload: map[string]any{}}})
	if err != nil {
		t.Fatal(err)
	}
	for _, raw := range plain["products"].([]any) {
		if _, has := raw.(map[string]any)["market"]; has {
			t.Fatal("a search that did not ask carried a history")
		}
	}

	charted, err := domain.Handle(ctx, Request{Action: ActionSearchPrices, Event: Event{Payload: map[string]any{"history": true}}})
	if err != nil {
		t.Fatal(err)
	}
	byKey := map[string]map[string]any{}
	for _, raw := range charted["products"].([]any) {
		entry := raw.(map[string]any)
		byKey[entry["product"].(string)] = entry
	}
	// Only the last year, oldest first; a product without a market series has none.
	want := []any{map[string]any{"on": day(-2), "cents": int64(900)}, map[string]any{"on": day(-1), "cents": int64(800)}}
	if !reflect.DeepEqual(byKey["ssd-a"]["market"], want) {
		t.Fatalf("market %v", byKey["ssd-a"]["market"])
	}
	if _, has := byKey["hd-b"]["market"]; has {
		t.Fatal("a store-only product carried a market history")
	}
	if _, err := domain.Handle(ctx, Request{Action: ActionSearchPrices, Event: Event{Payload: map[string]any{"history": "yes"}}}); err == nil {
		t.Fatal("a history flag that is not a boolean was accepted")
	}
}
