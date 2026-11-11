package competitorcache

import (
	"context"
	"path/filepath"
	"testing"
)

func TestStoreRoundTripsQueries(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "cache.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	key, err := Key(map[string]any{"category": "cpu", "budget_cents": 190000})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"processador 8 cores 16 threads am5"}
	if err := store.Put(context.Background(), key, want); err != nil {
		t.Fatal(err)
	}
	got, hit, err := store.Get(context.Background(), key)
	if err != nil || !hit {
		t.Fatalf("get: %v hit=%v", err, hit)
	}
	if len(got) != 1 || got[0] != want[0] {
		t.Fatalf("queries %v", got)
	}
}

func TestStoreRoundTripsEquivalences(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "cache.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	items := []map[string]any{{
		"product_key":    "pn:equivalent",
		"title":          "Processador 8 cores 16 threads am5",
		"query":          "ABC123",
		"classification": "technically_comparable",
	}}
	if err := store.PutEquivalences(context.Background(), "source", items); err != nil {
		t.Fatal(err)
	}
	got, hit, err := store.GetEquivalences(context.Background(), "source")
	if err != nil || !hit {
		t.Fatalf("get: %v hit=%v", err, hit)
	}
	if len(got) != 1 || got[0]["product_key"] != "pn:equivalent" || got[0]["query"] != "ABC123" {
		t.Fatalf("equivalences %v", got)
	}
}

func TestReopeningRemovesLegacyModelOpinionsAndKeepsDiscovery(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cache.db")
	store, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if err := store.PutEquivalences(ctx, "legacy", []map[string]any{{
		"product_key": "other", "estimated": true, "relationship": "comparable", "explanation": "Old opinion",
	}}); err != nil {
		t.Fatal(err)
	}
	if err := store.Put(ctx, "query", []string{"CPU Other"}); err != nil {
		t.Fatal(err)
	}
	if err := store.PutEquivalences(ctx, "discovery", []map[string]any{{"product_key": "other", "query": "CPU Other"}}); err != nil {
		t.Fatal(err)
	}
	store.Close()
	store, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if _, hit, err := store.GetEquivalences(ctx, "legacy"); err != nil || hit {
		t.Fatalf("opinion retained: %v %v", hit, err)
	}
	if _, hit, err := store.Get(ctx, "query"); err != nil || !hit {
		t.Fatalf("query lost: %v %v", hit, err)
	}
	if _, hit, err := store.GetEquivalences(ctx, "discovery"); err != nil || !hit {
		t.Fatalf("competitor lost: %v %v", hit, err)
	}
}
