package main

import (
	"context"
	"database/sql"
	"strings"
	"testing"

	pricedomain "read_books/dokja_domain/dokja_price"
)

func TestPriceHistoryFallsBackToMemoryWithoutLeakingTheDSN(t *testing.T) {
	repository, closeIt := configurePriceHistory("")
	defer closeIt()
	if _, ok := repository.(*pricedomain.MemoryRepository); !ok {
		t.Fatalf("unset DSN gave %T", repository)
	}

	for _, dsn := range []string{"host='private-invalid", unavailableJournalDSN(t)} {
		store, db, reason := openPriceHistory(dsn)
		if store != nil || db != nil {
			t.Fatalf("%q opened a store", dsn)
		}
		if reason == "" || strings.Contains(reason, "private-") || strings.Contains(reason, "127.0.0.1") {
			t.Fatalf("reason %q", reason)
		}
		repository, closeIt := configurePriceHistory(dsn)
		closeIt()
		if _, ok := repository.(*pricedomain.MemoryRepository); !ok {
			t.Fatalf("broken DSN gave %T", repository)
		}
	}
}

func TestPriceStorageStatusReportsSelectedMemoryRepository(t *testing.T) {
	for _, tc := range []struct{ dsn, reason string }{
		{"", "price_storage_memory"},
		{"not a connection configuration", "price_storage_fallback"},
	} {
		repository, closeIt, probe := configurePriceHistoryStatus(tc.dsn)
		if repository == nil {
			t.Fatal("missing fallback repository")
		}
		_, err := probe(context.Background())
		closeIt()
		if err == nil || err.Error() != tc.reason {
			t.Fatalf("wanted %s, got %v", tc.reason, err)
		}
	}
}

func TestPriceStorageProbeDoesNotExposeConnectionErrors(t *testing.T) {
	db, err := sql.Open("pgx", "")
	if err != nil {
		t.Fatal(err)
	}
	db.Close()
	detail, err := priceStorageProbe(db)(context.Background())
	if detail != "" || err == nil || err.Error() != "price_storage_unavailable" {
		t.Fatal(detail, err)
	}
}
