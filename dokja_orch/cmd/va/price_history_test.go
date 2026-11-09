package main

import (
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
