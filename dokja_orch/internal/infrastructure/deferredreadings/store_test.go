package deferredreadings

import (
	"context"
	"path/filepath"
	"testing"
)

func TestStoreRoundTripsAndRemoves(t *testing.T) {
	directory := t.TempDir()
	store, err := Open(filepath.Join(directory, "deferred.json"))
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()

	if _, found, err := store.Load(ctx); err != nil || found {
		t.Fatal("a fresh store must be empty")
	}
	document := []byte(`[{"channel_id":"c1","url":"https://example.com/a"}]`)
	if err := store.Save(ctx, document); err != nil {
		t.Fatal(err)
	}
	loaded, found, err := store.Load(ctx)
	if err != nil || !found || string(loaded) != string(document) {
		t.Fatalf("round trip lost data: found=%v err=%v data=%s", found, err, loaded)
	}
	if err := store.Save(ctx, nil); err != nil {
		t.Fatal(err)
	}
	if _, found, err := store.Load(ctx); err != nil || found {
		t.Fatal("empty save must remove the document")
	}
}

func TestStoreRejectsOversizeDocuments(t *testing.T) {
	store, err := Open(filepath.Join(t.TempDir(), "deferred.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Save(context.Background(), make([]byte, maxBytes+1)); err == nil {
		t.Fatal("an oversize document was accepted")
	}
}
