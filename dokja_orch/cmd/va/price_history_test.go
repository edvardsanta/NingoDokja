package main

import (
	"context"
	"os"
	"path/filepath"
	pricedomain "read_books/dokja_domain/dokja_price"
	"testing"
)

func TestPriceBufferStatusIsDurableWithoutPostgres(t *testing.T) {
	for _, dsn := range []string{"", "not a connection configuration"} {
		_, closeIt, probe, err := configureDurablePriceHistory(dsn, filepath.Join(t.TempDir(), "dokja.db"))
		if err != nil {
			t.Fatal(err)
		}
		detail, probeErr := probe(context.Background())
		if dsn == "" {
			if detail != "price_storage_local" || probeErr != nil {
				t.Fatal(detail, probeErr)
			}
		} else if probeErr == nil || probeErr.Error() != "price_storage_buffered:0" {
			t.Fatal(detail, probeErr)
		}
		closeIt()
		_, probeErr = probe(context.Background())
		if probeErr == nil || probeErr.Error() != "price_storage_local_unavailable" {
			t.Fatal(probeErr)
		}
	}
}
func TestPriceBufferCannotSilentlyFallBackWhenDiskIsUnavailable(t *testing.T) {
	path := t.TempDir()
	if err := os.WriteFile(filepath.Join(path, "price_buffer.db"), []byte("blocked"), 0600); err != nil {
		t.Fatal(err)
	}
	// Using a directory as the shared file's parent buffer destination refuses startup.
	repository, closeIt, probe, err := configureDurablePriceHistory("", filepath.Join(path, "price_buffer.db", "shared.db"))
	if err == nil {
		closeIt()
		t.Fatal("expected unavailable parent")
	}
	defer closeIt()
	if _, writeErr := repository.AddWatch(context.Background(), pricedomain.Watch{Category: "fixture"}); writeErr == nil {
		t.Fatal("unavailable storage accepted a write")
	}
	if _, probeErr := probe(context.Background()); probeErr == nil {
		t.Fatal("unavailable storage reported healthy")
	}
}
