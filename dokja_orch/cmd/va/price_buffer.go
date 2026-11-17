package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"path/filepath"
	"time"

	price "read_books/dokja_domain/dokja_price"
	"read_books/internal/handlers"
	pricehistory "read_books/internal/infrastructure/price_history"
)

// Fail closed if the durable local store cannot be opened. A memory-only
// fallback would acknowledge writes that a restart could silently discard.
func configureDurablePriceHistory(dsn, sharedPath string) (price.Repository, func(), handlers.ServiceProbe, error) {
	local, err := pricehistory.OpenLocal(filepath.Join(filepath.Dir(sharedPath), "price_buffer.db"))
	if err != nil {
		return price.UnavailableRepository{}, func() {}, func(context.Context) (string, error) { return "", errors.New("price_storage_local_unavailable") }, errors.New("durable price buffer unavailable")
	}
	repository := price.NewBufferedRepository(local)
	var db *sql.DB
	if dsn != "" {
		db, err = sql.Open("pgx", dsn)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})

	go func() {
		defer close(done)
		if db == nil {
			return
		}
		repository.RunSync(ctx, func(attempt context.Context) (price.Repository, error) {
			target, openErr := preparePriceHistory(attempt, db)
			return target, openErr
		}, 30*time.Second, 100)
	}()

	closeIt := func() {
		cancel()
		<-done
		if db != nil {
			db.Close()
		}
		local.Close()
	}
	probe := func(ctx context.Context) (string, error) {
		pending, err := repository.PendingCount(ctx)
		if err != nil {
			return "", errors.New("price_storage_local_unavailable")
		}
		if dsn == "" {
			return "price_storage_local", nil
		}
		if pending > 0 || repository.LastSync().IsZero() {
			return "", handlers.ProbeWarning{Reason: fmt.Sprintf("price_storage_buffered:%d", pending)}
		}
		// A failed periodic check clears availability even after an earlier success.
		if !repository.Online() {
			return "", handlers.ProbeWarning{Reason: fmt.Sprintf("price_storage_buffered:%d", pending)}
		}
		return "price_storage_persistent", nil
	}
	return repository, closeIt, probe, nil
}
