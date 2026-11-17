package main

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
	pricedomain "read_books/dokja_domain/dokja_price"
	"read_books/internal/handlers"
	pricehistory "read_books/internal/infrastructure/price_history"
	"read_books/internal/logger"
)

// schemaReason is why Open refuses a database whose price namespace is absent or
// older than this build. It is exactly what Initialize repairs.
const schemaReason = "the namespace is missing or has another schema version; run price-history-init"

// configurePriceHistory picks where the Price domain keeps its history (ADR 0015). With
// DOKJA_PRICE_POSTGRES_DSN set, it is the initialized PostgreSQL namespace: a missing or
// older namespace is installed or upgraded at startup — Initialize is idempotent and
// keeps existing points — because silently falling back to memory would lose every
// recorded price on the next restart. A database that stays unusable keeps history in
// memory; the failure is logged without the DSN, and the orchestrator still starts.
func configurePriceHistory(dsn string) (pricedomain.Repository, func()) {
	repository, closeIt, _ := configurePriceHistoryStatus(dsn)
	return repository, closeIt
}

func configurePriceHistoryStatus(dsn string) (pricedomain.Repository, func(), handlers.ServiceProbe) {
	if dsn == "" {
		logger.Info("Price history is kept in memory: DOKJA_PRICE_POSTGRES_DSN is unset")
		return pricedomain.NewMemoryRepository(), func() {}, priceStorageFallback("price_storage_memory")
	}
	store, db, reason := openPriceHistory(dsn)
	if store != nil {
		logger.Info("Price history is kept in PostgreSQL")
		return store, func() { db.Close() }, priceStorageProbe(db)
	}
	if reason != schemaReason {
		logger.Info("Price history is kept in memory, so a restart forgets it: " + reason)
		return pricedomain.NewMemoryRepository(), func() {}, priceStorageFallback("price_storage_fallback")
	}
	installed, installDB, installReason := initializePriceHistory(dsn)
	if installed == nil {
		logger.Info("Price history is kept in memory, so a restart forgets it: " + installReason)
		return pricedomain.NewMemoryRepository(), func() {}, priceStorageFallback("price_storage_fallback")
	}
	logger.Info("Price history namespace was installed or upgraded at startup and is kept in PostgreSQL")
	return installed, func() { installDB.Close() }, priceStorageProbe(installDB)
}

// openPriceHistory never returns native errors, which could carry the DSN.
func openPriceHistory(dsn string) (*pricehistory.Store, *sql.DB, string) {
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		return nil, nil, "the connection configuration is invalid"
	}
	db.SetMaxOpenConns(4)
	db.SetMaxIdleConns(2)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := db.PingContext(ctx); err != nil {
		db.Close()
		return nil, nil, "the database is unavailable; check that it is running and the connection configuration"
	}
	store, err := pricehistory.Open(ctx, db)
	if err != nil {
		db.Close()
		return nil, nil, schemaReason
	}
	return store, db, ""
}

// initializePriceHistory installs or upgrades the namespace and reopens it. It is the
// startup path of the explicit price-history-init command, for deployments where the
// init step was missed; the first boot repairs it instead of demoting history to memory.
func initializePriceHistory(dsn string) (*pricehistory.Store, *sql.DB, string) {
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		return nil, nil, "the connection configuration is invalid"
	}
	db.SetMaxOpenConns(4)
	db.SetMaxIdleConns(2)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := db.PingContext(ctx); err != nil {
		db.Close()
		return nil, nil, "the database is unavailable; check that it is running and the connection configuration"
	}
	if _, err := pricehistory.Initialize(ctx, db); err != nil {
		db.Close()
		return nil, nil, "the namespace could not be installed or upgraded"
	}
	store, err := pricehistory.Open(ctx, db)
	if err != nil {
		db.Close()
		return nil, nil, schemaReason
	}
	return store, db, ""
}

// Report the selected repository, not just whether a server can accept connections.
// A repository selected as memory at startup stays memory until the orchestrator restarts.
func priceStorageFallback(reason string) handlers.ServiceProbe {
	return func(context.Context) (string, error) { return "", fmt.Errorf("%s", reason) }
}

func priceStorageProbe(db *sql.DB) handlers.ServiceProbe {
	return func(parent context.Context) (string, error) {
		ctx, cancel := context.WithTimeout(parent, 2*time.Second)
		defer cancel()
		if _, err := pricehistory.Open(ctx, db); err != nil {
			return "", fmt.Errorf("price_storage_unavailable")
		}
		return "price_storage_persistent", nil
	}
}
