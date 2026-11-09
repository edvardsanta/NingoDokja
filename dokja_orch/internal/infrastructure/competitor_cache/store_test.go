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
