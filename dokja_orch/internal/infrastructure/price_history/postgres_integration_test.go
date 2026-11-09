//go:build integration

package pricehistory

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"reflect"
	"testing"

	_ "github.com/jackc/pgx/v5/stdlib"
	pricedomain "read_books/dokja_domain/dokja_price"
)

// database opens the disposable price_test database named by DOKJA_PRICE_TEST_DSN and drops
// this namespace, so every test starts from nothing.
func database(t *testing.T) *sql.DB {
	t.Helper()
	dsn := os.Getenv("DOKJA_PRICE_TEST_DSN")
	if dsn == "" {
		t.Fatal("integration tests require a disposable DOKJA_PRICE_TEST_DSN")
	}
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatal("open fixture database failed")
	}
	t.Cleanup(func() { db.Close() })
	var name string
	if err := db.QueryRow(`SELECT current_database()`).Scan(&name); err != nil || name != "price_test" {
		t.Fatal("price integration fixtures require the disposable price_test database")
	}
	if _, err := db.Exec(`DROP SCHEMA IF EXISTS dokja_price CASCADE`); err != nil {
		t.Fatal("reset fixture schema failed")
	}
	return db
}

func observation(kind pricedomain.Kind, points ...pricedomain.Point) pricedomain.Observation {
	return pricedomain.Observation{
		Product: "ssd-example-1tb", Title: "SSD Example 1TB", Merchant: "store-a",
		Series: pricedomain.SeriesMerchant, Kind: kind, Points: points,
	}
}

func TestOpenRefusesAnUninitializedNamespace(t *testing.T) {
	db := database(t)
	if _, err := Open(context.Background(), db); !errors.Is(err, ErrSchema) {
		t.Fatalf("err %v", err)
	}
	if _, err := Initialize(context.Background(), db); err != nil {
		t.Fatal(err)
	}
	if _, err := Initialize(context.Background(), db); err != nil {
		t.Fatalf("second initialize: %v", err)
	}
	if _, err := Open(context.Background(), db); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`UPDATE dokja_price.schema_version SET version = 99`); err != nil {
		t.Fatal(err)
	}
	if _, err := Open(context.Background(), db); !errors.Is(err, ErrSchema) {
		t.Fatalf("future schema accepted: %v", err)
	}
}

// TestStoreBehavesLikeTheMemoryRepository runs the same writes against both and compares what
// they answer, so the durable adapter cannot drift from the reference behavior.
func TestStoreBehavesLikeTheMemoryRepository(t *testing.T) {
	ctx := context.Background()
	store, err := Initialize(ctx, database(t))
	if err != nil {
		t.Fatal(err)
	}
	memory := pricedomain.NewMemoryRepository()
	writes := []pricedomain.Observation{
		observation(pricedomain.KindPix,
			pricedomain.Point{On: "2026-11-01", Cents: 50000, ListCents: 90000},
			pricedomain.Point{On: "2026-11-02", Cents: 49000, Seller: "Store B"}),
		observation(pricedomain.KindPix, pricedomain.Point{On: "2026-11-02", Cents: 49000, Seller: "Store B"}),
		observation(pricedomain.KindPix, pricedomain.Point{On: "2026-11-02", Cents: 45000}),
		observation(pricedomain.KindCard, pricedomain.Point{On: "2026-11-02", Cents: 52000}),
		described("store-a", "samsung", "https://a.example/1"),
		described("store-b", "", "https://b.example/1"),
		described("store-a", "", ""),
	}
	for index, write := range writes {
		want, _ := memory.Upsert(ctx, write)
		got, err := store.Upsert(ctx, write)
		if err != nil {
			t.Fatal(err)
		}
		if got != want {
			t.Fatalf("write %d: store wrote %d, memory %d", index, got, want)
		}
	}

	wantKeys, _ := memory.Keys(ctx, "ssd-example-1tb")
	gotKeys, err := store.Keys(ctx, "ssd-example-1tb")
	if err != nil || !reflect.DeepEqual(gotKeys, wantKeys) {
		t.Fatalf("keys %v (%v), want %v", gotKeys, err, wantKeys)
	}
	for _, key := range wantKeys {
		for _, bounds := range [][2]string{{"", ""}, {"2026-11-02", ""}, {"", "2026-11-01"}} {
			want, _ := memory.History(ctx, key, bounds[0], bounds[1])
			got, err := store.History(ctx, key, bounds[0], bounds[1])
			if err != nil || !reflect.DeepEqual(got, want) {
				t.Fatalf("%v %v: history %+v (%v), want %+v", key, bounds, got, err, want)
			}
		}
	}
	wantProducts, _ := memory.Products(ctx)
	gotProducts, err := store.Products(ctx)
	if err != nil || !reflect.DeepEqual(gotProducts, wantProducts) {
		t.Fatalf("products %v (%v), want %v", gotProducts, err, wantProducts)
	}
	wantMerchants, _ := memory.Merchants(ctx)
	gotMerchants, err := store.Merchants(ctx)
	if err != nil || !reflect.DeepEqual(gotMerchants, wantMerchants) {
		t.Fatalf("merchants %v (%v), want %v", gotMerchants, err, wantMerchants)
	}
	wantURLs, _ := memory.OfferURLs(ctx, "ssd-example-1tb")
	gotURLs, err := store.OfferURLs(ctx, "ssd-example-1tb")
	if err != nil || !reflect.DeepEqual(gotURLs, wantURLs) || len(gotURLs) != 2 {
		t.Fatalf("urls %v (%v), want %v", gotURLs, err, wantURLs)
	}

	for _, step := range []struct {
		add   bool
		watch pricedomain.Watch
	}{
		{true, pricedomain.Watch{Category: "ssd sata", Brand: "samsung"}},
		{true, pricedomain.Watch{Category: "hd"}},
		{true, pricedomain.Watch{Category: "hd"}},
		{false, pricedomain.Watch{Category: "nvme"}},
		{false, pricedomain.Watch{Category: "hd"}},
	} {
		var want, got bool
		if step.add {
			want, _ = memory.AddWatch(ctx, step.watch)
			got, err = store.AddWatch(ctx, step.watch)
		} else {
			want, _ = memory.RemoveWatch(ctx, step.watch)
			got, err = store.RemoveWatch(ctx, step.watch)
		}
		if err != nil || got != want {
			t.Fatalf("%+v: store %v (%v), memory %v", step, got, err, want)
		}
	}
	wantWatches, _ := memory.Watches(ctx)
	gotWatches, err := store.Watches(ctx)
	if err != nil || !reflect.DeepEqual(gotWatches, wantWatches) {
		t.Fatalf("watches %v (%v), want %v", gotWatches, err, wantWatches)
	}
}

