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