func described(merchant, brand, address string) pricedomain.Observation {
	o := observation(pricedomain.KindPix, pricedomain.Point{On: "2026-11-03", Cents: 47000})
	o.Merchant, o.Brand, o.URL, o.Category, o.PartNumber = merchant, brand, address, "ssd sata", ""
	if brand != "" {
		o.PartNumber = "MZ-EXAMPLE"
	}
	return o
}

func TestAVersionOneNamespaceUpgradesAndKeepsItsPoints(t *testing.T) {
	ctx := context.Background()
	db := database(t)
	if _, err := db.Exec(`
CREATE SCHEMA dokja_price;
CREATE TABLE dokja_price.schema_version (id SMALLINT PRIMARY KEY CHECK (id = 1), version INTEGER NOT NULL);
CREATE TABLE dokja_price.products (product_key TEXT PRIMARY KEY, title TEXT NOT NULL, updated_at TIMESTAMPTZ NOT NULL DEFAULT now());
CREATE TABLE dokja_price.points (
	product_key TEXT NOT NULL REFERENCES dokja_price.products (product_key), merchant TEXT NOT NULL,
	series TEXT NOT NULL, price_kind TEXT NOT NULL, observed_on DATE NOT NULL, cents BIGINT NOT NULL,
	list_cents BIGINT NOT NULL DEFAULT 0, seller TEXT NOT NULL DEFAULT '',
	PRIMARY KEY (product_key, merchant, series, price_kind, observed_on));
INSERT INTO dokja_price.schema_version VALUES (1, 1);
INSERT INTO dokja_price.products (product_key, title) VALUES ('ssd-example-1tb', 'SSD Example 1TB');
INSERT INTO dokja_price.points VALUES ('ssd-example-1tb', 'store-a', 'merchant', 'pix', '2026-11-01', 50000, 0, '');`); err != nil {
		t.Fatal(err)
	}
	if _, err := Open(ctx, db); !errors.Is(err, ErrSchema) {
		t.Fatalf("a version 1 namespace must wait for its upgrade: %v", err)
	}
	store, err := Initialize(ctx, db)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Open(ctx, db); err != nil {
		t.Fatal(err)
	}
	key := pricedomain.Key{Product: "ssd-example-1tb", Merchant: "store-a", Series: pricedomain.SeriesMerchant, Kind: pricedomain.KindPix}
	points, err := store.History(ctx, key, "", "")
	if err != nil || len(points) != 1 || points[0].Cents != 50000 {
		t.Fatalf("points after the upgrade %+v (%v)", points, err)
	}
	products, err := store.Products(ctx)
	if err != nil || len(products) != 1 || products[0].Brand != "" {
		t.Fatalf("products after the upgrade %+v (%v)", products, err)
	}
	if added, err := store.AddWatch(ctx, pricedomain.Watch{Category: "ssd"}); err != nil || !added {
		t.Fatalf("the upgraded namespace must hold watches: %v %v", added, err)
	}
}

func TestTheDomainAssessesFromTheStore(t *testing.T) {
	ctx := context.Background()
	store, err := Initialize(ctx, database(t))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Upsert(ctx, observation(pricedomain.KindPix, pricedomain.Point{On: "2026-11-02", Cents: 100})); err != nil {
		t.Fatal(err)
	}
	assessments, err := pricedomain.New(store).AssessProduct(ctx, "")
	if err != nil || len(assessments) != 1 {
		t.Fatalf("assessments %+v (%v)", assessments, err)
	}
}
